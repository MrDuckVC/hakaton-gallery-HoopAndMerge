package websocket

import (
	"math"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
)

func decodeObject(p map[string]any, c *game.Command) bool {
	a := &c.Object
	a.ObjectID, _ = p["object_id"].(string)
	a.Action, _ = p["action"].(string)
	a.ExpectedRevision = integer(p["expected_revision"])
	c.MovementEpoch = integer(p["movement_epoch"])
	if a.ObjectID != "lamp_01" || a.ExpectedRevision == 0 || c.MovementEpoch == 0 {
		return false
	}
	required := []string{"object_id", "action", "expected_revision", "movement_epoch"}
	switch a.Action {
	case "take":
		return keys(p, required)
	case "place":
		a.SlotID, _ = p["slot_id"].(string)
		return keys(p, append(required, "slot_id")) && (a.SlotID == "a" || a.SlotID == "b")
	case "aim":
		var yaw, pitch bool
		a.Yaw, yaw = number(p["yaw"])
		a.HeadPitch, pitch = number(p["head_pitch"])
		return keys(p, append(required, "yaw", "head_pitch")) && yaw && pitch && a.Yaw >= -math.Pi && a.Yaw < math.Pi && math.Abs(a.HeadPitch) <= math.Pi/3
	case "power":
		var ok bool
		a.On, ok = p["on"].(bool)
		return keys(p, append(required, "on")) && ok
	default:
		return false
	}
}
