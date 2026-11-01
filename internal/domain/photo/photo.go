// Package photo defines immutable saved photographs and upload identity.
package photo

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

const (
	MaxImageBytes     = 2 * 1024 * 1024
	MaxMultipartBytes = MaxImageBytes + 64*1024
	MaxThumbnailBytes = 128 * 1024
)

// Photo is committed only after both private files are durable.
type Photo struct {
	ID, UserID, Key, WorldVersion, RoomID, Caption, ContentType string
	ExhibitID                                                   *string
	Epoch                                                       int64
	Fingerprint                                                 [32]byte
	ImageBytes, ThumbnailBytes                                  int64
	Width, Height                                               int
	Published                                                   bool
	CreatedAt                                                   time.Time
}

// Request contains the original bytes and normalized capture context, never wire objects.
type Request struct {
	Key, RoomID, Caption, ExhibitID string
	Epoch                           int64
	Image                           []byte
}

// Normalize validates the upload's stable identity before accessing storage.
func (r Request) Normalize() (Request, error) {
	r.Key = strings.ToLower(r.Key)
	r.RoomID = strings.TrimSpace(r.RoomID)
	r.Caption = strings.TrimSpace(r.Caption)
	r.ExhibitID = strings.TrimSpace(r.ExhibitID)
	if !UUIDv4(r.Key) || !world.ValidID(r.RoomID) || (r.ExhibitID != "" && !world.ValidID(r.ExhibitID)) || r.Epoch < 1 || r.Epoch > world.MaxSafeInteger || !utf8.ValidString(r.Caption) || strings.ContainsRune(r.Caption, 0) || utf8.RuneCountInString(r.Caption) > 200 || len(r.Image) == 0 {
		return Request{}, auth.Validation
	}
	if len(r.Image) > MaxImageBytes {
		return Request{}, auth.Code("PAYLOAD_TOO_LARGE")
	}
	return r, nil
}

// UUIDv4 checks the key independently of a UUID library or transport.
func UUIDv4(s string) bool {
	if len(s) != 36 || s[14] != '4' || !strings.ContainsRune("89abAB", rune(s[19])) {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// Fingerprint hashes original bytes and length-delimited normalized fields.
func (r Request) Fingerprint() [32]byte {
	h := sha256.New()
	fileHash := sha256.Sum256(r.Image)
	_, _ = h.Write(fileHash[:])
	var size [8]byte
	for _, v := range []string{r.RoomID, r.Caption, r.ExhibitID} {
		binary.BigEndian.PutUint64(size[:], uint64(len(v)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(v))
	}
	binary.BigEndian.PutUint64(size[:], uint64(r.Epoch))
	_, _ = h.Write(size[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
