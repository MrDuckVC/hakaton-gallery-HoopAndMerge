package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	photocase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/photo"
)

func photoRecord(session auth.Session) domain.Photo {
	return domain.Photo{ID: security.Crypto{}.NewID(), UserID: session.UserID, Key: security.Crypto{}.NewID(), WorldVersion: "v1", RoomID: "room", Epoch: 1, Caption: "test", ContentType: "image/jpeg", Width: 10, Height: 10, ImageBytes: 50, ThumbnailBytes: 20, CreatedAt: time.Now().UTC()}
}
func TestPhotoKeySerializationAndMemoryIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	session := socialSession(ctx, t, pool, "photo_owner")
	record := photoRecord(session)
	store := NewPhotoStore(pool)
	errs := make(chan error, 5)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			errs <- store.WithKey(ctx, session.UserID, record.Key, func(locked photocase.LockedStore) error {
				p, err := locked.Find(ctx)
				if err == nil {
					if p.ID != record.ID {
						return errors.New("different record")
					}
					return nil
				}
				if !errors.Is(err, auth.Code("NOT_FOUND")) {
					return err
				}
				_, err = locked.Create(ctx, session, record, func() error { return nil })
				return err
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var n int
	for _, table := range []string{"photos", "memory_events"} {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 1 {
			t.Fatal(table, n, err)
		}
	}
	if err := store.WithKey(ctx, session.UserID, record.Key, func(locked photocase.LockedStore) error {
		p, err := locked.Find(ctx)
		if err != nil || p.ID != record.ID || p.Fingerprint != record.Fingerprint || p.ContentType != "image/jpeg" {
			t.Fatal(p, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	other := socialSession(ctx, t, pool, "photo_other")
	if err := store.WithKey(ctx, other.UserID, record.Key, func(locked photocase.LockedStore) error {
		_, err := locked.Find(ctx)
		if !errors.Is(err, auth.Code("NOT_FOUND")) {
			t.Fatal("isolation", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestPhotoRollbackFinalCheckAndSessionIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	session := socialSession(ctx, t, pool, "photo_failure")
	store := NewPhotoStore(pool)
	for _, mode := range []string{"check", "memory", "revoked", "expired"} {
		t.Run(mode, func(t *testing.T) {
			p := photoRecord(session)
			if mode == "memory" {
				if _, err := pool.Exec(ctx, `ALTER TABLE memory_events ADD CONSTRAINT reject_photo CHECK(type<>'PHOTO_CREATED')`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "revoked" {
				if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, session.ID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "expired" {
				if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=NULL,created_at=clock_timestamp()-interval '2 days',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, session.ID); err != nil {
					t.Fatal(err)
				}
			}
			err := store.WithKey(ctx, session.UserID, p.Key, func(locked photocase.LockedStore) error {
				_, e := locked.Create(ctx, session, p, func() error {
					if mode == "check" {
						return auth.Code("GAME_NOT_READY")
					}
					return nil
				})
				return e
			})
			if err == nil {
				t.Fatal("failure committed")
			}
			var n int
			if e := pool.QueryRow(ctx, `SELECT count(*) FROM photos`).Scan(&n); e != nil || n != 0 {
				t.Fatal(n, e)
			}
			if mode == "memory" {
				if _, e := pool.Exec(ctx, `ALTER TABLE memory_events DROP CONSTRAINT reject_photo`); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
