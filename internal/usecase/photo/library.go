package photo

import (
	"context"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// LibraryStore provides owner-scoped pages and transactional publication.
// Publish revalidates the session, calls authorize with the locked photo,
// and commits published=true before returning. Failures never return metadata.
type LibraryStore interface {
	Album(context.Context, string, int, string) ([]domain.Photo, string, error)
	GetPhoto(context.Context, string) (domain.Photo, error)
	Publish(context.Context, auth.Session, string, func(domain.Photo) error) (domain.Photo, error)
}

// Reader returns bounded, immutable bytes without exposing storage paths.
type Reader interface {
	Read(context.Context, string, bool) ([]byte, error)
}

// File is an authorized original or JPEG thumbnail.
type File struct {
	Bytes       []byte
	ContentType string
}

// Library handles albums, protected reads and irreversible publication without game state.
type Library struct {
	sessions Sessions
	store    LibraryStore
	files    Reader
}

// NewLibrary binds persisted identity and photo storage independently of the game.
func NewLibrary(sessions Sessions, store LibraryStore, files Reader) *Library {
	return &Library{sessions: sessions, store: store, files: files}
}

// Album lists only the authenticated owner's photos in descending keyset order.
func (s *Library) Album(ctx context.Context, token string, limit int, cursor string) ([]domain.Photo, string, error) {
	session, _, err := s.sessions.Authenticate(ctx, token)
	if err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 6 {
		return nil, "", auth.Validation
	}
	return s.store.Album(ctx, session.UserID, limit, cursor)
}

// Read authorizes metadata before opening any bytes; published photos still require login.
func (s *Library) Read(ctx context.Context, token, id string, thumbnail bool) (File, error) {
	session, _, err := s.sessions.Authenticate(ctx, token)
	if err != nil {
		return File{}, err
	}
	if !world.ValidID(id) {
		return File{}, auth.Validation
	}
	p, err := s.store.GetPhoto(ctx, id)
	if err != nil {
		return File{}, err
	}
	if p.UserID != session.UserID && !p.Published {
		return File{}, auth.Code("NOT_FOUND")
	}
	data, err := s.files.Read(ctx, p.ID, thumbnail)
	if err != nil {
		return File{}, err
	}
	size, mime := p.ImageBytes, p.ContentType
	if thumbnail {
		size, mime = p.ThumbnailBytes, "image/jpeg"
	}
	if int64(len(data)) != size || size < 1 || (mime != "image/jpeg" && mime != "image/webp") {
		return File{}, auth.Unavailable
	}
	return File{Bytes: data, ContentType: mime}, nil
}

// Publish permits only the owner, including repeat publication, within the storage transaction.
func (s *Library) Publish(ctx context.Context, session auth.Session, id string) (domain.Photo, error) {
	if !world.ValidID(id) {
		return domain.Photo{}, auth.Validation
	}
	return s.store.Publish(ctx, session, id, func(p domain.Photo) error {
		if p.UserID != session.UserID {
			return auth.Code("NOT_FOUND")
		}
		return nil
	})
}
