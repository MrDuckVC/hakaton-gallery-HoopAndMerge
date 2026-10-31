//go:build linux

package world

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFIFOWithoutWriterReturnsAndClosesDescriptors(t *testing.T) {
	for _, name := range []string{"catalog.json", "room.json", "a.jpg", "profile.json"} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t)
			path := filepath.Join(dir, name)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			// A subprocess bounds the regression even with the old blocking Open:
			// cancellation kills it and the OS closes all descriptors and goroutines.
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestFIFOReaderHelper$")
			cmd.Env = append(os.Environ(), "GALLERY_WORLD_FIFO_DIR="+dir, "GORACE=atexit_sleep_ms=0")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("FIFO without writer did not return promptly: %v (%v) %s", err, ctx.Err(), output)
			}
		})
	}
}

func TestFIFOReaderHelper(t *testing.T) {
	dir := os.Getenv("GALLERY_WORLD_FIFO_DIR")
	if dir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if _, err := (Files{Dir: dir}).Load(ctx); err == nil {
		t.Fatal("special file accepted")
	}
	// Load has returned synchronously: it starts no goroutines. Every opened
	// file/root descriptor must also have been closed, including the rejected FIFO.
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		target, readErr := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if readErr == nil && (target == dir || strings.HasPrefix(target, dir+"/")) {
			t.Fatalf("world descriptor leaked: %s -> %s", entry.Name(), target)
		}
	}
}
