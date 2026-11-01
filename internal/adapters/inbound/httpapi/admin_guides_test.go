package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	usecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
)

type adminProfiles struct {
	updates int
	err     error
}

func (a *adminProfiles) Guide(_ context.Context, id string) (usecase.AdminGuide, error) {
	return usecase.AdminGuide{Override: guide.Override{GuideID: id, WorldVersion: "world", Revision: 1, Settings: guide.Settings{DisplayName: "Name", Subtitle: "Subtitle", Personality: "P", Style: "S", Topic: "T", Constraints: []string{"C"}, MaterialIDs: []string{}}}, Materials: []guide.Material{{ID: "own", Text: strings.Repeat("я", 250), Sources: []string{"private source"}, ExhibitIDs: []string{"ex"}}}}, a.err
}
func (a *adminProfiles) Guides(ctx context.Context) ([]usecase.AdminGuide, error) {
	g, err := a.Guide(ctx, "guide")
	return []usecase.AdminGuide{g}, err
}
func (a *adminProfiles) UpdateGuide(ctx context.Context, id, _ string, _ int64, _ guide.Settings) (usecase.AdminGuide, error) {
	a.updates++
	return a.Guide(ctx, id)
}

const guideUpdate = `{"world_version":"world","expected_revision":1,"settings":{"display_name":"Name","subtitle":"Subtitle","personality":"P","style":"S","topic":"T","constraints":["C"],"material_ids":[]}}`

func TestAdminGuideAuthorizationDTOAndSharedPUTLimits(t *testing.T) {
	s, role, _ := adminServer()
	profiles := &adminProfiles{}
	s.ConfigureAdminGuides(profiles)
	for _, path := range []string{"/api/v1/admin/guides", "/api/v1/admin/guides/guide"} {
		assertResponse(t, perform(s, "GET", path, "", "", ""), 401, "UNAUTHENTICATED")
		role.roleErr = runtime.Forbidden
		assertResponse(t, perform(s, "GET", path, "", "", "token"), 403, "FORBIDDEN")
		role.roleErr = nil
		w := perform(s, "GET", path, "", "", "token")
		assertResponse(t, w, 200, "")
		if strings.Contains(w.Body.String(), "private source") {
			t.Fatal("source leaked")
		}
	}
	w := perform(s, "GET", "/api/v1/admin/guides/guide", "", "", "token")
	var body struct {
		Materials []struct {
			Excerpt string `json:"excerpt"`
		} `json:"available_materials"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Materials) != 1 || utf8.RuneCountInString(body.Materials[0].Excerpt) != 200 {
		t.Fatal(body, err)
	}
	assertResponse(t, perform(s, "PUT", "/api/v1/admin/guides/guide", guideUpdate, "", "token"), 403, "FORBIDDEN")
	for range 5 {
		assertResponse(t, perform(s, "PUT", "/api/v1/admin/guides/guide", guideUpdate, "http://localhost:8080", "token"), 200, "")
		assertResponse(t, perform(s, "PUT", "/api/v1/admin/rooms/room", roomUpdate, "http://localhost:8080", "token"), 200, "")
	}
	assertResponse(t, perform(s, "PUT", "/api/v1/admin/guides/guide", guideUpdate, "http://localhost:8080", "token"), 429, "RATE_LIMITED")
	if profiles.updates != 5 {
		t.Fatal(profiles.updates)
	}
}
func TestAdminGuideRejectsAmbiguousJSON(t *testing.T) {
	for _, body := range []string{`null`, `{}`, guideUpdate + ` {}`,
		strings.Replace(guideUpdate, `"settings":{`, `"settings":{"unexpected":true,`, 1),
		strings.Replace(guideUpdate, `"style":"S"`, `"style":"S","style":"T"`, 1),
		strings.Replace(guideUpdate, `"material_ids":[]`, `"material_ids":null`, 1),
		strings.Replace(guideUpdate, `"material_ids":[]`, `"material_ids":[1]`, 1),
		strings.Replace(guideUpdate, `"expected_revision":1`, `"expected_revision":9007199254740992`, 1),
	} {
		s, _, _ := adminServer()
		a := &adminProfiles{}
		s.ConfigureAdminGuides(a)
		w := perform(s, "PUT", "/api/v1/admin/guides/guide", body, "http://localhost:8080", "token")
		if w.Code != 400 || a.updates != 0 {
			t.Fatal(body, w.Code, w.Body, a.updates)
		}
	}
}
