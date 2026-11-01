// Package config reads and validates process configuration at the composition boundary.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// Config contains startup settings. Never log the struct: DatabaseURL contains credentials.
type Config struct {
	FrontendDistDir string
	HTTPAddr        string
	DatabaseURL     string
	AllowedOrigins  []string
	CookieSecure    bool
	WorldDir        string
	WorldVersion    string
	PhotoDir        string
	OllamaBaseURL   string
	OllamaModel     string
	LogLevel        slog.Level
}

// Load validates environment values without including their contents in errors.
func Load(lookup func(string) (string, bool)) (Config, error) {
	var cfg Config
	fields := []struct {
		key, fallback string
		target        *string
	}{
		{"HTTP_ADDR", "0.0.0.0:8080", &cfg.HTTPAddr},
		{"DATABASE_URL", "", &cfg.DatabaseURL},
		{"WORLD_DIR", "./content/world", &cfg.WorldDir},
		{"WORLD_VERSION", "", &cfg.WorldVersion},
		{"PHOTO_DIR", "./data/photos", &cfg.PhotoDir},
		{"OLLAMA_BASE_URL", "http://127.0.0.1:11434", &cfg.OllamaBaseURL},
		{"OLLAMA_MODEL", "", &cfg.OllamaModel},
	}
	for _, field := range fields {
		value, exists := lookup(field.key)
		if !exists {
			value = field.fallback
		}
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\x00\r\n") {
			return Config{}, fmt.Errorf("%s: a non-empty value without control characters is required", field.key)
		}
		*field.target = value
	}
	if value, exists := lookup("FRONTEND_DIST_DIR"); exists {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\x00\r\n") {
			return Config{}, errors.New("FRONTEND_DIST_DIR: expected a non-empty path without control characters")
		}
		cfg.FrontendDistDir = value
	}
	if !validAddr(cfg.HTTPAddr) {
		return Config{}, errors.New("HTTP_ADDR: expected host:port with port 1..65535")
	}
	dbURL, err := url.Parse(cfg.DatabaseURL)
	if err != nil || (dbURL.Scheme != "postgres" && dbURL.Scheme != "postgresql") || dbURL.Hostname() == "" || strings.Trim(dbURL.Path, "/") == "" || dbURL.Fragment != "" {
		return Config{}, errors.New("DATABASE_URL: expected a PostgreSQL URL with host and database")
	}
	origins, ok := lookup("ALLOWED_ORIGINS")
	if !ok || strings.TrimSpace(origins) == "" {
		return Config{}, errors.New("ALLOWED_ORIGINS: at least one exact HTTP(S) origin is required")
	}
	cfg.AllowedOrigins = make([]string, 0, strings.Count(origins, ",")+1)
	for _, origin := range strings.Split(origins, ",") {
		origin = strings.TrimSpace(origin)
		parsed, parseErr := url.Parse(origin)
		if parseErr != nil || !validHTTPURL(parsed) || parsed.Path != "" || strings.ContainsAny(origin, "?#") {
			return Config{}, errors.New("ALLOWED_ORIGINS: expected exact HTTP(S) origins without credentials, path, query or fragment")
		}
		cfg.AllowedOrigins = append(cfg.AllowedOrigins, origin)
	}
	cfg.CookieSecure = true
	if value, exists := lookup("COOKIE_SECURE"); exists {
		if value != "true" && value != "false" {
			return Config{}, errors.New("COOKIE_SECURE: expected true or false")
		}
		cfg.CookieSecure = value == "true"
	}
	if !opaqueID(cfg.WorldVersion) {
		return Config{}, errors.New("WORLD_VERSION: expected 1..128 printable ASCII characters without spaces")
	}
	modelURL, err := url.Parse(cfg.OllamaBaseURL)
	if err != nil || !validHTTPURL(modelURL) || strings.ContainsAny(cfg.OllamaBaseURL, "?#") {
		return Config{}, errors.New("OLLAMA_BASE_URL: expected an HTTP(S) URL without credentials, query or fragment")
	}
	if strings.IndexFunc(cfg.OllamaModel, unicode.IsSpace) >= 0 {
		return Config{}, errors.New("OLLAMA_MODEL: expected a prepared model tag without whitespace")
	}
	level := "info"
	if value, exists := lookup("LOG_LEVEL"); exists {
		level = value
	}
	switch level {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		return Config{}, errors.New("LOG_LEVEL: expected debug, info, warn or error")
	}
	return cfg, nil
}

func validAddr(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number > 0 && number <= 65535
}

func validHTTPURL(parsed *url.URL) bool {
	if parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	if parsed.Hostname() == "*" || strings.ContainsAny(parsed.Host, " \t\r\n") {
		return false
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		return err == nil && number > 0 && number <= 65535
	}
	return !strings.HasSuffix(parsed.Host, ":")
}

func opaqueID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char < '!' || char > '~' {
			return false
		}
	}
	return true
}
