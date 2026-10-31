package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/config"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/testworld"
)

func TestPrepareWorld(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{WorldDir: dir, WorldVersion: "v1"}
	if _, err := prepareWorld(t.Context(), cfg); err == nil {
		t.Fatal("missing catalog accepted")
	}
	if err := testworld.Write(t.Context(), dir, "v1"); err != nil {
		t.Fatal(err)
	}
	c, err := prepareWorld(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.Start(t.Context())
	if err != nil || state.RoomID != "r0" || state.WorldVersion != "v1" {
		t.Fatal(state, err)
	}
	cfg.WorldVersion = "other"
	if _, err := prepareWorld(t.Context(), cfg); err == nil {
		t.Fatal("wrong version accepted")
	}
	cfg.WorldVersion = "v1"
	if err := os.WriteFile(filepath.Join(dir, "a0.jpg"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareWorld(t.Context(), cfg); err == nil || !strings.Contains(err.Error(), "world") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := prepareWorld(ctx, cfg); err == nil {
		t.Fatal("ignored cancellation")
	}
}
