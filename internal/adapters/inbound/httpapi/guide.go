package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/inbound/guidewire"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/guide"
	world "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

// Guides is the private history application boundary.
type Guides interface {
	History(context.Context, string, string, int, string) ([]domain.Generation, string, error)
	ByRequest(context.Context, string, string, string) (domain.Generation, error)
}

// ConfigureGuides installs history routes before serving.
func (s *Server) ConfigureGuides(g Guides) { s.guides = g }

func (s *Server) serveGuides(w http.ResponseWriter, r *http.Request) bool {
	if s.guides == nil || r.Method != http.MethodGet || !strings.HasPrefix(r.URL.EscapedPath(), "/api/v1/guides/") {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1/guides/"), "/")
	if len(segments) < 2 || segments[1] != "generations" {
		return false
	}
	guideID, err := url.PathUnescape(segments[0])
	if err != nil || !world.ValidID(guideID) {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректный ID", 0)
		return true
	}
	if len(segments) == 4 && segments[2] == "by-request" {
		request, decodeErr := url.PathUnescape(segments[3])
		if decodeErr != nil || !uuidV4(request) {
			writeError(w, 400, "VALIDATION_ERROR", "Некорректный UUID", 0)
			return true
		}
		g, e := s.guides.ByRequest(r.Context(), sessionToken(r), guideID, request)
		if e != nil {
			applicationError(w, e)
		} else {
			writeJSON(w, 200, guidewire.Public(g))
		}
		return true
	}
	if len(segments) != 2 {
		return false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["limit"]) > 1 || len(query["before"]) > 1 {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректная пагинация", 0)
		return true
	}
	limit := 20
	if values, ok := query["limit"]; ok {
		limit, err = strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > 50 {
			writeError(w, 400, "VALIDATION_ERROR", "Некорректный limit", 0)
			return true
		}
	}
	items, cursor, err := s.guides.History(r.Context(), sessionToken(r), guideID, limit, query.Get("before"))
	if err != nil {
		applicationError(w, err)
		return true
	}
	dtos := make([]guidewire.Generation, len(items))
	for i, g := range items {
		dtos[i] = guidewire.Public(g)
	}
	var next *string
	if cursor != "" {
		next = &cursor
	}
	writeJSON(w, 200, struct {
		Items []guidewire.Generation `json:"items"`
		Next  *string                `json:"next_before"`
	}{dtos, next})
	return true
}
func uuidV4(s string) bool {
	if len(s) != 36 || s[14] != '4' || !strings.ContainsRune("89abAB", rune(s[19])) {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
