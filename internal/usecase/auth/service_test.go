package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
)

type fakeStore struct {
	account        domain.Account
	session        domain.Session
	state          player.State
	err            error
	created        *Registration
	old            *[32]byte
	revoked        string
	repairs        int
	repairConflict bool
	logins         int
}

func (f *fakeStore) FindAccount(context.Context, string) (domain.Account, error) {
	return f.account, f.err
}
func (f *fakeStore) CreateLogin(_ context.Context, r *Registration, s domain.Session, old *[32]byte, _ time.Time) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.created = r
	f.old = old
	f.session = s
	f.logins++
	return f.revoked, nil
}
func (f *fakeStore) FindSession(context.Context, [32]byte) (domain.Session, domain.User, error) {
	return f.session, f.account.User, f.err
}
func (f *fakeStore) SessionByID(context.Context, string) (domain.Session, error) {
	return f.session, f.err
}
func (f *fakeStore) Revoke(context.Context, [32]byte, time.Time) (string, error) {
	return f.revoked, f.err
}
func (f *fakeStore) PlayerState(context.Context, string) (player.State, error) { return f.state, f.err }
func (f *fakeStore) RepairState(_ context.Context, _ domain.Session, _, next player.State) (bool, error) {
	f.repairs++
	if f.err != nil {
		return false, f.err
	}
	if f.repairConflict {
		f.repairConflict = false
		return false, nil
	}
	f.state = next
	return true, nil
}

type fakeSecurity struct {
	password string
	match    bool
	err      error
}

func (f *fakeSecurity) HashPassword(_ context.Context, p string) (string, error) {
	f.password = p
	return "encoded", f.err
}
func (f *fakeSecurity) VerifyPassword(_ context.Context, p, _ string) (bool, error) {
	f.password = p
	return f.match, f.err
}
func (f *fakeSecurity) NewToken() (string, [32]byte, error) { return "token", [32]byte{1}, f.err }
func (*fakeSecurity) TokenHash(string) [32]byte             { return [32]byte{2} }
func (*fakeSecurity) NewID() string                         { return "id" }
func (*fakeSecurity) AvatarColor() string                   { return "#AABBCC" }

type fakeWorld struct {
	start   player.State
	valid   bool
	err     error
	onValid func()
}

func (f *fakeWorld) Start(context.Context) (player.State, error) { return f.start, f.err }
func (f *fakeWorld) Valid(context.Context, player.State) (bool, error) {
	if f.onValid != nil {
		f.onValid()
	}
	return f.valid, f.err
}

type fakeEvents struct {
	ids       []string
	observed  bool
	ch        chan EndReason
	onObserve func()
}

