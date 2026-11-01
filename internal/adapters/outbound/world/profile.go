package world

import domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"

type profile struct {
	Personality string     `json:"personality"`
	Style       string     `json:"style"`
	Topic       string     `json:"topic"`
	Constraints []string   `json:"constraints"`
	Materials   []material `json:"materials"`
}
type material struct {
	ID         string   `json:"id"`
	Text       string   `json:"text"`
	Sources    []string `json:"sources"`
	ExhibitIDs []string `json:"exhibit_ids"`
}

func (p profile) domain() domain.Profile {
	out := domain.Profile{Personality: p.Personality, Style: p.Style, Topic: p.Topic, Constraints: p.Constraints, Materials: make([]domain.Material, 0, len(p.Materials))}
	for _, m := range p.Materials {
		out.Materials = append(out.Materials, domain.Material{ID: m.ID, Text: m.Text, Sources: m.Sources, ExhibitIDs: m.ExhibitIDs})
	}
	return out
}

// ParseProfile decodes the same strict server-only format used by world loading.
func ParseProfile(data []byte) (domain.Profile, error) {
	var dto profile
	if err := decode(data, &dto); err != nil {
		return domain.Profile{}, err
	}
	p := dto.domain()
	if err := p.Validate(); err != nil {
		return domain.Profile{}, err
	}
	return p, nil
}
