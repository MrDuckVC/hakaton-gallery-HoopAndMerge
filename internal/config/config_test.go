package config

import (
	"log/slog"
	"strings"
	"testing"
)

func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":    "postgres://gallery:private-password@localhost:5432/gallery?sslmode=disable",
		"ALLOWED_ORIGINS": "http://localhost:5173,http://192.168.1.2:8080",
		"WORLD_VERSION":   "world_v1", "OLLAMA_MODEL": "prepared-model:tag",
	}
}

func loadEnv(values map[string]string) (Config, error) {
	return Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
}

func TestDefaultsAndOverrides(t *testing.T) {
	env := validEnv()
	cfg, err := loadEnv(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "0.0.0.0:8080" || !cfg.CookieSecure || cfg.WorldDir != "./content/world" || cfg.PhotoDir != "./data/photos" || cfg.OllamaBaseURL != "http://127.0.0.1:11434" || cfg.LogLevel != slog.LevelInfo || len(cfg.AllowedOrigins) != 2 {
		t.Fatalf("unexpected defaults: %+v", cfgWithoutSecret(cfg))
	}
	env["HTTP_ADDR"] = "[::1]:9000"
	env["COOKIE_SECURE"] = "false"
	env["LOG_LEVEL"] = "debug"
	env["WORLD_DIR"] = "/srv/world"
	env["PHOTO_DIR"] = "/srv/photos"
	env["OLLAMA_BASE_URL"] = "http://localhost:11434/"
	cfg, err = loadEnv(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CookieSecure || cfg.LogLevel != slog.LevelDebug || cfg.HTTPAddr != "[::1]:9000" || cfg.WorldDir != "/srv/world" || cfg.PhotoDir != "/srv/photos" {
		t.Fatal("overrides not applied")
	}
}

func cfgWithoutSecret(cfg Config) Config { cfg.DatabaseURL = "redacted"; return cfg }

func TestInvalidConfigDoesNotExposeValues(t *testing.T) {
	cases := []struct{ key, value string }{
		{"HTTP_ADDR", "localhost"}, {"HTTP_ADDR", "localhost:0"}, {"HTTP_ADDR", "localhost:65536"},
		{"DATABASE_URL", "https://private-password@host/db"}, {"DATABASE_URL", "postgres://user:private-password@/db"},
		{"DATABASE_URL", "postgres://user:private-password@host/"},
		{"ALLOWED_ORIGINS", "null"}, {"ALLOWED_ORIGINS", "*"}, {"ALLOWED_ORIGINS", "http://host/"},
		{"ALLOWED_ORIGINS", "http://host?secret=private-password"}, {"ALLOWED_ORIGINS", "http://host#"},
		{"ALLOWED_ORIGINS", "http://user:private-password@host"}, {"ALLOWED_ORIGINS", "http://host,"},
		{"COOKIE_SECURE", "no"}, {"COOKIE_SECURE", "0"}, {"LOG_LEVEL", "verbose"},
		{"WORLD_VERSION", "has space"}, {"WORLD_VERSION", strings.Repeat("v", 129)},
		{"OLLAMA_BASE_URL", "ftp://host"}, {"OLLAMA_BASE_URL", "http://user:private-password@host"},
		{"OLLAMA_BASE_URL", "http://host?token=private-password"}, {"OLLAMA_MODEL", "\nmodel"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			env := validEnv()
			env[tc.key] = tc.value
			_, err := loadEnv(env)
			if err == nil || !strings.Contains(err.Error(), tc.key) || strings.Contains(err.Error(), "private-password") {
				t.Fatalf("expected safe error naming %s, got %v", tc.key, err)
			}
		})
	}
	for _, key := range []string{"HTTP_ADDR", "DATABASE_URL", "ALLOWED_ORIGINS", "COOKIE_SECURE", "WORLD_DIR", "WORLD_VERSION", "PHOTO_DIR", "OLLAMA_BASE_URL", "OLLAMA_MODEL", "LOG_LEVEL"} {
		t.Run("empty/"+key, func(t *testing.T) {
			env := validEnv()
			env[key] = ""
			if _, err := loadEnv(env); err == nil {
				t.Fatal("accepted empty value")
			}
		})
	}
	for _, key := range []string{"DATABASE_URL", "ALLOWED_ORIGINS", "WORLD_VERSION", "OLLAMA_MODEL"} {
		t.Run("missing/"+key, func(t *testing.T) {
			env := validEnv()
			delete(env, key)
			if _, err := loadEnv(env); err == nil {
				t.Fatal("accepted missing value")
			}
		})
	}
}
