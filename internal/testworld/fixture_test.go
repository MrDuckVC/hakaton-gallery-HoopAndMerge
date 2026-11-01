package testworld_test

import (
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/testworld"
)

// The synthetic budget must not require repeatedly decoding large colour images
// in every instrumented server test. C01 still has separate real-image coverage.
func TestFixtureBoundsDecodedPixels(t *testing.T) {
	dir := t.TempDir()
	if err := testworld.Write(t.Context(), dir, "fixture_test"); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "a0.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	config, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if config.Width > 896 || config.Height > 896 || config.ColorModel != color.GrayModel {
		t.Fatalf("expensive fixture decode: %dx%d, grayscale=%v", config.Width, config.Height, config.ColorModel == color.GrayModel)
	}
}
