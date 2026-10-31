package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	ws "github.com/coder/websocket"
)

func TestShutdownDeadlineWithSilentClients(t *testing.T) {
	server, tr, _ := setup(t)
	for _, token := range []string{"a", "b"} {
		c := dial(t, server, token)
		readEvent(t, c)
	}
	// No client reads again, so neither will answer the server's Close frame.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := tr.Shutdown(ctx)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown: %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("shutdown exceeded deadline: %v", elapsed)
	}
	// Forced socket closure must also release workers, not leave a handshake behind.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
	defer cleanupCancel()
	if err = tr.Shutdown(cleanupCtx); err != nil {
		t.Fatalf("workers still running after abort: %v", err)
	}
}

func TestHandshakeErrorsUseContractJSON(t *testing.T) {
	server, _, _ := setup(t)
	base := http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=a"}, "Connection": {"keep-alive, Upgrade"}, "Upgrade": {"websocket"}, "Sec-WebSocket-Version": {"13"}, "Sec-WebSocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}}
	canonical := make(http.Header)
	for key, values := range base {
		for _, value := range values {
			canonical.Add(key, value)
		}
	}
	base = canonical
	for _, tc := range []struct {
		name, key string
		values    []string
	}{
		{"missing connection", "Connection", nil}, {"wrong connection", "Connection", []string{"keep-alive"}},
		{"missing upgrade", "Upgrade", nil}, {"wrong upgrade", "Upgrade", []string{"h2c"}},
		{"missing version", "Sec-WebSocket-Version", nil}, {"wrong version", "Sec-WebSocket-Version", []string{"12"}},
		{"missing key", "Sec-WebSocket-Key", nil}, {"invalid base64 key", "Sec-WebSocket-Key", []string{"!"}},
		{"short key", "Sec-WebSocket-Key", []string{"YQ=="}}, {"duplicate key", "Sec-WebSocket-Key", []string{"dGhlIHNhbXBsZSBub25jZQ==", "dGhlIHNhbXBsZSBub25jZQ=="}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v1/ws", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header = base.Clone()
			req.Header.Del(tc.key)
			for _, value := range tc.values {
				req.Header.Add(tc.key, value)
			}
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Body.Close() }()
			var body struct {
				Error struct {
					Code, Message string
					RequestID     string `json:"request_id"`
				}
			}
			if err = json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("HTTP %d %s: %v", response.StatusCode, response.Header.Get("Content-Type"), err)
			}
			if response.StatusCode != 400 || body.Error.Code != "VALIDATION_ERROR" || body.Error.Message == "" || body.Error.RequestID == "" || body.Error.RequestID != response.Header.Get("X-Request-ID") || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("HTTP %d headers=%v body=%+v", response.StatusCode, response.Header, body)
			}
			if tc.key == "Sec-WebSocket-Version" && response.Header.Get("Sec-WebSocket-Version") != "13" {
				t.Fatal("missing supported version header")
			}
			if tc.key == "Connection" || tc.key == "Upgrade" {
				if response.Header.Get("Upgrade") != "websocket" || response.Header.Get("Connection") != "Upgrade" {
					t.Fatal("missing upgrade headers")
				}
			}
		})
	}
	// Failed upgrades must release their reservations and permit the same account to connect.
	conn := dial(t, server, "a")
	readEvent(t, conn)
}

func TestInvalidPayloadRetainsOrdinaryRequestID(t *testing.T) {
	server, _, _ := setup(t)
	conn := dial(t, server, "a")
	readEvent(t, conn)
	const id = "12345678-1234-4123-8123-123456789abc"
	for i, payload := range []string{`{"world_version":"v1"}`, `{"world_version":"v1","movement_epoch":1}`} {
		body := `{"type":"world.ready","request_id":"` + id + `","payload":` + payload + `}`
		if err := conn.Write(t.Context(), ws.MessageText, []byte(body)); err != nil {
			t.Fatal(err)
		}
		event := readEvent(t, conn)
		want := "VALIDATION_ERROR"
		if i == 1 {
			want = "DUPLICATE_REQUEST"
		}
		if event["request_id"] != id || event["payload"].(map[string]any)["code"] != want {
			t.Fatal(event)
		}
	}
}

func TestRejectedSpecialCommandsDoNotRetainOrdinaryIDs(t *testing.T) {
	for _, kind := range []string{"guide.ask", "player.move", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			server, _, _ := setup(t)
			conn := dial(t, server, "a")
			readEvent(t, conn)
			const id = "12345678-1234-4123-8123-123456789abc"
			if err := conn.Write(t.Context(), ws.MessageText, []byte(`{"type":"`+kind+`","request_id":"`+id+`","payload":{}}`)); err != nil {
				t.Fatal(err)
			}
			event := readEvent(t, conn)
			if kind == "player.move" {
				if _, exists := event["request_id"]; exists {
					t.Fatal("move acquired request_id", event)
				}
			}
			if err := conn.Write(t.Context(), ws.MessageText, []byte(`{"type":"world.ready","request_id":"`+id+`","payload":{"world_version":"v1","movement_epoch":1}}`)); err != nil {
				t.Fatal(err)
			}
			event = readEvent(t, conn)
			if event["payload"].(map[string]any)["code"] != "GAME_NOT_READY" {
				t.Fatal(event)
			}
		})
	}
}
