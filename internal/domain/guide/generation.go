// Package guide defines durable questions and generation states.
package guide

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// Payload is the normalized client question, without client-supplied context.
type Payload struct{ GuideID, Text, ExhibitID string }

// Normalize checks values that must be representable in PostgreSQL text.
func Normalize(guideID, text, exhibitID string) (Payload, error) {
	text = strings.TrimSpace(text)
	if !world.ValidID(guideID) || (exhibitID != "" && !world.ValidID(exhibitID)) || !utf8.ValidString(text) || strings.ContainsRune(text, 0) || utf8.RuneCountInString(text) < 1 || utf8.RuneCountInString(text) > 2000 {
		return Payload{}, auth.Validation
	}
	return Payload{guideID, text, exhibitID}, nil
}

// Fingerprint uses length-delimited fields to avoid ambiguous concatenation.
func (p Payload) Fingerprint() string {
	var value strings.Builder
	for _, s := range []string{p.GuideID, p.Text, p.ExhibitID} {
		value.WriteString(strconv.Itoa(len(s)))
		value.WriteByte(':')
		value.WriteString(s)
	}
	sum := sha256.Sum256([]byte(value.String()))
	return hex.EncodeToString(sum[:])
}

// Message is a saved question or a fully completed answer.
type Message struct {
	ID, Text  string
	CreatedAt time.Time
}

// Error is the public, persisted generation failure.
type Error struct {
	Code, Message string
	Retryable     bool
}

// Generation contains no partial response. ConnectionID never crosses the wire.
type Generation struct {
	ID, UserID, RequestID, GuideID, ExhibitID, Fingerprint, Status, ConnectionID string
	UserMessage                                                                  Message
	AssistantMessage                                                             *Message
	Error                                                                        *Error
	CreatedAt                                                                    time.Time
	StartedAt                                                                    *time.Time
	FinishedAt                                                                   *time.Time
}

// Terminal reports immutable states.
func (g Generation) Terminal() bool { return g.Status == "completed" || g.Status == "failed" }

// Failure gives every terminal error a stable safe description and retry policy.
func Failure(code string) *Error {
	message := "Генерация отменена"
	switch code {
	case "AI_TIMEOUT":
		message = "Превышено время ожидания ответа"
	case "AI_UNAVAILABLE":
		message = "Модель недоступна"
	case "AI_OUTPUT_LIMIT":
		message = "Ответ превысил допустимую длину"
	case "SERVER_RESTARTED":
		message = "Генерация прервана сервером"
	}
	return &Error{code, message, code == "AI_TIMEOUT" || code == "AI_UNAVAILABLE" || code == "SERVER_RESTARTED"}
}
