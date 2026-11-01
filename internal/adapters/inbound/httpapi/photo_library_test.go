package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	photocase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/photo"
)

type photoLibraryFake struct {
	err               error
	limit             int
	cursor, token, id string
	thumbnail         bool
}

func (f *photoLibraryFake) Album(_ context.Context, token string, limit int, cursor string) ([]domain.Photo, string, error) {
	f.token, f.limit, f.cursor = token, limit, cursor
	return nil, "", f.err
}
func (f *photoLibraryFake) Read(_ context.Context, token, id string, thumbnail bool) (photocase.File, error) {
	f.token, f.id, f.thumbnail = token, id, thumbnail
	return photocase.File{Bytes: []byte("exact"), ContentType: "image/jpeg"}, f.err
}
func TestPhotoLibraryHTTPRoutesAndPagination(t *testing.T) {
	f := &photoLibraryFake{}
	s := NewServer("")
	s.ConfigurePhotoLibrary(f)
	for _, url := range []string{"/api/v1/photos", "/api/v1/photos?limit=1&cursor=opaque"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", url, nil)
		r.Header.Set("Cookie", "museum_session=token")
		s.Handler.ServeHTTP(w, r)
		if w.Code != 200 || f.token != "token" {
			t.Fatal(w.Code, w.Body.String())
		}
		if strings.Contains(url, "limit=1") {
			if f.limit != 1 || f.cursor != "opaque" {
				t.Fatal(f)
			}
		} else if f.limit != 6 {
			t.Fatal(f.limit)
		}
		var result struct {
			Items []any   `json:"items"`
			Next  *string `json:"next_cursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Items == nil || result.Next != nil {
			t.Fatal(w.Body.String(), err)
		}
	}
	for _, query := range []string{"limit=0", "limit=7", "limit=", "limit=x", "limit=1&limit=2", "cursor=x&cursor=y", "limit=%xx"} {
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/photos?"+query, nil))
		if w.Code != 400 {
			t.Fatal(query, w.Code, w.Body.String())
		}
	}
	for _, thumbnail := range []bool{false, true} {
		suffix := "image"
		if thumbnail {
			suffix = "thumbnail"
		}
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/photos/p/"+suffix, nil))
		if w.Code != 200 || w.Body.String() != "exact" || f.thumbnail != thumbnail || f.id != "p" {
			t.Fatal(w.Code, w.Body.String(), f)
		}
		for header, want := range map[string]string{"Content-Type": "image/jpeg", "Content-Length": "5", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"} {
			if w.Header().Get(header) != want {
				t.Fatal(header, w.Header())
			}
		}
	}
	for _, tc := range []struct {
		err  error
		code int
	}{{auth.Unauthenticated, 401}, {auth.Code("NOT_FOUND"), 404}, {auth.Code("INVALID_CURSOR"), 400}, {auth.Unavailable, 503}} {
		f.err = tc.err
		for _, url := range []string{"/api/v1/photos", "/api/v1/photos/p/image", "/api/v1/photos/p/thumbnail"} {
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
			if w.Code != tc.code {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
}
