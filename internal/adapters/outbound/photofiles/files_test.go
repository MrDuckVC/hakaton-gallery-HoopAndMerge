package photofiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const testID = "abcdef12-1234-4123-8123-123456789abc"

func TestAtomicPairAndOwnCleanup(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx := context.Background()
	if err = f.Save(ctx, testID, []byte("original"), []byte("thumbnail")); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"image": "original", "thumbnail.jpg": "thumbnail"} {
		b, e := os.ReadFile(filepath.Join(dir, testID, name))
		if e != nil || string(b) != want {
			t.Fatal(string(b), e)
		}
	}
	if err = f.Save(ctx, testID, []byte("wrong"), nil); err == nil {
		t.Fatal("overwrite")
	}
	b, _ := os.ReadFile(filepath.Join(dir, testID, "image"))
	if string(b) != "original" {
		t.Fatal("foreign files changed")
	}
	if err = f.Delete(ctx, testID); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal(entries)
	}
}
func TestFilesCancelInvalidAndForeign(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = f.Save(ctx, testID, []byte("a"), []byte("b")); err == nil {
		t.Fatal("cancel accepted")
	}
	for _, id := range []string{"../escape", "", ".staging-x"} {
		if err = f.Save(context.Background(), id, nil, nil); err == nil {
			t.Fatal("unsafe id")
		}
		if err = f.Delete(context.Background(), id); err == nil {
			t.Fatal("unsafe delete")
		}
	}
	if err = os.Mkdir(filepath.Join(dir, ".staging-foreign"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = f.Delete(context.Background(), testID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, ".staging-foreign")); err != nil {
		t.Fatal("foreign staging removed")
	}
}
func TestFailedRenameCleansOnlyOwnedStage(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = os.WriteFile(filepath.Join(dir, testID), []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(context.Background(), testID, []byte("a"), []byte("b")); err == nil {
		t.Fatal("collision accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != testID {
		t.Fatal(entries)
	}
}
