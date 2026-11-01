package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	usecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
)

type adminSessions struct {
	err   error
	calls int
}

func (s *adminSessions) Authenticate(_ context.Context, token string) (authdomain.Session, authdomain.User, error) {
	s.calls++
	if token == "" {
		return authdomain.Session{}, authdomain.User{}, authdomain.Unauthenticated
	}
	return authdomain.Session{}, authdomain.User{ID: "admin-user"}, s.err
}

type adminRuntime struct {
	roleErr, error     error
	roleCalls, updates int
	config             domain.Config
}

func (a *adminRuntime) RequireAdmin(context.Context, string) error { a.roleCalls++; return a.roleErr }
func (a *adminRuntime) Rooms(context.Context) ([]usecase.Summary, error) {
	return []usecase.Summary{{RoomID: "room", Title: "Room", Revision: 1}}, a.error
}
func (a *adminRuntime) Read(_ context.Context, id string) (domain.Room, error) {
	return domain.Room{Overlay: domain.Overlay{WorldVersion: "world", RoomID: id, Revision: 1}}, a.error
}
func (a *adminRuntime) Update(_ context.Context, id, version string, revision int64, config domain.Config) (domain.Room, error) {
	a.updates++
	a.config = config
	return domain.Room{Overlay: domain.Overlay{WorldVersion: version, RoomID: id, Revision: revision + 1, Config: config}}, a.error
}
func adminServer() (*Server, *adminRuntime, *adminSessions) {
	s := NewServer(":0")
	a := &adminRuntime{}
	sessions := &adminSessions{}
	s.ConfigureRuntime(a, sessions, []string{"http://localhost:8080"})
	return s, a, sessions
}

const roomUpdate = `{"world_version":"world","expected_revision":1,"config":{"accent_color":"#D9D2C3","lighting":"warm","guide_placements":[{"guide_id":"guide","position":{"x":6,"y":0,"z":7.5},"rotation_y":0}],"lamp_slots":[{"slot_id":"a","position":{"x":3,"y":0,"z":4}},{"slot_id":"b","position":{"x":9,"y":0,"z":4}}],"npc_count":0,"installation_visible":false}}`

