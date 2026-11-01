package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendRoutes(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"index.html": "<!doctype html><h1>gallery</h1>", "assets/app.js": "console.log('local')", ".env": "secret", "empty/.keep": "hidden"} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	frontend, err := NewFrontend(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = frontend.Close() })
	server := NewServer("")
	server.ConfigureFrontend(frontend)
	server.ConfigureWebSocket(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		method, target, accept string
		status                 int
		body, mime             string
	}{
		{"GET", "/", "", 200, "gallery", "text/html"},
		{"HEAD", "/", "", 200, "", "text/html"},
		{"GET", "/assets/app.js", "", 200, "console.log", "javascript"},
		{"GET", "/gallery/room", "text/html", 200, "gallery", "text/html"},
		{"GET", "/gallery/room", "", 404, "NOT_FOUND", "application/json"},
		{"GET", "/assets/missing.js", "text/html", 404, "NOT_FOUND", "application/json"},
		{"GET", "/assets/missing", "text/html", 404, "NOT_FOUND", "application/json"},
		{"GET", "/empty/", "", 404, "NOT_FOUND", "application/json"},
		{"GET", "/.env", "text/html", 404, "NOT_FOUND", "application/json"},
		{"GET", "/assets/../.env", "text/html", 404, "NOT_FOUND", "application/json"},
		{"GET", "/%2e%2e/private.txt", "text/html", 404, "NOT_FOUND", "application/json"},
		{"GET", "/linked/private.txt", "", 404, "NOT_FOUND", "application/json"},
		{"POST", "/", "text/html", 404, "NOT_FOUND", "application/json"},
		{"GET", "/api/v1/missing", "text/html", 404, "NOT_FOUND", "application/json"},
		{"GET", "/api", "text/html", 404, "NOT_FOUND", "application/json"},
		{"GET", "/api/v1/ws", "", 204, "", ""},
	} {
		t.Run(tc.method+tc.target+tc.accept, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			req.Header.Set("Accept", tc.accept)
			rec := httptest.NewRecorder()
			server.Handler.ServeHTTP(rec, req)
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.body) || !strings.Contains(rec.Header().Get("Content-Type"), tc.mime) {
				t.Fatalf("got %d %v %s", rec.Code, rec.Header(), rec.Body)
			}
			if tc.method == "HEAD" && rec.Body.Len() != 0 {
				t.Fatal("HEAD has body")
			}
			if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing common headers")
			}
		})
	}
}

func TestFrontendRequiresReadableIndex(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{filepath.Join(dir, "missing"), dir} {
		if f, err := NewFrontend(name); err == nil {
			_ = f.Close()
			t.Fatal("accepted incomplete dist")
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "index.html"), 0700); err != nil {
		t.Fatal(err)
	}
	if f, err := NewFrontend(dir); err == nil {
		_ = f.Close()
		t.Fatal("accepted directory as index")
	}
	if err := os.Remove(filepath.Join(dir, "index.html")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "index.html")); err != nil {
		t.Fatal(err)
	}
	if f, err := NewFrontend(dir); err == nil {
		_ = f.Close()
		t.Fatal("accepted linked index")
	}
}
