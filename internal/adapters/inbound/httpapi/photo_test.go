package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/photofiles"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
	photocase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/photo"
)

func TestPhotoFilesystemFailureReturnsServiceUnavailable(t *testing.T) {
	for _, mode := range []string{"permission", "removed_directory"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			files, err := photofiles.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = files.Close() }()
			cause := os.ErrNotExist
			if mode == "permission" {
				cause = os.ErrPermission
				t.Cleanup(func() {
					if restoreErr := os.Chmod(dir, 0700); restoreErr != nil {
						t.Error(restoreErr)
					}
				})
				if err = os.Chmod(dir, 0555); err != nil {
					t.Fatal(err)
				}
			} else if err = os.Remove(dir); err != nil {
				t.Fatal(err)
			}
			err = files.Save(t.Context(), "abcdef12-1234-4123-8123-123456789abc", []byte("original"), []byte("thumbnail"))
			if err == nil && mode == "permission" {
				t.Skip("filesystem permissions do not restrict this process")
			}
			if !errors.Is(err, cause) {
				t.Fatalf("filesystem cause lost: %v", err)
			}
			var pathErr *os.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("filesystem error chain lost: %v", err)
			}
			if !errors.Is(err, auth.Unavailable) {
				t.Errorf("filesystem error not classified as unavailable: %v", err)
			}
			server := NewServer("")
			server.ConfigurePhotos(&photoHTTPFake{err: err})
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, multipartRequest(t, [][2]string{{"room_id", "room"}, {"movement_epoch", "1"}}, 1))
			var body errorResponse
			if err = json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if response.Code != 503 || body.Error.Code != "SERVICE_UNAVAILABLE" {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), dir) {
				t.Fatal("filesystem path exposed in HTTP error")
			}
		})
	}
}

type photoHTTPFake struct {
	err      error
	input    domain.Request
	created  bool
	calls    int
	beginErr error
}

func (f *photoHTTPFake) Begin(context.Context, string) (photocase.Admission, error) {
	return photocase.Admission{}, f.beginErr
}
func (f *photoHTTPFake) Upload(_ context.Context, _ photocase.Admission, r domain.Request) (domain.Photo, bool, error) {
	f.calls++
	f.input = r
	return domain.Photo{ID: "p", UserID: "u", RoomID: r.RoomID, Caption: r.Caption, Width: 1, Height: 1, ImageBytes: 1, ThumbnailBytes: 1, CreatedAt: time.Unix(100, 0)}, f.created, f.err
}
func multipartRequest(t *testing.T, fields [][2]string, images int) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, kv := range fields {
		if err := w.WriteField(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	for range images {
		p, e := w.CreateFormFile("image", "capture.jpg")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Write([]byte{255, 216, 255}); e != nil {
			t.Fatal(e)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/photos", &buf)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set("Idempotency-Key", "abcdef12-1234-4123-8123-123456789abc")
	r.Header.Set("Origin", "http://localhost")
	return r
}
func TestPhotoMultipartStrictAndResponses(t *testing.T) {
	fields := [][2]string{{"room_id", "room"}, {"movement_epoch", "1"}, {"caption", "текст"}}
	f := &photoHTTPFake{created: true}
	s := NewServer("")
	s.ConfigurePhotos(f)
	for _, created := range []bool{true, false} {
		f.created = created
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, multipartRequest(t, fields, 1))
		want := 201
		if !created {
			want = 200
		}
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
			t.Fatal(w.Code, w.Body.String())
		}
		var body map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil || body["image_url"] != "/api/v1/photos/p/image" || body["thumbnail_url"] != "/api/v1/photos/p/thumbnail" || body["exhibit_id"] != nil {
			t.Fatal(body, e)
		}
	}
	for _, tc := range []struct {
		fields [][2]string
		images int
	}{{fields, 0}, {fields, 2}, {append(append([][2]string{}, fields...), [2]string{"caption", "twice"}), 1}, {append(append([][2]string{}, fields...), [2]string{"unknown", "x"}), 1}, {[][2]string{{"room_id", "room"}, {"movement_epoch", "01"}}, 1}, {[][2]string{{"room_id", "room"}, {"movement_epoch", "9007199254740992"}}, 1}, {[][2]string{{"room_id", "room"}}, 1}, {[][2]string{{"room_id", "room"}, {"movement_epoch", "1"}, {"exhibit_id", ""}}, 1}} {
		before := f.calls
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, multipartRequest(t, tc.fields, tc.images))
		if w.Code != 400 || f.calls != before {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestPhotoHeaderLimitsAndErrors(t *testing.T) {
	f := &photoHTTPFake{}
	s := NewServer("")
	s.ConfigurePhotos(f)
	fields := [][2]string{{"room_id", "room"}, {"movement_epoch", "1"}}
	for _, kind := range []string{"missing", "duplicate", "uuid", "content", "oversize", "auth", "rate", "conflict", "range", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			r := multipartRequest(t, fields, 1)
			f.beginErr = nil
			f.err = nil
			want := 400
			switch kind {
			case "missing":
				r.Header.Del("Idempotency-Key")
			case "duplicate":
				r.Header.Add("Idempotency-Key", r.Header.Get("Idempotency-Key"))
			case "uuid":
				r.Header.Set("Idempotency-Key", "invalid")
			case "content":
				r.Header.Set("Content-Type", "application/json")
				want = 415
			case "oversize":
				r.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", domain.MaxMultipartBytes+1)))
				r.ContentLength = domain.MaxMultipartBytes + 1
				want = 413
			case "auth":
				f.beginErr = auth.Unauthenticated
				want = 401
			case "rate":
				f.beginErr = photocase.RateError{RetryAfter: 1501 * time.Millisecond}
				want = 429
			case "conflict":
				f.err = auth.Code("IDEMPOTENCY_CONFLICT")
				want = 409
			case "range":
				f.err = auth.Code("OUT_OF_RANGE")
				want = 403
			case "cancel":
				f.err = errors.Join(auth.Unavailable, context.Canceled)
				want = 503
			}
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
			if want == 429 && w.Header().Get("Retry-After") != "2" {
				t.Fatal(w.Header())
			}
		})
	}
}

func TestPhotoMultipartByteBoundaries(t *testing.T) {
	for _, size := range []int{domain.MaxImageBytes, domain.MaxImageBytes + 1} {
		var b bytes.Buffer
		writer := multipart.NewWriter(&b)
		if err := writer.WriteField("room_id", "room"); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteField("movement_epoch", "1"); err != nil {
			t.Fatal(err)
		}
		p, err := writer.CreateFormFile("image", "capture.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = p.Write(make([]byte, size)); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		data := append([]byte{}, b.Bytes()...)
		for _, bodySize := range []int{len(data), domain.MaxMultipartBytes, domain.MaxMultipartBytes + 1} {
			payload := append(append([]byte{}, data...), make([]byte, bodySize-len(data))...)
			r := httptest.NewRequest("POST", "/api/v1/photos", bytes.NewReader(payload))
			r.ContentLength = -1
			r.Header.Set("Content-Type", writer.FormDataContentType())
			r.Header.Set("Idempotency-Key", "abcdef12-1234-4123-8123-123456789abc")
			got, err := readPhoto(httptest.NewRecorder(), r)
			if size > domain.MaxImageBytes || bodySize > domain.MaxMultipartBytes {
				if !errors.Is(err, auth.Code("PAYLOAD_TOO_LARGE")) {
					t.Fatal(size, bodySize, err)
				}
			} else if err != nil || len(got.Image) != size {
				t.Fatal(size, bodySize, err)
			}
		}
	}
}
