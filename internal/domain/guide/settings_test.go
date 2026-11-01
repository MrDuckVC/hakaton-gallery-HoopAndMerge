package guide

import (
	"errors"
	"strings"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
)

func TestSettingsNormalizeAndSelectOnlyOwnMaterials(t *testing.T) {
	base := Profile{Personality: "P", Style: "S", Topic: "T", Constraints: []string{"C"}, Materials: []Material{{ID: "own", Text: "knowledge", Sources: []string{"local"}}}}
	seed := SeedSettings("Name", "Subtitle", base)
	if len(seed.MaterialIDs) != 1 || seed.MaterialIDs[0] != "own" {
		t.Fatal(seed)
	}
	seed.DisplayName = "  Имя  "
	seed.Personality = "  Личность  "
	clean, err := NormalizeSettings(seed, base)
	if err != nil || clean.DisplayName != "Имя" || clean.Personality != "Личность" {
		t.Fatal(clean, err)
	}
	for _, ids := range [][]string{{"foreign"}, {"own", "own"}} {
		bad := seed
		bad.MaterialIDs = ids
		if _, err = NormalizeSettings(bad, base); !errors.Is(err, auth.Validation) {
			t.Fatalf("accepted %v: %v", ids, err)
		}
	}
	clean.MaterialIDs = []string{}
	p := clean.Effective(base)
	if len(p.Materials) != 0 || len(base.Materials) != 1 {
		t.Fatal("selection mutated base", p, base)
	}
}

func TestSettingsUnicodeAndInstructionBounds(t *testing.T) {
	base := Profile{Personality: "P", Style: "S", Topic: "T", Constraints: []string{"C"}}
	for _, change := range []func(*Settings){
		func(s *Settings) { s.DisplayName = strings.Repeat("я", 65) },
		func(s *Settings) { s.Subtitle = " \t " },
		func(s *Settings) { s.Topic = "a\x00b" },
		func(s *Settings) { s.Constraints = nil },
		func(s *Settings) { s.Constraints = []string{" "} },
		func(s *Settings) {
			s.Personality = strings.Repeat("я", 2000)
			s.Style = strings.Repeat("я", 1000)
			s.Topic = strings.Repeat("я", 1000)
		},
	} {
		s := SeedSettings("Name", "Subtitle", base)
		change(&s)
		if _, err := NormalizeSettings(s, base); !errors.Is(err, auth.Validation) {
			t.Fatal("invalid settings accepted", err)
		}
	}
	s := SeedSettings(strings.Repeat("я", 64), strings.Repeat("я", 200), base)
	if _, err := NormalizeSettings(s, base); err != nil {
		t.Fatal(err)
	}
}
