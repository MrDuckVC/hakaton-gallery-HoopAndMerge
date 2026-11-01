package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestCommandRejectsMissingConfigAndUnsupportedOperations(t *testing.T) {
	for _, args := range [][]string{nil, {"down"}, {"up", "extra"}, {"up"}} {
		var output bytes.Buffer
		if code := command(context.Background(), args, func(string) string { return "" }, &output); code != 1 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
}

func TestCommandRedactsDatabaseErrors(t *testing.T) {
	var output bytes.Buffer
	code := command(context.Background(), []string{"up"}, func(string) string { return "postgres://private-password@bad host/db" }, &output)
	if code != 1 || strings.Contains(output.String(), "private-password") {
		t.Fatalf("code=%d output=%s", code, &output)
	}
}

func TestCommandIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	if dsn == os.Getenv("DATABASE_URL") {
		t.Fatal("test and runtime database must differ")
	}
	for _, operation := range []string{"up", "status", "up"} {
		var output bytes.Buffer
		if code := command(context.Background(), []string{operation}, func(string) string { return dsn }, &output); code != 0 {
			t.Fatalf("%s code=%d: %s", operation, code, &output)
		}
		if !strings.Contains(output.String(), "1 applied") {
			t.Fatalf("unexpected status: %s", &output)
		}
	}
}
