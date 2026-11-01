package postgres

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

func TestDemoGalleryMigrationPreservesSettings(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	// Start before 00008 to exercise the actual historical 40 -> 10 -> 5 path.
	migrateBeforeNPCLimit(ctx, t, dsn)
	body, err := os.ReadFile("../../../../content/world/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct{ Rooms []struct{ ID string } }
	if err = json.Unmarshal(body, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Rooms) != 24 {
		t.Fatal("expected 24 demo rooms")
	}
	store := NewRuntimeStore(pool)
	originals := make([]domain.Overlay, 0, 24)
	for i, r := range catalog.Rooms {
		o := testOverlay()
		o.WorldVersion, o.RoomID = "gallery_c01_v1", r.ID
		o.Config.NPCCount = []int{40, 10, 0, 5}[i%4]
		o.Config.Lighting = "warm"
		o.Config.InstallationVisible = true
		o.Lamp.On = false
		if err = store.CompareAndSwap(ctx, 1, o); err != nil {
			t.Fatal(err)
		}
		originals = append(originals, o)
	}
	// Existing target settings win over the old version; only count is normalized.
	target := originals[0].Clone()
	target.WorldVersion = "gallery_c01_v2"
	target.Config.NPCCount = 7
	target.Config.Lighting = "cool"
	if err = store.CompareAndSwap(ctx, 1, target); err != nil {
		t.Fatal(err)
	}
	foreign := originals[1].Clone()
	foreign.WorldVersion = "unrelated"
	if err = store.CompareAndSwap(ctx, 1, foreign); err != nil {
		t.Fatal(err)
	}
	unknown := foreign.Clone()
	unknown.WorldVersion, unknown.RoomID = "gallery_c01_v2", "custom_room"
	if err = store.CompareAndSwap(ctx, 1, unknown); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO guide_overrides(world_version,guide_id,revision,settings) VALUES ('gallery_c01_v1','guide_leonardo',7,'{"display_name":"Custom","material_ids":["lesson_room_leonardo_portrait"]}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	for i, original := range originals {
		wantOld := original.Clone()
		if wantOld.Config.NPCCount > 10 {
			wantOld.Config.NPCCount = 10
			wantOld.Revision++
		}
		got, exists, readErr := store.Read(ctx, original.WorldVersion, original.RoomID)
		if readErr != nil || !exists || !reflect.DeepEqual(got, wantOld) {
			t.Fatalf("source changed: got=%+v want=%+v err=%v", got, wantOld, readErr)
		}
		want := wantOld.Clone()
		want.WorldVersion = "gallery_c01_v2"
		if i == 0 {
			want = target.Clone()
		}
		want.Config.NPCCount = 5
		want.Revision++
		got, exists, readErr = NewRuntimeStore(pool).Read(ctx, want.WorldVersion, want.RoomID)
		if readErr != nil || !exists || !reflect.DeepEqual(got, want) {
			t.Fatalf("target: got=%+v want=%+v err=%v", got, want, readErr)
		}
	}
	for _, want := range []domain.Overlay{foreign, unknown} {
		got, exists, readErr := store.Read(ctx, want.WorldVersion, want.RoomID)
		if readErr != nil || !exists || !reflect.DeepEqual(got, want) {
			t.Fatal("unrelated overlay changed", got, readErr)
		}
	}
	var preserved bool
	if err = pool.QueryRow(ctx, `SELECT a.revision=b.revision AND a.settings=b.settings FROM guide_overrides a JOIN guide_overrides b USING(guide_id) WHERE a.world_version='gallery_c01_v1' AND b.world_version='gallery_c01_v2'`).Scan(&preserved); err != nil || !preserved {
		t.Fatal("guide settings lost", err)
	}
	// An admin can still select ten; rerunning migrations must not reset that choice.
	current, _, err := store.Read(ctx, target.WorldVersion, target.RoomID)
	if err != nil {
		t.Fatal(err)
	}
	next := current.Clone()
	next.Revision++
	next.Config.NPCCount = 10
	if err = store.CompareAndSwap(ctx, current.Revision, next); err != nil {
		t.Fatal(err)
	}
	if _, err = Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	got, _, err := NewRuntimeStore(pool).Read(ctx, next.WorldVersion, next.RoomID)
	if err != nil || !reflect.DeepEqual(got, next) {
		t.Fatal("migration replay reset admin settings", got, err)
	}
}