func (f *fakeEvents) Revoke(id string) { f.ids = append(f.ids, id) }
func (f *fakeEvents) Observe(domain.Session) (<-chan EndReason, func()) {
	f.observed = true
	if f.onObserve != nil {
		f.onObserve()
	}
	if f.ch == nil {
		f.ch = make(chan EndReason, 1)
	}
	return f.ch, func() {}
}
func fixture() (*Service, *fakeStore, *fakeSecurity, *fakeWorld, *fakeEvents) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	db := &fakeStore{account: domain.Account{User: domain.User{ID: "alice", Username: "alice"}, PasswordHash: "encoded"}, session: domain.Session{ID: "session", UserID: "alice", ExpiresAt: now.Add(time.Hour)}}
	sec := &fakeSecurity{match: true}
	world := &fakeWorld{start: player.State{WorldVersion: "v1", RoomID: "start"}, valid: true}
	events := &fakeEvents{}
	return New(db, sec, world, events, func() time.Time { return now }), db, sec, world, events
}
func TestRegisterAtomicInput(t *testing.T) {
	s, db, sec, _, events := fixture()
	db.revoked = "old"
	result, err := s.Register(t.Context(), "\u2003ALICE ", " Алиса ", " password ", "old-cookie")
	if err != nil || result.User.Username != "alice" || result.Token != "token" || db.created == nil || db.created.State.RoomID != "start" || db.old == nil || sec.password != " password " || len(events.ids) != 1 {
		t.Fatalf("result=%+v db=%+v err=%v", result, db, err)
	}
	if db.session.ExpiresAt.Sub(db.session.CreatedAt) != 24*time.Hour {
		t.Fatal("session lifetime")
	}
}
func TestRegisterWorldFailureHasNoSideEffects(t *testing.T) {
	s, db, _, world, events := fixture()
	world.err = domain.Unavailable
	_, err := s.Register(t.Context(), "alice", "Alice", "password", "cookie")
	if !errors.Is(err, domain.Unavailable) || db.logins != 0 || len(events.ids) != 0 {
		t.Fatal(err)
	}
}
func TestLoginAndFailedCredentials(t *testing.T) {
	s, db, sec, _, events := fixture()
	db.revoked = "old"
	result, err := s.Login(t.Context(), " ALICE ", " password ", "cookie")
	if err != nil || result.User.ID != "alice" || db.created != nil || db.old == nil || sec.password != " password " || len(events.ids) != 1 {
		t.Fatal(result, err)
	}
	sec.match = false
	_, err = s.Login(t.Context(), "alice", "password", "cookie")
	if !errors.Is(err, domain.InvalidCredentials) || db.logins != 1 || len(events.ids) != 1 {
		t.Fatal(err)
	}
}
func TestMeRestoresAndPersists(t *testing.T) {
	s, db, _, world, _ := fixture()
	world.valid = false
	db.state = player.State{RoomID: "removed"}
	result, err := s.Me(t.Context(), "token")
	if err != nil || result.State.RoomID != "start" || db.state.RoomID != "start" || db.repairs != 1 {
		t.Fatal(result, err)
	}
}
func TestMeConcurrentRepairRetries(t *testing.T) {
	s, db, _, world, _ := fixture()
	world.valid = false
	db.repairConflict = true
	_, err := s.Me(t.Context(), "token")
	if err != nil || db.repairs != 2 {
		t.Fatal(err, db.repairs)
	}
}
func TestAuthenticationAndWorldErrors(t *testing.T) {
	s, db, _, world, _ := fixture()
	world.err = domain.Unavailable
	if _, err := s.Me(t.Context(), ""); !errors.Is(err, domain.Unauthenticated) {
		t.Fatal(err)
	}
	if _, err := s.Me(t.Context(), "token"); !errors.Is(err, domain.Unavailable) {
		t.Fatal(err)
	}
	db.session.ExpiresAt = s.now()
	if _, _, err := s.Authenticate(t.Context(), "token"); !errors.Is(err, domain.Unauthenticated) {
		t.Fatal(err)
	}
}
func TestLogoutIdempotentAndNotifiesAfterCommit(t *testing.T) {
	s, db, _, _, events := fixture()
	if err := s.Logout(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	db.revoked = "old"
	if err := s.Logout(t.Context(), "token"); err != nil || len(events.ids) != 1 {
		t.Fatal(err)
	}
	db.err = domain.Unavailable
	if err := s.Logout(t.Context(), "token"); !errors.Is(err, domain.Unavailable) || len(events.ids) != 1 {
		t.Fatal(err)
	}
}
func TestObserveRechecksAfterSubscription(t *testing.T) {
	s, db, _, _, events := fixture()
	events.onObserve = func() { now := s.now(); db.session.RevokedAt = &now }
	_, cancel, err := s.Observe(t.Context(), db.session)
	if cancel != nil {
		cancel()
	}
	if !errors.Is(err, domain.Unauthenticated) || !events.observed || len(events.ids) != 1 {
		t.Fatal(err)
	}
}

func TestMeDoesNotRepairAfterSessionEndsDuringWorldValidation(t *testing.T) {
	for _, expire := range []bool{false, true} {
		service, store, _, world, _ := fixture()
		world.valid = false
		world.onValid = func() {
			if expire {
				store.session.ExpiresAt = service.now()
			} else {
				now := service.now()
				store.session.RevokedAt = &now
			}
		}
		_, err := service.Me(t.Context(), "token")
		if !errors.Is(err, domain.Unauthenticated) || store.repairs != 0 {
			t.Fatalf("ended session repaired: err=%v repairs=%d", err, store.repairs)
		}
	}
}
