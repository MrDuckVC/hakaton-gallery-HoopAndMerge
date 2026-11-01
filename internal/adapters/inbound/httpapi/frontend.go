package httpapi

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// Frontend serves only public build artifacts from a confined filesystem root.
// Keep world files, photographs, configuration and model weights outside this root.
type Frontend struct{ root *os.Root }

// NewFrontend verifies the supplied dist before the HTTP listener is opened.
func NewFrontend(dir string) (*Frontend, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open frontend dist: %w", err)
	}
	frontend := &Frontend{root: root}
	file, err := frontend.open("index.html")
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("read frontend index: %w", err)
	}
	_ = file.Close()
	return frontend, nil
}

// Close releases the root after HTTP requests have drained.
func (f *Frontend) Close() error { return f.root.Close() }

// ConfigureFrontend installs an optional static transport before serving.
func (s *Server) ConfigureFrontend(frontend *Frontend) { s.frontend = frontend }

func (f *Frontend) open(name string) (*os.File, error) {
	if !fs.ValidPath(name) || strings.Contains(name, "\\") {
		return nil, fs.ErrNotExist
	}
	// Reject hidden files and symlinks, even links pointing inside dist. os.Root
	// additionally confines opens when a directory changes between Lstat and Open.
	current := ""
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return nil, fs.ErrNotExist
		}
		current = path.Join(current, segment)
		info, err := f.root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fs.ErrNotExist
		}
	}
	file, err := f.root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fs.ErrNotExist
	}
	return file, nil
}

func (f *Frontend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	// Validate before SPA fallback: malformed paths must never return index.html.
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") || strings.Contains(segment, "\\") {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Ресурс не найден", 0)
			return
		}
	}
	file, err := f.open(name)
	if err != nil && path.Ext(name) == "" && !strings.HasPrefix(name, "assets/") && strings.Contains(r.Header.Get("Accept"), "text/html") {
		file, err = f.open("index.html")
		name = "index.html"
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Ресурс не найден", 0)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Ресурс не найден", 0)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), file)
}
