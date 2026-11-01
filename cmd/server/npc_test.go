package main

import (
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/world"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/npc"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func TestNPCPlansForAllC01Rooms(t *testing.T) {
	loaded, err := (world.Files{Dir: "../../content/world"}).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Input.Rooms) != 24 {
		t.Fatal("expected 24 C01 rooms")
	}
	for _, room := range loaded.Input.Rooms {
		plan, err := npc.Prepare(room)
		if err != nil {
			t.Fatalf("%s: %v", room.ID, err)
		}
		sim := npc.New(plan, 10)
		for step := range 100 {
			before := sim.States()
			sim.Tick(time.Unix(int64(step), 0), .2, nil)
			for i, n := range sim.States() {
				if n.State != "walking" || !worlddomain.Sweep(room, before[i].Position, n.Position) {
					t.Fatalf("%s: unsafe or idle visitor %+v", room.ID, n)
				}
			}
		}
	}
}
