package postgres

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func TestRuntimeCASDurabilityAndVersionIsolationIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewRuntimeStore(pool)
	if _, exists, err := store.Read(ctx, "v1", "room1"); err != nil || exists {
		t.Fatalf("initial overlay exists=%v err=%v", exists, err)
	}
	o := testOverlay()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- store.CompareAndSwap(ctx, 1, o) }()
	}
	wg.Wait()
	close(errs)
	success, conflicts := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, runtime.RevisionConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success %d conflicts %d", success, conflicts)
	}
	// A new adapter has no shared RAM state: restoration comes entirely from SQL.
	got, exists, err := NewRuntimeStore(pool).Read(ctx, "v1", "room1")
	if err != nil || !exists || !reflect.DeepEqual(got, o) {
		t.Fatalf("persisted=%#v exists=%v err=%v", got, exists, err)
	}
	if _, exists, err = store.Read(ctx, "v2", "room1"); err != nil || exists {
		t.Fatalf("old overlay leaked: exists %v err %v", exists, err)
	}
	next := o.Clone()
	next.Revision = 3
	next.Config.Lighting = "warm"
	if err = store.CompareAndSwap(ctx, 2, next); err != nil {
		t.Fatal(err)
	}
	if err = store.CompareAndSwap(ctx, 2, next); !errors.Is(err, runtime.RevisionConflict) {
		t.Fatal(err)
	}
	next.WorldVersion = "v2"
	next.Revision = 3
	if err = store.CompareAndSwap(ctx, 2, next); !errors.Is(err, runtime.RevisionConflict) {
		t.Fatal("missing version accepted stale revision", err)
	}
}

func TestRuntimeBusyAndRollbackIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewRuntimeStore(pool)
	o := testOverlay()
	if err := store.CompareAndSwap(ctx, 1, o); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE room_overlays SET lamp=jsonb_set(jsonb_set(lamp,'{holder_user_id}','"user"'),'{holder_connection_id}','"private-connection"')`); err != nil {
		t.Fatal(err)
	}
	next := o.Clone()
	next.Revision = 3
	if err := store.CompareAndSwap(ctx, 2, next); !errors.Is(err, runtime.ObjectBusy) {
		t.Fatal(err)
	}
	if err := store.CompareAndSwap(ctx, 1, o); !errors.Is(err, runtime.RevisionConflict) {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE room_overlays SET lamp=jsonb_set(jsonb_set(lamp,'{holder_user_id}','null'),'{holder_connection_id}','null'); ALTER TABLE room_overlays ADD CONSTRAINT reject_updates CHECK(revision < 3)`); err != nil {
		t.Fatal(err)
	}
	if err := store.CompareAndSwap(ctx, 2, next); !errors.Is(err, auth.Unavailable) {
		t.Fatalf("SQL failure=%v", err)
	}
	got, exists, err := store.Read(ctx, "v1", "room1")
	if err != nil || !exists || got.Revision != 2 {
		t.Fatalf("failed SQL changed state: %+v, %v", got, err)
	}
}

func TestAdminMembershipGrantIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	const userID = "00000000-0000-4000-8000-000000000037"
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,display_name,avatar_color,password_hash) VALUES($1,'alice','Alice','#AABBCC','encoded')`, userID); err != nil {
		t.Fatal(err)
	}
	store := NewRuntimeStore(pool)
	if admin, err := store.IsAdmin(ctx, userID); err != nil || admin {
		t.Fatal(admin, err)
	}
	if err := store.GrantAdmin(ctx, "missing"); !errors.Is(err, world.NotFound) {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.GrantAdmin(ctx, "ALICE"); err != nil {
			t.Fatal(err)
		}
	}
	if admin, err := store.IsAdmin(ctx, userID); err != nil || !admin {
		t.Fatal(admin, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_memberships`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM admin_memberships WHERE user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if admin, err := store.IsAdmin(ctx, userID); err != nil || admin {
		t.Fatal("revoked admin retained", admin, err)
	}
}
