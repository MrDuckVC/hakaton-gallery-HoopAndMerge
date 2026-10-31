package auth

import (
	"strings"
	"testing"
)

func TestCredentials(t *testing.T) {
	for _, tc := range []struct {
		username, display, password string
		valid                       bool
	}{
		{"\u2003ALIce\u00a0", "\u2003Алиса\u00a0", " password ", true},
		{"bob", "😀", "😀😀", true}, {"ab", "Bob", "password", false},
		{"аlice", "Bob", "password", false}, {"alice", " ", "password", false},
		{"alice", strings.Repeat("😀", 32), strings.Repeat("a", 128), true},
		{"alice", strings.Repeat("😀", 33), "password", false},
		{"alice", "Alice", strings.Repeat("a", 7), false},
		{"alice", "Alice", strings.Repeat("a", 129), false},
		{"alice", "Alice", string([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}), false},
	} {
		t.Run(tc.username+tc.display, func(t *testing.T) {
			username, display, err := Credentials(tc.username, tc.display, tc.password, true)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.valid && (strings.ContainsAny(username, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") || strings.TrimSpace(display) != display) {
				t.Fatal("credentials not normalized")
			}
		})
	}
}
