package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	usecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
)

// AdminGuides is the administrative personality application boundary.
type AdminGuides interface {
	Guides(context.Context) ([]usecase.AdminGuide, error)
	Guide(context.Context, string) (usecase.AdminGuide, error)
	UpdateGuide(context.Context, string, string, int64, guide.Settings) (usecase.AdminGuide, error)
}

// ConfigureAdminGuides shares existing administrative authentication and PUT limits.
func (s *Server) ConfigureAdminGuides(g AdminGuides) { s.admin.guides = g }

type guideSettingsDTO struct {
	DisplayName string   `json:"display_name"`
	Subtitle    string   `json:"subtitle"`
	Personality string   `json:"personality"`
	Style       string   `json:"style"`
	Topic       string   `json:"topic"`
	Constraints []string `json:"constraints"`
	MaterialIDs []string `json:"material_ids"`
}
type materialChoiceDTO struct {
	ID         string   `json:"id"`
	Excerpt    string   `json:"excerpt"`
	ExhibitIDs []string `json:"exhibit_ids"`
}
type adminGuideDTO struct {
	WorldVersion       string              `json:"world_version"`
	GuideID            string              `json:"guide_id"`
	Revision           int64               `json:"revision"`
	Settings           guideSettingsDTO    `json:"settings"`
	AvailableMaterials []materialChoiceDTO `json:"available_materials"`
}

func publicAdminGuide(g usecase.AdminGuide) adminGuideDTO {
	o := g.Override
	p := o.Settings
	settings := guideSettingsDTO{p.DisplayName, p.Subtitle, p.Personality, p.Style, p.Topic, append([]string{}, p.Constraints...), append([]string{}, p.MaterialIDs...)}
	materials := make([]materialChoiceDTO, len(g.Materials))
	for i, m := range g.Materials {
		excerpt := []rune(m.Text)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200]
		}
		materials[i] = materialChoiceDTO{ID: m.ID, Excerpt: string(excerpt), ExhibitIDs: append([]string{}, m.ExhibitIDs...)}
	}
	return adminGuideDTO{WorldVersion: o.WorldVersion, GuideID: o.GuideID, Revision: o.Revision, Settings: settings, AvailableMaterials: materials}
}
func (s *Server) serveAdminGuides(w http.ResponseWriter, r *http.Request) bool {
	service := s.admin.guides
	if service == nil {
		return false
	}
	path := r.URL.EscapedPath()
	if path == "/api/v1/admin/guides" && r.Method == http.MethodGet {
		guides, err := service.Guides(r.Context())
		if err != nil {
			applicationError(w, err)
			return true
		}
		type summary struct {
			GuideID     string `json:"guide_id"`
			DisplayName string `json:"display_name"`
			Revision    int64  `json:"revision"`
		}
		list := make([]summary, len(guides))
		for i, g := range guides {
			list[i] = summary{g.Override.GuideID, g.Override.Settings.DisplayName, g.Override.Revision}
		}
		writeJSON(w, 200, struct {
			Guides []summary `json:"guides"`
		}{list})
		return true
	}
	if !strings.HasPrefix(path, "/api/v1/admin/guides/") || (r.Method != http.MethodGet && r.Method != http.MethodPut) {
		return false
	}
	segment := strings.TrimPrefix(path, "/api/v1/admin/guides/")
	id, err := url.PathUnescape(segment)
	if err != nil || strings.Contains(segment, "/") || !world.ValidResourceID(id) {
		applicationError(w, auth.Validation)
		return true
	}
	var result usecase.AdminGuide
	if r.Method == http.MethodGet {
		result, err = service.Guide(r.Context(), id)
	} else {
		root, ok := readAdminObject(w, r)
		if !ok {
			return true
		}
		version, vok := root["world_version"].(string)
		revision, rok := adminInteger(root["expected_revision"], 1, world.MaxSafeInteger)
		settings, sok := adminGuideSettings(root["settings"])
		if !adminKeys(root, "world_version", "expected_revision", "settings") || !vok || !world.ValidID(version) || !rok || !sok {
			applicationError(w, auth.Validation)
			return true
		}
		result, err = service.UpdateGuide(r.Context(), id, version, revision, settings)
	}
	if err != nil {
		applicationError(w, err)
	} else {
		writeJSON(w, 200, publicAdminGuide(result))
	}
	return true
}
func adminStrings(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i], ok = item.(string)
		if !ok {
			return nil, false
		}
	}
	return out, true
}
func adminGuideSettings(value any) (guide.Settings, bool) {
	m, ok := value.(map[string]any)
	if !ok || !adminKeys(m, "display_name", "subtitle", "personality", "style", "topic", "constraints", "material_ids") {
		return guide.Settings{}, false
	}
	var s guide.Settings
	fields := []struct {
		key string
		dst *string
	}{{"display_name", &s.DisplayName}, {"subtitle", &s.Subtitle}, {"personality", &s.Personality}, {"style", &s.Style}, {"topic", &s.Topic}}
	for _, f := range fields {
		*f.dst, ok = m[f.key].(string)
		if !ok {
			return guide.Settings{}, false
		}
	}
	s.Constraints, ok = adminStrings(m["constraints"])
	if !ok {
		return guide.Settings{}, false
	}
	s.MaterialIDs, ok = adminStrings(m["material_ids"])
	return s, ok
}
