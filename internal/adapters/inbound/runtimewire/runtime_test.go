package runtimewire

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func TestPublicRuntimePrivateFieldsAndExactSizer(t *testing.T) {
	state := domain.Room{Overlay: domain.Overlay{WorldVersion: "world", RoomID: "room", Revision: 2, Config: domain.Config{AccentColor: "#FFFFFF", Lighting: "warm", GuidePlacements: []domain.GuidePlacement{{GuideID: "guide", Position: player.Vector{X: 6, Z: 7.5}, RotationY: 1}}, LampSlots: []domain.LampSlot{{SlotID: "a", Position: player.Vector{X: 3, Z: 4}}, {SlotID: "b", Position: player.Vector{X: 9, Z: 4}}}, NPCCount: 10}, Lamp: domain.Lamp{ObjectID: "lamp_01", SlotID: "a", On: true, HolderConnectionID: "private-connection"}}, Guides: []world.Guide{{ID: "guide", DisplayName: "Леонардо", Subtitle: "Учебная реконструкция", Position: player.Vector{X: 6, Z: 7.5}, Rotation: 1}}}
	for _, holder := range []string{"", "user"} {
		state.Lamp.HolderUserID = holder
		result := Public(state)
		body, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		size, err := (Sizer{}).RuntimeBytes(state)
		if err != nil || size != len(body) {
			t.Fatal(size, len(body), err)
		}
		if strings.Contains(string(body), "private-connection") || strings.Contains(string(body), "connection") {
			t.Fatal(string(body))
		}
		if holder == "" {
			if result.Lamp.HolderUserID != nil || !strings.Contains(string(body), `"holder_user_id":null`) {
				t.Fatal(string(body))
			}
		} else if result.Lamp.HolderUserID == nil || *result.Lamp.HolderUserID != holder {
			t.Fatal(result.Lamp)
		}
		if len(result.Guides) != 1 || result.Guides[0].Position.X != 6 || result.Guides[0].RotationY != 1 || result.Config.GuidePlacements[0].GuideID != "guide" || result.Config.LampSlots[1].Position.X != 9 {
			t.Fatal(result)
		}
	}
	state.Lamp.Yaw = math.Inf(1)
	if _, err := (Sizer{}).RuntimeBytes(state); err == nil {
		t.Fatal("accepted non-finite public runtime")
	}
}
func TestPublicEmptyListsAreArrays(t *testing.T) {
	body, err := json.Marshal(Public(domain.Room{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"guides":[]`, `"guide_placements":[]`, `"lamp_slots":[]`} {
		if !strings.Contains(string(body), field) {
			t.Fatal(string(body))
		}
	}
}
