package game

import (
	"context"
	"testing"
	"time"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func setupNPC(t *testing.T) (*Service, *runtimeSource, *time.Time) {
	t.Helper()
	now := time.Unix(100, 0)
	s := New(func() time.Time { return now })
	room := world.Room{ID: "room", Version: "v1", Max: player.Vector{X: 12, Y: 3, Z: 12}}
	next := room
	next.ID = "next"
	if err := s.ConfigureNPCs([]world.Room{room, next}, func() string { return "instance" }); err != nil {
		t.Fatal(err)
	}
	source := runtimeFixture()
	source.room.Config.NPCCount = 10
	s.ConfigureRuntime(source)
	s.ConfigureSocial(nil, &photoFake{}, func() string { return "msg" })
	return s, source, &now
}

func lastNPC(t *testing.T, sink *testSink) NPCSnapshot {
	t.Helper()
	for i := len(sink.events) - 1; i >= 0; i-- {
		if sink.events[i].Type == "npc.state" {
			return sink.events[i].NPCs
		}
	}
	t.Fatal("missing npc.state")
	return NPCSnapshot{}
}

func TestNPCSnapshotLifecycleAndFiveHz(t *testing.T) {
	s, _, now := setupNPC(t)
	a := &testSink{}
	id := attach(t, s, "a", a)
	first := lastNPC(t, a)
	if first.Seq != 1 || len(first.NPCs) != 10 || a.events[1].Type != "world.runtime" || a.events[2].Type != "npc.state" {
		t.Fatal(first, a.events)
	}
	n := len(a.events)
	*now = now.Add(100 * time.Millisecond)
	s.PublishNPCs()
	if len(a.events) != n {
		t.Fatal("published faster than 5Hz")
	}
	*now = now.Add(100 * time.Millisecond)
	s.PublishNPCs()
	second := lastNPC(t, a)
	if second.Seq <= first.Seq || second.InstanceID != first.InstanceID || second.NPCs[0].Position == first.NPCs[0].Position {
		t.Fatal(second)
	}
	// The same snapshot/sequence is shared by all recipients.
	b := &testSink{}
	bID := attach(t, s, "b", b)
	*now = now.Add(200 * time.Millisecond)
	s.PublishNPCs()
	if lastNPC(t, a).Seq != lastNPC(t, b).Seq {
		t.Fatal("per-recipient seq")
	}
	replacement := &testSink{}
	newID := attach(t, s, "a", replacement)
	s.Detach("a", id)
	if lastNPC(t, replacement).InstanceID != first.InstanceID {
		t.Fatal("replacement reset instance")
	}
	s.Detach("b", bID)
	s.Detach("a", newID)
	if len(s.npcRooms) != 0 {
		t.Fatal("empty simulation retained")
	}
	n = len(replacement.events)
	*now = now.Add(time.Second)
	s.PublishNPCs()
	if len(replacement.events) != n {
		t.Fatal("empty room emitted state")
	}
	again := &testSink{}
	attach(t, s, "a", again)
	snapshot := lastNPC(t, again)
	if snapshot.InstanceID == first.InstanceID || snapshot.Seq != 1 {
		t.Fatal("instance was reused", snapshot)
	}
}

func TestNPCCountChangesSyncAndRoomIsolation(t *testing.T) {
	s, source, _ := setupNPC(t)
	a, b := &testSink{}, &testSink{}
	id := attach(t, s, "a", a)
	other := identity("b")
	other.State.RoomID = "next"
	other.Package.RoomID = "next"
	ticket, err := s.Reserve(other)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Attach(ticket, b); err != nil {
		t.Fatal(err)
	}
	original := lastNPC(t, a)
	n := len(b.events)
	source.room.Config.NPCCount = 0
	source.room.Revision = 2
	s.PublishRuntime(source.room)
	zero := lastNPC(t, a)
	if zero.NPCs == nil || len(zero.NPCs) != 0 || zero.InstanceID != original.InstanceID || zero.Seq <= original.Seq {
		t.Fatal(zero)
	}
	if len(b.events) != n {
		t.Fatal("cross-room event")
	}
	if f := s.Handle(t.Context(), "a", id, Command{Type: "room.sync", RequestID: "sync"}); f.Code != "" {
		t.Fatal(f)
	}
	events := a.events[len(a.events)-2:]
	if events[0].Type != "world.runtime" || events[1].Type != "npc.state" || events[1].RequestID != "sync" || lastNPC(t, a).RoomID != "room" {
		t.Fatal(events)
	}
	s.InvalidateRuntime("room")
	if len(s.npcRooms) != 1 {
		t.Fatal("invalidation retained dead room")
	}
}

func TestNPCChatOnlyAcceptedTextInSameRoom(t *testing.T) {
	for _, tc := range []struct {
		name, text, photo string
		fail              bool
		want              string
	}{
		{name: "text", text: " hi ", want: "talking"},
		{name: "photo", photo: "photo", want: "idle"},
		{name: "rejected photo", text: "hi", photo: "photo", fail: true, want: "idle"},
		{name: "empty", text: "  ", want: "idle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, now := setupNPC(t)
			sink := &testSink{}
			id := attach(t, s, "a", sink)
			p := lastNPC(t, sink).NPCs[0].Position
			s.active["a"].identity.State.Position = p
			photos := &photoFake{}
			if tc.fail {
				photos.err = runtime.Forbidden
			}
			s.ConfigureSocial(nil, photos, func() string { return "msg" })
			s.Handle(t.Context(), "a", id, Command{Type: "chat.send", Text: tc.text, PhotoID: tc.photo})
			*now = now.Add(200 * time.Millisecond)
			s.PublishNPCs()
			if got := lastNPC(t, sink).NPCs[0].State; got != tc.want {
				t.Fatalf("state %s want %s", got, tc.want)
			}
		})
	}
}

