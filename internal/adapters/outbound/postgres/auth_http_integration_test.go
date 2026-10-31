package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/httpapi"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/world"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
)

// fixtureWorld is exclusively a test catalog; production must never use its coordinates.
type fixtureWorld struct{}

func (fixtureWorld) Start(context.Context) (player.State, error) {
	return player.State{WorldVersion: "v1", RoomID: "safe", Position: player.Vector{X: 2, Z: -3}}, nil
}
func (fixtureWorld) Valid(_ context.Context, state player.State) (bool, error) {
	return state.WorldVersion == "v1" && state.RoomID == "safe" && state.Position.X == 2 && state.Position.Z == -3, nil
}

func TestAuthHTTPPostgresIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewAuthStore(pool)
	crypto := security.Crypto{}
	events := sessionevents.New()
	service := auth.New(store, crypto, fixtureWorld{}, events, time.Now)
	server := httpapi.NewServer(":8080")
	server.ConfigureAuth(service, []string{"http://localhost:8080"}, false)
	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(ctx)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Origin", "http://localhost:8080")
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("HTTP %d want %d: %s", w.Code, status, w.Body)
		}
	}
	check(request("GET", "/api/v1/me", "", nil), 401)
	registration := `{"username":"\u2003ALICE\u00a0","display_name":" Алиса ","password":"😀😀"}`
	response := request("POST", "/api/v1/auth/register", registration, nil)
	check(response, 201)
	first := response.Result().Cookies()[0]
	var user struct{ User struct{ ID, Username string } }
	if err := json.Unmarshal(response.Body.Bytes(), &user); err != nil || user.User.Username != "alice" {
		t.Fatal(err, response.Body.String())
	}
	var storedHash []byte
	var encoded string
	if err := pool.QueryRow(ctx, `SELECT s.token_hash,u.password_hash FROM sessions s JOIN users u ON u.id=s.user_id WHERE u.id=$1`, user.User.ID).Scan(&storedHash, &encoded); err != nil {
		t.Fatal(err)
	}
	expectedHash := crypto.TokenHash(first.Value)
	if string(storedHash) != string(expectedHash[:]) || strings.Contains(encoded, "😀") {
		t.Fatal("plaintext persisted")
	}
	loginBody := `{"username":"Alice","password":"😀😀"}`
	// A distinct browser login preserves the first browser's session.
	response = request("POST", "/api/v1/auth/login", loginBody, nil)
	check(response, 200)
	second := response.Result().Cookies()[0]
	firstSession, _, err := service.Authenticate(ctx, first.Value)
	if err != nil {
		t.Fatal(err)
	}
	observed, cancel, err := service.Observe(ctx, firstSession)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	response = request("POST", "/api/v1/auth/login", loginBody, first)
	check(response, 200)
	third := response.Result().Cookies()[0]
	select {
	case reason := <-observed:
		if reason != auth.Revoked {
			t.Fatal(reason)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement notification lost")
	}
	check(request("GET", "/api/v1/me", "", first), 401)
	check(request("GET", "/api/v1/me", "", second), 200)
	check(request("POST", "/api/v1/auth/register", registration, third), 409)
	check(request("GET", "/api/v1/me", "", third), 200)
	// Repair a removed room and check that /me persisted the corrected pose.
	if _, err = pool.Exec(ctx, `UPDATE player_states SET room_id='removed' WHERE user_id=$1`, user.User.ID); err != nil {
		t.Fatal(err)
	}
	response = request("GET", "/api/v1/me", "", third)
	check(response, 200)
	state, err := store.PlayerState(ctx, user.User.ID)
	if err != nil || state.RoomID != "safe" || state.Position.X != 2 {
		t.Fatal(state, err)
	}
	secondSession, _, err := service.Authenticate(ctx, second.Value)
	if err != nil {
		t.Fatal(err)
	}
	end, stop, err := service.Observe(ctx, secondSession)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	check(request("POST", "/api/v1/auth/logout", "", second), 204)
	select {
	case reason := <-end:
		if reason != auth.Revoked {
			t.Fatal(reason)
		}
	case <-time.After(time.Second):
		t.Fatal("logout notification lost")
	}
	check(request("POST", "/api/v1/auth/logout", "", second), 204)
	check(request("POST", "/api/v1/auth/logout", "", nil), 204)
	// Validity never slides, and an already expired persisted session is rejected.
	if _, err = pool.Exec(ctx, `UPDATE sessions SET created_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE token_hash=$1`, cryptoHashBytes(crypto, third.Value)); err != nil {
		t.Fatal(err)
	}
	check(request("GET", "/api/v1/me", "", third), 401)
	// Absence of B03 must not turn test coordinates into a production spawn.
	server.ConfigureAuth(auth.New(store, crypto, world.Unavailable{}, events, time.Now), []string{"http://localhost:8080"}, false)
	check(request("POST", "/api/v1/auth/register", `{"username":"newuser","display_name":"New","password":"password"}`, nil), 503)
	response = request("POST", "/api/v1/auth/login", loginBody, nil)
	check(response, 200)
	check(request("GET", "/api/v1/me", "", response.Result().Cookies()[0]), 503)
}

func cryptoHashBytes(crypto security.Crypto, token string) []byte {
	hash := crypto.TokenHash(token)
	return hash[:]
}
