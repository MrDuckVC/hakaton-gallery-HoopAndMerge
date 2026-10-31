// Package guidewire maps private domain generations to the public v1 contract.
package guidewire

import (
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
)

// Message is a fully saved dialog message.
type Message struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

// Error is a stable public generation failure.
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// Generation deliberately excludes ownership, fingerprints and internal timestamps.
type Generation struct {
	ID               string   `json:"id"`
	RequestID        string   `json:"request_id"`
	GuideID          string   `json:"guide_id"`
	ExhibitID        *string  `json:"exhibit_id"`
	Status           string   `json:"status"`
	UserMessage      Message  `json:"user_message"`
	AssistantMessage *Message `json:"assistant_message"`
	Error            *Error   `json:"error"`
}

// Public serializes nulls explicitly, including failures with no partial answer.
func Public(g domain.Generation) Generation {
	dto := Generation{ID: g.ID, RequestID: g.RequestID, GuideID: g.GuideID, Status: g.Status, UserMessage: message(g.UserMessage, "user")}
	if g.ExhibitID != "" {
		id := g.ExhibitID
		dto.ExhibitID = &id
	}
	if g.AssistantMessage != nil {
		m := message(*g.AssistantMessage, "assistant")
		dto.AssistantMessage = &m
	}
	if g.Error != nil {
		dto.Error = &Error{Code: g.Error.Code, Message: g.Error.Message, Retryable: g.Error.Retryable}
	}
	return dto
}
func message(m domain.Message, role string) Message {
	return Message{ID: m.ID, Role: role, Text: m.Text, CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339Nano)}
}
