// Package testworld creates synthetic content exclusively for tests and Docker smoke checks.
// The production server never imports this package or falls back to these files.
package testworld

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// Write creates a valid 40-room world using genuine JPEG bytes in a new empty directory.
// High-quality grayscale keeps the byte budget valid with less instrumented decode work.
func Write(ctx context.Context, dir, version string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("fixture directory must be empty")
	}
	rng := rand.New(rand.NewPCG(4, 3))
	img := image.NewGray(image.Rect(0, 0, 896, 896))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Uint64())
	}
	refs := make([]map[string]string, 0, 40)
	assets := make([]map[string]string, 0, 40)
	spawns := make(map[string]any)
	vector := func(x, y, z float64) map[string]float64 { return map[string]float64{"x": x, "y": y, "z": z} }
	pose := func(room string) map[string]any {
		return map[string]any{"room_id": room, "position": vector(2, 0, 2), "rotation": map[string]int{"yaw": 0, "pitch": 0}}
	}
	save := func(name string, value any) error {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		b = append(b, '\n')
		return os.WriteFile(filepath.Join(dir, name), b, 0644)
	}
	for n := 0; n < 40; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		id := fmt.Sprintf("r%d", n)
		asset := fmt.Sprintf("a%d", n)
		next := fmt.Sprintf("r%d", (n+1)%40)
		door := fmt.Sprintf("d%d", n)
		img.Pix[0] = byte(n)
		var jpegBytes bytes.Buffer
		if err := jpeg.Encode(&jpegBytes, img, &jpeg.Options{Quality: 100}); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, asset+".jpg"), jpegBytes.Bytes(), 0644); err != nil {
			return err
		}
		room := map[string]any{"id": id, "world_version": version, "title": "Synthetic test room " + id, "bounds": map[string]any{"min": vector(0, 0, 0), "max": vector(10, 4, 10)}, "surfaces": []any{map[string]any{"id": "floor", "position": vector(5, -0.1, 5), "size": vector(10, 0.2, 10), "color": "#FFFFFF", "collidable": true}}, "doors": []any{map[string]any{"id": door, "target_room_id": next, "label": "Next", "position": vector(3, 0, 3), "interaction_radius_m": 1.5}}, "exhibits": []any{map[string]any{"id": "e" + id, "title": "Test image", "description": "Synthetic noise, not educational content", "image_asset_id": asset, "position": vector(5, 2, 5), "rotation_y": 0, "width_m": 1, "height_m": 1, "interaction_radius_m": 3}}, "guides": []any{}}
		if err := save(id+".json", room); err != nil {
			return err
		}
		refs = append(refs, map[string]string{"id": id, "file": id + ".json"})
		assets = append(assets, map[string]string{"id": asset, "file": asset + ".jpg"})
		spawns[door] = pose(next)
	}
	return save("catalog.json", map[string]any{"world_id": "test_world", "world_version": version, "rooms": refs, "assets": assets, "profiles": []any{}, "start": pose("r0"), "spawns": spawns})
}
