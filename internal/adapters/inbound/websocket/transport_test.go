package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	ws "github.com/coder/websocket"
)

type fakeSessions struct {
	hub     *sessionevents.Hub
	expires time.Time
}

func (f *fakeSessions) Authenticate(_ context.Context, token string) (domain.Session, domain.User, error) {
	if token == "" {
		return domain.Session{}, domain.User{}, domain.Unauthenticated
	}
	return domain.Session{ID: token, UserID: token, ExpiresAt: f.expires}, domain.User{ID: token, DisplayName: token, AvatarColor: "#123456"}, nil
}
func (f *fakeSessions) Me(ctx context.Context, token string) (auth.MeResult, error) {
	_, u, e := f.Authenticate(ctx, token)
	return auth.MeResult{User: u, State: player.State{RoomID: "r", WorldVersion: "v1"}}, e
}
func (f *fakeSessions) Observe(_ context.Context, s domain.Session) (<-chan auth.EndReason, func(), error) {
	events, cancel := f.hub.Observe(s)
	return events, cancel, nil
}

type fakeCatalog struct{}

func (fakeCatalog) Package(id string) (world.RoomPackage, error) {
	return world.RoomPackage{RoomID: id}, nil
}
func setup(t *testing.T) (*httptest.Server, *Transport, *fakeSessions) {
	t.Helper()
	s := &fakeSessions{hub: sessionevents.New(), expires: time.Now().Add(time.Hour)}
	tr := New(game.New(time.Now), s, fakeCatalog{}, game.PendingHandler{}, []string{"http://allowed"})
	server := httptest.NewServer(tr)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tr.Shutdown(ctx)
		server.Close()
	})
	return server, tr, s
}
func dial(t *testing.T, server *httptest.Server, token string) *ws.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=" + token}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}
func readEvent(t *testing.T, c *ws.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, b, e := c.Read(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func closeCode(t *testing.T, c *ws.Conn, want ws.StatusCode) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if ws.CloseStatus(err) != want {
		t.Fatalf("close=%d want %d: %v", ws.CloseStatus(err), want, err)
	}
}
func TestTransportSnapshotPresenceReplacement(t *testing.T) {
	server, tr, _ := setup(t)
	a := dial(t, server, "a")
	if e := readEvent(t, a); e["type"] != "world.snapshot" {
		t.Fatal(e)
	}
	b := dial(t, server, "b")
	if e := readEvent(t, b); e["type"] != "world.snapshot" {
		t.Fatal(e)
	}
	readEvent(t, a)
	replacement := dial(t, server, "a")
	if e := readEvent(t, replacement); e["type"] != "world.snapshot" {
		t.Fatal(e)
	}
	if e := readEvent(t, b); e["type"] != "player.left" {
		t.Fatal(e)
	}
	if e := readEvent(t, b); e["type"] != "player.joined" {
		t.Fatal(e)
	}
	closeCode(t, a, 4009)
	if room, err := tr.service.OwningRoom(t.Context(), "a", "a"); err != nil || room != "r" {
		t.Fatal(room, err)
	}
}
func TestTransportRejectsBeforeUpgrade(t *testing.T) {
	server, _, _ := setup(t)
	for _, tc := range []struct {
		origin, token string
		status        int
	}{{"", "a", 403}, {"null", "a", 403}, {"http://evil", "a", 403}, {"http://allowed", "", 401}} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		_, resp, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &ws.DialOptions{HTTPHeader: http.Header{"Origin": {tc.origin}, "Cookie": {"museum_session=" + tc.token}}})
		cancel()
		if err == nil || resp == nil || resp.StatusCode != tc.status {
			t.Fatalf("%+v: %v %v", tc, resp, err)
		}
	}
}
func TestTransportSessionRevocation(t *testing.T) {
	server, _, sessions := setup(t)
	c := dial(t, server, "a")
	readEvent(t, c)
	sessions.hub.Revoke("a")
	closeCode(t, c, 4001)
}
func TestTransportFrameLimits(t *testing.T) {
	for _, tc := range []struct {
		kind ws.MessageType
		body string
		code ws.StatusCode
	}{{ws.MessageBinary, "abc", 1003}, {ws.MessageText, strings.Repeat("x", 16385), 1009}} {
		t.Run(tc.code.String(), func(t *testing.T) {
			server, _, _ := setup(t)
			c := dial(t, server, "a")
			readEvent(t, c)
			_ = c.Write(t.Context(), tc.kind, []byte(tc.body))
			closeCode(t, c, tc.code)
		})
	}
}
func TestTransportCommandErrors(t *testing.T) {
	server, _, _ := setup(t)
	c := dial(t, server, "a")
	readEvent(t, c)
	for _, tc := range []struct{ body, code string }{{`{`, "INVALID_JSON"}, {`{"type":"unknown","payload":{}}`, "UNKNOWN_EVENT"}, {`{"type":"world.ready","request_id":"12345678-1234-4123-8123-123456789abc","payload":{"world_version":"v1","movement_epoch":1}}`, "GAME_NOT_READY"}} {
		if err := c.Write(t.Context(), ws.MessageText, []byte(tc.body)); err != nil {
			t.Fatal(err)
		}
		e := readEvent(t, c)
		if e["type"] != "error" || e["payload"].(map[string]any)["code"] != tc.code {
			t.Fatal(e)
		}
	}
}

