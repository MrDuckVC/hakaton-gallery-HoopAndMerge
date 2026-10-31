// Package httpapi owns HTTP transport and wire responses.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Server owns the HTTP listener and admission state.
type Server struct {
	*http.Server
	mu       sync.Mutex
	listener net.Listener
	stopping atomic.Bool
}

type errorResponse struct {
	Error errorBody `json:"error"`
}
type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// NewServer constructs the transport; routes are introduced by their subsystem tasks.
func NewServer(addr string) *Server {
	server := &Server{}
	server.Server = &http.Server{
		Addr: addr, Handler: http.HandlerFunc(server.fallback),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	return server
}

func (s *Server) fallback(w http.ResponseWriter, _ *http.Request) {
	id := rand.Text()
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	status, code, message := http.StatusNotFound, "NOT_FOUND", "Ресурс не найден"
	if s.stopping.Load() {
		status, code, message = http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Сервер завершает работу"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: errorBody{Code: code, Message: message, RequestID: id}})
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
