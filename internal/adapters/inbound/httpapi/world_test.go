package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	usecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/world"
)

type fakeWorld struct {
	calls              int
	token, version, id string
	err                error
}

func TestManifestAdvertisesIntegratedSunday(t *testing.T) {
	s, _ := authServer(false)
	s.ConfigureWorld(&fakeWorld{})
	w := perform(s, "GET", "/api/v1/world/manifest", "", "", "owner")
	assertResponse(t, w, 200, "")
	var manifest struct {
		Extensions   []string `json:"extensions"`
		MaxRooms     int      `json:"max_loaded_rooms"`
		PhotoReserve int64    `json:"photo_reserve_bytes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Extensions) != 1 || manifest.Extensions[0] != "sunday_v1" {
		t.Fatalf("Sunday capability missing: %s", w.Body.String())
	}
	if manifest.MaxRooms != 1 || manifest.PhotoReserve != world.PhotoReserve {
		t.Fatalf("base manifest changed: %s", w.Body.String())
	}
}

func (f *fakeWorld) Manifest(_ context.Context, token string) (world.Manifest, error) {
	f.calls++
	f.token = token
	if token == "" {
		return world.Manifest{}, domain.Unauthenticated
	}
	return world.Manifest{ID: "world", Version: "v1", TotalBytes: 40000000, CacheLimit: 4000000}, f.err
}
func (f *fakeWorld) Room(_ context.Context, token, version, id string) (usecase.File, error) {
	f.calls++
	f.token = token
	f.version = version
	f.id = id
	return usecase.File{Bytes: []byte("{ \"title\": \"Зал\" }\n"), ContentType: "application/json"}, f.err
}
func (f *fakeWorld) Asset(_ context.Context, token, version, id string) (usecase.File, error) {
	f.calls++
	f.token = token
	f.version = version
	f.id = id
	return usecase.File{Bytes: []byte{0xff, 0xd8, 1, 2}, ContentType: "image/jpeg"}, f.err
}
func TestWorldHTTP(t *testing.T) {
	s, _ := authServer(false)
	f := &fakeWorld{}
	s.ConfigureWorld(f)
	for _, tc := range []struct {
		path   string
		status int
		code   string
	}{
		{"/api/v1/world/manifest", 200, ""},
		{"/api/v1/world/rooms/room?version=v1", 200, ""},
		{"/api/v1/world/assets/asset?version=v1", 200, ""},
		{"/api/v1/world/rooms/room", 400, "VALIDATION_ERROR"},
		{"/api/v1/world/assets/asset?version=", 400, "VALIDATION_ERROR"},
		{"/api/v1/world/assets/asset?version=v1&version=v2", 400, "VALIDATION_ERROR"},
		{"/api/v1/world/assets/..%2Fprivate?version=v1", 400, "VALIDATION_ERROR"},
		{"/api/v1/world/assets/%2E%2E?version=v1", 400, "VALIDATION_ERROR"},
		{"/api/v1/world/rooms/a/b?version=v1", 400, "VALIDATION_ERROR"},
		{"/world/manifest", 404, "NOT_FOUND"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := perform(s, "GET", tc.path, "", "", "owner")
			assertResponse(t, w, tc.status, tc.code)
			if tc.status == 200 && tc.path != "/api/v1/world/manifest" {
				if w.Header().Get("Content-Length") == "" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal(w.Header())
				}
			}
		})
	}
	w := perform(s, "GET", "/api/v1/world/rooms/r?version=v1", "", "", "owner")
	if w.Body.String() != "{ \"title\": \"Зал\" }\n" {
		t.Fatal(w.Body.String())
	}
	id := `opaque?"#`
	w = perform(s, "GET", "/api/v1/world/assets/"+url.PathEscape(id)+"?version=v1", "", "", "owner")
	assertResponse(t, w, 200, "")
	if f.id != id || f.token != "owner" || f.version != "v1" {
		t.Fatal(f)
	}
	w = perform(s, "GET", "/api/v1/me", "", "", "owner")
	assertResponse(t, w, 200, "")
	w = perform(s, "GET", "/api/v1/world/manifest", "", "", "")
	assertResponse(t, w, 401, "UNAUTHENTICATED")
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{world.VersionMismatch, 409, "WORLD_VERSION_MISMATCH"}, {world.RoomNotAccessible, 403, "ROOM_NOT_ACCESSIBLE"}, {world.NotReady, 409, "GAME_NOT_READY"}, {world.NotFound, 404, "NOT_FOUND"}, {domain.Unavailable, 503, "SERVICE_UNAVAILABLE"}, {context.DeadlineExceeded, 503, "SERVICE_UNAVAILABLE"}, {errors.New("private storage detail"), 500, "INTERNAL_ERROR"},
	} {
		f.err = tc.err
		w = perform(s, "GET", "/api/v1/world/assets/a?version=v1", "", "", "owner")
		assertResponse(t, w, tc.status, tc.code)
	}
}
