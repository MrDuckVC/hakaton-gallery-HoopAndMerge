package guide

import (
	"slices"
	"strings"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
)

// Settings contains only the administrator-editable instructions and source selection.
type Settings struct {
	DisplayName, Subtitle, Personality, Style, Topic string
	Constraints, MaterialIDs                         []string
}

// Override is scoped to an immutable content version and a personality.
type Override struct {
	WorldVersion, GuideID string
	Revision              int64
	Settings              Settings
}

// Snapshot is the detached profile selected at the queued-to-running transition.
type Snapshot struct {
	Profile  Profile
	Revision int64
}

// Clone prevents editor and worker callers from sharing mutable slices.
func (s Settings) Clone() Settings {
	s.Constraints = slices.Clone(s.Constraints)
	s.MaterialIDs = slices.Clone(s.MaterialIDs)
	return s
}

// SeedSettings includes all of the personality's prepared materials.
func SeedSettings(name, subtitle string, p Profile) Settings {
	ids := make([]string, len(p.Materials))
	for i, m := range p.Materials {
		ids[i] = m.ID
	}
	return Settings{DisplayName: name, Subtitle: subtitle, Personality: p.Personality, Style: p.Style, Topic: p.Topic, Constraints: slices.Clone(p.Constraints), MaterialIDs: ids}
}

// NormalizeSettings enforces Sunday limits after Unicode trimming and source ownership.
func NormalizeSettings(s Settings, base Profile) (Settings, error) {
	s = s.Clone()
	s.DisplayName = strings.TrimSpace(s.DisplayName)
	s.Subtitle = strings.TrimSpace(s.Subtitle)
	s.Personality = strings.TrimSpace(s.Personality)
	s.Style = strings.TrimSpace(s.Style)
	s.Topic = strings.TrimSpace(s.Topic)
	for i, c := range s.Constraints {
		s.Constraints[i] = strings.TrimSpace(c)
	}
	if !validText(s.DisplayName, 64) || !validText(s.Subtitle, 200) || len(s.MaterialIDs) > 100 {
		return Settings{}, auth.Validation
	}
	own := make(map[string]bool, len(base.Materials))
	for _, m := range base.Materials {
		own[m.ID] = true
	}
	seen := make(map[string]bool, len(s.MaterialIDs))
	for _, id := range s.MaterialIDs {
		if !own[id] || seen[id] {
			return Settings{}, auth.Validation
		}
		seen[id] = true
	}
	if err := s.Effective(base).Validate(); err != nil {
		return Settings{}, auth.Validation
	}
	return s, nil
}

// Effective selects immutable source text; administrators cannot change fixed system rules.
func (s Settings) Effective(base Profile) Profile {
	p := Profile{Personality: s.Personality, Style: s.Style, Topic: s.Topic, Constraints: slices.Clone(s.Constraints), Materials: make([]Material, 0, len(s.MaterialIDs))}
	for _, id := range s.MaterialIDs {
		for _, m := range base.Materials {
			if m.ID == id {
				p.Materials = append(p.Materials, m)
				break
			}
		}
	}
	return p.Clone()
}
