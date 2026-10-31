// Package images validates browser captures and creates bounded thumbnails.
package images

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/photo"
	photocase "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/photo"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Codec uses the Go JPEG decoder and upstream x/image WebP decoder (BSD-3-Clause).
type Codec struct{}

// Prepare checks dimensions before allocating decoded pixels; originals are never re-encoded.
func (Codec) Prepare(ctx context.Context, data []byte) (photocase.Prepared, error) {
	if err := ctx.Err(); err != nil {
		return photocase.Prepared{}, err
	}
	if len(data) > photo.MaxImageBytes {
		return photocase.Prepared{}, auth.Code("PAYLOAD_TOO_LARGE")
	}
	var cfg image.Config
	var decoded image.Image
	var err error
	kind := ""
	switch {
	case len(data) >= 2 && data[0] == 255 && data[1] == 216:
		kind = "image/jpeg"
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		kind = "image/webp"
		if !staticWebP(data) {
			return photocase.Prepared{}, auth.Validation
		}
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
	default:
		return photocase.Prepared{}, auth.Code("UNSUPPORTED_MEDIA_TYPE")
	}
	if err != nil || cfg.Width < 1 || cfg.Width > 1920 || cfg.Height < 1 || cfg.Height > 1080 {
		return photocase.Prepared{}, auth.Validation
	}
	if kind == "image/jpeg" {
		decoded, err = jpeg.Decode(bytes.NewReader(data))
	} else {
		decoded, err = webp.Decode(bytes.NewReader(data))
	}
	if err != nil || decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height {
		return photocase.Prepared{}, auth.Validation
	}
	if err = ctx.Err(); err != nil {
		return photocase.Prepared{}, err
	}
	w, h := cfg.Width, cfg.Height
	if max(w, h) > 320 {
		if w >= h {
			h = max(1, h*320/w)
			w = 320
		} else {
			w = max(1, w*320/h)
			h = 320
		}
	}
	thumb := image.NewRGBA(image.Rect(0, 0, w, h))
	stddraw.Draw(thumb, thumb.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	draw.CatmullRom.Scale(thumb, thumb.Bounds(), decoded, decoded.Bounds(), stddraw.Over, nil)
	var out bytes.Buffer
	for q := 85; q >= 5; q -= 10 {
		if err = ctx.Err(); err != nil {
			return photocase.Prepared{}, err
		}
		out.Reset()
		if err = jpeg.Encode(&out, thumb, &jpeg.Options{Quality: q}); err != nil {
			return photocase.Prepared{}, auth.Unavailable
		}
		if out.Len() <= photo.MaxThumbnailBytes {
			return photocase.Prepared{ContentType: kind, Width: cfg.Width, Height: cfg.Height, Thumbnail: out.Bytes()}, nil
		}
	}
	return photocase.Prepared{}, auth.Unavailable
}

func staticWebP(data []byte) bool {
	if uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return false
	}
	for offset := 12; offset < len(data); {
		if len(data)-offset < 8 {
			return false
		}
		kind := string(data[offset : offset+4])
		size := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		end := uint64(offset) + 8 + size
		if end > uint64(len(data)) || kind == "ANIM" || kind == "ANMF" || (kind == "VP8X" && size > 0 && data[offset+8]&2 != 0) {
			return false
		}
		offset = int(end + (size & 1))
		if offset > len(data) {
			return false
		}
	}
	return true
}
