package websocket

import (
	"encoding/json"
	"testing"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/guide"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/game"
	guide "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/guide"
)

func TestGuideWireEvents(t *testing.T) {
	for _, kind := range []string{"guide.accepted", "guide.started", "guide.delta", "guide.completed", "guide.failed", "guide.cancelled"} {
		g := domain.Generation{ID: "generation", RequestID: "ask", GuideID: "guide", Status: "queued", UserMessage: domain.Message{ID: "question", Text: "q", CreatedAt: time.Now()}}
		body, err := encode(game.Event{Type: kind, RequestID: "correlation", Guide: guide.Event{Type: kind, Generation: g, GenerationID: g.ID, Index: 2, Text: "delta"}})
		if err != nil {
			t.Fatal(err)
		}
		var e struct {
			RequestID string                     `json:"request_id"`
			Payload   map[string]json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(body, &e) != nil || e.RequestID != "correlation" {
			t.Fatal(string(body))
		}
		if kind == "guide.started" || kind == "guide.delta" {
			if string(e.Payload["generation_id"]) != `"generation"` {
				t.Fatal(string(body))
			}
		} else {
			var generation map[string]any
			if json.Unmarshal(e.Payload["generation"], &generation) != nil || generation["id"] != "generation" || generation["assistant_message"] != nil || generation["error"] != nil || generation["exhibit_id"] != nil {
				t.Fatal(string(body))
			}
			for _, private := range []string{"user_id", "fingerprint", "connection_id"} {
				if _, ok := generation[private]; ok {
					t.Fatal("private field", string(body))
				}
			}
		}
	}
}
