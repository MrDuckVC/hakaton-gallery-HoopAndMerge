package websocket

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
)

func TestLampWireUnionAndPrivateOwner(t *testing.T) {
	for _, action := range []string{"take", "place", "aim", "power"} {
		t.Run(action, func(t *testing.T) {
			body, err := os.ReadFile("../../../../docs/api/examples/sunday/object.act." + action + ".json")
			if err != nil {
				t.Fatal(err)
			}
			cmd, f := decode(body)
			if f.Code != "" || cmd.Type != "object.act" {
				t.Fatalf("command %+v failure %+v", cmd, f)
			}
			var root map[string]any
			if err = json.Unmarshal(body, &root); err != nil {
				t.Fatal(err)
			}
			p := root["payload"].(map[string]any)
			for _, mutate := range []func(){func() { p["on"] = nil }, func() { p["unexpected"] = true }, func() { p["expected_revision"] = 0 }} {
				// Independent mutation, restoring the original payload each time.
				if err = json.Unmarshal(body, &root); err != nil {
					t.Fatal(err)
				}
				p = root["payload"].(map[string]any)
				mutate()
				bad, _ := json.Marshal(root)
				rejected, failure := decode(bad)
				if failure.Code != "VALIDATION_ERROR" || rejectedCommand(rejected).RequestID == "" {
					t.Fatalf("accepted invalid %s: %+v", bad, failure)
				}
			}
		})
	}
}
func TestObjectActedEncoding(t *testing.T) {
	e := game.Event{Type: "object.acted", RequestID: "request", ObjectAction: "power"}
	e.Runtime.Lamp.HolderUserID = "u"
	e.Runtime.Lamp.HolderConnectionID = "private"
	body, err := encode(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "private") || !strings.Contains(string(body), `"action":"power"`) {
		t.Fatal(string(body))
	}
	for _, extra := range []string{`,"on":false`, `,"slot_id":"a"`, `,"yaw":0`} {
		b := fmt.Sprintf(`{"type":"object.act","request_id":"11111111-1111-4111-8111-111111111111","payload":{"object_id":"lamp_01","action":"take","movement_epoch":1,"expected_revision":1%s}}`, extra)
		if _, f := decode([]byte(b)); f.Code != "VALIDATION_ERROR" {
			t.Fatal("union accepted", b)
		}
	}
}
