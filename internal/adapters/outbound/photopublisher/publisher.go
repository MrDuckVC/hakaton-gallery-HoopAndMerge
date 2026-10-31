// Package photopublisher connects durable photo publication to the game's chat port.
package photopublisher

import (
	"context"
	"net/url"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
)

// Publication is the photo application boundary; success means published=true is committed.
type Publication interface {
	Publish(context.Context, auth.Session, string) (domain.Photo, error)
}

// Publisher adapts committed domain metadata to B06's existing port.
type Publisher struct{ photos Publication }

// New connects the photo application without coupling the two usecase packages.
func New(photos Publication) *Publisher { return &Publisher{photos: photos} }

// PublishPhoto returns chat metadata only after durable, authorized publication.
func (p *Publisher) PublishPhoto(ctx context.Context, session auth.Session, id string) (game.PublishedPhoto, error) {
	photo, err := p.photos.Publish(ctx, session, id)
	if err != nil {
		return game.PublishedPhoto{}, err
	}
	base := "/api/v1/photos/" + url.PathEscape(photo.ID)
	return game.PublishedPhoto{ID: photo.ID, OwnerID: photo.UserID, Caption: photo.Caption, RoomID: photo.RoomID, ExhibitID: photo.ExhibitID, Published: photo.Published, ImageURL: base + "/image", ImageBytes: photo.ImageBytes, ThumbnailURL: base + "/thumbnail", ThumbnailBytes: photo.ThumbnailBytes, Width: photo.Width, Height: photo.Height, CreatedAt: photo.CreatedAt}, nil
}
