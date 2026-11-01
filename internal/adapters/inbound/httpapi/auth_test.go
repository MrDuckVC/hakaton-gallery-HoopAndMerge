package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
)

type fakeAuth struct {
	err      error
	calls    int
	password string
}

func (f *fakeAuth) Register(_ context.Context, _, _, password, _ string) (auth.LoginResult, error) {
	f.calls++
	f.password = password
	return auth.LoginResult{User: domain.User{ID: "user", Username: "alice", DisplayName: "Алиса", AvatarColor: "#AABBCC"}, Token: "opaque"}, f.err
}
func (f *fakeAuth) Login(_ context.Context, _, password, _ string) (auth.LoginResult, error) {
	f.calls++
	f.password = password
	return auth.LoginResult{User: domain.User{ID: "user", Username: "alice"}, Token: "opaque"}, f.err
}
func (f *fakeAuth) Logout(context.Context, string) error { f.calls++; return f.err }
func (f *fakeAuth) Me(_ context.Context, token string) (auth.MeResult, error) {
	f.calls++
	if token == "" {
		return auth.MeResult{}, domain.Unauthenticated
	}
	return auth.MeResult{User: domain.User{ID: "user"}, State: player.State{RoomID: "start"}}, f.err
}
func authServer(secure bool) (*Server, *fakeAuth) {
	f := &fakeAuth{}
	s := NewServer(":8080")
	s.ConfigureAuth(f, []string{"http://localhost:8080"}, secure)
	return s, f
}
func perform(s *Server, method, path, body, origin, cookie string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: "museum_session", Value: cookie})
	}
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, r)
	return w
}
func assertResponse(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
		t.Fatalf("response %d %v %s", w.Code, w.Header(), w.Body)
	}
	if code != "" {
		var body struct {
			Error struct {
				Code      string `json:"code"`
				RequestID string `json:"request_id"`
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code != code || body.Error.RequestID != w.Header().Get("X-Request-ID") {
			t.Fatal(w.Body.String(), err)
		}
	}
}
func TestAuthHTTPRoutesAndCookies(t *testing.T) {
	for _, secure := range []bool{false, true} {
		s, f := authServer(secure)
		w := perform(s, "POST", "/api/v1/auth/register", `{"username":"alice","display_name":"Alice","password":" password "}`, "http://localhost:8080", "")
		assertResponse(t, w, 201, "")
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != "museum_session" || cookies[0].Value != "opaque" || !cookies[0].HttpOnly || cookies[0].Secure != secure || cookies[0].MaxAge != 86400 || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" || f.password != " password " {
			t.Fatal(cookies)
		}
		if strings.Contains(w.Body.String(), "opaque") {
			t.Fatal("token leaked into body")
		}
		w = perform(s, "POST", "/api/v1/auth/login", `{"username":"alice","password":"password"}`, "http://localhost:8080", "")
		assertResponse(t, w, 200, "")
		w = perform(s, "GET", "/api/v1/me", "", "", "")
		assertResponse(t, w, 401, "UNAUTHENTICATED")
		w = perform(s, "GET", "/api/v1/me", "", "", "token")
		assertResponse(t, w, 200, "")
		w = perform(s, "POST", "/api/v1/auth/logout", "", "http://localhost:8080", "")
		assertResponse(t, w, 204, "")
		if w.Body.Len() != 0 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
			t.Fatal(w.Body.String(), w.Header())
		}
	}
}
func TestOriginExactAndUpgrade(t *testing.T) {
	for _, origin := range []string{"", "null", "http://localhost:8080/", "http://localhost:8080.evil", "https://localhost:8080", "HTTP://localhost:8080"} {
		s, f := authServer(false)
		w := perform(s, "POST", "/api/v1/auth/logout", "", origin, "")
		assertResponse(t, w, 403, "FORBIDDEN")
		if f.calls != 0 {
			t.Fatal("called usecase")
		}
		r := httptest.NewRequest("GET", "/api/v1/ws", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w = httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		assertResponse(t, w, 403, "FORBIDDEN")
	}
	s, _ := authServer(false)
	r := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	r.Header["Origin"] = []string{"http://localhost:8080", "http://evil"}
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, r)
	assertResponse(t, w, 403, "FORBIDDEN")
}
func TestStrictAuthJSON(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{}`, `{"username":null,"password":"password"}`, `{"Username":"alice","password":"password"}`, `{"username":"alice","password":"password","extra":1}`, `{"username":"alice","password":3}`, `{"username":"alice","password":"password"} {}`, `{"username":`} {
		s, f := authServer(false)
		w := perform(s, "POST", "/api/v1/auth/login", body, "http://localhost:8080", "")
		if w.Code != 400 || f.calls != 0 {
			t.Fatal(body, w.Code, w.Body)
		}
	}
	s, f := authServer(false)
	w := perform(s, "POST", "/api/v1/auth/login", strings.Repeat(" ", 16385), "http://localhost:8080", "")
	assertResponse(t, w, 413, "PAYLOAD_TOO_LARGE")
	if f.calls != 0 {
		t.Fatal("oversized request reached usecase")
	}
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader("{}"))
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	s.Handler.ServeHTTP(w, r)
	assertResponse(t, w, 415, "UNSUPPORTED_MEDIA_TYPE")
}
func TestAuthErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{domain.UsernameTaken, 409, "USERNAME_TAKEN"}, {domain.InvalidCredentials, 401, "INVALID_CREDENTIALS"}, {domain.Unavailable, 503, "SERVICE_UNAVAILABLE"}, {context.DeadlineExceeded, 503, "SERVICE_UNAVAILABLE"}, {domain.Internal, 500, "INTERNAL_ERROR"}} {
		s, f := authServer(false)
		f.err = tc.err
		w := perform(s, "POST", "/api/v1/auth/login", `{"username":"alice","password":"password"}`, "http://localhost:8080", "")
		assertResponse(t, w, tc.status, tc.code)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("failed request replaced cookie")
		}
	}
}
func TestSlidingAuthLimiter(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	limit := newAuthLimiter(func() time.Time { return now })
	for range 10 {
		if wait := limit.allow("127.0.0.1"); wait != 0 {
			t.Fatal(wait)
		}
	}
	if wait := limit.allow("127.0.0.1"); wait != time.Minute {
		t.Fatal(wait)
	}
	if wait := limit.allow("127.0.0.2"); wait != 0 {
		t.Fatal(wait)
	}
	now = now.Add(time.Minute)
	if wait := limit.allow("127.0.0.1"); wait != 0 {
		t.Fatal(wait)
	}
}
func TestLoginRegisterShareLimitAndIgnoreForwardedIP(t *testing.T) {
	s, _ := authServer(false)
	for i := range 11 {
		path := "/api/v1/auth/login"
		if i%2 == 1 {
			path = "/api/v1/auth/register"
		}
		r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		r.RemoteAddr = "[::1]:1234"
		r.Header.Set("Origin", "http://localhost:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", string(rune('a'+i)))
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		if i == 10 {
			assertResponse(t, w, 429, "RATE_LIMITED")
			if w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "retry_after_ms") {
				t.Fatal(w.Body)
			}
		} else if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}

func TestUpgradeTokenListsRequireOrigin(t *testing.T) {
	for _, values := range [][]string{{"websocket, h2c"}, {"h2c", "websocket"}, {" WebSocket "}} {
		server, _ := authServer(false)
		request := httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil)
		request.Header["Upgrade"] = values
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, request)
		assertResponse(t, response, 403, "FORBIDDEN")
	}
}

func TestAuthJSONRejectsInvalidUnicodeWithoutChangingPasswords(t *testing.T) {
	for _, password := range []string{`aaaaa\ud800`, `aaaaa\udc00`, `aaaaa\ud800\u0041`} {
		server, fake := authServer(false)
		response := perform(server, "POST", "/api/v1/auth/login", `{"username":"alice","password":"`+password+`"}`, "http://localhost:8080", "")
		assertResponse(t, response, 400, "INVALID_JSON")
		if fake.calls != 0 {
			t.Fatal("invalid Unicode reached KDF")
		}
	}
	for _, password := range []string{`\ud83d\ude00\ud83d\ude00`, `aaaaa�`, `literal\\ud800`} {
		server, _ := authServer(false)
		response := perform(server, "POST", "/api/v1/auth/login", `{"username":"alice","password":"`+password+`"}`, "http://localhost:8080", "")
		assertResponse(t, response, 200, "")
	}
	server, _ := authServer(false)
	response := perform(server, "POST", "/api/v1/auth/login", string([]byte{'{', '"', 0xff, '"', ':', '1', '}'}), "http://localhost:8080", "")
	assertResponse(t, response, 400, "INVALID_JSON")
}
