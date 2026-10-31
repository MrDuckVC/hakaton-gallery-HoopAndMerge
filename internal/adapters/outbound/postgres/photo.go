package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	photocase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/photo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PhotoStore serializes durable upload identities without transactions spanning file I/O.
type PhotoStore struct{ pool *pgxpool.Pool }

// NewPhotoStore shares the composition root's connection pool.
func NewPhotoStore(pool *pgxpool.Pool) *PhotoStore { return &PhotoStore{pool: pool} }

type lockedPhoto struct {
	conn      *pgxpool.Conn
	user, key string
}

// WithKey owns a session advisory lock and guarantees it never leaks into the pool.
func (s *PhotoStore) WithKey(ctx context.Context, user, key string, fn func(photocase.LockedStore) error) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return authStoreError(ctx, "acquire photo connection")
	}
	hash := sha256.Sum256([]byte("photo:" + user + ":" + key))
	lockID := int64(binary.BigEndian.Uint64(hash[:8]))
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		var unlocked bool
		if e := conn.QueryRow(cleanup, `SELECT pg_advisory_unlock($1)`, lockID).Scan(&unlocked); e != nil || !unlocked {
			_ = conn.Conn().Close(cleanup)
		}
		conn.Release()
	}()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockID); err != nil {
		// Cancellation can race with acquiring the lock; discard the session on any ambiguity.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = conn.Conn().Close(cleanup)
		return authStoreError(ctx, "lock photo key")
	}
	return fn(&lockedPhoto{conn: conn, user: user, key: key})
}

func (s *lockedPhoto) Find(ctx context.Context) (domain.Photo, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var p domain.Photo
	var fingerprint []byte
	err := s.conn.QueryRow(ctx, `SELECT id,user_id::text,idempotency_key::text,request_fingerprint,world_version,room_id,movement_epoch,exhibit_id,caption,published,content_type,image_bytes,thumbnail_bytes,width,height,created_at FROM photos WHERE user_id=$1 AND idempotency_key=$2`, s.user, s.key).Scan(&p.ID, &p.UserID, &p.Key, &fingerprint, &p.WorldVersion, &p.RoomID, &p.Epoch, &p.ExhibitID, &p.Caption, &p.Published, &p.ContentType, &p.ImageBytes, &p.ThumbnailBytes, &p.Width, &p.Height, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Photo{}, auth.Code("NOT_FOUND")
	}
	if err != nil {
		return domain.Photo{}, authStoreError(ctx, "read photo identity")
	}
	if len(fingerprint) != 32 {
		return domain.Photo{}, auth.Unavailable
	}
	copy(p.Fingerprint[:], fingerprint)
	return p, nil
}

func (s *lockedPhoto) Create(ctx context.Context, session auth.Session, p domain.Photo, check func() error) (domain.Photo, error) {
	if session.UserID != s.user || p.UserID != s.user || p.Key != s.key {
		return domain.Photo{}, auth.Validation
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return domain.Photo{}, authStoreError(ctx, "begin photo")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return domain.Photo{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO photos(id,user_id,idempotency_key,request_fingerprint,world_version,room_id,movement_epoch,exhibit_id,caption,published,content_type,original_path,thumbnail_path,image_bytes,thumbnail_bytes,width,height,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,false,$10,$11,$12,$13,$14,$15,$16,$17)`, p.ID, p.UserID, p.Key, p.Fingerprint[:], p.WorldVersion, p.RoomID, p.Epoch, p.ExhibitID, p.Caption, p.ContentType, p.ID+"/image", p.ID+"/thumbnail.jpg", p.ImageBytes, p.ThumbnailBytes, p.Width, p.Height, p.CreatedAt)
	if err != nil {
		return domain.Photo{}, authStoreError(ctx, "save photo")
	}
	_, err = tx.Exec(ctx, `INSERT INTO memory_events(id,user_id,type,world_version,room_id,exhibit_id,photo_id,created_at) VALUES($1,$2,'PHOTO_CREATED',$3,$4,$5,$6,$7)`, security.Crypto{}.NewID(), p.UserID, p.WorldVersion, p.RoomID, p.ExhibitID, p.ID, p.CreatedAt)
	if err != nil {
		return domain.Photo{}, authStoreError(ctx, "save photo memory")
	}
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return domain.Photo{}, err
	}
	if err = check(); err != nil {
		return domain.Photo{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return domain.Photo{}, authStoreError(ctx, "rollback photo commit")
		}
		return domain.Photo{}, photocase.ErrUncertain
	}
	return p, nil
}
