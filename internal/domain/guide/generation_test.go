package guide

import (
	"strings"
	"testing"
)

func TestNormalizeFingerprintAndErrors(t *testing.T) {
	a, err := Normalize("guide", "  Привет 🌍  ", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Normalize("guide", "Привет 🌍", "")
	if a != b || a.Fingerprint() != b.Fingerprint() {
		t.Fatal("normalization is not stable")
	}
	c, _ := Normalize("guide", "Привет 🌍", "ex")
	if a.Fingerprint() == c.Fingerprint() {
		t.Fatal("exhibit absent from fingerprint")
	}
	for _, value := range []string{"", " \n", strings.Repeat("界", 2001), "bad\x00text"} {
		if _, err := Normalize("guide", value, ""); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	for _, code := range []string{"CANCELLED", "AI_TIMEOUT", "AI_UNAVAILABLE", "AI_OUTPUT_LIMIT", "SERVER_RESTARTED"} {
		e := Failure(code)
		want := code == "AI_TIMEOUT" || code == "AI_UNAVAILABLE" || code == "SERVER_RESTARTED"
		if e.Code != code || e.Retryable != want || e.Message == "" {
			t.Fatal(e)
		}
	}
	if (Generation{Status: "running"}).Terminal() || !(Generation{Status: "failed"}).Terminal() || !(Generation{Status: "completed"}).Terminal() {
		t.Fatal("terminal classification")
	}
}
