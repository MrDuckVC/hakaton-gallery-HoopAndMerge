package photo

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/photo"
)

type sessionFake struct{ err error }

func (f *sessionFake) Authenticate(context.Context, string) (auth.Session, auth.User, error) {
	return auth.Session{ID: "s", UserID: "u", ExpiresAt: time.Now().Add(time.Hour)}, auth.User{ID: "u"}, f.err
}

type gameFake struct {
	mu     sync.Mutex
	enter  func()
	err    error
	id     string
	before func()
	checks int
}

func (g *gameFake) Capture(auth.Session) string { return g.id }
func (g *gameFake) WithCapture(_ context.Context, _ auth.Session, _ string, _ domain.Request, fn func(string, func() error) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.enter != nil {
		g.enter()
	}
	if g.err != nil {
		return g.err
	}
	return fn("v1", func() error {
		g.checks++
		if g.before != nil {
			g.before()
		}
		return g.err
	})
}

type imageFake struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (i *imageFake) Prepare(context.Context, []byte) (Prepared, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.calls++
	return Prepared{ContentType: "image/jpeg", Width: 10, Height: 10, Thumbnail: []byte{1}}, i.err
}

type fileFake struct {
	mu                   sync.Mutex
	saveErr, errorDelete error
	ids                  map[string]bool
	deleted              int
}

func (f *fileFake) Save(_ context.Context, id string, _, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.ids[id] = true
	return nil
}
func (f *fileFake) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.ids, id)
	f.deleted++
	return f.errorDelete
}

type repoFake struct {
	held           bool
	mu             sync.Mutex
	records        map[string]domain.Photo
	key            string
	err, createErr error
	uncertainSaved bool
	creates        int
}

func (r *repoFake) WithKey(_ context.Context, u, k string, fn func(LockedStore) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.key = u + k
	r.held = true
	defer func() { r.held = false }()
	return fn(r)
}
func (r *repoFake) Find(context.Context) (domain.Photo, error) {
	p, ok := r.records[r.key]
	if !ok {
		return p, auth.Code("NOT_FOUND")
	}
	return p, nil
}
func (r *repoFake) Create(_ context.Context, _ auth.Session, p domain.Photo, check func() error) (domain.Photo, error) {
	r.creates++
	if err := check(); err != nil {
		return domain.Photo{}, err
	}
	if r.createErr != nil && !r.uncertainSaved {
		return domain.Photo{}, r.createErr
	}
	r.records[r.key] = p
	return p, r.createErr
}
func fixture() (*Service, *repoFake, *fileFake, *gameFake, *imageFake, *sessionFake) {
	r := &repoFake{records: map[string]domain.Photo{}}
	f := &fileFake{ids: map[string]bool{}}
	g := &gameFake{id: "c"}
	i := &imageFake{}
	a := &sessionFake{}
	var n atomic.Int64
	s := New(a, r, f, i, g, time.Now, func() string { return strconv.FormatInt(n.Add(1), 10) })
	return s, r, f, g, i, a
}
func request() domain.Request {
	return domain.Request{Key: "abcdef12-1234-4123-8123-123456789abc", RoomID: "room", Epoch: 1, Image: []byte("image")}
}
func TestUploadReplayBeforeCurrentGameAndConflict(t *testing.T) {
	s, r, f, g, i, _ := fixture()
	ctx := context.Background()
	a, err := s.Begin(ctx, "token")
	if err != nil {
		t.Fatal(err)
	}
	p, created, err := s.Upload(ctx, a, request())
	if err != nil || !created || p.Published || len(f.ids) != 1 {
		t.Fatal(p, created, err)
	}
	g.err = auth.Code("GAME_NOT_READY")
	a.ConnectionID = ""
	p2, created, err := s.Upload(ctx, a, request())
	if err != nil || created || p2.ID != p.ID || r.creates != 1 || i.calls != 1 {
		t.Fatal(p2, created, err)
	}
	q := request()
	q.Image = []byte("different")
	if _, _, err = s.Upload(ctx, a, q); !errors.Is(err, auth.Code("IDEMPOTENCY_CONFLICT")) {
		t.Fatal(err)
	}
}
func TestFailuresAndErrUncertainCommit(t *testing.T) {
	for _, tc := range []string{"images", "files", "game", "sql", "finalcheck", "uncertainSaved", "uncertainRollback", "unresolved"} {
		t.Run(tc, func(t *testing.T) {
			s, r, f, g, i, _ := fixture()
			a, _ := s.Begin(context.Background(), "token")
			switch tc {
			case "images":
				i.err = auth.Validation
			case "files":
				f.saveErr = auth.Unavailable
			case "game":
				g.err = auth.Code("GAME_NOT_READY")
			case "sql":
				r.createErr = auth.Unavailable
			case "finalcheck":
				g.before = func() {
					if g.checks >= 3 {
						g.err = auth.Code("GAME_NOT_READY")
					}
				}
			case "uncertainSaved":
				r.createErr = ErrUncertain
				r.uncertainSaved = true
			case "uncertainRollback":
				r.createErr = ErrUncertain
			case "unresolved":
				r.createErr = ErrUncertain
				g.before = func() {
					if g.checks >= 3 {
						r.err = auth.Unavailable
					}
				}
			}
			p, created, err := s.Upload(context.Background(), a, request())
			if tc == "uncertainSaved" {
				if err != nil || !created || p.ID == "" || len(f.ids) != 1 {
					t.Fatal(p, err)
				}
				return
			}
			if err == nil {
				t.Fatal("failure accepted")
			}
			if tc == "unresolved" {
				if len(f.ids) != 1 {
					t.Fatal("unknown commit files deleted")
				}
			} else if len(f.ids) != 0 {
				t.Fatal("partial files", f.ids)
			}
		})
	}
}
func TestConcurrentUploadAndRate(t *testing.T) {
	s, r, _, _, _, _ := fixture()
	ctx := context.Background()
	a, _ := s.Begin(ctx, "token")
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Go(func() { _, _, err := s.Upload(ctx, a, request()); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if r.creates != 1 {
		t.Fatal(r.creates)
	}
	now := time.Unix(100, 0)
	s.now = func() time.Time { return now }
	s.attempts = make(map[string][]time.Time)
	for range 6 {
		if _, err := s.Begin(ctx, "token"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Begin(ctx, "token"); err == nil {
		t.Fatal("seventh allowed")
	} else {
		var rate RateError
		if !errors.As(err, &rate) || rate.RetryAfter != time.Minute {
			t.Fatal(err)
		}
	}
	now = now.Add(time.Minute)
	if _, err := s.Begin(ctx, "token"); err != nil {
		t.Fatal(err)
	}
}

func TestGameGateNeverWaitsWhileHoldingRepositoryConnection(t *testing.T) {
	s, r, _, g, _, _ := fixture()
	g.enter = func() {
		if r.held {
			t.Error("database connection held while waiting for game gate: inverted lock order")
		}
	}
	a, _ := s.Begin(context.Background(), "token")
	if _, _, err := s.Upload(context.Background(), a, request()); err != nil {
		t.Fatal(err)
	}
}
