package images

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/photo"
)

func jpegImage(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 255, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, im, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestPrepareJPEGAndWebP(t *testing.T) {
	webp, err := os.ReadFile("../world/testdata/static.webp")
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{jpegImage(t, 1920, 1080), jpegImage(t, 1, 1), webp} {
		out, e := (Codec{}).Prepare(context.Background(), data)
		if e != nil {
			t.Fatal(e)
		}
		thumb, e := jpeg.Decode(bytes.NewReader(out.Thumbnail))
		if e != nil || thumb.Bounds().Dx() > 320 || thumb.Bounds().Dy() > 320 || len(out.Thumbnail) > photo.MaxThumbnailBytes || out.Width < 1 || out.Height < 1 {
			t.Fatal(out, e)
		}
	}
}
func TestRejectImages(t *testing.T) {
	webp, _ := os.ReadFile("../world/testdata/static.webp")
	animated := append(append([]byte{}, webp...), []byte("ANIM\x00\x00\x00\x00")...)
	binary.LittleEndian.PutUint32(animated[4:8], uint32(len(animated)-8))
	for _, tc := range []struct {
		data []byte
		code auth.Code
	}{{[]byte("GIF89a"), "UNSUPPORTED_MEDIA_TYPE"}, {[]byte{255, 216, 1}, auth.Validation}, {webp[:len(webp)-1], auth.Validation}, {animated, auth.Validation}, {jpegImage(t, 1921, 100), auth.Validation}, {jpegImage(t, 100, 1081), auth.Validation}} {
		if _, err := (Codec{}).Prepare(context.Background(), tc.data); !errors.Is(err, tc.code) {
			t.Fatalf("got %v want %v", err, tc.code)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Codec{}).Prepare(ctx, webp); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
