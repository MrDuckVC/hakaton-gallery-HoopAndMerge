package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	"github.com/jackc/pgx/v5"
)

type profileSettings struct {
	DisplayName string   `json:"display_name"`
	Subtitle    string   `json:"subtitle"`
	Personality string   `json:"personality"`
	Style       string   `json:"style"`
	Topic       string   `json:"topic"`
	Constraints []string `json:"constraints"`
	MaterialIDs []string `json:"material_ids"`
}

func storedSettings(s guide.Settings) profileSettings {
	return profileSettings{s.DisplayName, s.Subtitle, s.Personality, s.Style, s.Topic, s.Constraints, s.MaterialIDs}
}
func (s profileSettings) domain() guide.Settings {
	return guide.Settings{DisplayName: s.DisplayName, Subtitle: s.Subtitle, Personality: s.Personality, Style: s.Style, Topic: s.Topic, Constraints: s.Constraints, MaterialIDs: s.MaterialIDs}
}

type profileQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readProfile(ctx context.Context, db profileQuerier, version, id string) (guide.Override, bool, error) {
	o := guide.Override{WorldVersion: version, GuideID: id}
	var raw []byte
	err := db.QueryRow(ctx, `SELECT revision,settings FROM guide_overrides WHERE world_version=$1 AND guide_id=$2`, version, id).Scan(&o.Revision, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return guide.Override{}, false, nil
	}
	if err != nil {
		return o, false, auth.Unavailable
	}
	var settings profileSettings
	if err = json.Unmarshal(raw, &settings); err != nil {
		return o, false, auth.Unavailable
	}
	o.Settings = settings.domain()
	return o, true, nil
}

// ReadProfile returns only this immutable world's override; absence is revision 1.
func (s *RuntimeStore) ReadProfile(ctx context.Context, version, id string) (guide.Override, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	return readProfile(ctx, s.pool, version, id)
}

// UpdateProfile commits profile CAS and every public room revision as one transaction.
// The scheduler row establishes the same linearization order as worker Claim.
func (s *RuntimeStore) UpdateProfile(ctx context.Context, expected int64, next guide.Override, rooms []runtime.Overlay) error {
	if expected < 1 || expected >= world.MaxSafeInteger || next.Revision != expected+1 {
		return auth.Validation
	}
	raw, err := json.Marshal(storedSettings(next.Settings))
	if err != nil {
		return auth.Validation
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return auth.Unavailable
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, `SELECT id FROM guide_scheduler WHERE id=1 FOR UPDATE`); err != nil {
		return auth.Unavailable
	}
	query := `UPDATE guide_overrides SET revision=$3,settings=$4,updated_at=now() WHERE world_version=$1 AND guide_id=$2 AND revision=$5`
	if expected == 1 {
		query = `INSERT INTO guide_overrides(world_version,guide_id,revision,settings) VALUES($1,$2,$3,$4) ON CONFLICT(world_version,guide_id) DO NOTHING`
	}
	args := []any{next.WorldVersion, next.GuideID, next.Revision, raw}
	if expected != 1 {
		args = append(args, expected)
	}
	result, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return auth.Unavailable
	}
	if result.RowsAffected() != 1 {
		return runtime.RevisionConflict
	}
	ordered := append([]runtime.Overlay(nil), rooms...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].RoomID < ordered[j].RoomID })
	for _, r := range ordered {
		if r.WorldVersion != next.WorldVersion || r.Revision < 2 || r.Revision > world.MaxSafeInteger {
			return auth.Validation
		}
		c, l := storageRuntime(r)
		config, e := json.Marshal(c)
		if e != nil {
			return auth.Validation
		}
		lamp, e := json.Marshal(l)
		if e != nil {
			return auth.Validation
		}
		query = `UPDATE room_overlays SET revision=$3,updated_at=now() WHERE world_version=$1 AND room_id=$2 AND revision=$4`
		args = []any{r.WorldVersion, r.RoomID, r.Revision, r.Revision - 1}
		if r.Revision == 2 {
			query = `INSERT INTO room_overlays(world_version,room_id,revision,config,lamp) VALUES($1,$2,$3,$4,$5) ON CONFLICT(world_version,room_id) DO UPDATE SET revision=EXCLUDED.revision,updated_at=now() WHERE room_overlays.revision=1`
			args = []any{r.WorldVersion, r.RoomID, r.Revision, config, lamp}
		}
		result, e = tx.Exec(ctx, query, args...)
		if e != nil {
			return auth.Unavailable
		}
		if result.RowsAffected() != 1 {
			return runtime.RevisionConflict
		}
	}
	if err = tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return auth.Unavailable
		}
		return runtime.CommitUnknown
	}
	return nil
}
