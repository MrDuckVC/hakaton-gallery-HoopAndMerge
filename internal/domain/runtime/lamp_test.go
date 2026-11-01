package runtime

import (
	"errors"
	"math"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func TestLampActionsAndRejectedTransitions(t *testing.T) {
	r := world.Room{ID: "r", Version: "v", Max: player.Vector{X: 12, Y: 4, Z: 12}}
	o := Seed(r)
	actor := ObjectActor{Holder: Holder{UserID: "u", ConnectionID: "c"}, Position: player.Vector{X: 3, Z: 4}}
	act := ObjectAction{ObjectID: "lamp_01", Action: "take", ExpectedRevision: 1}
	held, err := ApplyObject(r, o, actor, act)
	if err != nil || held.Revision != 2 || held.Lamp.HolderUserID != "u" || held.Lamp.SlotID != "a" {
		t.Fatalf("take %+v %v", held, err)
	}
	for _, a := range []string{"take", "aim", "power"} {
		act.Action = a
		if _, err = ApplyObject(r, held, actor, act); !errors.Is(err, ObjectBusy) {
			t.Fatalf("held %s: %v", a, err)
		}
	}
	act.Action = "place"
	act.SlotID = "b"
	act.ExpectedRevision = 2
	if _, err = ApplyObject(r, held, actor, act); !errors.Is(err, OutOfRange) {
		t.Fatalf("far place: %v", err)
	}
	actor.Position.X = 9
	other := actor
	other.ConnectionID = "old"
	if _, err = ApplyObject(r, held, other, act); !errors.Is(err, ObjectBusy) {
		t.Fatalf("foreign place: %v", err)
	}
	placed, err := ApplyObject(r, held, actor, act)
	if err != nil || placed.Lamp.SlotID != "b" || placed.Lamp.HolderUserID != "" || placed.Revision != 3 {
		t.Fatalf("place %+v %v", placed, err)
	}
	act = ObjectAction{ObjectID: "lamp_01", Action: "power", On: true, ExpectedRevision: 3}
	powered, err := ApplyObject(r, placed, actor, act)
	if err != nil || powered.Revision != 4 {
		t.Fatalf("no-op: %+v %v", powered, err)
	}
	act.ExpectedRevision = 3
	if _, err = ApplyObject(r, powered, actor, act); !errors.Is(err, RevisionConflict) {
		t.Fatalf("CAS %v", err)
	}
	act = ObjectAction{ObjectID: "lamp_01", Action: "aim", ExpectedRevision: 4, Yaw: math.Pi}
	if _, err = ApplyObject(r, powered, actor, act); !errors.Is(err, auth.Validation) {
		t.Fatalf("angle %v", err)
	}
	act.Yaw = -math.Pi
	act.HeadPitch = math.Pi / 3
	aimed, err := ApplyObject(r, powered, actor, act)
	if err != nil || aimed.Lamp.Yaw != -math.Pi {
		t.Fatalf("aim %+v %v", aimed, err)
	}
	r.Surfaces = []world.Surface{{Collidable: true, Position: player.Vector{X: 8, Y: 1, Z: 4}, Size: player.Vector{X: .1, Y: 2, Z: 4}}}
	actor.Position.X = 7
	act.ExpectedRevision = 5
	if _, err = ApplyObject(r, aimed, actor, act); !errors.Is(err, OutOfRange) {
		t.Fatalf("wall: %v", err)
	}
}

func TestLampDistancePrecedesBusyState(t *testing.T) {
	r := world.Room{ID: "r", Version: "v"}
	o := Seed(r)
	o.Lamp.HolderUserID = "u"
	o.Lamp.HolderConnectionID = "c"
	o.Revision = 2
	actor := ObjectActor{Holder: Holder{UserID: "other", ConnectionID: "other"}, Position: player.Vector{X: 11, Z: 11}}
	if _, err := ApplyObject(r, o, actor, ObjectAction{ObjectID: "lamp_01", Action: "power", ExpectedRevision: 1}); !errors.Is(err, OutOfRange) {
		t.Fatalf("distant held lamp: %v", err)
	}
}
