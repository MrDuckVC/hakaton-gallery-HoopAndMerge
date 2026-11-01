package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

func (s *store) CompareAndSwapLamp(_ context.Context, expected int64, owner domain.Holder, next domain.Overlay) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	rev := int64(1)
	old := domain.Holder{}
	if s.exists {
		rev = s.o.Revision
		old = s.o.Lamp.Owner()
	}
	if rev != expected || old != owner {
		return domain.RevisionConflict
	}
	s.o = next.Clone()
	s.exists = true
	return nil
}
func TestLampCommitGuardAndConditionalCleanup(t *testing.T) {
	s, st, p := fixture(t)
	actor := domain.ObjectActor{Holder: domain.Holder{UserID: "u", ConnectionID: "c"}, Position: player.Vector{X: 3, Z: 4}}
	a := domain.ObjectAction{ObjectID: "lamp_01", Action: "take", ExpectedRevision: 1}
	emitted := 0
	guard := func() error { return auth.Unauthenticated }
	if err := s.Act(t.Context(), "r", actor, a, guard, func(domain.Room) { emitted++ }); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	if st.exists || emitted != 0 {
		t.Fatal("rejected action committed")
	}
	if err := s.Act(t.Context(), "r", actor, a, func() error { return nil }, func(r domain.Room) {
		emitted++
		if r.Revision != 2 {
			t.Error(r.Revision)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if emitted != 1 || len(p.rooms) != 0 {
		t.Fatal("action publication bypassed callback")
	}
	if err := s.ReleaseLamp(t.Context(), "r", domain.Holder{UserID: "u", ConnectionID: "old"}); err != nil {
		t.Fatal(err)
	}
	if st.o.Revision != 2 || st.o.Lamp.HolderConnectionID != "c" {
		t.Fatal("late cleanup removed new owner")
	}
	if err := s.ReconcileLamp(t.Context(), "r", func(u, c string) bool { return u == "u" && c == "c" }); err != nil {
		t.Fatal(err)
	}
	if st.o.Revision != 2 {
		t.Fatal("live holder released")
	}
	st.err = auth.Unavailable
	if err := s.ReleaseLamp(t.Context(), "r", actor.Holder); err == nil {
		t.Fatal("SQL release failure hidden")
	}
	st.err = nil
	if err := s.ReconcileLamp(t.Context(), "r", func(string, string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if st.o.Revision != 3 || st.o.Lamp.HolderUserID != "" || st.o.Lamp.SlotID != "a" || len(p.rooms) != 1 {
		t.Fatalf("cleanup: %+v", st.o)
	}
	if err := s.ReleaseLamp(t.Context(), "r", actor.Holder); err != nil || st.o.Revision != 3 {
		t.Fatal("release not idempotent", err)
	}
}
func TestLampRecoveryAndUnknownCommit(t *testing.T) {
	s, st, p := fixture(t)
	actor := domain.ObjectActor{Holder: domain.Holder{UserID: "u", ConnectionID: "c"}, Position: player.Vector{X: 3, Z: 4}}
	a := domain.ObjectAction{ObjectID: "lamp_01", Action: "take", ExpectedRevision: 1}
	st.err = domain.CommitUnknown
	if err := s.Act(t.Context(), "r", actor, a, func() error { return nil }, func(domain.Room) { t.Error("uncertain publish") }); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
	if len(p.invalid) != 1 {
		t.Fatal("unknown commit did not close sockets")
	}
	st.err = nil
	if err := s.Act(t.Context(), "r", actor, a, func() error { return nil }, func(domain.Room) {}); err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st.o.Lamp.HolderUserID != "" || st.o.Revision != 3 {
		t.Fatal("startup retained holder", st.o)
	}
}

type uncertainLampStore struct {
	*store
	commit, later, unreadable bool
	reads                     int
	fault                     bool
}

func (s *uncertainLampStore) Read(ctx context.Context, v, r string) (domain.Overlay, bool, error) {
	s.reads++
	if s.fault && s.unreadable {
		return domain.Overlay{}, false, auth.Unavailable
	}
	return s.store.Read(ctx, v, r)
}
func (s *uncertainLampStore) CompareAndSwapLamp(ctx context.Context, rev int64, h domain.Holder, next domain.Overlay) error {
	if s.commit {
		if err := s.store.CompareAndSwapLamp(ctx, rev, h, next); err != nil {
			return err
		}
		if s.later {
			s.o.Revision++
			s.o.Lamp.HolderUserID = "new"
			s.o.Lamp.HolderConnectionID = "new-connection"
		}
	}
	s.fault = true
	return domain.CommitUnknown
}
func TestLampUncertainCommitRereadsWithoutAcknowledging(t *testing.T) {
	for _, mode := range []struct {
		name                      string
		commit, later, unreadable bool
	}{{"committed", true, false, false}, {"rolled back", false, false, false}, {"later revision", true, true, false}, {"unreadable", true, false, true}} {
		t.Run(mode.name, func(t *testing.T) {
			s, st, p := fixture(t)
			fault := &uncertainLampStore{store: st, commit: mode.commit, later: mode.later, unreadable: mode.unreadable}
			s.store = fault
			actor := domain.ObjectActor{Holder: domain.Holder{UserID: "u", ConnectionID: "c"}, Position: player.Vector{X: 3, Z: 4}}
			err := s.Act(t.Context(), "r", actor, domain.ObjectAction{ObjectID: "lamp_01", Action: "take", ExpectedRevision: 1}, func() error { return nil }, func(domain.Room) { t.Error("uncertain action acknowledged") })
			if !errors.Is(err, auth.Unavailable) || len(p.invalid) != 1 || fault.reads < 2 {
				t.Fatalf("err %v invalid %v reads %d", err, p.invalid, fault.reads)
			}
			fault.unreadable = false
			got, e := s.Read(t.Context(), "r")
			if e != nil {
				t.Fatal(e)
			}
			want := int64(1)
			if mode.commit {
				want = 2
			}
			if mode.later {
				want = 3
			}
			if got.Revision != want {
				t.Fatal("speculative rollback", got.Revision, want)
			}
		})
	}
}
func TestLampUncertainReleaseConfirmsCanonicalOwner(t *testing.T) {
	s, st, _ := fixture(t)
	initial, _ := s.Read(t.Context(), "r")
	st.o = initial.Overlay
	st.o.Revision = 2
	st.o.Lamp.HolderUserID = "u"
	st.o.Lamp.HolderConnectionID = "c"
	st.exists = true
	fault := &uncertainLampStore{store: st, commit: true}
	s.store = fault
	if err := s.ReleaseLamp(t.Context(), "r", st.o.Lamp.Owner()); err != nil {
		t.Fatal("committed cleanup not confirmed", err)
	}
	if st.o.Revision != 3 || st.o.Lamp.HolderUserID != "" {
		t.Fatal(st.o)
	}
}

type conflictingReleaseStore struct {
	*store
	conflicted bool
}

func (s *conflictingReleaseStore) CompareAndSwapLamp(ctx context.Context, rev int64, h domain.Holder, next domain.Overlay) error {
	if !s.conflicted {
		s.conflicted = true
		s.o.Revision++
		return domain.RevisionConflict
	}
	return s.store.CompareAndSwapLamp(ctx, rev, h, next)
}
func TestLampReleaseRereadsRevisionConflictForSameOwner(t *testing.T) {
	s, st, p := fixture(t)
	initial, _ := s.Read(t.Context(), "r")
	st.o = initial.Overlay
	st.o.Revision = 2
	st.o.Lamp.HolderUserID = "u"
	st.o.Lamp.HolderConnectionID = "c"
	st.exists = true
	s.store = &conflictingReleaseStore{store: st}
	if err := s.ReleaseLamp(t.Context(), "r", st.o.Lamp.Owner()); err != nil {
		t.Fatal(err)
	}
	if st.o.Revision != 4 || st.o.Lamp.HolderUserID != "" || len(p.rooms) != 1 {
		t.Fatal("release did not retry same owner", st.o, p.rooms)
	}
}

type cancelledCommitStore struct {
	*store
	cancel         context.CancelFunc
	fault, revived bool
}

func (s *cancelledCommitStore) CompareAndSwapLamp(context.Context, int64, domain.Holder, domain.Overlay) error {
	s.fault = true
	s.cancel()
	return domain.CommitUnknown
}
func (s *cancelledCommitStore) Read(ctx context.Context, v, r string) (domain.Overlay, bool, error) {
	if s.fault {
		if ctx.Err() == nil {
			s.revived = true
		}
		return domain.Overlay{}, false, auth.Unavailable
	}
	return s.store.Read(ctx, v, r)
}
func TestLampRecoveryDoesNotExtendShutdownCancellation(t *testing.T) {
	for _, release := range []bool{false, true} {
		s, st, _ := fixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		fault := &cancelledCommitStore{store: st, cancel: cancel}
		s.store = fault
		actor := domain.ObjectActor{Holder: domain.Holder{UserID: "u", ConnectionID: "c"}, Position: player.Vector{X: 3, Z: 4}}
		if release {
			initial, _ := s.Read(ctx, "r")
			st.o = initial.Overlay
			st.o.Revision = 2
			st.o.Lamp.HolderUserID = "u"
			st.o.Lamp.HolderConnectionID = "c"
			st.exists = true
			_ = s.ReleaseLamp(ctx, "r", actor.Holder)
		} else {
			_ = s.Act(ctx, "r", actor, domain.ObjectAction{ObjectID: "lamp_01", Action: "take", ExpectedRevision: 1}, func() error { return nil }, func(domain.Room) { t.Error("uncertain ack") })
		}
		if fault.revived {
			t.Fatal("canonical reread discarded caller shutdown cancellation")
		}
	}
}
