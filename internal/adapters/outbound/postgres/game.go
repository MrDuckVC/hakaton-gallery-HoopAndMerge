package postgres

import (
	"context"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GameStore persists authoritative poses using optimistic revisions.
type GameStore struct{ pool *pgxpool.Pool }

// NewGameStore reuses the process-owned pool.
func NewGameStore(pool *pgxpool.Pool) *GameStore { return &GameStore{pool: pool} }

// PlayerState reads the same durable pose used by authentication restoration.
func (s *GameStore) PlayerState(ctx context.Context, userID string) (player.State, error) {
	return NewAuthStore(s.pool).PlayerState(ctx, userID)
}

// SaveState commits a pose only if its expected revision is still current.
// requireSession is false only for flushing an already authorized server pose;
// it permits disconnect cleanup after logout or session expiry.
func (s *GameStore) SaveState(ctx context.Context, session domain.Session, old, next player.State, requireSession bool) (player.State, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return player.State{}, authStoreError(ctx, "begin player state save")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if requireSession {
		if err = lockRepairSession(ctx, tx, session); err != nil {
			return player.State{}, err
		}
	}
	var saved player.State
	err = tx.QueryRow(ctx, `UPDATE player_states
SET world_version=$2,room_id=$3,x=$4,y=$5,z=$6,yaw=$7,pitch=$8,
updated_at=GREATEST(clock_timestamp(),updated_at+interval '1 microsecond')
WHERE user_id=$1 AND updated_at=$9
RETURNING world_version,room_id,x,y,z,yaw,pitch,updated_at`,
		session.UserID, next.WorldVersion, next.RoomID, next.Position.X, next.Position.Y, next.Position.Z, next.Rotation.Yaw, next.Rotation.Pitch, old.UpdatedAt,
	).Scan(&saved.WorldVersion, &saved.RoomID, &saved.Position.X, &saved.Position.Y, &saved.Position.Z, &saved.Rotation.Yaw, &saved.Rotation.Pitch, &saved.UpdatedAt)
	if err != nil {
		return player.State{}, authStoreError(ctx, "save player state or resolve revision conflict")
	}
	if requireSession {
		if err = lockRepairSession(ctx, tx, session); err != nil {
			return player.State{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return player.State{}, authStoreError(ctx, "commit player state save")
	}
	return saved, nil
}
