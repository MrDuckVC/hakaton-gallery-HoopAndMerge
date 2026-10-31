package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/inbound/worldwire"
	files "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/world"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/testworld"
	world "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/world"
)

type worldSessions struct{}

func (worldSessions) Authenticate(_ context.Context, token string) (domain.Session, domain.User, error) {
	if token == "" {
		return domain.Session{}, domain.User{}, domain.Unauthenticated
	}
	return domain.Session{ID: token, UserID: "u"}, domain.User{ID: "u"}, nil
}

type worldOwner struct{ room string }

func (o *worldOwner) OwningRoom(_ context.Context, session, user string) (string, error) {
	if session != "owner" || user != "u" {
		return "", domain.Unauthenticated
	}
	return o.room, nil
}
func TestWorldFilesUsecaseHTTPIntegration(t *testing.T) {
	dir := t.TempDir()
	if err := testworld.Write(t.Context(), dir, "v1"); err != nil {
		t.Fatal(err)
	}
	c, err := world.Load(t.Context(), files.Files{Dir: dir}, worldwire.SnapshotSizer{}, "v1")
	if err != nil {
		t.Fatal(err)
	}
	owner := &worldOwner{room: "r0"}
	s, _ := authServer(false)
	s.ConfigureWorld(world.New(c, worldSessions{}, owner))
	w := perform(s, "GET", "/api/v1/world/manifest", "", "", "other")
	assertResponse(t, w, 200, "")
	var m manifestDTO
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	var total int64
	for n := 0; n < 40; n++ {
		for _, name := range []string{"r" + strconv.Itoa(n) + ".json", "a" + strconv.Itoa(n) + ".jpg"} {
			info, err := os.Stat(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			total += info.Size()
		}
	}
	if m.Total != total || m.Limit != total/10 || m.PhotoReserve != 3145728 {
		t.Fatal(m, total)
	}
	for _, tc := range []struct{ path, file string }{{"rooms/r0", "r0.json"}, {"assets/a0", "a0.jpg"}} {
		want, err := os.ReadFile(filepath.Join(dir, tc.file))
		if err != nil {
			t.Fatal(err)
		}
		w = perform(s, "GET", "/api/v1/world/"+tc.path+"?version=v1", "", "", "owner")
		assertResponse(t, w, 200, "")
		if w.Body.String() != string(want) || w.Header().Get("Content-Length") != strconv.Itoa(len(want)) {
			t.Fatal("bytes differ", tc)
		}
		if err := os.WriteFile(filepath.Join(dir, tc.file), []byte("changed after startup"), 0600); err != nil {
			t.Fatal(err)
		}
		w = perform(s, "GET", "/api/v1/world/"+tc.path+"?version=v1", "", "", "owner")
		assertResponse(t, w, 200, "")
		if w.Body.String() != string(want) {
			t.Fatal("catalog changed")
		}
	}
	for _, tc := range []struct {
		path, token string
		status      int
		code        string
	}{
		{"rooms/r0?version=old", "owner", 409, "WORLD_VERSION_MISMATCH"}, {"rooms/r1?version=v1", "owner", 403, "ROOM_NOT_ACCESSIBLE"}, {"assets/a1?version=v1", "owner", 403, "ROOM_NOT_ACCESSIBLE"}, {"rooms/r0?version=v1", "other", 401, "UNAUTHENTICATED"}, {"manifest", "", 401, "UNAUTHENTICATED"},
	} {
		w = perform(s, "GET", "/api/v1/world/"+tc.path, "", "", tc.token)
		assertResponse(t, w, tc.status, tc.code)
	}
	owner.room = "r1"
	w = perform(s, "GET", "/api/v1/world/assets/a1?version=v1", "", "", "owner")
	assertResponse(t, w, 200, "")
}
