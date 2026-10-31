package world

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	index := `{"world_id":"world","world_version":"v1","rooms":[{"id":"r","file":"room.json"}],"assets":[{"id":"a","file":"a.jpg"}],"profiles":[{"id":"g","file":"profile.json"}],"start":{"room_id":"r","position":{"x":2,"y":0,"z":2},"rotation":{"yaw":0,"pitch":0}},"spawns":{}}`
	write(t, dir, "catalog.json", []byte(index))
	write(t, dir, "profile.json", []byte(`{"personality":"Guide","style":"Наблюдение","topic":"Живопись","constraints":["Без выдуманных цитат"],"materials":[]}`))
	room := `{ "id":"r","world_version":"v1","title":"Зал","bounds":{"min":{"x":0,"y":0,"z":0},"max":{"x":10,"y":4,"z":10}},"surfaces":[],"doors":[],"exhibits":[],"guides":[] }` + "\n"
	write(t, dir, "room.json", []byte(room))
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.jpg", b.Bytes())
	return dir
}
func write(t *testing.T, dir, name string, b []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestFilesPreserveOriginal(t *testing.T) {
	dir := fixture(t)
	got, err := (Files{Dir: dir}).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join(dir, "room.json"))
	if !bytes.Equal(got.Rooms["r"].Bytes, want) || got.Input.Rooms[0].Title != "Зал" || got.Assets["a"].ContentType != "image/jpeg" {
		t.Fatal(got)
	}
}
func TestFilesRejectUnsafeInput(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		body       []byte
	}{
		{"missing", "catalog.json", []byte(`{}`)},
		{"unknown", "profile.json", []byte(`[]`)},
		{"trailing", "room.json", []byte(`{} {}`)},
		{"corrupt image", "a.jpg", []byte("not jpeg")},
		{"invalid utf8", "profile.json", []byte{'{', '"', 'x', '"', ':', '"', 255, '"', '}'}},
		{"duplicate", "profile.json", []byte(`{"x":1,"x":2}`)},
		{"null", "room.json", []byte(`null`)},
		{"surrogate", "profile.json", []byte(`{"x":"\ud800"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t)
			write(t, dir, tc.file, tc.body)
			if _, err := (Files{Dir: dir}).Load(t.Context()); err == nil {
				t.Fatal("accepted invalid source")
			}
		})
	}
	for _, path := range []string{"../outside.jpg", "/tmp/outside.jpg", "a/../a.jpg", "a\\b.jpg"} {
		t.Run(path, func(t *testing.T) {
			dir := fixture(t)
			b, _ := os.ReadFile(filepath.Join(dir, "catalog.json"))
			var v map[string]any
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatal(err)
			}
			v["assets"] = []any{map[string]any{"id": "a", "file": path}}
			b, _ = json.Marshal(v)
			write(t, dir, "catalog.json", b)
			if _, err := (Files{Dir: dir}).Load(t.Context()); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
	dir := fixture(t)
	outside := filepath.Join(t.TempDir(), "secret")
	write(t, filepath.Dir(outside), "secret", []byte("private"))
	if err := os.Remove(filepath.Join(dir, "a.jpg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "a.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Files{Dir: dir}).Load(t.Context()); err == nil {
		t.Fatal("symlink escape")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (Files{Dir: fixture(t)}).Load(ctx); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestWebPValidation(t *testing.T) {
	data, err := os.ReadFile("testdata/static.webp")
	if err != nil {
		t.Fatal(err)
	}
	kind, err := imageType(t.Context(), data)
	if err != nil || kind != "image/webp" {
		t.Fatal(kind, err)
	}
	animated := append([]byte{}, data...)
	animated = append(animated, []byte{'A', 'N', 'I', 'M', 0, 0, 0, 0}...)
	binary.LittleEndian.PutUint32(animated[4:8], uint32(len(animated)-8))
	if _, err = imageType(t.Context(), animated); err == nil {
		t.Fatal("animation accepted")
	}
	for n := 12; n < len(data); n++ {
		if _, err = imageType(t.Context(), data[:n]); err == nil {
			t.Fatalf("truncation %d accepted", n)
		}
	}
	dir := fixture(t)
	write(t, dir, "a.jpg", data) // Type is determined by bytes, never by extension.
	loaded, err := (Files{Dir: dir}).Load(t.Context())
	if err != nil || loaded.Assets["a"].ContentType != "image/webp" {
		t.Fatal(err)
	}
}
func TestFileAliasesHaveSameIdentity(t *testing.T) {
	dir := fixture(t)
	if err := os.Link(filepath.Join(dir, "a.jpg"), filepath.Join(dir, "alias.jpg")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	r := reader{root: root}
	a, err := r.read(t.Context(), "a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.read(t.Context(), "alias.jpg")
	if err != nil || a.Identity != b.Identity {
		t.Fatal(a.Identity, b.Identity, err)
	}
}

func TestRoomRejectsCaseVariantOverride(t *testing.T) {
	dir := fixture(t)
	data, err := os.ReadFile(filepath.Join(dir, "room.json"))
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"title":"Зал"`), []byte(`"title":"Зал","Title":"Override"`), 1)
	write(t, dir, "room.json", data)
	if _, err = (Files{Dir: dir}).Load(t.Context()); err == nil {
		t.Fatal("case variant overrides validated public content")
	}
}

func TestNestedCaseVariantDoesNotOverrideGeometry(t *testing.T) {
	data := []byte(`{"id":"r","world_version":"v1","title":"Hall","bounds":{"min":{"x":0,"y":0,"z":0},"max":{"x":10,"y":4,"z":10}},"surfaces":[{"id":"s","position":{"x":2,"y":1,"z":2},"size":{"x":1,"y":2,"z":1},"color":"#FFFFFF","collidable":true,"COLLIDABLE":false}],"doors":[],"exhibits":[],"guides":[]}`)
	var r room
	if err := decode(data, &r); err == nil {
		t.Fatal("nested case variant accepted")
	}
}

func TestDemoProfilesStrictAndDistinct(t *testing.T) {
	styles := make([]string, 0, 2)
	for _, name := range []string{"leonardo", "vangogh"} {
		data, err := os.ReadFile("../../../../examples/guides/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		p, err := ParseProfile(data)
		if err != nil {
			t.Fatal(err)
		}
		styles = append(styles, p.Style)
		for _, bad := range [][]byte{[]byte(`{"personality":"only"}`), append(append([]byte(nil), data[:bytes.LastIndexByte(data, '}')]...), []byte(`,"extra":true}`)...)} {
			if _, err = ParseProfile(bad); err == nil {
				t.Fatal("accepted invalid profile")
			}
		}
	}
	if styles[0] == styles[1] {
		t.Fatal("demo personalities are indistinguishable")
	}
}
