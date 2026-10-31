package postgres

import (
	"context"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/security"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/game"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SocialStore persists educational actions without retaining shared chat history.
type SocialStore struct{ pool *pgxpool.Pool }

// NewSocialStore shares the process-owned pool.
func NewSocialStore(pool *pgxpool.Pool) *SocialStore { return &SocialStore{pool: pool} }

// RecordInspection refreshes one achievement and its memory atomically across world versions.
func (s *SocialStore) RecordInspection(ctx context.Context, session domain.Session, state player.State, exhibitID string) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, authStoreError(ctx, "begin inspection")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return time.Time{}, err
	}
	var inspected time.Time
	err = tx.QueryRow(ctx, `INSERT INTO exhibit_inspections(user_id,exhibit_id,world_version,room_id,inspected_at)
VALUES($1,$2,$3,$4,clock_timestamp())
ON CONFLICT(user_id,exhibit_id) DO UPDATE SET world_version=EXCLUDED.world_version,room_id=EXCLUDED.room_id,
inspected_at=GREATEST(clock_timestamp(),exhibit_inspections.inspected_at+interval '1 microsecond')
RETURNING inspected_at`, session.UserID, exhibitID, state.WorldVersion, state.RoomID).Scan(&inspected)
	if err != nil {
		return time.Time{}, authStoreError(ctx, "save inspection")
	}
	// The inspection row lock serializes matching events, including concurrent retries.
	_, err = tx.Exec(ctx, `INSERT INTO memory_events(id,user_id,type,world_version,room_id,exhibit_id,created_at)
VALUES($1,$2,'EXHIBIT_INSPECTED',$3,$4,$5,$6)
ON CONFLICT(user_id,exhibit_id) WHERE type='EXHIBIT_INSPECTED'
DO UPDATE SET world_version=EXCLUDED.world_version,room_id=EXCLUDED.room_id,created_at=EXCLUDED.created_at`,
		security.Crypto{}.NewID(), session.UserID, state.WorldVersion, state.RoomID, exhibitID, inspected)
	if err != nil {
		return time.Time{}, authStoreError(ctx, "save inspection memory")
	}
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return time.Time{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return time.Time{}, authStoreError(ctx, "commit inspection and memory")
	}
	return inspected, nil
}

// ListMemoryEvents returns only the requested owner's most recent personal actions.
func (s *SocialStore) ListMemoryEvents(ctx context.Context, userID string, limit int) ([]game.MemoryEvent, error) {
	if limit < 1 || limit > 100 {
		return nil, domain.Validation
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT id::text,user_id::text,type,world_version,room_id,exhibit_id,photo_id,created_at
FROM memory_events WHERE user_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, authStoreError(ctx, "read personal memory")
	}
	defer rows.Close()
	events := make([]game.MemoryEvent, 0, limit)
	for rows.Next() {
		var event game.MemoryEvent
		if err = rows.Scan(&event.ID, &event.UserID, &event.Type, &event.WorldVersion, &event.RoomID, &event.ExhibitID, &event.PhotoID, &event.CreatedAt); err != nil {
			return nil, authStoreError(ctx, "decode personal memory")
		}
		events = append(events, event)
	}
	if rows.Err() != nil {
		return nil, authStoreError(ctx, "read personal memory rows")
	}
	return events, nil
}

var (
	_ game.InspectionStore = (*SocialStore)(nil)
	_ game.MemoryReader    = (*SocialStore)(nil)
)
