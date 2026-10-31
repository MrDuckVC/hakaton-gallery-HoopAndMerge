// Package worldwire defines shared world wire DTOs and conservative snapshot sizing.
package worldwire

import (
	"encoding/json"
	"strings"

	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

// Asset is an exact byte reservation in a public room package.
type Asset struct {
	ID          string `json:"id"`
	Bytes       int64  `json:"bytes"`
	ContentType string `json:"content_type"`
}

// RoomPackage is shared by snapshots and room-entered events.
type RoomPackage struct {
	RoomID          string  `json:"room_id"`
	DescriptorBytes int64   `json:"descriptor_bytes"`
	Assets          []Asset `json:"assets"`
}

// Package converts the domain reservation to its public representation.
func Package(p domain.RoomPackage) RoomPackage {
	out := RoomPackage{RoomID: p.RoomID, DescriptorBytes: p.DescriptorBytes, Assets: make([]Asset, len(p.Assets))}
	for i, a := range p.Assets {
		out.Assets[i] = Asset{ID: a.ID, Bytes: a.Bytes, ContentType: a.ContentType}
	}
	return out
}

// Vector is a wire point in global coordinates.
type Vector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Rotation is a wire orientation in radians.
type Rotation struct {
	Yaw   float64 `json:"yaw"`
	Pitch float64 `json:"pitch"`
}

// Player is the complete public presence record used by v1 snapshots.
type Player struct {
	UserID        string   `json:"user_id"`
	DisplayName   string   `json:"display_name"`
	AvatarColor   string   `json:"avatar_color"`
	RoomID        string   `json:"room_id"`
	Position      Vector   `json:"position"`
	Rotation      Rotation `json:"rotation"`
	MovementEpoch int64    `json:"movement_epoch"`
	AckSeq        int64    `json:"ack_seq"`
}

// SnapshotPayload contains self and all other players.
type SnapshotPayload struct {
	ProtocolVersion int         `json:"protocol_version"`
	WorldVersion    string      `json:"world_version"`
	Self            Player      `json:"self"`
	Players         []Player    `json:"players"`
	CurrentRoom     RoomPackage `json:"current_room"`
}

// SnapshotEnvelope includes every possible envelope field in the size estimate.
type SnapshotEnvelope struct {
	Type       string          `json:"type"`
	EventID    string          `json:"event_id"`
	ServerTime string          `json:"server_time"`
	RequestID  string          `json:"request_id,omitempty"`
	Payload    SnapshotPayload `json:"payload"`
}

// SnapshotSizer bounds the encoded snapshot for all 32 players, including JSON escaping.
type SnapshotSizer struct{}

// MaxSnapshotBytes uses maximum-length strings, safe integers and a conservative
// allowance of 32 bytes per float (finite float64 JSON is shorter, including exponents).
func (SnapshotSizer) MaxSnapshotBytes(p domain.RoomPackage) (int64, error) {
	id := strings.Repeat("<", 128) // encoding/json escapes each ASCII character to six bytes.
	player := Player{UserID: id, RoomID: id, DisplayName: strings.Repeat("\x01", 32), AvatarColor: "#FFFFFF", MovementEpoch: domain.MaxSafeInteger, AckSeq: domain.MaxSafeInteger}
	payload := SnapshotPayload{ProtocolVersion: 1, WorldVersion: id, Self: player, Players: make([]Player, 31), CurrentRoom: Package(p)}
	for i := range payload.Players {
		payload.Players[i] = player
	}
	encoded, err := json.Marshal(SnapshotEnvelope{Type: "world.snapshot", EventID: id, ServerTime: "9999-12-31T23:59:59.999999999Z", RequestID: "ffffffff-ffff-4fff-bfff-ffffffffffff", Payload: payload})
	// All five numeric pose components above encode as 0 (one byte).
	return int64(len(encoded)) + 32*5*31, err
}
