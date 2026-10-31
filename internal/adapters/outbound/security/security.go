// Package security implements password KDFs and cryptographically random credentials.
package security

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
)

const iterations = 600000

// Crypto uses the Go standard library and a fixed, versioned password encoding.
type Crypto struct{}

// HashPassword derives a salted verifier, checking caller cancellation around bounded KDF work.
func (Crypto) HashPassword(ctx context.Context, password string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", domain.Internal
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", domain.Internal
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("$pbkdf2-sha256$%d$%s$%s", iterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword also does full KDF work for missing or malformed stored verifiers.
// Only supported fixed parameters are accepted, preventing unbounded work from corrupt encodings.
func (Crypto) VerifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	salt := make([]byte, 16)
	expected := make([]byte, 32)
	valid := false
	parts := strings.Split(encoded, "$")
	if len(parts) == 5 && parts[0] == "" && parts[1] == "pbkdf2-sha256" && parts[2] == "600000" {
		decodedSalt, saltErr := base64.RawStdEncoding.DecodeString(parts[3])
		decodedKey, keyErr := base64.RawStdEncoding.DecodeString(parts[4])
		if saltErr == nil && keyErr == nil && len(decodedSalt) == 16 && len(decodedKey) == 32 {
			salt = decodedSalt
			expected = decodedKey
			valid = true
		}
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return false, domain.Internal
	}
	matches := subtle.ConstantTimeCompare(actual, expected) == 1
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return matches && valid, nil
}

// NewToken issues 256 random bits encoded as a cookie-safe opaque token.
func (c Crypto) NewToken() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, domain.Internal
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	return token, c.TokenHash(token), nil
}

// TokenHash derives the fixed-width database lookup key.
func (Crypto) TokenHash(token string) [32]byte { return sha256.Sum256([]byte(token)) }

// NewID issues a UUID v4 without database extensions.
func (Crypto) NewID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

// AvatarColor creates a server-owned public avatar color.
func (Crypto) AvatarColor() string {
	var raw [3]byte
	_, _ = rand.Read(raw[:])
	return "#" + strings.ToUpper(hex.EncodeToString(raw[:]))
}