func TestTransportExpiry(t *testing.T) {
	sessions := &fakeSessions{hub: sessionevents.New(), expires: time.Now().Add(150 * time.Millisecond)}
	tr := New(game.New(time.Now), sessions, fakeCatalog{}, game.PendingHandler{}, []string{"http://allowed"})
	server := httptest.NewServer(tr)
	defer server.Close()
	c := dial(t, server, "a")
	readEvent(t, c)
	closeCode(t, c, 4001)
}
func TestTransportCapacityAndReplacementAtCapacity(t *testing.T) {
	server, tr, _ := setup(t)
	for i := range 32 {
		ticket, err := tr.service.Reserve(game.Identity{Session: domain.Session{ID: strconv.Itoa(i)}, User: domain.User{ID: strconv.Itoa(i)}, State: player.State{RoomID: "r", WorldVersion: "v1"}})
		if err != nil {
			t.Fatal(err)
		}
		sink := newPeer(nil)
		if err = tr.service.Attach(ticket, sink); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, resp, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=new"}}})
	if err == nil || resp == nil || resp.StatusCode != 503 {
		t.Fatal(resp, err)
	}
	c := dial(t, server, "0")
	if e := readEvent(t, c); e["type"] != "world.snapshot" {
		t.Fatal(e)
	}
}
func TestTransportHeartbeat(t *testing.T) {
	for _, respond := range []bool{true, false} {
		t.Run(strconv.FormatBool(respond), func(t *testing.T) {
			sessions := &fakeSessions{hub: sessionevents.New(), expires: time.Now().Add(time.Hour)}
			tr := New(game.New(time.Now), sessions, fakeCatalog{}, game.PendingHandler{}, []string{"http://allowed"})
			tr.pingInterval = 10 * time.Millisecond
			tr.pongTimeout = 100 * time.Millisecond
			server := httptest.NewServer(tr)
			defer server.Close()
			c := dial(t, server, "a")
			defer func() { _ = c.CloseNow() }()
			readEvent(t, c)
			if respond {
				reading := c.CloseRead(t.Context())
				select {
				case <-reading.Done():
					t.Fatal("connection ended despite automatic pong")
				case <-time.After(250 * time.Millisecond):
				}

			} else {
				// No Read means no automatic pong. The server must terminate without client activity.
				deadline := time.Now().Add(time.Second)
				for {
					tr.mu.Lock()
					n := len(tr.peers)
					tr.mu.Unlock()
					if n == 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("missing pong did not terminate connection")
					}
					time.Sleep(time.Millisecond)
				}
			}
		})
	}
}
func TestTransportShutdown(t *testing.T) {
	server, tr, _ := setup(t)
	c := dial(t, server, "a")
	readEvent(t, c)
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done <- tr.Shutdown(ctx)
	}()
	closeCode(t, c, 1012)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestQueueBudgetAndSlowConsumerIsolation(t *testing.T) {
	server, _, _ := setup(t)
	slow := dial(t, server, "slow")
	readEvent(t, slow)
	healthy := dial(t, server, "healthy")
	readEvent(t, healthy)
	readEvent(t, slow)
	// A peer without a running writer deterministically represents a stalled socket.
	blocked := newPeer(nil)
	for range 10000 {
		if !blocked.Enqueue(game.Event{Type: "error", Failure: game.Failure{Code: "SERVICE_UNAVAILABLE"}}) {
			break
		}
	}
	if !blocked.isClosing() {
		t.Fatal("unbounded outgoing queue")
	}
	if code := <-blocked.closeCode; code != 1013 {
		t.Fatal(code)
	}
	blocked.mu.Lock()
	size := blocked.bytes
	blocked.mu.Unlock()
	if size > maxQueueBytes {
		t.Fatal(size)
	}
	// Exercise the real connection's close path with the same backpressure outcome.
	survivor := dial(t, server, "survivor")
	readEvent(t, survivor)
	if e := readEvent(t, healthy); e["type"] != "player.joined" {
		t.Fatal(e)
	}
}