func TestNPCTickNeverCallsRuntimeOrChangesPlayerPose(t *testing.T) {
	s, source, now := setupNPC(t)
	sink := &testSink{}
	attach(t, s, "a", sink)
	before := s.active["a"].identity.State
	source.read = func() { t.Fatal("NPC tick performed durable read") }
	for range 10 {
		*now = now.Add(200 * time.Millisecond)
		s.PublishNPCs()
	}
	if s.active["a"].identity.State != before {
		t.Fatal("NPC changed player state")
	}
	// A detached snapshot must survive subsequent mutations.
	frozen := lastNPC(t, sink)
	position := frozen.NPCs[0].Position
	*now = now.Add(200 * time.Millisecond)
	s.PublishNPCs()
	if frozen.NPCs[0].Position != position {
		t.Fatal("snapshot aliases simulation")
	}
}

func TestNPCDoorTransitionDiscardsOldRoom(t *testing.T) {
	s, source, _ := setupNPC(t)
	st := &poseStore{state: identity("a").State}
	s.Configure(gameCatalog{}, st, NoQuestions{})
	sink := &testSink{}
	id := attach(t, s, "a", sink)
	old := lastNPC(t, sink)
	if f := s.Handle(t.Context(), "a", id, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	if f := s.Handle(t.Context(), "a", id, Command{Type: "world.enter", DoorID: "door", MovementEpoch: 1}); f.Code != "" {
		t.Fatal(f)
	}
	last := sink.events[len(sink.events)-3:]
	if last[0].Type != "world.entered" || last[1].Type != "world.runtime" || last[2].Type != "npc.state" {
		t.Fatal(last)
	}
	next := lastNPC(t, sink)
	if next.RoomID != "next" || next.InstanceID == old.InstanceID || next.Seq != 1 || len(s.npcRooms) != 1 {
		t.Fatal(next)
	}
	before := len(sink.events)
	s.PublishRuntime(source.room)
	if len(sink.events) != before {
		t.Fatal("old room reached transitioned socket")
	}
}

type blockedNPCPhotos struct{ entered, release chan struct{} }

func (p blockedNPCPhotos) PublishPhoto(ctx context.Context, _ authdomain.Session, _ string) (PublishedPhoto, error) {
	close(p.entered)
	select {
	case <-p.release:
		return PublishedPhoto{ID: "photo", Published: true}, nil
	case <-ctx.Done():
		return PublishedPhoto{}, ctx.Err()
	}
}

func TestNPCTickWhilePublicationIOBlockedAndLateChatIgnored(t *testing.T) {
	s, _, now := setupNPC(t)
	sink := &testSink{}
	id := attach(t, s, "a", sink)
	s.active["a"].identity.State.Position = lastNPC(t, sink).NPCs[0].Position
	photos := blockedNPCPhotos{entered: make(chan struct{}), release: make(chan struct{})}
	s.ConfigureSocial(nil, photos, func() string { return "msg" })
	completed := make(chan Failure, 1)
	go func() {
		completed <- s.Handle(t.Context(), "a", id, Command{Type: "chat.send", Text: "hello", PhotoID: "photo"})
	}()
	<-photos.entered
	*now = now.Add(200 * time.Millisecond)
	ticked := make(chan struct{})
	go func() { s.PublishNPCs(); close(ticked) }()
	select {
	case <-ticked:
	case <-time.After(time.Second):
		t.Fatal("I/O held game mutex")
	}
	if lastNPC(t, sink).NPCs[0].State != "idle" {
		t.Fatal("uncommitted chat triggered talking")
	}
	// Invalidation revokes the connection while its I/O is still blocked.
	s.InvalidateRuntime("room")
	close(photos.release)
	if f := <-completed; !f.Drop {
		t.Fatal("stale chat accepted", f)
	}
	if len(s.npcRooms) != 0 {
		t.Fatal("stale chat resurrected simulation")
	}
}

func TestNPCSequenceExhaustionNeverEmitsUnsafeInteger(t *testing.T) {
	for _, action := range []string{"sync", "runtime", "tick", "attach"} {
		t.Run(action, func(t *testing.T) {
			s, source, now := setupNPC(t)
			sink := &testSink{}
			id := attach(t, s, "a", sink)
			s.npcRooms["room"].snapshot.Seq = world.MaxSafeInteger
			switch action {
			case "sync":
				s.Handle(t.Context(), "a", id, Command{Type: "room.sync"})
			case "runtime":
				s.PublishRuntime(source.room)
			case "tick":
				*now = now.Add(200 * time.Millisecond)
				s.PublishNPCs()
			case "attach":
				ticket, err := s.Reserve(identity("b"))
				if err != nil {
					t.Fatal(err)
				}
				other := &testSink{}
				if err = s.Attach(ticket, other); err == nil {
					t.Fatal("attached to exhausted instance")
				}
			}
			for _, e := range sink.events {
				if e.Type == "npc.state" && e.NPCs.Seq > world.MaxSafeInteger {
					t.Fatal("unsafe seq", e.NPCs.Seq)
				}
			}
			if sink.closed != 1012 || len(s.npcRooms) != 0 {
				t.Fatal("exhausted simulation retained", sink.closed)
			}
		})
	}
}

func TestNPCChatCannotTriggerAnotherRoom(t *testing.T) {
	s, _, now := setupNPC(t)
	a, b := &testSink{}, &testSink{}
	id := attach(t, s, "a", a)
	other := identity("b")
	other.State.RoomID = "next"
	other.Package.RoomID = "next"
	ticket, err := s.Reserve(other)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Attach(ticket, b); err != nil {
		t.Fatal(err)
	}
	// Even co-located positions in another room must never react to this sender.
	s.active["a"].identity.State.Position = lastNPC(t, b).NPCs[0].Position
	if f := s.Handle(t.Context(), "a", id, Command{Type: "chat.send", Text: "hello"}); f.Code != "" {
		t.Fatal(f)
	}
	*now = now.Add(200 * time.Millisecond)
	s.PublishNPCs()
	if lastNPC(t, b).NPCs[0].State == "talking" {
		t.Fatal("chat crossed room boundary")
	}
}

func TestNPCClosedOwnerAndShutdownImmediatelyFreeSimulation(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		s, _, _ := setupNPC(t)
		sink := &testSink{}
		id := attach(t, s, "a", sink)
		if shutdown {
			if err := s.StopAccepting(t.Context()); err != nil {
				t.Fatal(err)
			}
		} else {
			s.End("a", id, 4001)
		}
		if len(s.npcRooms) != 0 {
			t.Fatal("revoked ownership retained simulation")
		}
	}
}
