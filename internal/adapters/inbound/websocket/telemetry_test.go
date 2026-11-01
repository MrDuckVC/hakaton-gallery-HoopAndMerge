package websocket

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	ws "github.com/coder/websocket"
)

type telemetrySessions struct {
	*fakeSessions
	delay        atomic.Bool
	afterReceive func()
}

func (s *telemetrySessions) Authenticate(ctx context.Context, token string) (domain.Session, domain.User, error) {
	if s.delay.Swap(false) {
		s.afterReceive()
	}
	return s.fakeSessions.Authenticate(ctx, token)
}

func TestTelemetryRTTExcludesSessionAuthentication(t *testing.T) {
	server, tr, sessions := setup(t)
	start := time.Now()
	var elapsed atomic.Int64
	tr.service = game.New(func() time.Time { return start.Add(time.Duration(elapsed.Load())) })
	tr.handler = tr.service
	delayed := &telemetrySessions{fakeSessions: sessions, afterReceive: func() { elapsed.Add(int64(5 * time.Second)) }}
	tr.sessions = delayed
	c := dial(t, server, "a")
	readEvent(t, c)
	elapsed.Store(int64(5 * time.Second))
	tr.service.PublishTelemetry()
	probe := readEvent(t, c)
	nonce := probe["payload"].(map[string]any)["nonce"].(string)
	elapsed.Add(int64(12 * time.Millisecond))
	delayed.delay.Store(true)
	if err := c.Write(t.Context(), ws.MessageText, []byte(`{"type":"telemetry.pong","payload":{"nonce":"`+nonce+`"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(t.Context(), ws.MessageText, []byte(`{"type":"client.metrics","payload":{}}`)); err != nil {
		t.Fatal(err)
	}
	if event := readEvent(t, c); event["type"] != "error" {
		t.Fatal(event)
	}
	if got := tr.service.Clients()[0].PingMS; got == nil || *got != 12 {
		t.Fatal("RTT includes authentication", got)
	}
}

func TestDecodeTelemetry(t *testing.T) {
	for _, payload := range []string{`{"fps":0,"visible":true}`, `{"fps":1000,"visible":true}`, `{"fps":null,"visible":true}`, `{"fps":null,"visible":false}`} {
		c, f := decode([]byte(`{"type":"client.metrics","payload":` + payload + `}`))
		if f.Code != "" || c.Type != "client.metrics" || c.RequestID != "" {
			t.Fatal(payload, c, f)
		}
	}
	for _, body := range []string{
		`{"type":"client.metrics","payload":{"fps":-1,"visible":true}}`,
		`{"type":"client.metrics","payload":{"fps":1001,"visible":true}}`,
		`{"type":"client.metrics","payload":{"fps":1e999,"visible":true}}`,
		`{"type":"client.metrics","payload":{"fps":60,"visible":false}}`,
		`{"type":"client.metrics","payload":{"fps":"60","visible":true}}`,
		`{"type":"client.metrics","payload":{"visible":true}}`,
		`{"type":"client.metrics","payload":{"fps":60,"visible":null}}`,
		`{"type":"client.metrics","payload":{"fps":60,"visible":true,"extra":1}}`,
		`{"type":"client.metrics","request_id":"12345678-1234-4123-8123-123456789abc","payload":{"fps":60,"visible":true}}`,
		`{"type":"telemetry.pong","payload":{"nonce":"short"}}`,
		`{"type":"telemetry.pong","payload":{"nonce":"challenge_1234567890","ping_ms":1}}`,
		`{"type":"telemetry.pong","payload":{"nonce":"challenge_1234567890!"}}`,
		`{"type":"telemetry.pong","request_id":null,"payload":{"nonce":"challenge_1234567890"}}`,
	} {
		if _, f := decode([]byte(body)); f.Code != "VALIDATION_ERROR" {
			t.Fatal(body, f)
		}
	}
	if _, f := decode([]byte(`{"type":"telemetry.pong","payload":{"nonce":"challenge_1234567890"}}`)); f.Code != "" {
		t.Fatal(f)
	}
	encoded, err := encode(game.Event{Type: "telemetry.probe", Nonce: "challenge_1234567890"})
	if err != nil {
		t.Fatal(string(encoded), err)
	}
	var event map[string]any
	if err = json.Unmarshal(encoded, &event); err != nil {
		t.Fatal(err)
	}
	if event["type"] != "telemetry.probe" || event["request_id"] != nil || event["payload"].(map[string]any)["nonce"] != "challenge_1234567890" {
		t.Fatal(event)
	}
}

func TestTelemetryTransportBeforeReadyAndRateIsolation(t *testing.T) {
	server, tr, sessions := setup(t)
	var elapsed atomic.Int64
	start := time.Now()
	tr.service = game.New(func() time.Time { return start.Add(time.Duration(elapsed.Load())) })
	tr.handler = tr.service
	c := dial(t, server, "a")
	if event := readEvent(t, c); event["type"] != "world.snapshot" {
		t.Fatal(event)
	}
	send := func(body string) {
		t.Helper()
		if err := c.Write(t.Context(), ws.MessageText, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	barrier := func() {
		t.Helper()
		send(`{"type":"client.metrics","payload":{"fps":-1,"visible":true}}`)
		if event := readEvent(t, c); event["type"] != "error" || event["request_id"] != nil || event["payload"].(map[string]any)["code"] != "VALIDATION_ERROR" {
			t.Fatal(event)
		}
	}
	// Invalid telemetry never spends the ordinary command window.
	for range 31 {
		barrier()
	}
	send(`{"type":"world.ready","request_id":"12345678-1234-4123-8123-123456789abc","payload":{"world_version":"v1","movement_epoch":1}}`)
	if event := readEvent(t, c); event["type"] != "world.ready" {
		t.Fatal(event)
	}
	for _, fps := range []string{"60", "61", "62"} {
		send(`{"type":"client.metrics","payload":{"fps":` + fps + `,"visible":true}}`)
	}
	barrier()
	if got := tr.service.Clients()[0]; got.FPS == nil || *got.FPS != 61 {
		t.Fatal("excess/invalid sample changed metrics", got)
	}
	elapsed.Store(int64(5 * time.Second))
	tr.service.PublishTelemetry()
	probe := readEvent(t, c)
	if probe["type"] != "telemetry.probe" {
		t.Fatal(probe)
	}
	nonce := probe["payload"].(map[string]any)["nonce"].(string)
	elapsed.Store(int64(5*time.Second + 12*time.Millisecond))
	send(`{"type":"telemetry.pong","payload":{"nonce":"` + nonce + `"}}`)
	barrier()
	if got := tr.service.Clients()[0].PingMS; got == nil || *got != 12 {
		t.Fatal(got)
	}
	// Replacement is not ready and must still accept metrics immediately.
	replacement := dial(t, server, "a")
	readEvent(t, replacement)
	if err := replacement.Write(t.Context(), ws.MessageText, []byte(`{"type":"client.metrics","payload":{"fps":null,"visible":false}}`)); err != nil {
		t.Fatal(err)
	}
	if err := replacement.Write(t.Context(), ws.MessageText, []byte(`{"type":"client.metrics","payload":{}}`)); err != nil {
		t.Fatal(err)
	}
	if event := readEvent(t, replacement); event["type"] != "error" {
		t.Fatal(event)
	}
	if got := tr.service.Clients()[0]; got.SampledAt == nil || got.Stale || got.Visible || got.PingMS != nil {
		t.Fatal(got)
	}
	// Existing session lifecycle still closes a socket using telemetry.
	sessions.hub.Revoke("a")
	closeCode(t, replacement, 4001)
}
