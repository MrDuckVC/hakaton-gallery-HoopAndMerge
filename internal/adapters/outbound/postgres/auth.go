package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuthStore implements the atomic account/session and pose persistence ports.
type AuthStore struct{ pool *pgxpool.Pool }

// NewAuthStore reuses the process-owned pool.
func NewAuthStore(pool *pgxpool.Pool) *AuthStore { return &AuthStore{pool: pool} }

// FindAccount loads the private verifier for a normalized username.
func (s *AuthStore) FindAccount(ctx context.Context, username string) (domain.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var account domain.Account
	err := s.pool.QueryRow(ctx, `SELECT id::text,username,display_name,avatar_color,password_hash FROM users WHERE username=$1`, username).Scan(&account.User.ID, &account.User.Username, &account.User.DisplayName, &account.User.AvatarColor, &account.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, domain.InvalidCredentials
	}
	if err != nil {
		return domain.Account{}, authStoreError(ctx, "find account")
	}
	return account, nil
}

// CreateLogin commits all registration/replacement effects in one transaction.
func (s *AuthStore) CreateLogin(ctx context.Context, registration *auth.Registration, session domain.Session, previous *[32]byte, now time.Time) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", authStoreError(ctx, "begin login")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if registration != nil {
		user := registration.Account.User
		_, err = tx.Exec(ctx, `INSERT INTO users(id,username,display_name,avatar_color,password_hash) VALUES($1,$2,$3,$4,$5)`, user.ID, user.Username, user.DisplayName, user.AvatarColor, registration.Account.PasswordHash)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_username_key" {
				return "", domain.UsernameTaken
			}
			return "", authStoreError(ctx, "insert account")
		}
		state := registration.State
		_, err = tx.Exec(ctx, `INSERT INTO player_states(user_id,world_version,room_id,x,y,z,yaw,pitch) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, user.ID, state.WorldVersion, state.RoomID, state.Position.X, state.Position.Y, state.Position.Z, state.Rotation.Yaw, state.Rotation.Pitch)
		if err != nil {
			return "", authStoreError(ctx, "insert initial player state")
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO sessions(id,token_hash,user_id,created_at,expires_at) VALUES($1,$2,$3,$4,$5)`, session.ID, session.TokenHash[:], session.UserID, session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return "", authStoreError(ctx, "insert session")
	}
	var revoked string
	if previous != nil {
		err = tx.QueryRow(ctx, `UPDATE sessions SET revoked_at=$2 WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>$2 RETURNING id::text`, previous[:], now).Scan(&revoked)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", authStoreError(ctx, "replace cookie session")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", authStoreError(ctx, "commit login")
	}
	return revoked, nil
}

// FindSession loads identity and private session validity data from a token digest.
func (s *AuthStore) FindSession(ctx context.Context, hash [32]byte) (domain.Session, domain.User, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var session domain.Session
	var user domain.User
	err := s.pool.QueryRow(ctx, `SELECT s.id::text,s.user_id::text,s.created_at,s.expires_at,s.revoked_at,u.id::text,u.username,u.display_name,u.avatar_color FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1`, hash[:]).Scan(&session.ID, &session.UserID, &session.CreatedAt, &session.ExpiresAt, &session.RevokedAt, &user.ID, &user.Username, &user.DisplayName, &user.AvatarColor)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, domain.User{}, domain.Unauthenticated
	}
	if err != nil {
		return domain.Session{}, domain.User{}, authStoreError(ctx, "find session")
	}
	session.TokenHash = hash
	return session, user, nil
}

// SessionByID supports the validity recheck after registering a game observer.
func (s *AuthStore) SessionByID(ctx context.Context, id string) (domain.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var session domain.Session
	err := s.pool.QueryRow(ctx, `SELECT id::text,user_id::text,created_at,expires_at,revoked_at FROM sessions WHERE id=$1`, id).Scan(&session.ID, &session.UserID, &session.CreatedAt, &session.ExpiresAt, &session.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, domain.Unauthenticated
	}
	if err != nil {
		return domain.Session{}, authStoreError(ctx, "find session by ID")
	}
	return session, nil
}

// Revoke is an idempotent, persisted transition returning only a newly revoked session ID.
func (s *AuthStore) Revoke(ctx context.Context, hash [32]byte, now time.Time) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var id string
	err := s.pool.QueryRow(ctx, `UPDATE sessions SET revoked_at=$2 WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>$2 RETURNING id::text`, hash[:], now).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", authStoreError(ctx, "revoke session")
	}
	return id, nil
}

// PlayerState returns a zero revision when a legacy account has no saved position.
func (s *AuthStore) PlayerState(ctx context.Context, userID string) (player.State, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var state player.State
	err := s.pool.QueryRow(ctx, `SELECT world_version,room_id,x,y,z,yaw,pitch,updated_at FROM player_states WHERE user_id=$1`, userID).Scan(&state.WorldVersion, &state.RoomID, &state.Position.X, &state.Position.Y, &state.Position.Z, &state.Rotation.Yaw, &state.Rotation.Pitch, &state.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return player.State{}, nil
	}
	if err != nil {
		return player.State{}, authStoreError(ctx, "read player state")
	}
	return state, nil
}

// RepairState uses a revision comparison so restoration never overwrites a concurrent save.
func (s *AuthStore) RepairState(ctx context.Context, session domain.Session, old, next player.State) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, authStoreError(ctx, "begin player state repair")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return false, err
	}
	args := []any{session.UserID, next.WorldVersion, next.RoomID, next.Position.X, next.Position.Y, next.Position.Z, next.Rotation.Yaw, next.Rotation.Pitch}
	query := `INSERT INTO player_states(user_id,world_version,room_id,x,y,z,yaw,pitch) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(user_id) DO NOTHING`
	if !old.UpdatedAt.IsZero() {
		query = `UPDATE player_states SET world_version=$2,room_id=$3,x=$4,y=$5,z=$6,yaw=$7,pitch=$8,updated_at=GREATEST(clock_timestamp(),updated_at+interval '1 microsecond') WHERE user_id=$1 AND updated_at=$9`
		args = append(args, old.UpdatedAt)
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return false, authStoreError(ctx, "repair player state")
	}
	// Recheck expiry immediately before commit while the share lock excludes revocation.
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, authStoreError(ctx, "commit player state repair")
	}
	return tag.RowsAffected() == 1, nil
}

func authStoreError(ctx context.Context, operation string) error {
	return fmt.Errorf("%w: %w", domain.Unavailable, safeError(ctx, operation))
}

// The row lock serializes repair against logout/replacement without holding a transaction on world I/O.
func lockRepairSession(ctx context.Context, tx pgx.Tx, session domain.Session) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() FOR SHARE`, session.ID, session.UserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Unauthenticated
	}
	if err != nil {
		return authStoreError(ctx, "authorize player state repair")
	}
	return nil
}
