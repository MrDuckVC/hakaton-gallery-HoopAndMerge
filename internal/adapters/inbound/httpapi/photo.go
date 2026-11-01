package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	photocase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/photo"
)

// Photos is the application's upload boundary; each Begin counts exactly one POST.
type Photos interface {
	Begin(context.Context, string) (photocase.Admission, error)
	Upload(context.Context, photocase.Admission, domain.Request) (domain.Photo, bool, error)
}

// ConfigurePhotos enables uploads; reads are connected through ConfigurePhotoLibrary.
func (s *Server) ConfigurePhotos(photos Photos) { s.photos = photos }

func (s *Server) servePhotos(w http.ResponseWriter, r *http.Request) bool {
	if s.photos == nil || r.Method != http.MethodPost || r.URL.Path != "/api/v1/photos" {
		return false
	}
	admission, err := s.photos.Begin(r.Context(), sessionToken(r))
	if err != nil {
		photoError(w, err)
		return true
	}
	input, err := readPhoto(w, r)
	if err != nil {
		photoError(w, err)
		return true
	}
	p, created, err := s.photos.Upload(r.Context(), admission, input)
	if err != nil {
		photoError(w, err)
		return true
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, publicPhoto(p))
	return true
}

func readPhoto(w http.ResponseWriter, r *http.Request) (domain.Request, error) {
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !domain.UUIDv4(keys[0]) {
		return domain.Request{}, auth.Validation
	}
	kind, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/form-data" || params["boundary"] == "" {
		return domain.Request{}, auth.Code("UNSUPPORTED_MEDIA_TYPE")
	}
	if r.ContentLength > domain.MaxMultipartBytes {
		return domain.Request{}, auth.Code("PAYLOAD_TOO_LARGE")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, domain.MaxMultipartBytes))
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			return domain.Request{}, auth.Code("PAYLOAD_TOO_LARGE")
		}
		return domain.Request{}, auth.Validation
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	fields := make(map[string]string, 4)
	seen := make(map[string]bool, 5)
	input := domain.Request{Key: keys[0]}
	for {
		part, e := reader.NextRawPart()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return domain.Request{}, auth.Validation
		}
		name := part.FormName()
		if seen[name] {
			return domain.Request{}, auth.Validation
		}
		seen[name] = true
		disposition, attributes, e := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if e != nil || disposition != "form-data" || len(part.Header.Values("Content-Disposition")) != 1 || part.Header.Get("Content-Transfer-Encoding") != "" {
			return domain.Request{}, auth.Validation
		}
		_, filePart := attributes["filename"]
		switch name {
		case "image":
			if !filePart || attributes["filename"] == "" {
				return domain.Request{}, auth.Validation
			}
		case "room_id", "movement_epoch", "caption", "exhibit_id":
			if filePart {
				return domain.Request{}, auth.Validation
			}
		default:
			return domain.Request{}, auth.Validation
		}
		data, e := io.ReadAll(part)
		if e != nil {
			return domain.Request{}, auth.Validation
		}
		if name == "image" {
			if len(data) > domain.MaxImageBytes {
				return domain.Request{}, auth.Code("PAYLOAD_TOO_LARGE")
			}
			input.Image = data
			continue
		}
		if !utf8.Valid(data) {
			return domain.Request{}, auth.Validation
		}
		fields[name] = strings.TrimSpace(string(data))
	}
	if !seen["image"] || !seen["room_id"] || !seen["movement_epoch"] || (seen["exhibit_id"] && fields["exhibit_id"] == "") {
		return domain.Request{}, auth.Validation
	}
	epoch := fields["movement_epoch"]
	if epoch == "" || epoch[0] < '1' || epoch[0] > '9' {
		return domain.Request{}, auth.Validation
	}
	for _, c := range epoch {
		if c < '0' || c > '9' {
			return domain.Request{}, auth.Validation
		}
	}
	input.Epoch, err = strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		return domain.Request{}, auth.Validation
	}
	input.RoomID = fields["room_id"]
	input.Caption = fields["caption"]
	input.ExhibitID = fields["exhibit_id"]
	return input.Normalize()
}

func photoError(w http.ResponseWriter, err error) {
	var rate photocase.RateError
	if errors.As(err, &rate) {
		writeError(w, 429, "RATE_LIMITED", "Слишком много загрузок", rate.RetryAfter)
		return
	}
	switch {
	case errors.Is(err, auth.Code("PAYLOAD_TOO_LARGE")):
		writeError(w, 413, "PAYLOAD_TOO_LARGE", "Снимок или запрос слишком большой", 0)
	case errors.Is(err, auth.Code("UNSUPPORTED_MEDIA_TYPE")):
		writeError(w, 415, "UNSUPPORTED_MEDIA_TYPE", "Требуется неподвижный JPEG или WebP", 0)
	case errors.Is(err, auth.Code("IDEMPOTENCY_CONFLICT")):
		writeError(w, 409, "IDEMPOTENCY_CONFLICT", "Ключ уже использован для другого снимка", 0)
	case errors.Is(err, auth.Code("OUT_OF_RANGE")):
		writeError(w, 403, "OUT_OF_RANGE", "Экспонат недоступен для осмотра", 0)
	default:
		applicationError(w, err)
	}
}

type photoDTO struct {
	ID             string    `json:"id"`
	OwnerID        string    `json:"owner_id"`
	Caption        string    `json:"caption"`
	RoomID         string    `json:"room_id"`
	ExhibitID      *string   `json:"exhibit_id"`
	Published      bool      `json:"published"`
	ImageURL       string    `json:"image_url"`
	ImageBytes     int64     `json:"image_bytes"`
	ThumbnailURL   string    `json:"thumbnail_url"`
	ThumbnailBytes int64     `json:"thumbnail_bytes"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	CreatedAt      time.Time `json:"created_at"`
}

func publicPhoto(p domain.Photo) photoDTO {
	base := "/api/v1/photos/" + url.PathEscape(p.ID)
	return photoDTO{ID: p.ID, OwnerID: p.UserID, Caption: p.Caption, RoomID: p.RoomID, ExhibitID: p.ExhibitID, Published: p.Published, ImageURL: base + "/image", ImageBytes: p.ImageBytes, ThumbnailURL: base + "/thumbnail", ThumbnailBytes: p.ThumbnailBytes, Width: p.Width, Height: p.Height, CreatedAt: p.CreatedAt}
}