type stalledSocket struct {
	*ws.Conn
	writes atomic.Int32
}

func (s *stalledSocket) Write(ctx context.Context, kind ws.MessageType, b []byte) error {
	if s.writes.Add(1) == 1 {
		return s.Conn.Write(ctx, kind, b)
	}
	<-ctx.Done()
	return ctx.Err()
}
func TestSlowConsumerCloses1013WithoutBlockingOthers(t *testing.T) {
	sessions := &fakeSessions{hub: sessionevents.New(), expires: time.Now().Add(time.Hour)}
	tr := New(game.New(time.Now), sessions, fakeCatalog{}, game.PendingHandler{}, []string{"http://allowed"})
	captured := make(chan *peer, 1)
	var created atomic.Int32
	tr.makePeer = func(conn *ws.Conn) *peer {
		if created.Add(1) == 1 {
			p := newPeer(&stalledSocket{Conn: conn})
			captured <- p
			return p
		}
		return newPeer(conn)
	}
	server := httptest.NewServer(tr)
	defer server.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tr.Shutdown(ctx)
	}()
	slow := dial(t, server, "slow")
	readEvent(t, slow)
	p := <-captured
	healthy := dial(t, server, "healthy")
	readEvent(t, healthy)
	for range 10000 {
		if !p.Enqueue(game.Event{Type: "error", Failure: game.Failure{Code: "SERVICE_UNAVAILABLE"}}) {
			break
		}
	}
	closeCode(t, slow, 1013)
	if e := readEvent(t, healthy); e["type"] != "player.left" {
		t.Fatal(e)
	}
	survivor := dial(t, server, "survivor")
	readEvent(t, survivor)
	if e := readEvent(t, healthy); e["type"] != "player.joined" {
		t.Fatal(e)
	}
}

func TestMalformedCommandsAreRateLimitedAcrossReconnect(t *testing.T) {
	server, _, _ := setup(t)
	c := dial(t, server, "a")
	readEvent(t, c)
	for i := range 31 {
		if err := c.Write(t.Context(), ws.MessageText, []byte(`{"type":"unknown","payload":{}}`)); err != nil {
			t.Fatal(err)
		}
		e := readEvent(t, c)
		want := "UNKNOWN_EVENT"
		if i == 30 {
			want = "RATE_LIMITED"
		}
		if e["payload"].(map[string]any)["code"] != want {
			t.Fatal(e)
		}
	}
	next := dial(t, server, "a")
	readEvent(t, next)
	if err := next.Write(t.Context(), ws.MessageText, []byte(`{`)); err != nil {
		t.Fatal(err)
	}
	e := readEvent(t, next)
	if e["payload"].(map[string]any)["code"] != "RATE_LIMITED" {
		t.Fatal(e)
	}
}
