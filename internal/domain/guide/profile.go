package guide

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// Material is a prepared local source; references never trigger network access.
type Material struct {
	ID, Text            string
	Sources, ExhibitIDs []string
}

// Profile describes an artistic reconstruction, never an authentic historical speaker.
type Profile struct {
	Personality, Style, Topic string
	Constraints               []string
	Materials                 []Material
}

func validText(s string, limit int) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) <= limit
}

// Validate bounds trusted instructions and rejects ambiguous material identities.
func (p Profile) Validate() error {
	if !validText(p.Personality, 2000) || !validText(p.Style, 1000) || !validText(p.Topic, 1000) || len(p.Constraints) == 0 || len(p.Constraints) > 20 || len(p.Materials) > 100 {
		return errors.New("invalid personality fields")
	}
	size := utf8.RuneCountInString(p.Personality + p.Style + p.Topic)
	for _, c := range p.Constraints {
		if !validText(c, 1000) {
			return errors.New("invalid personality constraint")
		}
		size += utf8.RuneCountInString(c)
	}
	if size > 4000 {
		return errors.New("personality instructions exceed 4000 code points")
	}
	seen := make(map[string]bool, len(p.Materials))
	for _, m := range p.Materials {
		if !world.ValidID(m.ID) || seen[m.ID] || !validText(m.Text, 8000) || len(m.Sources) == 0 || len(m.Sources) > 20 || len(m.ExhibitIDs) > 100 {
			return errors.New("invalid personality material")
		}
		seen[m.ID] = true
		for _, s := range m.Sources {
			if !validText(s, 1000) {
				return errors.New("invalid material source")
			}
		}
		for _, id := range m.ExhibitIDs {
			if !world.ValidID(id) {
				return errors.New("invalid material exhibit")
			}
		}
	}
	return nil
}

// Clone prevents callers from mutating the loaded catalog.
func (p Profile) Clone() Profile {
	p.Constraints = append([]string(nil), p.Constraints...)
	p.Materials = append([]Material(nil), p.Materials...)
	for i := range p.Materials {
		p.Materials[i].Sources = append([]string(nil), p.Materials[i].Sources...)
		p.Materials[i].ExhibitIDs = append([]string(nil), p.Materials[i].ExhibitIDs...)
	}
	return p
}
