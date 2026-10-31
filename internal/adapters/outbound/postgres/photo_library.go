package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	"github.com/jackc/pgx/v5"
)

const photoReadColumns = `id,user_id::text,world_version,room_id,exhibit_id,caption,published,content_type,image_bytes,thumbnail_bytes,width,height,created_at`

func scanPhoto(row pgx.Row) (domain.Photo, error) {
	var p domain.Photo
	err := row.Scan(&p.ID, &p.UserID, &p.WorldVersion, &p.RoomID, &p.ExhibitID, &p.Caption, &p.Published, &p.ContentType, &p.ImageBytes, &p.ThumbnailBytes, &p.Width, &p.Height, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Photo{}, auth.Code("NOT_FOUND")
	}
	if err != nil {
		return domain.Photo{}, auth.Unavailable
	}
	return p, nil
}

// GetPhoto loads metadata only. The application authorizes before reading files.
func (s *PhotoStore) GetPhoto(ctx context.Context, id string) (domain.Photo, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	p, err := scanPhoto(s.pool.QueryRow(ctx, `SELECT `+photoReadColumns+` FROM photos WHERE id=$1`, id))
	return p, err
}

// Album validates the opaque boundary against its owner before descending keyset selection.
func (s *PhotoStore) Album(ctx context.Context, user string, limit int, cursor string) ([]domain.Photo, string, error) {
	if limit < 1 || limit > 6 {
		return nil, "", auth.Validation
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	var boundaryTime *time.Time
	boundaryID := ""
	if cursor != "" {
		if len(cursor) != 48 {
			return nil, "", auth.Code("INVALID_CURSOR")
		}
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
		if err != nil || !domain.UUIDv4(string(decoded)) || base64.RawURLEncoding.EncodeToString(decoded) != cursor {
			return nil, "", auth.Code("INVALID_CURSOR")
		}
		var created time.Time
		err = s.pool.QueryRow(ctx, `SELECT created_at FROM photos WHERE id=$1 AND user_id=$2`, string(decoded), user).Scan(&created)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", auth.Code("INVALID_CURSOR")
		}
		if err != nil {
			return nil, "", authStoreError(ctx, "read album boundary")
		}
		boundaryTime, boundaryID = &created, string(decoded)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+photoReadColumns+` FROM photos WHERE user_id=$1 AND ($2::timestamptz IS NULL OR (created_at,id)<($2::timestamptz,$3::text)) ORDER BY created_at DESC,id DESC LIMIT $4`, user, boundaryTime, boundaryID, limit+1)
	if err != nil {
		return nil, "", authStoreError(ctx, "read album")
	}
	defer rows.Close()
	items := make([]domain.Photo, 0, limit+1)
	for rows.Next() {
		p, e := scanPhoto(rows)
		if e != nil {
			return nil, "", e
		}
		items = append(items, p)
	}
	if rows.Err() != nil {
		return nil, "", authStoreError(ctx, "read album rows")
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = base64.RawURLEncoding.EncodeToString([]byte(items[len(items)-1].ID))
	}
	return items, next, nil
}

// Publish locks the photo and session, authorizes inside the transaction and commits before return.
func (s *PhotoStore) Publish(ctx context.Context, session auth.Session, id string, authorize func(domain.Photo) error) (domain.Photo, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Photo{}, authStoreError(ctx, "begin photo publication")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return domain.Photo{}, err
	}
	p, err := scanPhoto(tx.QueryRow(ctx, `SELECT `+photoReadColumns+` FROM photos WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return domain.Photo{}, err
	}
	if err = authorize(p); err != nil {
		return domain.Photo{}, err
	}
	if !p.Published {
		if _, err = tx.Exec(ctx, `UPDATE photos SET published=true WHERE id=$1`, id); err != nil {
			return domain.Photo{}, authStoreError(ctx, "publish photo")
		}
	}
	if err = lockRepairSession(ctx, tx, session); err != nil {
		return domain.Photo{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Photo{}, authStoreError(ctx, "commit photo publication")
	}
	p.Published = true
	return p, nil
}
