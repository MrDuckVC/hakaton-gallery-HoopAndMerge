package websocket

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/guidewire"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/runtimewire"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/worldwire"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
)

type envelope struct {
	Type       string `json:"type"`
	EventID    string `json:"event_id"`
	ServerTime string `json:"server_time"`
	RequestID  string `json:"request_id,omitempty"`
	Payload    any    `json:"payload"`
}
type failureDTO struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	RetryAfterMS int64  `json:"retry_after_ms,omitempty"`
}

func publicPlayer(p game.Player) worldwire.Player {
	return worldwire.Player{UserID: p.User.ID, DisplayName: p.User.DisplayName, AvatarColor: p.User.AvatarColor, RoomID: p.State.RoomID, Position: worldwire.Vector{X: p.State.Position.X, Y: p.State.Position.Y, Z: p.State.Position.Z}, Rotation: worldwire.Rotation{Yaw: p.State.Rotation.Yaw, Pitch: p.State.Rotation.Pitch}, MovementEpoch: p.MovementEpoch, AckSeq: p.AckSeq}
}
func encode(e game.Event) ([]byte, error) {
	var payload any
	switch e.Type {
	case "npc.state":
		payload = publicNPCs(e.NPCs)
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		if len(encoded) > 24*1024 {
			return nil, errors.New("NPC snapshot exceeds 24 KiB")
		}
	case "telemetry.probe":
		payload = struct {
			Nonce string `json:"nonce"`
		}{e.Nonce}
	case "object.acted":
		payload = struct {
			Action  string           `json:"action"`
			Runtime runtimewire.Room `json:"runtime"`
		}{e.ObjectAction, runtimewire.Public(e.Runtime)}
	case "world.runtime":
		payload = runtimewire.Public(e.Runtime)
	case "guide.accepted", "guide.completed", "guide.failed", "guide.cancelled":
		payload = struct {
			Generation guidewire.Generation `json:"generation"`
		}{guidewire.Public(e.Guide.Generation)}
	case "guide.started":
		payload = struct {
			GenerationID string `json:"generation_id"`
		}{e.Guide.GenerationID}
	case "guide.delta":
		payload = struct {
			GenerationID string `json:"generation_id"`
			Index        int    `json:"index"`
			Text         string `json:"text"`
		}{e.Guide.GenerationID, e.Guide.Index, e.Guide.Text}

	case "chat.message":
		payload = publicChat(e.Chat)
	case "exhibit.inspected":
		payload = struct {
			ExhibitID   string `json:"exhibit_id"`
			InspectedAt string `json:"inspected_at"`
		}{e.ExhibitID, e.InspectedAt.UTC().Format(time.RFC3339Nano)}
	case "world.snapshot":
		s := e.Snapshot
		players := make([]worldwire.Player, len(s.Players))
		for i, p := range s.Players {
			players[i] = publicPlayer(p)
		}
		payload = worldwire.SnapshotPayload{ProtocolVersion: 1, WorldVersion: s.WorldVersion, Self: publicPlayer(s.Self), Players: players, CurrentRoom: worldwire.Package(s.CurrentRoom)}
	case "world.ready":
		payload = struct {
			MovementEpoch int64 `json:"movement_epoch"`
		}{e.MovementEpoch}
	case "world.entered":
		payload = struct {
			Self        worldwire.Player      `json:"self"`
			CurrentRoom worldwire.RoomPackage `json:"current_room"`
		}{publicPlayer(e.Player), worldwire.Package(e.CurrentRoom)}
	case "players.state":
		players := make([]worldwire.Player, len(e.Players))
		for i, p := range e.Players {
			players[i] = publicPlayer(p)
		}
		payload = struct {
			Players []worldwire.Player `json:"players"`
		}{players}
	case "player.joined":
		payload = struct {
			Player worldwire.Player `json:"player"`
		}{publicPlayer(e.Player)}
	case "player.left":
		payload = struct {
			UserID string `json:"user_id"`
		}{e.UserID}
	default:
		payload = failureDTO{Code: e.Failure.Code, Message: "Команда не выполнена", RetryAfterMS: e.Failure.RetryAfterMS}
	}
	return json.Marshal(envelope{Type: e.Type, EventID: rand.Text(), ServerTime: time.Now().UTC().Format(time.RFC3339Nano), RequestID: e.RequestID, Payload: payload})
}
