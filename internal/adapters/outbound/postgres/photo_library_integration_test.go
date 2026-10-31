package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	photocase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/photo"
)

func saveLibraryPhoto(ctx context.Context, t *testing.T, store *PhotoStore, session auth.Session, p domain.Photo) {
	t.Helper()
	if err := store.WithKey(ctx, session.UserID, p.Key, func(locked photocase.LockedStore) error {
		_, err := locked.Create(ctx, session, p, func() error { return nil })
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
func TestPhotoAlbumKeysetIsolationIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	owner := socialSession(ctx, t, pool, "album_owner")
	other := socialSession(ctx, t, pool, "album_other")
	store := NewPhotoStore(pool)
	empty, next, err := store.Album(ctx, owner.UserID, 6, "")
	if err != nil || len(empty) != 0 || next != "" {
		t.Fatal(empty, next, err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for n := range 8 {
		p := photoRecord(owner)
		p.CreatedAt = now.Add(time.Duration(n/2) * time.Second)
		saveLibraryPhoto(ctx, t, store, owner, p)
	}
	foreign := photoRecord(other)
	saveLibraryPhoto(ctx, t, store, other, foreign)
	first, cursor, err := store.Album(ctx, owner.UserID, 3, "")
	if err != nil || len(first) != 3 || cursor == "" {
		t.Fatal(first, cursor, err)
	}
	inserted := photoRecord(owner)
	inserted.CreatedAt = now.Add(time.Hour)
	saveLibraryPhoto(ctx, t, store, owner, inserted)
	seen := map[string]bool{}
	visit := func(items []domain.Photo) {
		t.Helper()
		for i, p := range items {
			if p.UserID != owner.UserID || seen[p.ID] || p.ID == inserted.ID {
				t.Fatal("invalid page", p)
			}
			if i > 0 && (items[i-1].CreatedAt.Before(p.CreatedAt) || (items[i-1].CreatedAt.Equal(p.CreatedAt) && items[i-1].ID <= p.ID)) {
				t.Fatal("wrong order")
			}
			seen[p.ID] = true
		}
	}
	visit(first)
	for cursor != "" {
		items, nextCursor, e := store.Album(ctx, owner.UserID, 3, cursor)
		if e != nil {
			t.Fatal(e)
		}
		visit(items)
		cursor = nextCursor
	}
	if len(seen) != 8 {
		t.Fatal("shifted pages", len(seen))
	}
	for _, bad := range []string{"!", base64.RawURLEncoding.EncodeToString([]byte("missing")), base64.RawURLEncoding.EncodeToString([]byte("00000000-0000-4000-8000-000000000000")), base64.RawURLEncoding.EncodeToString([]byte(foreign.ID))} {
		if _, _, e := store.Album(ctx, owner.UserID, 6, bad); !errors.Is(e, auth.Code("INVALID_CURSOR")) {
			t.Fatal(bad, e)
		}
	}
	for _, limit := range []int{0, 7} {
		if _, _, e := store.Album(ctx, owner.UserID, limit, ""); !errors.Is(e, auth.Validation) {
			t.Fatal(e)
		}
	}
}
func TestPhotoPublicationTransactionIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	owner := socialSession(ctx, t, pool, "publish_owner")
	other := socialSession(ctx, t, pool, "publish_other")
	store := NewPhotoStore(pool)
	library := photocase.NewLibrary(nil, store, nil)
	p := photoRecord(owner)
	saveLibraryPhoto(ctx, t, store, owner, p)
	for _, id := range []string{p.ID, "missing"} {
		if _, err := library.Publish(ctx, other, id); !errors.Is(err, auth.Code("NOT_FOUND")) {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE photos ADD CONSTRAINT reject_publication CHECK(NOT published)`); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Publish(ctx, owner, p.ID); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
	stored, err := store.GetPhoto(ctx, p.ID)
	if err != nil || stored.Published {
		t.Fatal(stored, err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE photos DROP CONSTRAINT reject_publication`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Go(func() {
			published, e := library.Publish(ctx, owner, p.ID)
			if e == nil && (!published.Published || published.ID != p.ID || !published.CreatedAt.Equal(p.CreatedAt.Truncate(time.Microsecond))) {
				e = errors.New("wrong published metadata")
			}
			errs <- e
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	restarted := NewPhotoStore(pool)
	stored, err = restarted.GetPhoto(ctx, p.ID)
	if err != nil || !stored.Published {
		t.Fatal(stored, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM memory_events WHERE photo_id=$1`, p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	for _, mode := range []string{"revoked", "expired"} {
		if mode == "revoked" {
			_, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, owner.ID)
		} else {
			_, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=NULL,created_at=clock_timestamp()-interval '2 days',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, owner.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err = library.Publish(ctx, owner, p.ID); !errors.Is(err, auth.Unauthenticated) {
			t.Fatal(mode, err)
		}
	}
}