func TestAdminSessionRoleCheckedEveryRequest(t *testing.T) {
	for _, path := range []string{"/api/v1/admin/me", "/api/v1/admin/rooms", "/api/v1/admin/rooms/room"} {
		s, a, sessions := adminServer()
		assertResponse(t, perform(s, "GET", path, "", "", ""), 401, "UNAUTHENTICATED")
		a.roleErr = domain.Forbidden
		assertResponse(t, perform(s, "GET", path, "", "", "token"), 403, "FORBIDDEN")
		a.roleErr = nil
		assertResponse(t, perform(s, "GET", path, "", "", "token"), 200, "")
		a.roleErr = domain.Forbidden
		assertResponse(t, perform(s, "GET", path, "", "", "token"), 403, "FORBIDDEN")
		if a.roleCalls != 3 || sessions.calls != 4 {
			t.Fatal(a.roleCalls, sessions.calls)
		}
	}
	s, a, _ := adminServer()
	a.roleErr = domain.Forbidden
	assertResponse(t, perform(s, "PUT", "/api/v1/admin/rooms/room", "malformed", "http://localhost:8080", "token"), 403, "FORBIDDEN")
	if a.updates != 0 {
		t.Fatal("unauthorized update")
	}
}
func TestAdminPUTOriginAndJSON(t *testing.T) {
	for _, origin := range []string{"", "null", "http://localhost:8080/", "HTTP://localhost:8080", "http://localhost:8080.evil"} {
		s, a, _ := adminServer()
		assertResponse(t, perform(s, "PUT", "/api/v1/admin/rooms/room", roomUpdate, origin, "token"), 403, "FORBIDDEN")
		if a.updates != 0 {
			t.Fatal(origin)
		}
	}
	for _, body := range []string{
		`null`, `[]`, `{}`, `{"world_version":`, roomUpdate + ` {}`,
		strings.Replace(roomUpdate, `"expected_revision":1`, `"expected_revision":null`, 1),
		strings.Replace(roomUpdate, `"expected_revision":1`, `"expected_revision":1,"expected_revision":2`, 1),
		strings.Replace(roomUpdate, `"npc_count":0`, `"npc_count":0,"npc_count":1`, 1),
		strings.Replace(roomUpdate, `"installation_visible":false`, `"Installation_visible":false`, 1),
		strings.Replace(roomUpdate, `"installation_visible":false`, `"installation_visible":null`, 1),
		strings.Replace(roomUpdate, `,"installation_visible":false`, ``, 1),
		strings.Replace(roomUpdate, `"x":6`, `"x":6,"x":7`, 1),
		strings.Replace(roomUpdate, `"y":0`, `"y":null`, 1),
		strings.Replace(roomUpdate, `"npc_count":0`, `"npc_count":1.5`, 1),
		strings.Replace(roomUpdate, `"expected_revision":1`, `"expected_revision":9007199254740992`, 1),
		strings.Replace(roomUpdate, `"config":{`, `"config":{"unknown":true,`, 1),
		strings.Replace(roomUpdate, `"guide_placements":[`, `"guide_placements":null,"bad":[`, 1),
	} {
		s, a, _ := adminServer()
		w := perform(s, "PUT", "/api/v1/admin/rooms/room", body, "http://localhost:8080", "token")
		if w.Code != 400 || a.updates != 0 {
			t.Fatalf("body %s: %d %s calls=%d", body, w.Code, w.Body, a.updates)
		}
	}
	s, a, _ := adminServer()
	w := perform(s, "PUT", "/api/v1/admin/rooms/room", roomUpdate, "http://localhost:8080", "token")
	assertResponse(t, w, 200, "")
	if a.updates != 1 || a.config.NPCCount != 0 || a.config.InstallationVisible {
		t.Fatal(a)
	}
}
func TestAdminBodyMediaLimitsAndHeaderOriginMultiplicity(t *testing.T) {
	for _, tc := range []struct {
		body, media string
		status      int
		code        string
	}{{strings.Repeat(" ", 65537), "application/json", 413, "PAYLOAD_TOO_LARGE"}, {roomUpdate, "text/plain", 415, "UNSUPPORTED_MEDIA_TYPE"}, {roomUpdate, "application/json; charset=iso-8859-1", 415, "UNSUPPORTED_MEDIA_TYPE"}, {strings.Repeat(" ", 65536-len(roomUpdate)) + roomUpdate, "application/json; charset=UTF-8", 200, ""}, {`{"world_version":"\ud800"}`, "application/json", 400, "INVALID_JSON"}} {
		s, a, _ := adminServer()
		r := httptest.NewRequest("PUT", "/api/v1/admin/rooms/room", strings.NewReader(tc.body))
		r.Header.Set("Origin", "http://localhost:8080")
		r.Header.Set("Content-Type", tc.media)
		r.AddCookie(&http.Cookie{Name: "museum_session", Value: "token"})
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		assertResponse(t, w, tc.status, tc.code)
		if tc.status != 200 && a.updates != 0 {
			t.Fatal("bad body reached update")
		}
	}
	s, _, _ := adminServer()
	r := httptest.NewRequest("PUT", "/api/v1/admin/rooms/room", strings.NewReader(roomUpdate))
	r.Header["Origin"] = []string{"http://localhost:8080", "http://localhost:8080"}
	r.AddCookie(&http.Cookie{Name: "museum_session", Value: "token"})
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, r)
	assertResponse(t, w, 403, "FORBIDDEN")
}
func TestAdminPUTUserRateLimitAndErrors(t *testing.T) {
	s, a, _ := adminServer()
	now := time.Now()
	s.admin.limiter.now = func() time.Time { return now }
	for range 10 {
		assertResponse(t, perform(s, "PUT", "/api/v1/admin/rooms/room", roomUpdate, "http://localhost:8080", "token"), 200, "")
	}
	w := perform(s, "PUT", "/api/v1/admin/rooms/other", roomUpdate, "http://localhost:8080", "different-token")
	assertResponse(t, w, 429, "RATE_LIMITED")
	if w.Header().Get("Retry-After") != "10" || !strings.Contains(w.Body.String(), `"retry_after_ms":10000`) {
		t.Fatal(w.Body.String(), w.Header())
	}
	now = now.Add(10 * time.Second)
	assertResponse(t, perform(s, "PUT", "/api/v1/admin/rooms/room", roomUpdate, "http://localhost:8080", "token"), 200, "")
	if a.updates != 11 {
		t.Fatal(a.updates)
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{domain.RevisionConflict, 409, "REVISION_CONFLICT"}, {domain.ObjectBusy, 409, "OBJECT_BUSY"}, {domain.CommitUnknown, 503, "SERVICE_UNAVAILABLE"}, {authdomain.Unavailable, 503, "SERVICE_UNAVAILABLE"}} {
		s, a, _ := adminServer()
		a.error = tc.err
		assertResponse(t, perform(s, "PUT", "/api/v1/admin/rooms/room", roomUpdate, "http://localhost:8080", "token"), tc.status, tc.code)
	}
}
func TestAdminPublicRoomListAndMe(t *testing.T) {
	s, _, _ := adminServer()
	w := perform(s, "GET", "/api/v1/admin/me", "", "", "token")
	assertResponse(t, w, 200, "")
	var me map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil || len(me) != 2 || me["user_id"] != "admin-user" || me["role"] != "admin" {
		t.Fatal(w.Body.String(), err)
	}
	w = perform(s, "GET", "/api/v1/admin/rooms", "", "", "token")
	assertResponse(t, w, 200, "")
	if !strings.Contains(w.Body.String(), `"room_id":"room"`) {
		t.Fatal(w.Body)
	}
}
