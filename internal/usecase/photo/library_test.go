package photo

import (
	"context"
	"errors"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
)

type libraryStoreFake struct {
	p                       domain.Photo
	err                     error
	user, cursor            string
	limit, reads, publishes int
}

func (f *libraryStoreFake) Album(_ context.Context, user string, limit int, cursor string) ([]domain.Photo, string, error) {
	f.user, f.limit, f.cursor = user, limit, cursor
	return []domain.Photo{f.p}, "next", f.err
}
func (f *libraryStoreFake) GetPhoto(context.Context, string) (domain.Photo, error) { return f.p, f.err }
func (f *libraryStoreFake) Publish(_ context.Context, _ auth.Session, _ string, authorize func(domain.Photo) error) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	if err := authorize(f.p); err != nil {
		return domain.Photo{}, err
	}
	f.publishes++
	f.p.Published = true
	return f.p, nil
}

type readFilesFake struct {
	data      []byte
	err       error
	calls     int
	thumbnail bool
}

func (f *readFilesFake) Read(_ context.Context, _ string, thumbnail bool) ([]byte, error) {
	f.calls++
	f.thumbnail = thumbnail
	return f.data, f.err
}
func TestLibraryReadOwnershipAndExactBytes(t *testing.T) {
	for _, tc := range []struct {
		name, owner          string
		published, thumbnail bool
		want                 error
	}{
		{"owner", "u", false, false, nil},
		{"thumbnail", "u", false, true, nil},
		{"private", "other", false, false, auth.Code("NOT_FOUND")},
		{"privateThumbnail", "other", false, true, auth.Code("NOT_FOUND")},
		{"published", "other", true, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &libraryStoreFake{p: domain.Photo{ID: "photo", UserID: tc.owner, Published: tc.published, ContentType: "image/webp", ImageBytes: 3, ThumbnailBytes: 3}}
			files := &readFilesFake{data: []byte("abc")}
			s := NewLibrary(&sessionFake{}, store, files)
			file, err := s.Read(context.Background(), "token", "photo", tc.thumbnail)
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if tc.want != nil {
				if files.calls != 0 {
					t.Fatal("private bytes read")
				}
				return
			}
			mime := "image/webp"
			if tc.thumbnail {
				mime = "image/jpeg"
			}
			if string(file.Bytes) != "abc" || file.ContentType != mime || files.thumbnail != tc.thumbnail {
				t.Fatal(file)
			}
			files.data = []byte("corrupt")
			if _, err = s.Read(context.Background(), "token", "photo", tc.thumbnail); !errors.Is(err, auth.Unavailable) {
				t.Fatal(err)
			}
		})
	}
}
func TestLibraryAuthenticationAlbumAndPublication(t *testing.T) {
	ctx := context.Background()
	sessions := &sessionFake{}
	store := &libraryStoreFake{p: domain.Photo{ID: "photo", UserID: "u"}}
	files := &readFilesFake{}
	s := NewLibrary(sessions, store, files)
	items, next, err := s.Album(ctx, "token", 6, "cursor")
	if err != nil || len(items) != 1 || next != "next" || store.user != "u" || store.limit != 6 || store.cursor != "cursor" {
		t.Fatal(items, next, err)
	}
	for _, limit := range []int{0, 7} {
		if _, _, err = s.Album(ctx, "token", limit, ""); !errors.Is(err, auth.Validation) {
			t.Fatal(err)
		}
	}
	sessions.err = auth.Unauthenticated
	if _, _, err = s.Album(ctx, "", 6, ""); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	if _, err = s.Read(ctx, "", "photo", false); !errors.Is(err, auth.Unauthenticated) || files.calls != 0 {
		t.Fatal(err)
	}
	sessions.err = nil
	for range 2 {
		p, e := s.Publish(ctx, auth.Session{UserID: "u"}, "photo")
		if e != nil || !p.Published {
			t.Fatal(p, e)
		}
	}
	if _, err = s.Publish(ctx, auth.Session{UserID: "other"}, "photo"); !errors.Is(err, auth.Code("NOT_FOUND")) || store.publishes != 2 {
		t.Fatal(err)
	}
	store.err = auth.Unavailable
	if _, err = s.Publish(ctx, auth.Session{UserID: "u"}, "photo"); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
}
