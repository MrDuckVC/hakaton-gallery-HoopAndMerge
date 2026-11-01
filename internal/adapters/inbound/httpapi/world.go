package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	usecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/world"
)

// World is the inbound boundary for authorized immutable content downloads.
type World interface {
	Manifest(context.Context, string) (domain.Manifest, error)
	Room(context.Context, string, string, string) (usecase.File, error)
	Asset(context.Context, string, string, string) (usecase.File, error)
}

// ConfigureWorld attaches content routes before serving, preserving auth and Origin handling.
func (s *Server) ConfigureWorld(service World) { s.world = service }

func (s *Server) serveWorld(w http.ResponseWriter, r *http.Request) bool {
	if s.world == nil || r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v1/world/manifest" {
		m, err := s.world.Manifest(r.Context(), sessionToken(r))
		if err != nil {
			applicationError(w, err)
			return true
		}
		writeJSON(w, 200, manifestDTO{Extensions: []string{"sunday_v1"}, WorldID: m.ID, Version: m.Version, Total: m.TotalBytes, Limit: m.CacheLimit, MaxRooms: 1, PhotoReserve: domain.PhotoReserve, Movement: movementDTO{SendRate: 10, StateRate: 10, MaxSpeed: 4, Radius: 0.3, Height: 1.8, Camera: 1.65}})
		return true
	}
	path := r.URL.EscapedPath()
	kind := ""
	segment := ""
	for _, candidate := range []string{"rooms", "assets"} {
		prefix := "/api/v1/world/" + candidate + "/"
		if strings.HasPrefix(path, prefix) {
			kind = candidate
			segment = strings.TrimPrefix(path, prefix)
			break
		}
	}
	if kind == "" {
		return false
	}
	id, err := url.PathUnescape(segment)
	if err != nil || strings.Contains(segment, "/") || !domain.ValidResourceID(id) {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректный ID", 0)
		return true
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	versions := query["version"]
	if err != nil || len(versions) != 1 || !domain.ValidID(versions[0]) {
		writeError(w, 400, "VALIDATION_ERROR", "Требуется одна версия мира", 0)
		return true
	}
	var file usecase.File
	if kind == "rooms" {
		file, err = s.world.Room(r.Context(), sessionToken(r), versions[0], id)
	} else {
		file, err = s.world.Asset(r.Context(), sessionToken(r), versions[0], id)
	}
	if err != nil {
		applicationError(w, err)
		return true
	}
	contentType := file.ContentType
	if contentType == "application/json" {
		contentType += "; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Bytes)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(file.Bytes)
	return true
}

type movementDTO struct {
	SendRate  int     `json:"send_rate_hz"`
	StateRate int     `json:"state_rate_hz"`
	MaxSpeed  int     `json:"max_speed_mps"`
	Radius    float64 `json:"player_radius_m"`
	Height    float64 `json:"player_height_m"`
	Camera    float64 `json:"camera_height_m"`
}
type manifestDTO struct {
	Extensions   []string    `json:"extensions"`
	WorldID      string      `json:"world_id"`
	Version      string      `json:"world_version"`
	Total        int64       `json:"total_content_bytes"`
	Limit        int64       `json:"client_cache_limit_bytes"`
	MaxRooms     int         `json:"max_loaded_rooms"`
	PhotoReserve int64       `json:"photo_reserve_bytes"`
	Movement     movementDTO `json:"movement"`
}
