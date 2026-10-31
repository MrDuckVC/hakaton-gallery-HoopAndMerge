package security

import (
	"context"
	"strings"
	"testing"
)

func TestPasswordHashAndSalt(t *testing.T) {
	c := Crypto{}
	ctx := t.Context()
	a, err := c.HashPassword(ctx, " пароль ")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.HashPassword(ctx, " пароль ")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || strings.Contains(a, "пароль") {
		t.Fatal("passwords must have independent random salts")
	}
	for _, tc := range []struct {
		password, encoded string
		match             bool
	}{{" пароль ", a, true}, {"пароль", a, false}, {" пароль ", "", false}, {"пароль", "broken", false}, {"password", "$pbkdf2-sha256$999999999$bad$bad", false}} {
		match, err := c.VerifyPassword(ctx, tc.password, tc.encoded)
		if err != nil || match != tc.match {
			t.Fatal(match, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.HashPassword(cancelled, "password"); err == nil {
		t.Fatal("cancelled hashing")
	}
}
func TestTokensAndIDs(t *testing.T) {
	c := Crypto{}
	a, hash, err := c.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := c.NewToken()
	if err != nil || a == b || len(a) != 43 || c.TokenHash(a) != hash {
		t.Fatal("invalid tokens")
	}
	if id := c.NewID(); len(id) != 36 || id[14] != '4' {
		t.Fatal(id)
	}
	if color := c.AvatarColor(); len(color) != 7 || color[0] != '#' {
		t.Fatal(color)
	}
}
