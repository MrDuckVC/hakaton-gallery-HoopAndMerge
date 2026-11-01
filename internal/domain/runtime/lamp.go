package runtime

import (
	"math"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// OutOfRange covers both distance and obscured interaction targets.
const OutOfRange Code = "OUT_OF_RANGE"

// Holder identifies a connection's private durable ownership.
type Holder struct{ UserID, ConnectionID string }

// ObjectActor contains an already admitted player's authoritative pose.
type ObjectActor struct {
	Holder
	Position player.Vector
}

// ObjectAction is the transport-independent lamp command.
type ObjectAction struct {
	ObjectID, Action, SlotID string
	ExpectedRevision         int64
	Yaw, HeadPitch           float64
	On                       bool
}

// Owner returns the private holder identity used for conditional writes.
func (l Lamp) Owner() Holder { return Holder{l.HolderUserID, l.HolderConnectionID} }

// ApplyObject checks interaction rules and returns a detached next overlay.
func ApplyObject(r world.Room, o Overlay, actor ObjectActor, a ObjectAction) (Overlay, error) {
	if a.ObjectID != "lamp_01" {
		return Overlay{}, world.NotFound
	}
	if actor.UserID == "" || actor.ConnectionID == "" || a.ExpectedRevision < 1 || a.ExpectedRevision > world.MaxSafeInteger {
		return Overlay{}, auth.Validation
	}
	switch a.Action {
	case "take", "place", "aim", "power":
	default:
		return Overlay{}, auth.Validation
	}
	if a.Action == "aim" && (!finite(a.Yaw) || a.Yaw < -math.Pi || a.Yaw >= math.Pi || !finite(a.HeadPitch) || math.Abs(a.HeadPitch) > math.Pi/3) {
		return Overlay{}, auth.Validation
	}
	targetSlot := o.Lamp.SlotID
	if a.Action == "place" {
		targetSlot = a.SlotID
	}
	var target player.Vector
	found := false
	for _, slot := range o.Config.LampSlots {
		if slot.SlotID == targetSlot {
			target = slot.Position
			found = true
			break
		}
	}
	if !found {
		return Overlay{}, auth.Validation
	}
	camera := actor.Position
	camera.Y += 1.65
	if distance(actor.Position, target) > 3 || !world.Visible(r, camera, target) {
		return Overlay{}, OutOfRange
	}
	switch a.Action {
	case "take", "aim", "power":
		if o.Lamp.HolderUserID != "" {
			return Overlay{}, ObjectBusy
		}
	case "place":
		if o.Lamp.HolderUserID == "" || o.Lamp.Owner() != actor.Holder {
			return Overlay{}, ObjectBusy
		}
	default:
		return Overlay{}, auth.Validation
	}
	if a.ExpectedRevision != o.Revision {
		return Overlay{}, RevisionConflict
	}
	if o.Revision == world.MaxSafeInteger {
		return Overlay{}, auth.Unavailable
	}
	next := o.Clone()
	next.Revision++
	switch a.Action {
	case "take":
		next.Lamp.HolderUserID = actor.UserID
		next.Lamp.HolderConnectionID = actor.ConnectionID
	case "place":
		next.Lamp.SlotID = a.SlotID
		next.Lamp.HolderUserID = ""
		next.Lamp.HolderConnectionID = ""
	case "aim":
		next.Lamp.Yaw = a.Yaw
		next.Lamp.HeadPitch = a.HeadPitch
	case "power":
		next.Lamp.On = a.On
	}
	return next, nil
}
