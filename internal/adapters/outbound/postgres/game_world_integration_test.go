package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/inbound/httpapi"
	wsapi "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/inbound/websocket"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/inbound/worldwire"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/sessionevents"
	worldfiles "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/world"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/testworld"
	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/game"
	world "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/world"
	ws "github.com/coder/websocket"
)

// This scenario exercises actual wire messages and SQL across a fresh game service.
func TestGameWorldPersistenceAndTransitionIntegration(t *testing.T) {
	dir := t.TempDir()
	if err := testworld.Write(t.Context(), dir, "v1"); err != nil {
		t.Fatal(err)
	}
	catalog, err := world.Load(t.Context(), worldfiles.Files{Dir: dir}, worldwire.SnapshotSizer{}, "v1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, dsn, pool := testDatabase(t)
	if _, err = Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	sessions := auth.New(NewAuthStore(pool), security.Crypto{}, catalog, sessionevents.New(), time.Now)
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	start := func() (*httptest.Server, *game.Service, *wsapi.Transport) {
		connections := game.New(func() time.Time { return time.Unix(0, clock.Load()) })
		connections.Configure(catalog, NewGameStore(pool), game.NoQuestions{})
		transport := wsapi.New(connections, sessions, catalog, connections, []string{"http://allowed"})
		api := httpapi.NewServer(":0")
		api.ConfigureAuth(sessions, []string{"http://allowed"}, false)
		api.ConfigureWorld(world.New(catalog, sessions, connections))
		api.ConfigureWebSocket(transport)
		server := httptest.NewServer(api.Handler)
		t.Cleanup(server.Close)
		t.Cleanup(func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if shutdownErr := transport.Shutdown(shutdownCtx); shutdownErr != nil {
				t.Error(shutdownErr)
			}
		})
		return server, connections, transport
	}
	request := func(server *httptest.Server, method, path, body string, cookie *http.Cookie, status int) *http.Response {
		t.Helper()
		req, reqErr := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		req.Header.Set("Origin", "http://allowed")
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, reqErr := server.Client().Do(req)
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		if response.StatusCode != status {
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			t.Fatalf("%s: status=%d want=%d body=%s", path, response.StatusCode, status, body)
		}
		return response
	}
	read := func(conn *ws.Conn, want string, payload any) {
		t.Helper()
		_, body, readErr := conn.Read(ctx)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var event struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if decodeErr := json.Unmarshal(body, &event); decodeErr != nil || event.Type != want {
			t.Fatalf("event want %s: %s (%v)", want, body, decodeErr)
		}
		if payload != nil {
			if decodeErr := json.Unmarshal(event.Payload, payload); decodeErr != nil {
				t.Fatal(decodeErr)
			}
		}
	}
	connect := func(server *httptest.Server, cookie *http.Cookie) (*ws.Conn, worldwire.SnapshotPayload) {
		t.Helper()
		conn, _, dialErr := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {cookie.String()}}})
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		var snapshot worldwire.SnapshotPayload
		read(conn, "world.snapshot", &snapshot)
		return conn, snapshot
	}
	sequence := 0
	command := func(conn *ws.Conn, kind, payload string) {
		t.Helper()
		sequence++
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", sequence)
		body := fmt.Sprintf(`{"type":%q,"request_id":%q,"payload":%s}`, kind, id, payload)
		if writeErr := conn.Write(ctx, ws.MessageText, []byte(body)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	ready := func(conn *ws.Conn, epoch int) {
		t.Helper()
		command(conn, "world.ready", fmt.Sprintf(`{"world_version":"v1","movement_epoch":%d}`, epoch))
		read(conn, "world.ready", nil)
	}
	move := func(conn *ws.Conn, epoch int, x float64) {
		t.Helper()
		clock.Add(int64(100 * time.Millisecond))
		body := fmt.Sprintf(`{"type":"player.move","payload":{"movement_epoch":%d,"seq":1,"position":{"x":%g,"y":0,"z":2},"rotation":{"yaw":0.5,"pitch":0}}}`, epoch, x)
		if writeErr := conn.Write(ctx, ws.MessageText, []byte(body)); writeErr != nil {
			t.Fatal(writeErr)
		}
		// A subsequent ready acknowledgement is an ordered barrier for the move.
		ready(conn, epoch)
	}
	firstServer, firstGame, firstTransport := start()
	response := request(firstServer, "POST", "/api/v1/auth/register", `{"username":"state_player","display_name":"State Player","password":"password123"}`, nil, 201)
	cookie := response.Cookies()[0]
	_ = response.Body.Close()
	first, snapshot := connect(firstServer, cookie)
	userID := snapshot.Self.UserID
	if snapshot.Self.RoomID != "r0" || snapshot.Self.Position.X != 2 || snapshot.Self.MovementEpoch != 1 || snapshot.Self.AckSeq != 0 {
		t.Fatalf("initial snapshot: %+v", snapshot.Self)
	}
	ready(first, 1)
	move(first, 1, 2.4)
	firstGame.PublishState()
	var state struct {
		Players []worldwire.Player `json:"players"`
	}
	read(first, "players.state", &state)
	if len(state.Players) != 1 || state.Players[0].Position.X != 2.4 || state.Players[0].AckSeq != 1 || state.Players[0].Rotation.Yaw != 0.5 {
		t.Fatalf("accepted movement: %+v", state.Players)
	}
	if err = firstGame.SavePositions(ctx); err != nil {
		t.Fatal(err)
	}
	if err = firstTransport.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	firstServer.Close()
	secondServer, secondGame, secondTransport := start()
	second, restored := connect(secondServer, cookie)
	if restored.Self.Position.X != 2.4 || restored.Self.Rotation.Yaw != 0.5 || restored.Self.MovementEpoch != 1 || restored.Self.AckSeq != 0 {
		t.Fatalf("restart did not restore durable pose: %+v", restored.Self)
	}
	ready(second, 1)
	command(second, "world.enter", `{"door_id":"d0","movement_epoch":1}`)
	var entered struct {
		Self        worldwire.Player      `json:"self"`
		CurrentRoom worldwire.RoomPackage `json:"current_room"`
	}
	read(second, "world.entered", &entered)
	if entered.Self.RoomID != "r1" || entered.CurrentRoom.RoomID != "r1" || entered.Self.MovementEpoch != 2 || entered.Self.AckSeq != 0 || entered.Self.Position.X != 2 {
		t.Fatalf("transition: %+v", entered)
	}
	secondGame.PublishState()
	read(second, "players.state", &state)
	if len(state.Players) != 1 || state.Players[0].RoomID != "r1" || state.Players[0].MovementEpoch != 2 {
		t.Fatalf("transition state: %+v", state.Players)
	}
	response = request(secondServer, "GET", "/api/v1/world/rooms/r1?version=v1", "", cookie, 200)
	_ = response.Body.Close()
	response = request(secondServer, "GET", "/api/v1/world/rooms/r0?version=v1", "", cookie, 403)
	_ = response.Body.Close()
	response = request(secondServer, "GET", "/api/v1/world/assets/a1?version=v1", "", cookie, 200)
	_ = response.Body.Close()
	ready(second, 2)
	move(second, 2, 2.3)
	response = request(secondServer, "POST", "/api/v1/auth/logout", "", cookie, 204)
	_ = response.Body.Close()
	_, _, err = second.Read(ctx)
	if ws.CloseStatus(err) != 4001 {
		t.Fatalf("logout close: %v", err)
	}
	// Shutdown waits for the transport's disconnect flush, without polling SQL.
	if err = secondTransport.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	final, err := NewGameStore(pool).PlayerState(ctx, userID)
	if err != nil || final.RoomID != "r1" || final.Position.X != 2.3 || final.Rotation.Yaw != 0.5 {
		t.Fatalf("logout final flush: %+v err=%v", final, err)
	}
}
