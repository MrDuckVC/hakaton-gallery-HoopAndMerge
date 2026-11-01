package world

import (
	"math"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
)

func testInput() Input {
	pose := player.State{WorldVersion: "v1", RoomID: "a", Position: player.Vector{X: 2, Z: 2}}
	a := Room{ID: "a", Version: "v1", Title: "A", Min: player.Vector{}, Max: player.Vector{X: 10, Y: 4, Z: 10}, Doors: []Door{{ID: "ab", Target: "b", Position: player.Vector{X: 3, Z: 3}, Radius: 1.5}}}
	b := a
	b.ID = "b"
	b.Doors = []Door{{ID: "ba", Target: "a", Position: player.Vector{X: 3, Z: 3}, Radius: 1.5}}
	target := pose
	target.RoomID = "b"
	return Input{ID: "world", Version: "v1", Start: pose, Rooms: []Room{a, b}, Spawns: map[string]player.State{"ab": target, "ba": pose}}
}
func TestCatalogValidation(t *testing.T) {
	if err := Validate(testInput()); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Input){
		"duplicate room":   func(i *Input) { i.Rooms[1].ID = "a" },
		"bad bounds":       func(i *Input) { i.Rooms[0].Max.X = 0 },
		"nonfinite":        func(i *Input) { i.Rooms[0].Max.X = math.Inf(1) },
		"missing spawn":    func(i *Input) { delete(i.Spawns, "ab") },
		"wrong spawn room": func(i *Input) { s := i.Spawns["ab"]; s.RoomID = "a"; i.Spawns["ab"] = s },
		"unsafe spawn":     func(i *Input) { s := i.Spawns["ab"]; s.Position.X = 0; i.Spawns["ab"] = s },
		"missing target":   func(i *Input) { i.Rooms[0].Doors[0].Target = "absent" },
		"no exit":          func(i *Input) { i.Rooms[1].Doors = nil },
		"duplicate door":   func(i *Input) { i.Rooms[1].Doors[0].ID = "ab" },
		"bad id":           func(i *Input) { i.ID = "мир" },
		"missing asset": func(i *Input) {
			i.Rooms[0].Exhibits = []Exhibit{{ID: "e", AssetID: "missing", Position: player.Vector{X: 2, Y: 2, Z: 2}, Width: 1, Height: 1, Radius: 3}}
		},
		"missing profile": func(i *Input) { i.Rooms[0].Guides = []Guide{{ID: "g", Position: player.Vector{X: 2, Z: 2}, Radius: 3}} },
		"bad surface": func(i *Input) {
			i.Rooms[0].Surfaces = []Surface{{ID: "s", Size: player.Vector{X: -1, Y: 1, Z: 1}, Color: "#ffffff"}}
		},
		"unreachable": func(i *Input) {
			r := i.Rooms[0]
			r.ID = "c"
			r.Doors = []Door{{ID: "ca", Target: "a", Position: player.Vector{X: 3, Z: 3}, Radius: 1.5}}
			i.Rooms = append(i.Rooms, r)
			i.Spawns["ca"] = i.Start
		},
	} {
		t.Run(name, func(t *testing.T) {
			i := testInput()
			change(&i)
			if Validate(i) == nil {
				t.Fatal("accepted invalid catalog")
			}
		})
	}
}
func TestValidPoseCylinder(t *testing.T) {
	room := testInput().Rooms[0]
	room.Surfaces = []Surface{{Position: player.Vector{X: 5, Y: -0.1, Z: 5}, Size: player.Vector{X: 10, Y: 0.2, Z: 10}, Collidable: true}, {Position: player.Vector{X: 5, Y: 1, Z: 5}, Size: player.Vector{X: 2, Y: 2, Z: 2}, Collidable: true}}
	for _, tc := range []struct {
		name string
		p    player.Vector
		rot  player.Rotation
		want bool
	}{
		{"floor", player.Vector{X: 2, Z: 2}, player.Rotation{}, true},
		{"wall penetration", player.Vector{X: 3.8, Z: 5}, player.Rotation{}, false},
		{"corner outside circle", player.Vector{X: 3.78, Z: 3.78}, player.Rotation{}, true},
		{"corner inside circle", player.Vector{X: 3.8, Z: 3.8}, player.Rotation{}, false},
		{"boundary", player.Vector{X: 0.29, Z: 2}, player.Rotation{}, false},
		{"air", player.Vector{X: 2, Y: 0.01, Z: 2}, player.Rotation{}, false},
		{"nan", player.Vector{X: math.NaN(), Z: 2}, player.Rotation{}, false},
		{"yaw", player.Vector{X: 2, Z: 2}, player.Rotation{Yaw: math.Pi}, false},
		{"pitch", player.Vector{X: 2, Z: 2}, player.Rotation{Pitch: 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidPose(room, player.State{Position: tc.p, Rotation: tc.rot}); got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func TestBudgetExactBoundaryAndSafeIntegers(t *testing.T) {
	p := RoomPackage{RoomID: "r", DescriptorBytes: 123, Assets: []AssetInfo{{ID: "a", Bytes: 456}}}
	total := 10 * (PhotoReserve + 123 + 456)
	if err := CheckBudget(total, p); err != nil {
		t.Fatal(err)
	}
	if CheckBudget(total-1, p) == nil {
		t.Fatal("one byte above budget accepted")
	}
	for _, bad := range []int64{-1, MaxSafeInteger + 1} {
		if CheckBudget(bad, p) == nil {
			t.Fatal("invalid total", bad)
		}
	}
	p.Assets[0].Bytes = MaxSafeInteger
	if CheckBudget(MaxSafeInteger, p) == nil {
		t.Fatal("overflow accepted")
	}
}

func TestCatalogRejectsUnservableResourceIDs(t *testing.T) {
	for _, id := range []string{".", "..", "a/../b"} {
		in := testInput()
		in.Rooms[0].ID = id
		in.Start.RoomID = id
		in.Rooms[1].Doors[0].Target = id
		s := in.Spawns["ba"]
		s.RoomID = id
		in.Spawns["ba"] = s
		if err := Validate(in); err == nil {
			t.Fatalf("unservable room ID %q accepted", id)
		}
	}
}
