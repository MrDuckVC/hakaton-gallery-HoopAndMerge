package websocket

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	ws "github.com/coder/websocket"
)

// Transport owns network workers and implements the WS HTTP handler and bounded shutdown.
type Transport struct {
	service                   *game.Service
	sessions                  game.Sessions
	catalog                   game.Catalog
	handler                   game.Handler
	origins                   map[string]bool
	mu                        sync.Mutex
	peers                     map[*peer]struct{}
	stopping                  bool
	pingInterval, pongTimeout time.Duration
	makePeer                  func(*ws.Conn) *peer
}

// New connects application ports; origins are exact strings, never wildcard patterns.
func New(service *game.Service, sessions game.Sessions, catalog game.Catalog, handler game.Handler, origins []string) *Transport {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[origin] = true
	}
	return &Transport{service: service, sessions: sessions, catalog: catalog, handler: handler, origins: allowed, peers: make(map[*peer]struct{}), pingInterval: 20 * time.Second, pongTimeout: 10 * time.Second}
}

// ServeHTTP checks Origin, authentication and capacity before performing the upgrade.
func (t *Transport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origins := r.Header.Values("Origin")
	if len(origins) != 1 || origins[0] == "" || origins[0] == "null" || !t.origins[origins[0]] {
		httpError(w, 403, "FORBIDDEN")
		return
	}
	token := ""
	if cookie, err := r.Cookie("museum_session"); err == nil {
		token = cookie.Value
	}
	handshakeCtx, handshakeCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer handshakeCancel()
	identity, err := game.Prepare(handshakeCtx, t.sessions, t.catalog, token)
	if err != nil {
		httpApplicationError(w, err)
		return
	}
	ticket, err := t.service.Reserve(identity)
	if err != nil {
		httpApplicationError(w, err)
		return
	}
	defer t.service.Release(ticket)
	// Connection observers must outlive the HTTP/handshake deadline.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopHandshake := context.AfterFunc(handshakeCtx, cancel)
	defer stopHandshake()
	events, unobserve, err := t.sessions.Observe(ctx, identity.Session)
	if err != nil {
		httpApplicationError(w, err)
		return
	}
	defer unobserve()
	// Recheck after registering the session observer, before publishing ownership.
	if _, _, err = t.sessions.Authenticate(handshakeCtx, token); err != nil {
		httpApplicationError(w, err)
		return
	}
	// Exact Origin was checked above; coder's host-pattern rules are deliberately bypassed.
	handshake := &handshakeWriter{ResponseWriter: w}
	conn, err := ws.Accept(handshake, r, &ws.AcceptOptions{InsecureSkipVerify: true, CompressionMode: ws.CompressionDisabled})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	if !stopHandshake() || ctx.Err() != nil {
		return
	}
	conn.SetReadLimit(16 * 1024)
	p := newPeer(conn)
	if t.makePeer != nil {
		p = t.makePeer(conn)
	}
	p.abort = func() { cancel(); _ = handshake.network.Close() }
	t.mu.Lock()
	if t.stopping {
		t.mu.Unlock()
		_ = conn.Close(1012, "")
		return
	}
	t.peers[p] = struct{}{}
	t.mu.Unlock()
	defer func() { t.mu.Lock(); delete(t.peers, p); t.mu.Unlock(); close(p.done) }()
	if err = t.service.Attach(ticket, p); err != nil {
		_ = conn.Close(1012, "")
		return
	}
	defer t.service.Detach(identity.User.ID, ticket)
	var workers sync.WaitGroup
	workers.Go(func() { p.write(ctx) })
	workers.Go(func() { p.heartbeat(ctx, t.pingInterval, t.pongTimeout) })
	workers.Go(func() {
		select {
		case <-ctx.Done():
		case reason, ok := <-events:
			if ok && reason != "" {
				t.service.End(identity.User.ID, ticket, 4001)
			}
		}
	})
	workers.Go(func() {
		select {
		case <-ctx.Done():
		case code := <-p.closeCode:
			_ = conn.Close(ws.StatusCode(code), "")
			cancel()
		}
	})
	t.read(ctx, conn, p, token, identity.User.ID, ticket)
	cancel()
	_ = conn.CloseNow()
	workers.Wait()
}
func (t *Transport) read(ctx context.Context, conn *ws.Conn, p *peer, token, userID, id string) {
	for {
		kind, reader, err := conn.Reader(ctx)
		if err != nil {
			return
		}
		if kind != ws.MessageText {
			p.Close(1003)
			<-ctx.Done()
			return
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			return
		}
		if p.isClosing() || !t.service.Current(userID, id) {
			return
		}
		cmd, failure := decode(body)
		if failure.Code != "" {
			admission := t.service.Check(userID, id, rejectedCommand(cmd))
			if admission.Drop {
				continue
			}
			if admission.Code != "" {
				failure = admission
			}
			t.service.Send(userID, id, game.Event{Type: "error", RequestID: cmd.RequestID, Failure: failure})
			continue
		}
		failure = t.service.Check(userID, id, cmd)
		if failure.Drop {
			continue
		}
		if failure.Code != "" {
			t.service.Send(userID, id, game.Event{Type: "error", RequestID: cmd.RequestID, Failure: failure})
			continue
		}
		checkCtx, checkCancel := context.WithTimeout(ctx, 10*time.Second)
		_, _, err = t.sessions.Authenticate(checkCtx, token)
		checkCancel()
		if err != nil {
			if errors.Is(err, domain.Unauthenticated) {
				t.service.End(userID, id, 4001)
			} else {
				t.service.End(userID, id, 1011)
			}
			<-ctx.Done()
			return
		}
		if failure.Code == "" {
			if failure.Code == "" && !failure.Drop {
				commandCtx, commandCancel := context.WithTimeout(ctx, 10*time.Second)
				failure = t.handler.Handle(commandCtx, userID, id, cmd)
				commandCancel()
			}
		}
		if !failure.Drop && failure.Code != "" {
			t.service.Send(userID, id, game.Event{Type: "error", RequestID: cmd.RequestID, Failure: failure})
		}
	}
}

// Shutdown stops admission, closes hijacked sockets and waits within the lifecycle deadline.
func (t *Transport) Shutdown(ctx context.Context) error {
	_ = t.service.StopAccepting(ctx)
	t.mu.Lock()
	t.stopping = true
	peers := make([]*peer, 0, len(t.peers))
	for p := range t.peers {
		peers = append(peers, p)
		p.Close(1012)
	}
	t.mu.Unlock()
	for _, p := range peers {
		select {
		case <-p.done:
		case <-ctx.Done():
			for _, remaining := range peers {
				remaining.abort()
			}
			return ctx.Err()
		}
	}
	return nil
}

// CloseConnections implements lifecycle.Connections.
func (t *Transport) CloseConnections(ctx context.Context) error { return t.Shutdown(ctx) }
func httpApplicationError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.Unauthenticated) {
		httpError(w, 401, "UNAUTHENTICATED")
	} else {
		httpError(w, 503, "SERVICE_UNAVAILABLE")
	}
}
func httpError(w http.ResponseWriter, status int, code string) {
	id := w.Header().Get("X-Request-ID")
	if id == "" {
		id = rand.Text()
		w.Header().Set("X-Request-ID", id)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}{Error: struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}{Code: code, Message: "Подключение недоступно", RequestID: id}})
}
