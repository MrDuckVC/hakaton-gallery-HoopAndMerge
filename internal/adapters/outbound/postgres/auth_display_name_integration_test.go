package postgres

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/inbound/httpapi"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/sessionevents"
	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/auth"
)

func TestRegisterNULDisplayNameIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	service := auth.New(NewAuthStore(pool), security.Crypto{}, fixtureWorld{}, sessionevents.New(), time.Now)
	server := httpapi.NewServer(":8080")
	server.ConfigureAuth(service, []string{"http://localhost:8080"}, false)
	request := func(path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
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
	// The password's NUL must survive registration and login exactly.
	response := request("/api/v1/auth/register", `{"username":"alice","display_name":"Алиса","password":"pass\u0000word"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("register password with NUL: %d %s", response.Code, response.Body)
	}
	cookie := response.Result().Cookies()[0]
	for _, name := range []string{`\u0000`, `A\u0000B`, `\u0000Алиса`, `Алиса\u0000`} {
		response = request("/api/v1/auth/register", `{"username":"rejected","display_name":"`+name+`","password":"password"}`, cookie)
		var body struct {
			Error struct {
				Code string `json:"code"`
			}
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusBadRequest || body.Error.Code != "VALIDATION_ERROR" || len(response.Result().Cookies()) != 0 {
			t.Fatalf("invalid display name: %d %s", response.Code, response.Body)
		}
	}
	if _, _, err := service.Authenticate(ctx, cookie.Value); err != nil {
		t.Fatal("rejected registration revoked existing cookie", err)
	}
	var users, sessions, states int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM sessions),(SELECT count(*) FROM player_states)`).Scan(&users, &sessions, &states); err != nil {
		t.Fatal(err)
	}
	if users != 1 || sessions != 1 || states != 1 {
		t.Fatalf("rejected registrations changed data: users=%d sessions=%d states=%d", users, sessions, states)
	}
	response = request("/api/v1/auth/login", `{"username":"alice","password":"pass\u0000word"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("login password with NUL: %d %s", response.Code, response.Body)
	}
	response = request("/api/v1/auth/login", `{"username":"alice","password":"password"}`, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("password NUL was stripped")
	}
}
