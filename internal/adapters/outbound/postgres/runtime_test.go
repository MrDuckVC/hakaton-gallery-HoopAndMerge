package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

func testOverlay() runtime.Overlay {
	return runtime.Overlay{WorldVersion: "v1", RoomID: "room1", Revision: 2,
		Config: runtime.Config{AccentColor: "#D9D2C3", Lighting: "neutral", GuidePlacements: []runtime.GuidePlacement{{GuideID: "guide1", Position: player.Vector{X: 6, Z: 7.5}}}, LampSlots: []runtime.LampSlot{{SlotID: "a", Position: player.Vector{X: 3, Z: 4}}, {SlotID: "b", Position: player.Vector{X: 9, Z: 4}}}, NPCCount: 10},
		Lamp:   runtime.Lamp{ObjectID: "lamp_01", SlotID: "a", On: true}}
}

func TestRuntimeStorageRoundTripAndPrivateOwnership(t *testing.T) {
	for _, held := range []bool{false, true} {
		o := testOverlay()
		if held {
			o.Lamp.HolderUserID = "user"
			o.Lamp.HolderConnectionID = "private-connection"
		}
		c, l := storageRuntime(o)
		config, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		lamp, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeRuntime(runtime.Overlay{WorldVersion: o.WorldVersion, RoomID: o.RoomID, Revision: o.Revision}, config, lamp)
		if err != nil || !reflect.DeepEqual(got, o) {
			t.Fatalf("roundtrip got %#v err %v", got, err)
		}
		if held != strings.Contains(string(lamp), `"holder_connection_id":"private-connection"`) {
			t.Fatal(string(lamp))
		}
	}
	for _, pair := range [][2]string{{"invalid", "{}"}, {"{}", "invalid"}} {
		if _, err := decodeRuntime(runtime.Overlay{}, []byte(pair[0]), []byte(pair[1])); !errors.Is(err, auth.Unavailable) {
			t.Fatal(err)
		}
	}
}

func TestRuntimeCASRejectsInvalidRevisionBeforeDatabase(t *testing.T) {
	store := NewRuntimeStore(nil)
	for _, revision := range []int64{0, -1, 9007199254740991} {
		o := testOverlay()
		o.Revision = revision + 1
		if err := store.CompareAndSwap(context.Background(), revision, o); !errors.Is(err, auth.Validation) {
			t.Fatal(err)
		}
	}
	o := testOverlay()
	o.Revision = 4
	if err := store.CompareAndSwap(context.Background(), 1, o); !errors.Is(err, auth.Validation) {
		t.Fatal(err)
	}
	o = testOverlay()
	o.Config.GuidePlacements[0].RotationY = math.NaN()
	if err := store.CompareAndSwap(context.Background(), 1, o); !errors.Is(err, auth.Validation) {
		t.Fatal(err)
	}
	o = testOverlay()
	o.Lamp.Yaw = math.NaN()
	if err := store.CompareAndSwap(context.Background(), 1, o); !errors.Is(err, auth.Validation) {
		t.Fatal(err)
	}
}
