package photo

import (
	"bytes"
	"strings"
	"testing"
)

func TestNormalizeAndFingerprint(t *testing.T) {
	r := Request{Key: "ABCDEF12-1234-4123-8123-123456789ABC", RoomID: " room ", Epoch: 1, Caption: " \u2003Привет \n", Image: []byte("original")}
	a, err := r.Normalize()
	if err != nil || a.Caption != "Привет" || a.RoomID != "room" || a.Key != "abcdef12-1234-4123-8123-123456789abc" {
		t.Fatal(a, err)
	}
	b, _ := a.Normalize()
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("normalization changes fingerprint")
	}
	for _, change := range []func(*Request){func(r *Request) { r.Image = []byte("different") }, func(r *Request) { r.Epoch++ }, func(r *Request) { r.RoomID = "other" }, func(r *Request) { r.Caption = "other" }, func(r *Request) { r.ExhibitID = "ex" }} {
		c := a
		change(&c)
		if c.Fingerprint() == a.Fingerprint() {
			t.Fatal("collision")
		}
	}
}
func TestRequestValidation(t *testing.T) {
	base := Request{Key: "abcdef12-1234-4123-8123-123456789abc", RoomID: "room", Epoch: 1, Image: []byte{1}}
	for _, change := range []func(*Request){func(r *Request) { r.Key = "" }, func(r *Request) { r.Key = "abcdef12-1234-1123-8123-123456789abc" }, func(r *Request) { r.Epoch = 0 }, func(r *Request) { r.Epoch = 9007199254740992 }, func(r *Request) { r.RoomID = "" }, func(r *Request) { r.ExhibitID = "bad id" }, func(r *Request) { r.Caption = strings.Repeat("я", 201) }, func(r *Request) { r.Caption = "\xff" }, func(r *Request) { r.Caption = "a\x00b" }, func(r *Request) { r.Image = nil }, func(r *Request) { r.Image = bytes.Repeat([]byte{1}, MaxImageBytes+1) }} {
		r := base
		change(&r)
		if _, err := r.Normalize(); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	base.Caption = strings.Repeat("я", 200)
	base.Image = make([]byte, MaxImageBytes)
	if _, err := base.Normalize(); err != nil {
		t.Fatal(err)
	}
}
