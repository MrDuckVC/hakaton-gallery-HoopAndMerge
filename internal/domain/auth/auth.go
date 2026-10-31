// Package auth defines account and session invariants without transport or storage dependencies.
package auth

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Code is an application error safe to map at transport boundaries.
type Code string

const (
	Validation         Code = "VALIDATION_ERROR"
	Unauthenticated    Code = "UNAUTHENTICATED"
	InvalidCredentials Code = "INVALID_CREDENTIALS"
	UsernameTaken      Code = "USERNAME_TAKEN"
	Unavailable        Code = "SERVICE_UNAVAILABLE"
	Internal           Code = "INTERNAL_ERROR"
)

func (c Code) Error() string { return string(c) }

// User contains only public account attributes.
type User struct{ ID, Username, DisplayName, AvatarColor string }

// Account adds the encoded password verifier used inside the authentication boundary.
type Account struct {
	User         User
	PasswordHash string
}

// Session identifies one fixed-lifetime login. Tokens are never persisted in plaintext.
type Session struct {
	ID, UserID           string
	TokenHash            [32]byte
	CreatedAt, ExpiresAt time.Time
	RevokedAt            *time.Time
}

// Valid reports whether the session may authorize an operation at now.
func (s Session) Valid(now time.Time) bool {
	return s.ID != "" && s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

// Credentials normalizes public text while preserving password bytes exactly.
func Credentials(username, displayName, password string, register bool) (string, string, error) {
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if !utf8.ValidString(username) || !utf8.ValidString(displayName) || !utf8.ValidString(password) || len(username) < 3 || len(username) > 32 || len(password) < 8 || len(password) > 128 {
		return "", "", Validation
	}
	for _, c := range username {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return "", "", Validation
		}
	}
	if register && (strings.ContainsRune(displayName, '\x00') || utf8.RuneCountInString(displayName) < 1 || utf8.RuneCountInString(displayName) > 32) {
		return "", "", Validation
	}
	return strings.ToLower(username), displayName, nil
}
