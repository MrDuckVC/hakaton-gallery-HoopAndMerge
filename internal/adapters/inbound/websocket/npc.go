package websocket

import (
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/worldwire"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
)

type npcDTO struct {
	ID       string           `json:"npc_id"`
	Position worldwire.Vector `json:"position"`
	Yaw      float64          `json:"yaw"`
	State    string           `json:"state"`
	SpeedMPS float64          `json:"speed_mps"`
	Phase    float64          `json:"phase"`
}

type npcSnapshotDTO struct {
	WorldVersion string   `json:"world_version"`
	RoomID       string   `json:"room_id"`
	InstanceID   string   `json:"room_instance_id"`
	Seq          int64    `json:"seq"`
	NPCs         []npcDTO `json:"npcs"`
}

func publicNPCs(snapshot game.NPCSnapshot) npcSnapshotDTO {
	states := make([]npcDTO, len(snapshot.NPCs))
	for i, n := range snapshot.NPCs {
		states[i] = npcDTO{ID: n.ID, Position: worldwire.Vector{X: n.Position.X, Y: n.Position.Y, Z: n.Position.Z}, Yaw: n.Yaw, State: n.State, SpeedMPS: n.SpeedMPS, Phase: n.Phase}
	}
	return npcSnapshotDTO{WorldVersion: snapshot.WorldVersion, RoomID: snapshot.RoomID, InstanceID: snapshot.InstanceID, Seq: snapshot.Seq, NPCs: states}
}
