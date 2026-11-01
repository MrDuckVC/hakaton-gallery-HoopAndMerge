package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

func TestGuideActionDurableCommitAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name               string
		storeErr, guardErr error
		want               error
	}{
		{name: "commit"}, {name: "revoked", guardErr: auth.Unauthenticated, want: auth.Unauthenticated},
		{name: "sql", storeErr: auth.Unavailable, want: auth.Unavailable},
		{name: "cas", storeErr: domain.RevisionConflict, want: domain.RevisionConflict},
		{name: "unknown", storeErr: domain.CommitUnknown, want: auth.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st, p := fixture(t)
			st.err = tc.storeErr
			published := 0
			err := s.Action(t.Context(), "r", player.Vector{X: 6, Z: 6}, domain.GuideAction{GuideID: "g", Text: "покажи макет", ExpectedRevision: 1}, func() error { return tc.guardErr }, func(r domain.Room, e domain.GuideEffect) {
				published++
				if !st.exists || !st.o.Config.InstallationVisible || r.Revision != st.o.Revision || e != domain.ShowInstallation {
					t.Error("published before commit", r, e)
				}
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want != nil && (st.exists || published != 0) {
				t.Fatal("failure mutated or published")
			}
			if tc.want == nil && published != 1 {
				t.Fatal("missing completion")
			}
			if errors.Is(tc.storeErr, domain.CommitUnknown) && len(p.invalid) != 1 {
				t.Fatal("uncertain commit retained owning connections")
			}
		})
	}
}

func TestGuideActionKeepsHeldLampAndPersistsRepeatedShow(t *testing.T) {
	s, st, _ := fixture(t)
	initial, _ := s.Read(t.Context(), "r")
	st.o = initial.Overlay
	st.exists = true
	st.o.Revision = 2
	st.o.Lamp.HolderUserID = "u"
	st.o.Lamp.HolderConnectionID = "c"
	lamp := st.o.Lamp
	for _, text := range []string{"покажи макет", "покажи макет", "тёплый свет"} {
		err := s.Action(t.Context(), "r", player.Vector{X: 6, Z: 6}, domain.GuideAction{GuideID: "g", Text: text, ExpectedRevision: st.o.Revision}, func() error { return nil }, func(domain.Room, domain.GuideEffect) {})
		if err != nil {
			t.Fatal(err)
		}
	}
	if st.o.Revision != 5 || st.o.Lamp != lamp || !st.o.Config.InstallationVisible || st.o.Config.Lighting != "warm" {
		t.Fatal(st.o)
	}
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Read(t.Context(), "r")
	if err != nil || restored.Revision != 6 || !restored.Config.InstallationVisible || restored.Config.Lighting != "warm" {
		t.Fatal(restored, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = s.Action(ctx, "r", player.Vector{}, domain.GuideAction{}, func() error { return nil }, func(domain.Room, domain.GuideEffect) { t.Error("cancelled publication") }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestGuideActionCompetesWithAdminAndLamp(t *testing.T) {
	s, st, p := fixture(t)
	initial, err := s.Read(t.Context(), "r")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 3)
	go func() { _, e := s.Update(t.Context(), "r", "v", 1, initial.Config); results <- e }()
	go func() {
		results <- s.Act(t.Context(), "r", domain.ObjectActor{Holder: domain.Holder{UserID: "u", ConnectionID: "c"}, Position: player.Vector{X: 3, Z: 4}}, domain.ObjectAction{ObjectID: "lamp_01", Action: "take", ExpectedRevision: 1}, func() error { return nil }, func(r domain.Room) { p.PublishRuntime(r) })
	}()
	go func() {
		results <- s.Action(t.Context(), "r", player.Vector{X: 6, Z: 6}, domain.GuideAction{GuideID: "g", Text: "покажи макет", ExpectedRevision: 1}, func() error { return nil }, func(r domain.Room, _ domain.GuideEffect) { p.PublishRuntime(r) })
	}()
	winners := 0
	for range 3 {
		err = <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, domain.RevisionConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 || st.o.Revision != 2 || len(p.rooms) != 1 {
		t.Fatal(winners, st.o, p.rooms)
	}
}

func TestGuideActionRuntimeRejectsTargetAndSize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		position player.Vector
		text     string
		revision int64
		size     int
		want     error
	}{
		{"distance", player.Vector{}, "покажи макет", 1, 1000, domain.OutOfRange},
		{"unsupported", player.Vector{X: 6, Z: 6}, "удали картину", 1, 1000, domain.UnsupportedAction},
		{"cas", player.Vector{X: 6, Z: 6}, "покажи макет", 2, 1000, domain.RevisionConflict},
		{"size", player.Vector{X: 6, Z: 6}, "покажи макет", 1, domain.MaxRuntimeBytes + 1, auth.Validation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st, _ := fixture(t)
			s.sizer = sizer{tc.size}
			err := s.Action(t.Context(), "r", tc.position, domain.GuideAction{GuideID: "g", Text: tc.text, ExpectedRevision: tc.revision}, func() error { return nil }, func(domain.Room, domain.GuideEffect) { t.Error("rejected mutation published") })
			if !errors.Is(err, tc.want) || st.exists {
				t.Fatal(err, st.o)
			}
		})
	}
}
