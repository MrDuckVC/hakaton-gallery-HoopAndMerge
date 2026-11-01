// Package httpapi owns HTTP transport and wire responses.
package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Server owns the HTTP listener and admission state.
type Server struct {
	*http.Server
	mu           sync.Mutex
	listener     net.Listener
	stopping     atomic.Bool
	world        World
	guides       Guides
	photos       Photos
	photoLibrary PhotoLibrary
	websocket    http.Handler
	frontend     *Frontend
}

type errorResponse struct {
	Error errorBody `json:"error"`
}
type errorBody struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	RequestID    string `json:"request_id"`
	RetryAfterMS int64  `json:"retry_after_ms,omitempty"`
}

// NewServer constructs the transport; routes are introduced by their subsystem tasks.
func NewServer(addr string) *Server {
	server := &Server{}
	server.Server = &http.Server{
		Addr: addr, Handler: server.common(http.HandlerFunc(server.fallback)),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	return server
}

// ConfigureWebSocket connects the independently authenticated WS handler before serving.
func (s *Server) ConfigureWebSocket(handler http.Handler) { s.websocket = handler }

func (s *Server) fallback(w http.ResponseWriter, r *http.Request) {
	if s.websocket != nil && r.Method == http.MethodGet && r.URL.Path == "/api/v1/ws" {
		s.websocket.ServeHTTP(w, r)
		return
	}
	if s.servePhotoLibrary(w, r) {
		return
	}
	if s.servePhotos(w, r) {
		return
	}
	if s.serveGuides(w, r) {
		return
	}
	if s.serveWorld(w, r) {
		return
	}
	if s.frontend != nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
		s.frontend.ServeHTTP(w, r)
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "Ресурс не найден", 0)
}

func (s *Server) common(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", rand.Text())
		w.Header().Set("Cache-Control", "no-store")
		if s.stopping.Load() {
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Сервер завершает работу", 0)
			return
		}
		// The WS handler bounds its handshake and owns the upgraded connection lifetime.
		if s.websocket != nil && r.Method == http.MethodGet && r.URL.Path == "/api/v1/ws" {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Serve registers the listener before serving and rejects listeners after shutdown starts.
func (s *Server) Serve(listener net.Listener) error {
	s.mu.Lock()
	if s.stopping.Load() {
		s.mu.Unlock()
		_ = listener.Close()
		return http.ErrServerClosed
	}
	listener = &sharedCloseListener{Listener: listener}
	s.listener = listener
	s.mu.Unlock()
	return s.Server.Serve(listener)
}

// StopAccepting closes the listener, leaving existing requests available for draining.
func (s *Server) StopAccepting(_ context.Context) error {
	s.stopping.Store(true)
	s.SetKeepAlivesEnabled(false)
	s.mu.Lock()
	listener := s.listener
	s.mu.Unlock()
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	return nil
}

// sharedCloseListener gives admission shutdown and net/http the same close result.
// net/http wraps listeners itself, but StopAccepting must share that idempotency.
type sharedCloseListener struct {
	net.Listener
	once     sync.Once
	closeErr error
}

func (l *sharedCloseListener) Close() error {
	l.once.Do(func() { l.closeErr = l.Listener.Close() })
	return l.closeErr
}
