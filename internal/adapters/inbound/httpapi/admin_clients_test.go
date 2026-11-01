package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
)

type clientSource struct{ records []game.ClientMetric }

func (c clientSource) Clients() []game.ClientMetric { return c.records }

func TestAdminClientsAuthorizationAndWire(t *testing.T) {
	s, role, sessions := adminServer()
	s.ConfigureClients(clientSource{})
	const path = "/api/v1/admin/clients"
	assertResponse(t, perform(s, "GET", path, "", "", ""), 401, "UNAUTHENTICATED")
	role.roleErr = domain.Forbidden
	assertResponse(t, perform(s, "GET", path, "", "", "token"), 403, "FORBIDDEN")
	role.roleErr = nil
	w := perform(s, "GET", path, "", "", "token")
	assertResponse(t, w, 200, "")
	if w.Body.String() != "{\"clients\":[]}\n" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
		t.Fatal(w.Body.String(), w.Header())
	}
	fps, ping := 59.5, 12.25
	now := time.Now().UTC()
	s.ConfigureClients(clientSource{records: []game.ClientMetric{{UserID: "u", DisplayName: "Name", RoomID: "r", FPS: &fps, PingMS: &ping, Visible: true, SampledAt: &now}}})
	w = perform(s, "GET", path, "", "", "token")
	assertResponse(t, w, 200, "")
	var body struct {
		Clients []map[string]any `json:"clients"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Clients) != 1 || len(body.Clients[0]) != 8 || body.Clients[0]["fps"] != fps || body.Clients[0]["ping_ms"] != ping || body.Clients[0]["user_id"] != "u" {
		t.Fatal(body)
	}
	role.roleErr = domain.Forbidden
	assertResponse(t, perform(s, "GET", path, "", "", "token"), 403, "FORBIDDEN")
	if sessions.calls != 5 || role.roleCalls != 4 {
		t.Fatal(sessions.calls, role.roleCalls)
	}
}
