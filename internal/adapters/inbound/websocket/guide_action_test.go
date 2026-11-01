package websocket

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
)

func TestGuideActionWireValidation(t *testing.T) {
	body, err := os.ReadFile("../../../../docs/api/examples/sunday/guide.action.json")
	if err != nil {
		t.Fatal(err)
	}
	cmd, f := decode(body)
	if f.Code != "" || cmd.Type != "guide.action" || cmd.GuideID != "guide_leonardo" || cmd.ExpectedRevision != 1 || cmd.MovementEpoch != 1 || cmd.Text != "покажи макет" {
		t.Fatalf("%+v %+v", cmd, f)
	}
	for _, change := range []func(map[string]any){
		func(p map[string]any) { p["text"] = "\x00" }, func(p map[string]any) { p["text"] = " " }, func(p map[string]any) { p["text"] = strings.Repeat("я", 201) },
		func(p map[string]any) { p["text"] = nil }, func(p map[string]any) { p["guide_id"] = "" }, func(p map[string]any) { p["extra"] = 1 }, func(p map[string]any) { p["movement_epoch"] = 0 }, func(p map[string]any) { p["expected_revision"] = 1.5 }, func(p map[string]any) { delete(p, "expected_revision") },
	} {
		var root map[string]any
		if err = json.Unmarshal(body, &root); err != nil {
			t.Fatal(err)
		}
		change(root["payload"].(map[string]any))
		bad, _ := json.Marshal(root)
		c, failure := decode(bad)
		if failure.Code != "VALIDATION_ERROR" || rejectedCommand(c).RequestID == "" {
			t.Fatalf("%s %+v", bad, failure)
		}
	}
	duplicate := strings.Replace(string(body), `"text":`, `"text":"покажи макет","text":`, 1)
	if _, f = decode([]byte(duplicate)); f.Code != "VALIDATION_ERROR" {
		t.Fatal(f)
	}
}

func TestGuideActionCompletedEncoding(t *testing.T) {
	e := game.Event{Type: "guide.action.completed", RequestID: "r", GuideID: "g", GuideEffect: domain.WarmLight}
	e.Runtime.Lamp.HolderConnectionID = "private"
	body, err := encode(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"effect":"warm_light"`) || !strings.Contains(string(body), `"guide_id":"g"`) || strings.Contains(string(body), "private") {
		t.Fatal(string(body))
	}
}
