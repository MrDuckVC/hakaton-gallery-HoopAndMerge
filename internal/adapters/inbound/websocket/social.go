package websocket

import (
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/game"
)

type photoDTO struct {
	ID             string  `json:"id"`
	OwnerID        string  `json:"owner_id"`
	Caption        string  `json:"caption"`
	RoomID         string  `json:"room_id"`
	ExhibitID      *string `json:"exhibit_id"`
	Published      bool    `json:"published"`
	ImageURL       string  `json:"image_url"`
	ImageBytes     int64   `json:"image_bytes"`
	ThumbnailURL   string  `json:"thumbnail_url"`
	ThumbnailBytes int64   `json:"thumbnail_bytes"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	CreatedAt      string  `json:"created_at"`
}
type authorDTO struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}
type chatDTO struct {
	MessageID string    `json:"message_id"`
	Author    authorDTO `json:"author"`
	Text      string    `json:"text"`
	Photo     *photoDTO `json:"photo"`
}

func publicChat(c game.ChatMessage) chatDTO {
	dto := chatDTO{MessageID: c.ID, Author: authorDTO{UserID: c.AuthorID, DisplayName: c.DisplayName}, Text: c.Text}
	if p := c.Photo; p != nil {
		dto.Photo = &photoDTO{ID: p.ID, OwnerID: p.OwnerID, Caption: p.Caption, RoomID: p.RoomID, ExhibitID: p.ExhibitID, Published: p.Published, ImageURL: p.ImageURL, ImageBytes: p.ImageBytes, ThumbnailURL: p.ThumbnailURL, ThumbnailBytes: p.ThumbnailBytes, Width: p.Width, Height: p.Height, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339Nano)}
	}
	return dto
}
