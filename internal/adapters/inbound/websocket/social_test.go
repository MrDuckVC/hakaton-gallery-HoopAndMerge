package websocket

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/game"
)

func TestSocialWireEvents(t *testing.T) {
	for _, e := range []game.Event{
		{Type: "chat.message", RequestID: "correlation", Chat: game.ChatMessage{ID: "m", AuthorID: "u", DisplayName: "Имя", Text: "hello"}},
		{Type: "exhibit.inspected", RequestID: "correlation", ExhibitID: "ex", InspectedAt: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)},
	} {
		b, err := encode(e)
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			RequestID string         `json:"request_id"`
			Payload   map[string]any `json:"payload"`
		}
		if err = json.Unmarshal(b, &value); err != nil {
			t.Fatal(err)
		}
		if value.RequestID != "correlation" {
			t.Fatal(string(b))
		}
		p := value.Payload
		if e.Type == "chat.message" {
			author, _ := p["author"].(map[string]any)
			if p["message_id"] != "m" || p["text"] != "hello" || author["user_id"] != "u" || author["display_name"] != "Имя" {
				t.Fatal(string(b))
			}
			if v, ok := p["photo"]; !ok || v != nil {
				t.Fatal("photo must be explicit null:", string(b))
			}
		} else if p["exhibit_id"] != "ex" || p["inspected_at"] != "2026-10-31T10:00:00Z" {
			t.Fatal(string(b))
		}
	}
}
func TestChatUnicodeAndPayloadValidation(t *testing.T) {
	for _, tc := range []struct{ payload, code string }{
		{`{"text":"\u2003` + strings.Repeat("я", 500) + `\u00a0"}`, ""},
		{`{"text":"` + strings.Repeat("я", 501) + `"}`, "VALIDATION_ERROR"},
		{`{"text":"\u2003"}`, "VALIDATION_ERROR"},
		{`{"photo_id":"photo"}`, ""},
		{`{"text":" ","photo_id":"photo"}`, ""},
		{`{"text":null,"photo_id":"photo"}`, "VALIDATION_ERROR"},
		{`{"text":"hi","user_id":"foreign"}`, "VALIDATION_ERROR"},
	} {
		c, f := decode([]byte(`{"type":"chat.send","request_id":"00000000-0000-4000-8000-000000000001","payload":` + tc.payload + `}`))
		if f.Code != tc.code {
			t.Fatal(tc.payload, f)
		}
		if f.Code == "" && strings.TrimSpace(c.Text) != c.Text {
			t.Fatal("not trimmed")
		}
	}
}
