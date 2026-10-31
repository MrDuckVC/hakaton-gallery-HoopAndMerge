package photopublisher

import (
	"context"
	"errors"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
)

type publishFake struct{ err error }

func (f publishFake) Publish(context.Context, auth.Session, string) (domain.Photo, error) {
	return domain.Photo{ID: "photo", UserID: "owner", Published: true, ContentType: "image/jpeg", ImageBytes: 42, ThumbnailBytes: 12}, f.err
}
func TestPublisherMapsOnlySuccessfulPublication(t *testing.T) {
	p, err := New(publishFake{}).PublishPhoto(t.Context(), auth.Session{}, "photo")
	if err != nil || !p.Published || p.OwnerID != "owner" || p.ImageURL != "/api/v1/photos/photo/image" || p.ThumbnailURL != "/api/v1/photos/photo/thumbnail" || p.ImageBytes != 42 || p.ThumbnailBytes != 12 {
		t.Fatal(p, err)
	}
	p, err = New(publishFake{err: auth.Unavailable}).PublishPhoto(t.Context(), auth.Session{}, "photo")
	if !errors.Is(err, auth.Unavailable) || p.ID != "" {
		t.Fatal(p, err)
	}
}
