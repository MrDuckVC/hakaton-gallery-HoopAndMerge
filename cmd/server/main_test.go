package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestMainReportsUnavailableAPI(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))

	main()

	var record struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode startup log: %v", err)
	}
	if record.Level != "INFO" || !strings.Contains(record.Message, "not implemented") {
		t.Fatalf("startup must report unavailable API at INFO level, got %+v", record)
	}
}
