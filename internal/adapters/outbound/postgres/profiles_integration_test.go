package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

func TestProfileCASAndRoomRollbackIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	st := NewRuntimeStore(pool)
	base, _ := guideCatalog{}.Profile("guide")
	next := guide.Override{WorldVersion: "v1", GuideID: "guide", Revision: 2, Settings: guide.SeedSettings("Name", "Subtitle", base)}
	room := testOverlay()
	other := room.Clone()
	other.RoomID = "room2"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- st.UpdateProfile(ctx, 1, next, []runtime.Overlay{room, other}) })
	}
	wg.Wait()
	a, b := <-results, <-results
	valid := a == nil && errors.Is(b, runtime.RevisionConflict) || b == nil && errors.Is(a, runtime.RevisionConflict)
	if !valid {
		t.Fatal(a, b)
	}
	got, ok, err := NewRuntimeStore(pool).ReadProfile(ctx, "v1", "guide")
	if err != nil || !ok || got.Revision != 2 {
		t.Fatal(got, ok, err)
	}
	if _, ok, err = st.ReadProfile(ctx, "v2", "guide"); err != nil || ok {
		t.Fatal("version leak", err)
	}
	// A late room CAS failure must roll back profile and earlier room writes.
	next.Revision = 3
	next.Settings.DisplayName = "rolled back"
	room.Revision = 3
	other.Revision = 4
	if err = st.UpdateProfile(ctx, 2, next, []runtime.Overlay{room, other}); !errors.Is(err, runtime.RevisionConflict) {
		t.Fatal(err)
	}
	got, _, _ = st.ReadProfile(ctx, "v1", "guide")
	saved, _, _ := st.Read(ctx, "v1", room.RoomID)
	if got.Revision != 2 || saved.Revision != 2 {
		t.Fatal("partial update", got, saved)
	}
}

func TestClaimCapturesLatestProfileAndKeepsRunningSnapshotIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	st := NewRuntimeStore(pool)
	worker := NewGuideStore(pool)
	worker.ConfigureProfiles("v1", guideCatalog{})
	session := socialSession(ctx, t, pool, "snapshot_user")
	g := question(session.UserID, "connection", time.Now())
	if _, err := worker.Create(ctx, session, g); err != nil {
		t.Fatal(err)
	}
	base, _ := guideCatalog{}.Profile("guide")
	settings := guide.SeedSettings("Name", "Subtitle", base)
	settings.Personality = "before running"
	if err := st.UpdateProfile(ctx, 1, guide.Override{WorldVersion: "v1", GuideID: "guide", Revision: 2, Settings: settings}, nil); err != nil {
		t.Fatal(err)
	}
	claimed, err := worker.Claim(ctx, time.Now())
	if err != nil || claimed.ProfileSnapshot == nil || claimed.ProfileSnapshot.Revision != 2 || claimed.ProfileSnapshot.Profile.Personality != "before running" {
		t.Fatal(claimed, err)
	}
	settings.Personality = "after running"
	if err = st.UpdateProfile(ctx, 2, guide.Override{WorldVersion: "v1", GuideID: "guide", Revision: 3, Settings: settings}, nil); err != nil {
		t.Fatal(err)
	}
	if claimed.ProfileSnapshot.Profile.Personality != "before running" {
		t.Fatal("running snapshot changed")
	}
	now := time.Now()
	claimed.Status = "completed"
	claimed.FinishedAt = &now
	claimed.AssistantMessage = &guide.Message{ID: g.UserMessage.ID, Text: "completed answer", CreatedAt: now}
	if _, _, err = worker.Finish(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	second := question(session.UserID, "connection", time.Now())
	if _, err = worker.Create(ctx, session, second); err != nil {
		t.Fatal(err)
	}
	claimed, err = worker.Claim(ctx, time.Now())
	if err != nil || claimed.ProfileSnapshot.Profile.Personality != "after running" || claimed.ProfileSnapshot.Revision != 3 {
		t.Fatal(claimed, err)
	}
	data, err := worker.ReadContext(ctx, session.UserID, "guide")
	if err != nil || len(data.Completed) != 1 || data.Summary == "" {
		t.Fatal(data, err)
	}
	other := socialSession(ctx, t, pool, "snapshot_other")
	data, err = worker.ReadContext(ctx, other.UserID, "guide")
	if err != nil || len(data.Completed) != 0 || data.Summary != "" {
		t.Fatal("foreign memory", data, err)
	}
}
