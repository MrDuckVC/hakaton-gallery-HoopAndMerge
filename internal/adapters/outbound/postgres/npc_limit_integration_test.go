package postgres

import (
	"context"
	"database/sql"
	"io/fs"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/runtimewire"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	runtimecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/migrations"
	"github.com/pressly/goose/v3"
)

func migrateBeforeNPCLimit(ctx context.Context, t *testing.T, dsn string) {
	t.Helper()
	files := fstest.MapFS{}
	entries, readErr := fs.ReadDir(migrations.Files(), ".")
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			continue
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatal(err)
		}
		if version >= 8 {
			continue
		}
		data, err := fs.ReadFile(migrations.Files(), e.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = &fstest.MapFile{Data: data}
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNPCLimitMigration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	migrateBeforeNPCLimit(ctx, t, dsn)
	store := NewRuntimeStore(pool)
	originals := make([]domain.Overlay, 0, 5)
	for i, count := range []int{0, 10, 11, 40, 40} {
		o := testOverlay()
		o.RoomID = "npc_" + strconv.Itoa(i)
		o.Config.NPCCount = count
		o.Config.Lighting = "warm"
		o.Lamp.HolderUserID = "00000000-0000-4000-8000-000000000123"
		o.Lamp.HolderConnectionID = "held-connection"
		if i == 4 {
			o.WorldVersion = "older-world"
		}
		if err := store.CompareAndSwapLamp(ctx, 1, domain.Holder{}, o); err != nil {
			t.Fatal(err)
		}
		originals = append(originals, o)
	}
	catalog := runtimeCatalog{}
	seed := domain.Seed(catalog.Rooms()[0])
	seed.Config.NPCCount = 40
	seed.Revision = 2
	if err := store.CompareAndSwap(ctx, 1, seed); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	for _, original := range originals {
		want := original.Clone()
		if want.Config.NPCCount > 10 {
			want.Config.NPCCount = 10
			want.Revision++
		}
		got, exists, err := store.Read(ctx, original.WorldVersion, original.RoomID)
		if err != nil || !exists || !reflect.DeepEqual(got, want) {
			t.Fatalf("got=%+v want=%+v exists=%v err=%v", got, want, exists, err)
		}
	}
	// Reapplying migrations must not increment revisions a second time.
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	got, _, err := store.Read(ctx, "v1", "npc_3")
	if err != nil || got.Revision != 3 {
		t.Fatal(got, err)
	}
	rooms := runtimecase.New(catalog, store, store, game.New(time.Now), runtimewire.Sizer{})
	if err = rooms.Initialize(ctx); err != nil {
		t.Fatal("restart failed after limit migration", err)
	}
	restored, err := rooms.Read(ctx, "safe")
	if err != nil || restored.Config.NPCCount != 10 || restored.Revision != 3 {
		t.Fatal(restored, err)
	}
}

func TestNPCLimitOverflow(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	migrateBeforeNPCLimit(ctx, t, dsn)
	store := NewRuntimeStore(pool)
	for _, id := range []string{"first", "overflow"} {
		o := testOverlay()
		o.RoomID = id
		o.Config.NPCCount = 40
		if err := store.CompareAndSwap(ctx, 1, o); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE room_overlays SET revision=9007199254740991 WHERE room_id='overflow'`); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, dsn, "up"); err == nil {
		t.Fatal("overflow migration succeeded")
	}
	for _, id := range []string{"first", "overflow"} {
		got, exists, err := store.Read(ctx, "v1", id)
		wantRevision := int64(2)
		if id == "overflow" {
			wantRevision = 9007199254740991
		}
		if err != nil || !exists || got.Config.NPCCount != 40 || got.Revision != wantRevision {
			t.Fatal("migration partially applied", got, err)
		}
	}
	if err := CheckSchema(ctx, pool); err == nil {
		t.Fatal("failed migration marked applied")
	}
}
