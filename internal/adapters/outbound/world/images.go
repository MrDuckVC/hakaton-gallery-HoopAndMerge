package world

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image/jpeg"

	"golang.org/x/image/webp"
)

func imageType(ctx context.Context, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8 {
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			return "", fmt.Errorf("decode JPEG: %w", err)
		}
		return "image/jpeg", ctx.Err()
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return "", errors.New("expected static JPEG or WebP")
	}
	for offset := 12; offset < len(data); {
		if len(data)-offset < 8 {
			return "", errors.New("truncated WebP chunk")
		}
		kind := string(data[offset : offset+4])
		size := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		end := uint64(offset) + 8 + size
		if end > uint64(len(data)) {
			return "", errors.New("truncated WebP data")
		}
		if kind == "ANIM" || kind == "ANMF" || (kind == "VP8X" && size > 0 && data[offset+8]&2 != 0) {
			return "", errors.New("animated WebP is not supported")
		}
		offset = int(end + (size & 1))
		if offset > len(data) {
			return "", errors.New("missing WebP chunk padding")
		}
	}
	if _, err := webp.Decode(bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("decode WebP: %w", err)
	}
	return "image/webp", ctx.Err()
}
