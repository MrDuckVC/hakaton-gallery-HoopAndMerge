package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestRunRejectsInvalidWorld(t *testing.T) {
	for _, args := range [][]string{{}, {"-dir", t.TempDir(), "-version", "gallery_c01_v2"}, {"-unknown"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out); err == nil {
			t.Fatal("invalid input accepted")
		}
		if out.Len() != 0 {
			t.Fatal("failed validation emitted a successful report")
		}
	}
}

func TestPreparedGallery(t *testing.T) {
	var out bytes.Buffer
	if err := run(t.Context(), []string{"-dir", "../../content/world", "-version", "gallery_c01_v2"}, &out); err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../docs/content/budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), committed) {
		t.Fatal("committed budget report is stale; regenerate it with worldcheck")
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.WorldVersion != "gallery_c01_v2" || len(got.Rooms) < 24 || len(got.Assets) < 72 {
		t.Fatalf("incomplete gallery: %s", out.String())
	}
	if got.CacheLimitBytes != got.TotalContentBytes/10 {
		t.Fatal("incorrect denominator")
	}
	var total int64
	for _, r := range got.Rooms {
		total += r.DescriptorBytes
		if r.PackageBytes+3145728 > got.CacheLimitBytes || r.HeadroomBytes != got.CacheLimitBytes-r.PackageBytes-3145728 {
			t.Fatalf("invalid budget: %+v", r)
		}
		if r.MaxSnapshotBytes <= 0 || r.MaxSnapshotBytes > 262144 {
			t.Fatal("invalid snapshot size")
		}
	}
	for _, a := range got.Assets {
		total += a.Bytes
		if len(a.SHA256) != 64 {
			t.Fatal("missing asset digest")
		}
	}
	if total != got.TotalContentBytes {
		t.Fatal("incorrect file accounting")
	}
}
