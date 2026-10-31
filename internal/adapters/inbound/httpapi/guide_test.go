package httpapi

import (
	"context"
	"testing"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
)

type historyFake struct {
	limit                  int
	guide, request, before string
}

func (f *historyFake) History(_ context.Context, token, g string, limit int, before string) ([]domain.Generation, string, error) {
	f.limit = limit
	f.guide = g
	f.before = before
	if token == "" {
		return nil, "", auth.Unauthenticated
	}
	if before == "bad" {
		return nil, "", auth.Code("INVALID_CURSOR")
	}
	return []domain.Generation{}, "", nil
}
func (f *historyFake) ByRequest(_ context.Context, _, g, r string) (domain.Generation, error) {
	f.guide = g
	f.request = r
	return domain.Generation{}, auth.Code("NOT_FOUND")
}
func TestGuideHistoryHTTP(t *testing.T) {
	s, _ := authServer(false)
	f := &historyFake{}
	s.ConfigureGuides(f)
	for _, tc := range []struct {
		path, token string
		status      int
		code        string
	}{
		{"/api/v1/guides/guide/generations", "owner", 200, ""},
		{"/api/v1/guides/guide/generations?limit=50", "owner", 200, ""},
		{"/api/v1/guides/guide/generations", "", 401, "UNAUTHENTICATED"},
		{"/api/v1/guides/guide/generations?limit=0", "owner", 400, "VALIDATION_ERROR"},
		{"/api/v1/guides/guide/generations?limit=51", "owner", 400, "VALIDATION_ERROR"},
		{"/api/v1/guides/guide/generations?limit=2&limit=3", "owner", 400, "VALIDATION_ERROR"},
		{"/api/v1/guides/guide/generations?before=bad", "owner", 400, "INVALID_CURSOR"},
		{"/api/v1/guides/guide/generations/by-request/00000000-0000-4000-8000-000000000001", "owner", 404, "NOT_FOUND"},
		{"/api/v1/guides/guide/generations/by-request/bad", "owner", 400, "VALIDATION_ERROR"},
		{"/api/v1/guides/guide/messages", "owner", 404, "NOT_FOUND"},
	} {
		w := perform(s, "GET", tc.path, "", "", tc.token)
		assertResponse(t, w, tc.status, tc.code)
	}
}
