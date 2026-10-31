// Package sessionevents delivers process-local session termination to game observers.
package sessionevents

import (
	"sync"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
)

type observer struct {
	events chan auth.EndReason
	timer  *time.Timer
}

// Hub tracks only active observers, never all historical sessions.
type Hub struct {
	mu        sync.Mutex
	observers map[string]map[*observer]struct{}
}

// New constructs an empty notification hub shared by authentication and game adapters.
func New() *Hub { return &Hub{observers: make(map[string]map[*observer]struct{})} }

// Observe registers synchronously and schedules expiration without extending its deadline.
func (h *Hub) Observe(session domain.Session) (<-chan auth.EndReason, func()) {
	item := &observer{events: make(chan auth.EndReason, 1)}
	h.mu.Lock()
	if h.observers[session.ID] == nil {
		h.observers[session.ID] = make(map[*observer]struct{})
	}
	h.observers[session.ID][item] = struct{}{}
	item.timer = time.AfterFunc(time.Until(session.ExpiresAt), func() { h.finish(session.ID, item, auth.Expired) })
	h.mu.Unlock()
	return item.events, func() { h.finish(session.ID, item, "") }
}

// Revoke broadcasts to this session alone after a successful database commit.
func (h *Hub) Revoke(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for item := range h.observers[id] {
		h.finishLocked(id, item, auth.Revoked)
	}
}
func (h *Hub) finish(id string, item *observer, reason auth.EndReason) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.finishLocked(id, item, reason)
}
func (h *Hub) finishLocked(id string, item *observer, reason auth.EndReason) {
	if _, exists := h.observers[id][item]; !exists {
		return
	}
	delete(h.observers[id], item)
	if len(h.observers[id]) == 0 {
		delete(h.observers, id)
	}
	item.timer.Stop()
	if reason != "" {
		item.events <- reason
	}
	close(item.events)
}
