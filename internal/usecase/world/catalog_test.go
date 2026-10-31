package world

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	guide "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/guide"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

type fakeSource struct {
	data LoadedWorld
	err  error
}

func (f fakeSource) Load(context.Context) (LoadedWorld, error) { return f.data, f.err }

type fakeSizer int64

func (f fakeSizer) MaxSnapshotBytes(domain.RoomPackage) (int64, error) { return int64(f), nil }
func input() LoadedWorld {
	d := LoadedWorld{Input: domain.Input{ID: "world", Version: "v1", Spawns: map[string]player.State{}}, Rooms: map[string]File{}, Assets: map[string]File{}}
	image := bytes.Repeat([]byte{1}, 1024*1024)
	for n := 0; n < 42; n++ {
		id := fmt.Sprintf("r%d", n)
		next := fmt.Sprintf("r%d", (n+1)%42)
		asset := fmt.Sprintf("a%d", n)
		door := fmt.Sprintf("d%d", n)
		d.Input.Rooms = append(d.Input.Rooms, domain.Room{ID: id, Version: "v1", Title: id, Max: player.Vector{X: 10, Y: 4, Z: 10}, Doors: []domain.Door{{ID: door, Target: next, Position: player.Vector{X: 3, Z: 3}, Radius: 1.5}}, Exhibits: []domain.Exhibit{{ID: "e" + id, AssetID: asset, Position: player.Vector{X: 2, Y: 2, Z: 2}, Width: 1, Height: 1, Radius: 3}}})
		d.Input.Assets = append(d.Input.Assets, asset)
		d.Input.Spawns[door] = player.State{WorldVersion: "v1", RoomID: next, Position: player.Vector{X: 2, Z: 2}}
		d.Rooms[id] = File{Identity: id, Bytes: []byte("{ \"title\": \"Зал\" }\n"), ContentType: "application/json"}
		d.Assets[asset] = File{Identity: asset, Bytes: image, ContentType: "image/jpeg"}
	}
	d.Input.Start = player.State{WorldVersion: "v1", RoomID: "r0", Position: player.Vector{X: 2, Z: 2}}
	return d
}
func load(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load(t.Context(), fakeSource{data: input()}, fakeSizer(1000), "v1")
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestCatalogSizesAndImmutableBytes(t *testing.T) {
	d := input()
	// Shared dependency across rooms counts once in total, once in each package.
	d.Input.Rooms[0].Exhibits = append(d.Input.Rooms[0].Exhibits, domain.Exhibit{ID: "shared", AssetID: "a1", Position: player.Vector{X: 2, Y: 2, Z: 2}, Width: 1, Height: 1, Radius: 3})
	// Sharing two large images would exceed budget, so reuse the same dependency instead.
	d.Input.Rooms[0].Exhibits[1].AssetID = "a0"
	c, err := Load(t.Context(), fakeSource{data: d}, fakeSizer(262144), "v1")
	if err != nil {
		t.Fatal(err)
	}
	m := c.Manifest()
	want := int64(42 * (1024*1024 + len(d.Rooms["r0"].Bytes)))
	if m.TotalBytes != want || m.CacheLimit != want/10 {
		t.Fatal(m, want)
	}
	p, err := c.Package("r0")
	if err != nil || len(p.Assets) != 1 || p.DescriptorBytes != int64(len(d.Rooms["r0"].Bytes)) {
		t.Fatal(p, err)
	}
	d.Rooms["r0"].Bytes[0] = '!'
	f, _ := c.Descriptor("r0")
	if f.Bytes[0] != '{' {
		t.Fatal("source mutation changed catalog")
	}
	f.Bytes[0] = '!'
	f, _ = c.Descriptor("r0")
	if f.Bytes[0] != '{' {
		t.Fatal("response mutation changed catalog")
	}
	p.Assets[0].ID = "changed"
	p, _ = c.Package("r0")
	if p.Assets[0].ID != "a0" {
		t.Fatal(p)
	}
	start, err := c.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ok, err := c.Valid(t.Context(), start)
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	start.WorldVersion = "old"
	ok, _ = c.Valid(t.Context(), start)
	if ok {
		t.Fatal("old world accepted")
	}
}
func TestLoadFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*LoadedWorld)
		sizer   fakeSizer
		version string
	}{
		{"version", func(*LoadedWorld) {}, 1, "other"},
		{"snapshot", func(*LoadedWorld) {}, 262145, "v1"},
		{"budget", func(d *LoadedWorld) {
			d.Input.Rooms = d.Input.Rooms[:2]
			d.Input.Rooms[1].Doors[0].Target = "r0"
			s := d.Input.Spawns["d1"]
			s.RoomID = "r0"
			d.Input.Spawns = map[string]player.State{"d0": d.Input.Spawns["d0"], "d1": s}
		}, 1, "v1"},
		{"missing bytes", func(d *LoadedWorld) { delete(d.Rooms, "r0") }, 1, "v1"},
		{"alias", func(d *LoadedWorld) { a := d.Assets["a1"]; a.Identity = "a0"; d.Assets["a1"] = a }, 1, "v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := input()
			tc.change(&d)
			if _, err := Load(t.Context(), fakeSource{data: d}, tc.sizer, tc.version); err == nil {
				t.Fatal("accepted invalid world")
			}
		})
	}
	if _, err := Load(t.Context(), fakeSource{err: errors.New("read")}, fakeSizer(1), "v1"); err == nil {
		t.Fatal("lost source error")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Load(ctx, fakeSource{data: input()}, fakeSizer(1), "v1"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type sessions struct{ err error }

func (s sessions) Authenticate(_ context.Context, token string) (auth.Session, auth.User, error) {
	if token == "" {
		return auth.Session{}, auth.User{}, auth.Unauthenticated
	}
	return auth.Session{ID: token, UserID: "u"}, auth.User{ID: "u"}, s.err
}

type ownership struct {
	room  string
	err   error
	calls int
}

func (o *ownership) OwningRoom(_ context.Context, s, u string) (string, error) {
	o.calls++
	if s != "owner" || u != "u" {
		return "", domain.NotReady
	}
	return o.room, o.err
}
func TestAccessRules(t *testing.T) {
	c := load(t)
	o := &ownership{room: "r0"}
	s := New(c, sessions{}, o)
	if _, err := s.Manifest(t.Context(), "other"); err != nil || o.calls != 0 {
		t.Fatal(err, o.calls)
	}
	if _, err := s.Manifest(t.Context(), ""); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	if _, err := s.Room(t.Context(), "other", "v1", "r0"); !errors.Is(err, domain.NotReady) {
		t.Fatal(err)
	}
	if _, err := s.Room(t.Context(), "owner", "old", "r0"); !errors.Is(err, domain.VersionMismatch) {
		t.Fatal(err)
	}
	if _, err := s.Room(t.Context(), "owner", "v1", "r1"); !errors.Is(err, domain.RoomNotAccessible) {
		t.Fatal(err)
	}
	if _, err := s.Asset(t.Context(), "owner", "v1", "a1"); !errors.Is(err, domain.RoomNotAccessible) {
		t.Fatal(err)
	}
	if _, err := s.Asset(t.Context(), "owner", "v1", "missing"); !errors.Is(err, domain.NotFound) {
		t.Fatal(err)
	}
	f, err := s.Room(t.Context(), "owner", "v1", "r0")
	if err != nil {
		t.Fatal(err)
	}
	o.room = "r1" // The already authorized bytes survive transition; later calls use the new room.
	if len(f.Bytes) == 0 {
		t.Fatal("lost authorized body")
	}
	if _, err := s.Room(t.Context(), "owner", "v1", "r0"); !errors.Is(err, domain.RoomNotAccessible) {
		t.Fatal(err)
	}
	if _, err := s.Asset(t.Context(), "owner", "v1", "a1"); err != nil {
		t.Fatal(err)
	}
	o.err = errors.New("ownership failed")
	if _, err := s.Asset(t.Context(), "owner", "v1", "a1"); !errors.Is(err, o.err) {
		t.Fatal(err)
	}
}

func TestCatalogKeepsDirectedSpawns(t *testing.T) {
	d := input()
	c, err := Load(t.Context(), fakeSource{data: d}, fakeSizer(1), "v1")
	if err != nil {
		t.Fatal(err)
	}
	delete(d.Input.Spawns, "d0")
	s, err := c.Spawn("d0")
	if err != nil || s.RoomID != "r1" {
		t.Fatal(s, err)
	}
	if _, err := c.Spawn("absent"); !errors.Is(err, domain.NotFound) {
		t.Fatal(err)
	}
}

func TestSharedAssetAcrossRoomsCountsOnce(t *testing.T) {
	d := input()
	d.Input.Rooms[1].Exhibits[0].AssetID = "a0"
	c, err := Load(t.Context(), fakeSource{data: d}, fakeSizer(1), "v1")
	if err != nil {
		t.Fatal(err)
	}
	m := c.Manifest()
	want := int64(41*1024*1024 + 42*len(d.Rooms["r0"].Bytes))
	if m.TotalBytes != want {
		t.Fatal(m.TotalBytes, want)
	}
	p, err := c.Package("r1")
	if err != nil || len(p.Assets) != 1 || p.Assets[0].ID != "a0" {
		t.Fatal(p, err)
	}
	s := New(c, sessions{}, &ownership{room: "r1"})
	if _, err := s.Asset(t.Context(), "owner", "v1", "a0"); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogProfilesArePrivateImmutableAndReferenced(t *testing.T) {
	data := input()
	data.Input.Profiles = []string{"g"}
	data.Profiles = map[string]guide.Profile{"g": {Personality: "Художник", Style: "Наблюдение", Topic: "Живопись", Constraints: []string{"Без выдуманных цитат"}, Materials: []guide.Material{{ID: "m", Text: "Свет", Sources: []string{"Каталог"}, ExhibitIDs: []string{"er0"}}}}}
	catalog, err := Load(t.Context(), fakeSource{data: data}, fakeSizer(1000), "v1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := catalog.Profile("g")
	if err != nil {
		t.Fatal(err)
	}
	p.Materials[0].Sources[0] = "changed"
	data.Profiles["g"].Constraints[0] = "changed"
	again, _ := catalog.Profile("g")
	if again.Materials[0].Sources[0] != "Каталог" || again.Constraints[0] == "changed" {
		t.Fatal("mutable profile escaped")
	}
	if _, err = catalog.Profile("missing"); err == nil {
		t.Fatal("unknown profile")
	}
	if ex, e := catalog.Exhibit("er0"); e != nil || ex.ID != "er0" {
		t.Fatal(ex, e)
	}
	if _, err = catalog.Exhibit("missing"); err == nil {
		t.Fatal("unknown exhibit")
	}
	data.Profiles["g"].Materials[0].ExhibitIDs[0] = "missing"
	if _, err = Load(t.Context(), fakeSource{data: data}, fakeSizer(1000), "v1"); err == nil {
		t.Fatal("unknown material exhibit accepted")
	}
	delete(data.Profiles, "g")
	if _, err = Load(t.Context(), fakeSource{data: data}, fakeSizer(1000), "v1"); err == nil {
		t.Fatal("missing profile accepted")
	}
}
