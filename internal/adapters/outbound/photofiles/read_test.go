package photofiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
)

func TestReadPhotoPairBoundedAndConfined(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	id := "abcdef12-1234-4123-8123-123456789abc"
	if err = f.Save(t.Context(), id, []byte("original"), []byte("thumbnail")); err != nil {
		t.Fatal(err)
	}
	for _, thumbnail := range []bool{false, true} {
		data, e := f.Read(t.Context(), id, thumbnail)
		want := "original"
		if thumbnail {
			want = "thumbnail"
		}
		if e != nil || string(data) != want {
			t.Fatal(string(data), e)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = f.Read(ctx, id, false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = f.Read(t.Context(), "../outside", false); !errors.Is(err, auth.Validation) {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id, "image")
	if err = os.WriteFile(path, make([]byte, photo.MaxImageBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Read(t.Context(), id, false); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Read(t.Context(), id, false); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Read(t.Context(), id, false); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err = os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Read(t.Context(), id, false); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
}
