package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/runtimewire"
	authdomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	usecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
)

// Runtime is the administrative room application boundary.
type Runtime interface {
	RequireAdmin(context.Context, string) error
	Rooms(context.Context) ([]usecase.Summary, error)
	Read(context.Context, string) (domain.Room, error)
	Update(context.Context, string, string, int64, domain.Config) (domain.Room, error)
}

// RuntimeSessions authenticates each request directly from persisted sessions.
type RuntimeSessions interface {
	Authenticate(context.Context, string) (authdomain.Session, authdomain.User, error)
}
type adminRoutes struct {
	service  Runtime
	sessions RuntimeSessions
	origins  map[string]bool
	limiter  *adminLimiter
}

// ConfigureRuntime connects room administration independently of game ownership.
func (s *Server) ConfigureRuntime(service Runtime, sessions RuntimeSessions, origins []string) {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[origin] = true
	}
	s.admin = &adminRoutes{service: service, sessions: sessions, origins: allowed, limiter: &adminLimiter{now: time.Now, attempts: make(map[string][]time.Time)}}
}
func (s *Server) serveAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.admin == nil || !strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
		return false
	}
	a := s.admin
	_, user, err := a.sessions.Authenticate(r.Context(), sessionToken(r))
	if err != nil {
		applicationError(w, err)
		return true
	}
	if err = a.service.RequireAdmin(r.Context(), user.ID); err != nil {
		applicationError(w, err)
		return true
	}
	if r.Method == http.MethodPut {
		origins := r.Header.Values("Origin")
		if len(origins) != 1 || origins[0] == "" || origins[0] == "null" || !a.origins[origins[0]] {
			writeError(w, 403, "FORBIDDEN", "Недопустимый Origin", 0)
			return true
		}
		if wait := a.limiter.allow(user.ID); wait > 0 {
			writeError(w, 429, "RATE_LIMITED", "Слишком много изменений", wait)
			return true
		}
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/clients":
		s.adminClients(w)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/me":
		writeJSON(w, 200, struct {
			UserID string `json:"user_id"`
			Role   string `json:"role"`
		}{UserID: user.ID, Role: "admin"})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/rooms":
		rooms, readErr := a.service.Rooms(r.Context())
		if readErr != nil {
			applicationError(w, readErr)
			return true
		}
		type summary struct {
			RoomID   string `json:"room_id"`
			Title    string `json:"title"`
			Revision int64  `json:"revision"`
		}
		list := make([]summary, len(rooms))
		for i, room := range rooms {
			list[i] = summary{RoomID: room.RoomID, Title: room.Title, Revision: room.Revision}
		}
		writeJSON(w, 200, struct {
			Rooms []summary `json:"rooms"`
		}{Rooms: list})
	case (r.Method == http.MethodGet || r.Method == http.MethodPut) && strings.HasPrefix(r.URL.EscapedPath(), "/api/v1/admin/rooms/"):
		segment := strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1/admin/rooms/")
		id, decodeErr := url.PathUnescape(segment)
		if decodeErr != nil || strings.Contains(segment, "/") || !world.ValidResourceID(id) {
			applicationError(w, authdomain.Validation)
			return true
		}
		var room domain.Room
		if r.Method == http.MethodGet {
			room, err = a.service.Read(r.Context(), id)
		} else {
			input, ok := readRoomUpdate(w, r)
			if !ok {
				return true
			}
			room, err = a.service.Update(r.Context(), id, input.version, input.revision, input.config)
		}
		if err != nil {
			applicationError(w, err)
			return true
		}
		writeJSON(w, 200, runtimewire.Public(room))
	default:
		writeError(w, 404, "NOT_FOUND", "Ресурс не найден", 0)
	}
	return true
}

type adminLimiter struct {
	mu          sync.Mutex
	now         func() time.Time
	attempts    map[string][]time.Time
	lastCleanup time.Time
}

func (l *adminLimiter) allow(userID string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-10 * time.Second)
	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= time.Minute {
		for id, attempts := range l.attempts {
			if len(attempts) == 0 || !attempts[len(attempts)-1].After(cutoff) {
				delete(l.attempts, id)
			}
		}
		l.lastCleanup = now
	}
	attempts := l.attempts[userID]
	first := 0
	for first < len(attempts) && !attempts[first].After(cutoff) {
		first++
	}
	attempts = attempts[first:]
	if len(attempts) >= 10 {
		l.attempts[userID] = attempts
		return attempts[0].Add(10 * time.Second).Sub(now)
	}
	l.attempts[userID] = append(attempts, now)
	return 0
}
