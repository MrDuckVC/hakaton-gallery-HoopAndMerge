package postgres

import (
	"errors"
	"sync"
	"testing"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

func TestLampCASAndLateReleaseIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	next := testOverlay()
	next.Lamp.HolderUserID = "u"
	next.Lamp.HolderConnectionID = "c"
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { results <- NewRuntimeStore(pool).CompareAndSwapLamp(ctx, 1, domain.Holder{}, next) })
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, domain.RevisionConflict):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success %d conflicts %d", success, conflict)
	}
	store := NewRuntimeStore(pool)
	release := next.Clone()
	release.Revision = 3
	release.Lamp.HolderUserID = ""
	release.Lamp.HolderConnectionID = ""
	if err := store.CompareAndSwapLamp(ctx, 2, domain.Holder{UserID: "u", ConnectionID: "old"}, release); !errors.Is(err, domain.RevisionConflict) {
		t.Fatalf("stale owner: %v", err)
	}
	if err := store.CompareAndSwap(ctx, 2, release); !errors.Is(err, domain.ObjectBusy) {
		t.Fatalf("admin held: %v", err)
	}
	if err := store.CompareAndSwapLamp(ctx, 2, next.Lamp.Owner(), release); err != nil {
		t.Fatal(err)
	}
	newer := release.Clone()
	newer.Revision = 4
	newer.Lamp.HolderUserID = "v"
	newer.Lamp.HolderConnectionID = "new"
	if err := store.CompareAndSwapLamp(ctx, 3, domain.Holder{}, newer); err != nil {
		t.Fatal(err)
	}
	release.Revision = 5
	if err := store.CompareAndSwapLamp(ctx, 4, next.Lamp.Owner(), release); !errors.Is(err, domain.RevisionConflict) {
		t.Fatal("old cleanup removed new owner", err)
	}
	got, _, err := store.Read(ctx, next.WorldVersion, next.RoomID)
	if err != nil || got.Revision != 4 || got.Lamp.Owner() != newer.Lamp.Owner() {
		t.Fatalf("durable holder %+v %v", got, err)
	}
}

func TestLampMissingRowCannotReleaseForeignOwnerIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	next := testOverlay()
	if err := NewRuntimeStore(pool).CompareAndSwapLamp(ctx, 1, domain.Holder{UserID: "u", ConnectionID: "old"}, next); !errors.Is(err, domain.RevisionConflict) {
		t.Fatalf("missing row accepted foreign owner: %v", err)
	}
}
