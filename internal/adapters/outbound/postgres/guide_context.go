package postgres

import (
	"context"
	"errors"
	"slices"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	usecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
	"github.com/jackc/pgx/v5"
)

func completedContext(ctx context.Context, tx pgx.Tx, user, guide string) ([]domain.Generation, error) {
	rows, err := tx.Query(ctx, `SELECT `+generationColumns+` FROM guide_generations WHERE user_id=$1 AND guide_id=$2 AND status='completed' ORDER BY created_at DESC,id DESC LIMIT 4`, user, guide)
	if err != nil {
		return nil, auth.Unavailable
	}
	defer rows.Close()
	items := make([]domain.Generation, 0, 4)
	for rows.Next() {
		g, e := scanGeneration(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, g)
	}
	if rows.Err() != nil {
		return nil, auth.Unavailable
	}
	slices.Reverse(items)
	return items, nil
}

// ReadContext snapshots only this owner's successful dialogues and saved actions.
func (s *GuideStore) ReadContext(ctx context.Context, user, guide string) (usecase.ContextData, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var out usecase.ContextData
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, auth.Unavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out.Completed, err = completedContext(ctx, tx, user, guide)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT summary FROM guide_memories WHERE user_id=$1 AND guide_id=$2`, user, guide).Scan(&out.Summary)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, auth.Unavailable
	}
	rows, err := tx.Query(ctx, `SELECT id::text,type,world_version,room_id,COALESCE(exhibit_id,''),COALESCE(photo_id::text,''),created_at FROM memory_events WHERE user_id=$1 ORDER BY created_at DESC,id DESC LIMIT 20`, user)
	if err != nil {
		return out, auth.Unavailable
	}
	out.Events = make([]usecase.Action, 0, 20)
	for rows.Next() {
		var e usecase.Action
		if err = rows.Scan(&e.ID, &e.Type, &e.WorldVersion, &e.RoomID, &e.ExhibitID, &e.PhotoID, &e.At); err != nil {
			rows.Close()
			return out, auth.Unavailable
		}
		out.Events = append(out.Events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, auth.Unavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return out, auth.Unavailable
	}
	return out, nil
}

var _ usecase.ContextReader = (*GuideStore)(nil)
