package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	photocase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/photo"
)

// PhotoLibrary is the application boundary for albums and protected file reads.
type PhotoLibrary interface {
	Album(context.Context, string, int, string) ([]domain.Photo, string, error)
	Read(context.Context, string, string, bool) (photocase.File, error)
}

// ConfigurePhotoLibrary enables authenticated reads independently of the game connection.
func (s *Server) ConfigurePhotoLibrary(library PhotoLibrary) { s.photoLibrary = library }

func (s *Server) servePhotoLibrary(w http.ResponseWriter, r *http.Request) bool {
	if s.photoLibrary == nil || r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v1/photos" {
		s.photoAlbum(w, r)
		return true
	}
	path := r.URL.EscapedPath()
	if !strings.HasPrefix(path, "/api/v1/photos/") {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(path, "/api/v1/photos/"), "/")
	if len(segments) != 2 || (segments[1] != "image" && segments[1] != "thumbnail") {
		return false
	}
	id, err := url.PathUnescape(segments[0])
	if err != nil || !world.ValidID(id) {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректный ID", 0)
		return true
	}
	file, err := s.photoLibrary.Read(r.Context(), sessionToken(r), id, segments[1] == "thumbnail")
	if err != nil {
		applicationError(w, err)
		return true
	}
	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Bytes)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Bytes)
	return true
}
func (s *Server) photoAlbum(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["limit"]) > 1 || len(query["cursor"]) > 1 {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректная пагинация", 0)
		return
	}
	limit := 6
	if values, ok := query["limit"]; ok {
		limit, err = strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > 6 {
			writeError(w, 400, "VALIDATION_ERROR", "Некорректный limit", 0)
			return
		}
	}
	items, cursor, err := s.photoLibrary.Album(r.Context(), sessionToken(r), limit, query.Get("cursor"))
	if err != nil {
		applicationError(w, err)
		return
	}
	dtos := make([]photoDTO, len(items))
	for i, p := range items {
		dtos[i] = publicPhoto(p)
	}
	var next *string
	if cursor != "" {
		next = &cursor
	}
	writeJSON(w, 200, struct {
		Items []photoDTO `json:"items"`
		Next  *string    `json:"next_cursor"`
	}{dtos, next})
}
