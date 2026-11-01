package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	worldfiles "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/world"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/world"
)

type curation struct {
	Rooms []struct {
		ID        string `json:"id"`
		GuideID   string `json:"guide_id"`
		Question  string `json:"question"`
		Paintings []struct {
			ID                string `json:"id"`
			Artist            string `json:"artist"`
			OriginalTitle     string `json:"original_title"`
			VisualDescription string `json:"visual_description"`
			Date              string `json:"date"`
			Medium            string `json:"medium"`
			ObjectURL         string `json:"object_url"`
			ImageURL          string `json:"image_url"`
			Rights            string `json:"rights"`
			RightsURL         string `json:"rights_url"`
			SHA256            string `json:"sha256"`
			File              string `json:"file"`
			Bytes             int64  `json:"bytes"`
			OpenAccess        bool   `json:"open_access"`
		} `json:"paintings"`
	} `json:"rooms"`
}

func TestGalleryContent(t *testing.T) {
	loaded, err := (worldfiles.Files{Dir: "../../content/world"}).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Run("ProvenanceAndEducationalCoverage", func(t *testing.T) { checkGalleryProvenance(t, loaded) })
	t.Run("InteractionRoutes", func(t *testing.T) { checkGalleryRoutes(t, loaded) })
}

func checkGalleryProvenance(t *testing.T, loaded world.LoadedWorld) {
	t.Helper()
	const dir = "../../content/world"
	body, err := os.ReadFile("../../content/gallery.json")
	if err != nil {
		t.Fatal(err)
	}
	var source curation
	if err := json.Unmarshal(body, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Rooms) != len(loaded.Input.Rooms) || len(loaded.Profiles) != 3 {
		t.Fatal("missing curated rooms or artists")
	}
	rooms := map[string]domain.Room{}
	for _, r := range loaded.Input.Rooms {
		rooms[r.ID] = r
	}
	hashes := map[string]bool{}
	for _, r := range source.Rooms {
		geometry, ok := rooms[r.ID]
		if !ok || len(r.Paintings) != 3 || strings.TrimSpace(r.Question) == "" || len(geometry.Guides) != 1 || geometry.Guides[0].ID != r.GuideID {
			t.Fatalf("missing educational content in %s", r.ID)
		}
		profile, ok := loaded.Profiles[r.GuideID]
		if !ok || !strings.Contains(strings.ToLower(profile.Personality), "реконструкц") || !strings.Contains(strings.ToLower(geometry.Guides[0].Subtitle), "реконструкц") {
			t.Fatal("artist must be identified as reconstruction")
		}
		known := map[string]bool{}
		for _, m := range profile.Materials {
			for _, id := range m.ExhibitIDs {
				known[id] = true
			}
		}
		for _, p := range r.Paintings {
			if p.VisualDescription == "" {
				t.Fatalf("missing museum visual evidence: %s", p.ID)
			}
			if p.Artist == "" || p.OriginalTitle == "" || p.Date == "" || p.Medium == "" || p.Rights == "" || !p.OpenAccess || !strings.HasPrefix(p.ObjectURL, "https://www.nga.gov/") || !strings.HasPrefix(p.ImageURL, "https://api.nga.gov/iiif/") || p.RightsURL == "" {
				t.Fatalf("incomplete provenance: %s", p.ID)
			}
			if !known["exhibit_"+p.ID] {
				t.Fatalf("guide has no material for %s", p.ID)
			}
			data, err := os.ReadFile(filepath.Join(dir, p.File))
			if err != nil {
				t.Fatal(err)
			}
			hash := digest(data)
			if hash != p.SHA256 || int64(len(data)) != p.Bytes || hashes[hash] {
				t.Fatalf("changed or duplicate image: %s", p.ID)
			}
			hashes[hash] = true
		}
	}
}

func checkGalleryRoutes(t *testing.T, loaded world.LoadedWorld) {
	t.Helper()
	if err := domain.Validate(loaded.Input); err != nil {
		t.Fatal(err)
	}
	for _, r := range loaded.Input.Rooms {
		center := player.Vector{X: 6, Z: 6}
		check := func(name string, approach, target player.Vector, radius float64) {
			t.Helper()
			camera := approach
			camera.Y = 1.65
			if !domain.Sweep(r, center, approach) || math.Hypot(approach.X-target.X, approach.Z-target.Z) > radius || !domain.Visible(r, camera, target) {
				t.Fatalf("%s/%s is not reachable and visible", r.ID, name)
			}
		}
		for _, e := range r.Exhibits {
			check(e.ID, player.Vector{X: e.Position.X, Z: e.Position.Z + 2}, e.Position, e.Radius)
		}
		for _, g := range r.Guides {
			check(g.ID, player.Vector{X: g.Position.X, Z: g.Position.Z - 1.5}, g.Position, g.Radius)
		}
		for _, d := range r.Doors {
			check(d.ID, d.Position, d.Position, d.Radius)
		}
	}
	for id, p := range loaded.Input.Spawns {
		var target domain.Room
		for _, r := range loaded.Input.Rooms {
			if r.ID == p.RoomID {
				target = r
			}
		}
		if !domain.Sweep(target, p.Position, player.Vector{X: 6, Z: 6}) {
			t.Fatalf("spawn %s is trapped", id)
		}
	}
}
