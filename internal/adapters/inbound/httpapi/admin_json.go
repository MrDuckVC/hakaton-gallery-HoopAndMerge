package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/big"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

type roomUpdateInput struct {
	version  string
	revision int64
	config   domain.Config
}

func readAdminObject(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		writeError(w, 415, "UNSUPPORTED_MEDIA_TYPE", "Требуется JSON UTF-8", 0)
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			writeError(w, 413, "PAYLOAD_TOO_LARGE", "Запрос слишком большой", 0)
		} else {
			writeError(w, 400, "INVALID_JSON", "Некорректный JSON", 0)
		}
		return nil, false
	}
	if !utf8.Valid(body) || !validUnicodeEscapes(body) || !json.Valid(body) {
		writeError(w, 400, "INVALID_JSON", "Некорректный JSON UTF-8", 0)
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	value, err := uniqueAdminValue(decoder)
	root, ok := value.(map[string]any)
	if err != nil || !ok {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректные поля", 0)
		return nil, false
	}
	return root, true
}
func readRoomUpdate(w http.ResponseWriter, r *http.Request) (roomUpdateInput, bool) {
	var input roomUpdateInput
	root, ok := readAdminObject(w, r)
	if !ok {
		return input, false
	}
	if !adminKeys(root, "world_version", "expected_revision", "config") {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректные поля", 0)
		return input, false
	}
	input.version, ok = root["world_version"].(string)
	if !ok || !world.ValidID(input.version) {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректная версия мира", 0)
		return input, false
	}
	input.revision, ok = adminInteger(root["expected_revision"], 1, world.MaxSafeInteger)
	config, cok := adminConfig(root["config"])
	if !ok || !cok {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректные поля", 0)
		return input, false
	}
	input.config = config
	return input, true
}
func uniqueAdminValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		if token == nil {
			return nil, errors.New("null field")
		}
		return token, nil
	}
	switch delim {
	case '{':
		value := make(map[string]any)
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return nil, keyErr
			}
			key, _ := keyToken.(string)
			if _, exists := value[key]; exists {
				return nil, errors.New("duplicate field")
			}
			field, fieldErr := uniqueAdminValue(decoder)
			if fieldErr != nil {
				return nil, fieldErr
			}
			value[key] = field
		}
		_, err = decoder.Token()
		return value, err
	case '[':
		value := make([]any, 0)
		for decoder.More() {
			item, itemErr := uniqueAdminValue(decoder)
			if itemErr != nil {
				return nil, itemErr
			}
			value = append(value, item)
		}
		_, err = decoder.Token()
		return value, err
	default:
		return nil, errors.New("unexpected delimiter")
	}
}
func adminKeys(m map[string]any, names ...string) bool {
	if len(m) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := m[name]; !ok {
			return false
		}
	}
	return true
}
func adminNumber(value any) (float64, bool) {
	n, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}
func adminInteger(value any, minValue, maxValue int64) (int64, bool) {
	f, valid := adminNumber(value)
	if !valid || f < float64(minValue) || f > float64(maxValue) || math.Trunc(f) != f {
		return 0, false
	}
	n, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	r, ok := new(big.Rat).SetString(string(n))
	if !ok || !r.IsInt() || !r.Num().IsInt64() {
		return 0, false
	}
	i := r.Num().Int64()
	return i, i >= minValue && i <= maxValue
}
func adminVector(value any) (player.Vector, bool) {
	m, ok := value.(map[string]any)
	if !ok || !adminKeys(m, "x", "y", "z") {
		return player.Vector{}, false
	}
	x, xok := adminNumber(m["x"])
	y, yok := adminNumber(m["y"])
	z, zok := adminNumber(m["z"])
	return player.Vector{X: x, Y: y, Z: z}, xok && yok && zok && y == 0
}
func adminConfig(value any) (domain.Config, bool) {
	var c domain.Config
	m, ok := value.(map[string]any)
	if !ok || !adminKeys(m, "accent_color", "lighting", "guide_placements", "lamp_slots", "npc_count", "installation_visible") {
		return c, false
	}
	c.AccentColor, ok = m["accent_color"].(string)
	if !ok {
		return c, false
	}
	c.Lighting, ok = m["lighting"].(string)
	if !ok {
		return c, false
	}
	c.InstallationVisible, ok = m["installation_visible"].(bool)
	if !ok {
		return c, false
	}
	count, ok := adminInteger(m["npc_count"], 0, 40)
	if !ok {
		return c, false
	}
	c.NPCCount = int(count)
	placements, ok := m["guide_placements"].([]any)
	if !ok {
		return c, false
	}
	c.GuidePlacements = make([]domain.GuidePlacement, len(placements))
	for i, value := range placements {
		placement, valid := value.(map[string]any)
		if !valid || !adminKeys(placement, "guide_id", "position", "rotation_y") {
			return c, false
		}
		id, iok := placement["guide_id"].(string)
		position, pok := adminVector(placement["position"])
		rotation, rok := adminNumber(placement["rotation_y"])
		if !iok || !world.ValidID(id) || !pok || !rok {
			return c, false
		}
		c.GuidePlacements[i] = domain.GuidePlacement{GuideID: id, Position: position, RotationY: rotation}
	}
	slots, ok := m["lamp_slots"].([]any)
	if !ok {
		return c, false
	}
	c.LampSlots = make([]domain.LampSlot, len(slots))
	for i, value := range slots {
		slot, valid := value.(map[string]any)
		if !valid || !adminKeys(slot, "slot_id", "position") {
			return c, false
		}
		id, iok := slot["slot_id"].(string)
		position, pok := adminVector(slot["position"])
		if !iok || !pok {
			return c, false
		}
		c.LampSlots[i] = domain.LampSlot{SlotID: id, Position: position}
	}
	return c, true
}
