package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/httpapi"
	wsapi "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/websocket"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/ollama"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
	ws "github.com/coder/websocket"
)

type guideCatalog struct{ socialWorld }

func (guideCatalog) HasGuide(id string) bool { return id == "guide" }
func (guideCatalog) Geometry(id string) (world.Room, error) {
	return world.Room{ID: id, Guides: []world.Guide{{ID: "guide", Position: player.Vector{X: 2, Z: -3}}}, Exhibits: []world.Exhibit{{ID: "inspected", Title: "Свет", Description: "Контраст", Position: player.Vector{X: 2, Y: 1, Z: -3}}}}, nil
}

func TestGuideWebSocketHistoryReconnectAndOutageIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	crypto := security.Crypto{}
	catalog := guideCatalog{}
	accounts := auth.New(NewAuthStore(pool), crypto, catalog, sessionevents.New(), time.Now)
	registry := game.New(time.Now)
	store := NewGuideStore(pool)
	finish := make(chan string)
	requests := make(chan string, 16)
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct{ Role, Content string }
		}
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			t.Error(e)
			return
		}
		var contents strings.Builder
		for _, m := range payload.Messages {
			contents.WriteString(m.Content)
			contents.WriteByte('\n')
		}
		requests <- contents.String()
		select {
		case <-r.Context().Done():
			return
		case text := <-finish:
			w.Header().Set("Content-Type", "application/x-ndjson")
			if e := json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": text}, "done": true}); e != nil {
				t.Error(e)
			}
		}
	}))
	defer modelServer.Close()
	model := ollama.New(modelServer.URL, "fixture:tag")
	service := guide.New(store, catalog, accounts, registry, model, guide.Clock{Now: time.Now, After: time.After}, crypto.NewID)
	service.ConfigureContext(catalog, store)
	if err := service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	registry.Configure(catalog, NewGameStore(pool), service)
	registry.ConfigureGuides(service)
	registry.ConfigureSocial(NewSocialStore(pool), game.UnavailablePhotos{}, crypto.NewID)
	transport := wsapi.New(registry, accounts, catalog, registry, []string{"http://allowed"})
	server := httpapi.NewServer("")
	server.ConfigureAuth(accounts, []string{"http://allowed"}, false)
	server.ConfigureGuides(service)
	server.ConfigureWebSocket(transport)
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); service.Run(runCtx) }()
	defer func() {
		stop()
		<-done
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = transport.Shutdown(shutdown)
	}()
	owner, err := accounts.Register(ctx, "guide_owner", "Owner", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := accounts.Register(ctx, "guide_stranger", "Stranger", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	read := func(conn *ws.Conn, want string) map[string]any {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_, body, e := conn.Read(bounded)
		if e != nil {
			t.Fatal(e)
		}
		var event map[string]any
		if json.Unmarshal(body, &event) != nil || event["type"] != want {
			t.Fatalf("want %s: %s", want, body)
		}
		return event
	}
	connect := func(token string) *ws.Conn {
		t.Helper()
		conn, _, e := ws.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=" + token}}})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		read(conn, "world.snapshot")
		return conn
	}
	send := func(conn *ws.Conn, kind, id, payload string) {
		t.Helper()
		if e := conn.Write(ctx, ws.MessageText, []byte(fmt.Sprintf(`{"type":%q,"request_id":%q,"payload":%s}`, kind, id, payload))); e != nil {
			t.Fatal(e)
		}
	}
	get := func(token, path string, status int) map[string]any {
		t.Helper()
		req, e := http.NewRequestWithContext(ctx, "GET", httpServer.URL+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		req.AddCookie(&http.Cookie{Name: "museum_session", Value: token})
		response, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = response.Body.Close() }()
		var result map[string]any
		if e = json.NewDecoder(response.Body).Decode(&result); e != nil || response.StatusCode != status {
			t.Fatal(response.StatusCode, result, e)
		}
		return result
	}
	askPayload := `{"guide_id":"guide","text":"question"}`
	a := connect(owner.Token)
	send(a, "world.ready", crypto.NewID(), `{"world_version":"v1","movement_epoch":1}`)
	read(a, "world.ready")
	send(a, "exhibit.inspect", crypto.NewID(), `{"exhibit_id":"inspected"}`)
	read(a, "exhibit.inspected")
	request := strings.ToUpper(crypto.NewID())
	send(a, "guide.ask", request, askPayload)
	accepted := read(a, "guide.accepted")
	if accepted["request_id"] != request {
		t.Fatal(accepted)
	}
	read(a, "guide.started")
	firstContext := <-requests
	if !strings.Contains(firstContext, "EXHIBIT_INSPECTED") {
		t.Fatal("inspection absent from model context")
	}
	finish <- "answer"
	read(a, "guide.delta")
	completed := read(a, "guide.completed")
	if completed["request_id"] != request {
		t.Fatal(completed)
	}
	path := "/api/v1/guides/guide/generations/by-request/" + request
	restored := get(owner.Token, path, 200)
	if restored["status"] != "completed" {
		t.Fatal(restored)
	}
	get(stranger.Token, path, 404)
	send(a, "guide.ask", strings.ToLower(request), askPayload)
	replay := read(a, "guide.accepted")
	if replay["payload"].(map[string]any)["generation"].(map[string]any)["status"] != "completed" {
		t.Fatal(replay)
	}
	second := crypto.NewID()
	send(a, "guide.ask", second, askPayload)
	accepted = read(a, "guide.accepted")
	read(a, "guide.started")
	nextContext := <-requests
	if !strings.Contains(nextContext, "answer") {
		t.Fatal("completed response absent from next model context")
	}
	id := accepted["payload"].(map[string]any)["generation"].(map[string]any)["id"].(string)
	cancelID := crypto.NewID()
	send(a, "guide.cancel", cancelID, fmt.Sprintf(`{"generation_id":%q}`, id))
	failed := read(a, "guide.failed")
	cancelled := read(a, "guide.cancelled")
	if failed["request_id"] != second || cancelled["request_id"] != cancelID {
		t.Fatal(failed, cancelled)
	}
	// Drop the connection without consuming accepted/completed; recover the saved question.
	third := crypto.NewID()
	send(a, "guide.ask", third, askPayload)
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, e := store.ByRequest(ctx, owner.User.ID, third)
		if e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(time.Millisecond)
	}
	_ = a.CloseNow()
	for {
		g, e := store.ByRequest(ctx, owner.User.ID, third)
		if e == nil && g.Terminal() {
			if g.Error.Code != "CANCELLED" {
				t.Fatal(g)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disconnect did not cancel", e)
		}
		time.Sleep(time.Millisecond)
	}
	a = connect(owner.Token)
	send(a, "guide.ask", third, askPayload)
	read(a, "guide.accepted") // Replay before ready.
	get(owner.Token, "/api/v1/guides/guide/generations?limit=2", 200)
	// Use another account for a fresh rate window, then reject memory at completion.
	b := connect(stranger.Token)
	read(a, "player.joined")
	send(b, "world.ready", crypto.NewID(), `{"world_version":"v1","movement_epoch":1}`)
	read(b, "world.ready")
	if _, err = pool.Exec(ctx, `ALTER TABLE guide_memories ADD CONSTRAINT reject_completion CHECK(summary='never') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	outageID := crypto.NewID()
	send(b, "guide.ask", outageID, askPayload)
	read(b, "guide.accepted")
	read(b, "guide.started")
	finish <- "unsaved answer"
	read(b, "guide.delta")
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	_, _, err = b.Read(bounded)
	cancel()
	if ws.CloseStatus(err) != 1011 {
		t.Fatal("completion outage did not close 1011", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		g, e := store.ByRequest(ctx, stranger.User.ID, outageID)
		if e == nil && g.Terminal() {
			if g.Error.Code != "SERVER_RESTARTED" || g.AssistantMessage != nil {
				t.Fatal(g)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("outage recovery", e)
		}
		time.Sleep(time.Millisecond)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE guide_memories DROP CONSTRAINT reject_completion`); err != nil {
		t.Fatal(err)
	}
	if err = accounts.Logout(ctx, owner.Token); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func (guideCatalog) Profile(string) (domain.Profile, error) {
	return domain.Profile{Personality: "Гид", Style: "Объяснение", Topic: "Живопись", Constraints: []string{"Без выдуманных цитат"}}, nil
}

func (guideCatalog) Exhibit(id string) (world.Exhibit, error) {
	return world.Exhibit{ID: id, Title: "Свет", Description: "Контраст"}, nil
}
