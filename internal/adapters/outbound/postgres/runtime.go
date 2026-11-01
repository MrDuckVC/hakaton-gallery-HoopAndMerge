package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimeStore persists version-scoped room overlays and administrator membership.
type RuntimeStore struct{ pool *pgxpool.Pool }

// NewRuntimeStore shares the composition root's database pool.
func NewRuntimeStore(pool *pgxpool.Pool) *RuntimeStore { return &RuntimeStore{pool: pool} }

// These rows are storage DTOs; private holder connection IDs never reach public wire DTOs.
type runtimeVector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}
type runtimePlacement struct {
	GuideID   string        `json:"guide_id"`
	Position  runtimeVector `json:"position"`
	RotationY float64       `json:"rotation_y"`
}
type runtimeSlot struct {
	SlotID   string        `json:"slot_id"`
	Position runtimeVector `json:"position"`
}
type runtimeConfig struct {
	AccentColor         string             `json:"accent_color"`
	Lighting            string             `json:"lighting"`
	GuidePlacements     []runtimePlacement `json:"guide_placements"`
	LampSlots           []runtimeSlot      `json:"lamp_slots"`
	NPCCount            int                `json:"npc_count"`
	InstallationVisible bool               `json:"installation_visible"`
}
type runtimeLamp struct {
	ObjectID           string  `json:"object_id"`
	SlotID             string  `json:"slot_id"`
	HolderUserID       *string `json:"holder_user_id"`
	HolderConnectionID *string `json:"holder_connection_id"`
	Yaw                float64 `json:"yaw"`
	HeadPitch          float64 `json:"head_pitch"`
	On                 bool    `json:"on"`
}

func storageRuntime(o runtime.Overlay) (runtimeConfig, runtimeLamp) {
	c := runtimeConfig{AccentColor: o.Config.AccentColor, Lighting: o.Config.Lighting,
		GuidePlacements: make([]runtimePlacement, len(o.Config.GuidePlacements)),
		LampSlots:       make([]runtimeSlot, len(o.Config.LampSlots)), NPCCount: o.Config.NPCCount,
		InstallationVisible: o.Config.InstallationVisible}
	for i, p := range o.Config.GuidePlacements {
		c.GuidePlacements[i] = runtimePlacement{p.GuideID, runtimeVector{p.Position.X, p.Position.Y, p.Position.Z}, p.RotationY}
	}
	for i, s := range o.Config.LampSlots {
		c.LampSlots[i] = runtimeSlot{s.SlotID, runtimeVector{s.Position.X, s.Position.Y, s.Position.Z}}
	}
	l := runtimeLamp{ObjectID: o.Lamp.ObjectID, SlotID: o.Lamp.SlotID, Yaw: o.Lamp.Yaw, HeadPitch: o.Lamp.HeadPitch, On: o.Lamp.On}
	if o.Lamp.HolderUserID != "" {
		l.HolderUserID = &o.Lamp.HolderUserID
	}
	if o.Lamp.HolderConnectionID != "" {
		l.HolderConnectionID = &o.Lamp.HolderConnectionID
	}
	return c, l
}

func decodeRuntime(o runtime.Overlay, config, lamp []byte) (runtime.Overlay, error) {
	var c runtimeConfig
	var l runtimeLamp
	if err := json.Unmarshal(config, &c); err != nil {
		return runtime.Overlay{}, runtimeStoreError("decode room config")
	}
	if err := json.Unmarshal(lamp, &l); err != nil {
		return runtime.Overlay{}, runtimeStoreError("decode room lamp")
	}
	o.Config = runtime.Config{AccentColor: c.AccentColor, Lighting: c.Lighting, GuidePlacements: make([]runtime.GuidePlacement, len(c.GuidePlacements)), LampSlots: make([]runtime.LampSlot, len(c.LampSlots)), NPCCount: c.NPCCount, InstallationVisible: c.InstallationVisible}
	for i, p := range c.GuidePlacements {
		o.Config.GuidePlacements[i] = runtime.GuidePlacement{GuideID: p.GuideID, Position: player.Vector{X: p.Position.X, Y: p.Position.Y, Z: p.Position.Z}, RotationY: p.RotationY}
	}
	for i, s := range c.LampSlots {
		o.Config.LampSlots[i] = runtime.LampSlot{SlotID: s.SlotID, Position: player.Vector{X: s.Position.X, Y: s.Position.Y, Z: s.Position.Z}}
	}
	o.Lamp = runtime.Lamp{ObjectID: l.ObjectID, SlotID: l.SlotID, Yaw: l.Yaw, HeadPitch: l.HeadPitch, On: l.On}
	if l.HolderUserID != nil {
		o.Lamp.HolderUserID = *l.HolderUserID
	}
	if l.HolderConnectionID != nil {
		o.Lamp.HolderConnectionID = *l.HolderConnectionID
	}
	return o, nil
}

