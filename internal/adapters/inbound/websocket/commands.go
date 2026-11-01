// Package websocket implements the v1 WebSocket transport without owning game rules.
package websocket

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
)

func decode(body []byte) (game.Command, game.Failure) {
	c := game.Command{}
	fail := func(code string) (game.Command, game.Failure) { return c, game.Failure{Code: code} }
	if !utf8.Valid(body) || !validUnicodeEscapes(body) || !json.Valid(body) {
		return fail("INVALID_JSON")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	value, err := uniqueValue(d)
	if err != nil {
		return fail("VALIDATION_ERROR")
	}
	root, ok := value.(map[string]any)
	if !ok {
		return fail("VALIDATION_ERROR")
	}
	c.Type, _ = root["type"].(string)
	if id, valid := root["request_id"].(string); valid && uuid(id) {
		c.RequestID = id
	}
	switch c.Type {
	case "player.move":
		c.RequestID = ""
	case "world.ready", "world.enter", "chat.send", "exhibit.inspect", "guide.ask", "guide.cancel", "room.sync":
	default:
		if c.Type == "" {
			return fail("VALIDATION_ERROR")
		}
		return fail("UNKNOWN_EVENT")
	}
	if !keys(root, []string{"type", "payload"}, "request_id") {
		return fail("VALIDATION_ERROR")
	}
	if c.Type == "player.move" {
		if _, exists := root["request_id"]; exists {
			return fail("VALIDATION_ERROR")
		}
	} else if c.RequestID == "" {
		return fail("VALIDATION_ERROR")
	}
	p, ok := root["payload"].(map[string]any)
	if !ok {
		return fail("VALIDATION_ERROR")
	}
	valid := false
	switch c.Type {
	case "room.sync":
		valid = keys(p, nil)
	case "world.ready":
		c.WorldVersion, _ = p["world_version"].(string)
		c.MovementEpoch = integer(p["movement_epoch"])
		valid = keys(p, []string{"world_version", "movement_epoch"}) && world.ValidID(c.WorldVersion) && c.MovementEpoch > 0
	case "world.enter":
		c.DoorID, _ = p["door_id"].(string)
		c.MovementEpoch = integer(p["movement_epoch"])
		valid = keys(p, []string{"door_id", "movement_epoch"}) && world.ValidID(c.DoorID) && c.MovementEpoch > 0
	case "player.move":
		c.MovementEpoch = integer(p["movement_epoch"])
		c.Seq = integer(p["seq"])
		pos, pok := p["position"].(map[string]any)
		rot, rok := p["rotation"].(map[string]any)
		valid = keys(p, []string{"movement_epoch", "seq", "position", "rotation"}) && c.MovementEpoch > 0 && c.Seq > 0 && pok && rok && keys(pos, []string{"x", "y", "z"}) && keys(rot, []string{"yaw", "pitch"})
		if valid {
			var x, y, z, yaw, pitch bool
			c.Position.X, x = number(pos["x"])
			c.Position.Y, y = number(pos["y"])
			c.Position.Z, z = number(pos["z"])
			c.Rotation.Yaw, yaw = number(rot["yaw"])
			c.Rotation.Pitch, pitch = number(rot["pitch"])
			valid = x && y && z && yaw && pitch && c.Position.Y == 0 && c.Rotation.Yaw >= -math.Pi && c.Rotation.Yaw < math.Pi && math.Abs(c.Rotation.Pitch) <= math.Pi/2
		}
	case "chat.send":
		c.Text, ok = text(p, "text", 500, false)
		c.PhotoID, valid = optionalID(p, "photo_id")
		valid = keys(p, nil, "text", "photo_id") && ok && valid && (c.Text != "" || c.PhotoID != "")
	case "exhibit.inspect":
		c.ExhibitID, _ = p["exhibit_id"].(string)
		valid = keys(p, []string{"exhibit_id"}) && world.ValidID(c.ExhibitID)
	case "guide.ask":
		c.GuideID, _ = p["guide_id"].(string)
		c.Text, ok = text(p, "text", 2000, true)
		c.ExhibitID, valid = optionalID(p, "exhibit_id")
		valid = keys(p, []string{"guide_id", "text"}, "exhibit_id") && world.ValidID(c.GuideID) && ok && valid
	case "guide.cancel":
		c.GenerationID, _ = p["generation_id"].(string)
		valid = keys(p, []string{"generation_id"}) && world.ValidID(c.GenerationID)
	}
	if !valid {
		return fail("VALIDATION_ERROR")
	}
	return c, game.Failure{}
}
func keys(m map[string]any, required []string, optional ...string) bool {
	for _, k := range required {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	for k := range m {
		found := false
		for _, r := range required {
			if k == r {
				found = true
			}
		}
		for _, r := range optional {
			if k == r {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
}
func integer(v any) int64 {
	f, ok := number(v)
	if !ok || f < 1 || f > 9007199254740991 || math.Trunc(f) != f {
		return 0
	}
	n, _ := v.(json.Number)
	exact, valid := new(big.Rat).SetString(string(n))
	if !valid || !exact.IsInt() || !exact.Num().IsInt64() {
		return 0
	}
	value := exact.Num().Int64()
	if value < 1 || value > 9007199254740991 {
		return 0
	}
	return value
}
func text(m map[string]any, key string, maxLen int, required bool) (string, bool) {
	v, exists := m[key]
	if !exists {
		return "", !required
	}
	s, ok := v.(string)
	s = strings.TrimSpace(s)
	return s, ok && utf8.RuneCountInString(s) <= maxLen && (!required || s != "")
}
func optionalID(m map[string]any, key string) (string, bool) {
	v, exists := m[key]
	if !exists {
		return "", true
	}
	s, ok := v.(string)
	return s, ok && world.ValidID(s)
}
func uuid(s string) bool {
	if len(s) != 36 || s[14] != '4' || !strings.ContainsRune("89abAB", rune(s[19])) {
		return false
	}
	for i, b := range []byte(s) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if b != '-' {
				return false
			}
		} else if (b < '0' || b > '9') && (b < 'a' || b > 'f') && (b < 'A' || b > 'F') {
			return false
		}
	}
	return true
}

// uniqueValue rejects duplicate object members, including nested payload fields.
func uniqueValue(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	if delim == '{' {
		m := make(map[string]any)
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return nil, e
			}
			key, _ := k.(string)
			if _, exists := m[key]; exists {
				return nil, io.ErrUnexpectedEOF
			}
			v, e := uniqueValue(d)
			if e != nil {
				return nil, e
			}
			m[key] = v
		}
		_, err = d.Token()
		return m, err
	}
	if delim == '[' {
		a := make([]any, 0)
		for d.More() {
			v, e := uniqueValue(d)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		_, err = d.Token()
		return a, err
	}
	return nil, io.ErrUnexpectedEOF
}

// validUnicodeEscapes prevents encoding/json's replacement of lone UTF-16 surrogates.
// Valid pairs and escaped backslashes retain their original password meaning.
func validUnicodeEscapes(body []byte) bool {
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		i++
		if i >= len(body) {
			return false
		}
		if body[i] != 'u' {
			continue
		}
		if i+4 >= len(body) {
			return false
		}
		point, err := strconv.ParseUint(string(body[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if point >= 0xdc00 && point <= 0xdfff {
			return false
		}
		if point < 0xd800 || point > 0xdbff {
			continue
		}
		if i+6 >= len(body) || body[i+1] != '\\' || body[i+2] != 'u' {
			return false
		}
		low, parseErr := strconv.ParseUint(string(body[i+3:i+7]), 16, 16)
		if parseErr != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}

// rejectedCommand retains readable IDs only for recognized ordinary commands.
// Invalid moves and guide asks still consume the aggregate error window, without
// acquiring an ordinary request ID or bypassing future durable guide idempotency.
func rejectedCommand(c game.Command) game.Command {
	switch c.Type {
	case "world.ready", "world.enter", "chat.send", "exhibit.inspect", "guide.cancel", "room.sync":
		return game.Command{Type: c.Type, RequestID: c.RequestID}
	default:
		return game.Command{}
	}
}