// Read returns only this version's overlay; absence represents the catalog's revision-1 seed.
func (s *RuntimeStore) Read(ctx context.Context, version, roomID string) (runtime.Overlay, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	o := runtime.Overlay{WorldVersion: version, RoomID: roomID}
	var config, lamp []byte
	err := s.pool.QueryRow(ctx, `SELECT revision,config,lamp FROM room_overlays WHERE world_version=$1 AND room_id=$2`, version, roomID).Scan(&o.Revision, &config, &lamp)
	if errors.Is(err, pgx.ErrNoRows) {
		return runtime.Overlay{}, false, nil
	}
	if err != nil {
		return runtime.Overlay{}, false, runtimeStoreError("read room overlay")
	}
	o, err = decodeRuntime(o, config, lamp)
	return o, err == nil, err
}

// CompareAndSwap persists the complete next overlay before returning success.
// Initial revision 1 is implicit; the first successful update inserts revision 2.
func (s *RuntimeStore) CompareAndSwap(ctx context.Context, expected int64, next runtime.Overlay) error {
	if expected < 1 || expected >= world.MaxSafeInteger || next.Revision != expected+1 {
		return auth.Validation
	}
	c, l := storageRuntime(next)
	config, err := json.Marshal(c)
	if err != nil {
		return auth.Validation
	}
	lamp, err := json.Marshal(l)
	if err != nil {
		return auth.Validation
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return runtimeStoreError("begin room update")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	query := `UPDATE room_overlays SET revision=$3,config=$4,lamp=$5,updated_at=now() WHERE world_version=$1 AND room_id=$2 AND revision=$6 AND lamp->>'holder_user_id' IS NULL`
	if expected == 1 {
		query = `INSERT INTO room_overlays(world_version,room_id,revision,config,lamp) VALUES($1,$2,$3,$4,$5) ON CONFLICT(world_version,room_id) DO UPDATE SET revision=EXCLUDED.revision,config=EXCLUDED.config,lamp=EXCLUDED.lamp,updated_at=now() WHERE room_overlays.revision=$6 AND room_overlays.lamp->>'holder_user_id' IS NULL`
	}
	result, err := tx.Exec(ctx, query, next.WorldVersion, next.RoomID, next.Revision, config, lamp, expected)
	if err != nil {
		return runtimeStoreError("write room overlay")
	}
	if result.RowsAffected() != 1 {
		var revision int64
		var held bool
		err = tx.QueryRow(ctx, `SELECT revision,lamp->>'holder_user_id' IS NOT NULL FROM room_overlays WHERE world_version=$1 AND room_id=$2`, next.WorldVersion, next.RoomID).Scan(&revision, &held)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return runtimeStoreError("classify room update conflict")
		}
		if err == nil && revision == expected && held {
			return runtime.ObjectBusy
		}
		return runtime.RevisionConflict
	}
	if err = tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return runtimeStoreError("room commit rolled back")
		}
		return runtime.CommitUnknown
	}
	return nil
}

// IsAdmin checks current persisted membership for every administrative request.
func (s *RuntimeStore) IsAdmin(ctx context.Context, userID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var admin bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_memberships WHERE user_id=$1)`, userID).Scan(&admin); err != nil {
		return false, runtimeStoreError("read administrator membership")
	}
	return admin, nil
}

// GrantAdmin grants an existing normalized username membership without creating an account.
func (s *RuntimeStore) GrantAdmin(ctx context.Context, username string) error {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var userID string
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM users WHERE username=$1`, strings.ToLower(username)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return world.NotFound
	}
	if err != nil {
		return runtimeStoreError("find administrator account")
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO admin_memberships(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return runtimeStoreError("grant administrator membership")
	}
	return nil
}

func runtimeStoreError(operation string) error {
	return fmt.Errorf("%s: %w", operation, auth.Unavailable)
}
