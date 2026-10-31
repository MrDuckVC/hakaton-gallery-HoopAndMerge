package photo

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/photo"
)

// RateError carries the exact remaining sliding-window delay.
type RateError struct{ RetryAfter time.Duration }

func (e RateError) Error() string { return "RATE_LIMITED" }

// Admission binds one counted HTTP attempt to its original authenticated socket.
type Admission struct {
	Session      auth.Session
	ConnectionID string
}

// Service coordinates durable replay, capture validation and file compensation.
type Service struct {
	sessions    Sessions
	repo        Repository
	files       Files
	images      Images
	game        Game
	now         func() time.Time
	newID       func() string
	mu          sync.Mutex
	attempts    map[string][]time.Time
	lastCleanup time.Time
}

// New binds the photo scenario without importing transport, database or filesystem APIs.
func New(sessions Sessions, repo Repository, files Files, images Images, game Game, now func() time.Time, newID func() string) *Service {
	return &Service{sessions: sessions, repo: repo, files: files, images: images, game: game, now: now, newID: newID, attempts: make(map[string][]time.Time)}
}

// Begin counts every authenticated POST before parsing, including malformed requests and retries.
func (s *Service) Begin(ctx context.Context, token string) (Admission, error) {
	session, _, err := s.sessions.Authenticate(ctx, token)
	if err != nil {
		return Admission{}, err
	}
	if delay := s.allow(session.UserID); delay > 0 {
		return Admission{}, RateError{RetryAfter: delay}
	}
	return Admission{Session: session, ConnectionID: s.game.Capture(session)}, nil
}
func (s *Service) allow(user string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	cutoff := now.Add(-time.Minute)
	if s.lastCleanup.IsZero() || now.Sub(s.lastCleanup) >= time.Minute {
		for id, times := range s.attempts {
			if len(times) == 0 || !times[len(times)-1].After(cutoff) {
				delete(s.attempts, id)
			}
		}
		s.lastCleanup = now
	}
	times := s.attempts[user]
	first := 0
	for first < len(times) && !times[first].After(cutoff) {
		first++
	}
	times = times[first:]
	if len(times) >= 6 {
		s.attempts[user] = times
		return times[0].Add(time.Minute).Sub(now)
	}
	s.attempts[user] = append(times, now)
	return 0
}

// Upload checks durable identity before current-game rules, preserving retries after transitions.
func (s *Service) Upload(ctx context.Context, a Admission, input domain.Request) (domain.Photo, bool, error) {
	r, err := input.Normalize()
	if err != nil {
		return domain.Photo{}, false, err
	}
	fingerprint := r.Fingerprint()
	previous, found, err := s.lookup(ctx, a.Session.UserID, r.Key, fingerprint)
	if err != nil || found {
		return previous, false, err
	}
	// Release the lookup connection before waiting for a game gate. Game operations
	// acquire the same shared pool while holding that gate, so the reverse order deadlocks.
	if err = s.game.WithCapture(ctx, a.Session, a.ConnectionID, r, func(_ string, check func() error) error { return check() }); err != nil {
		replayed, exists, lookupErr := s.lookup(ctx, a.Session.UserID, r.Key, fingerprint)
		if lookupErr != nil {
			return domain.Photo{}, false, lookupErr
		}
		if exists {
			return replayed, false, nil
		}
		return domain.Photo{}, false, err
	}
	prepared, err := s.images.Prepare(ctx, r.Image)
	if err != nil {
		return domain.Photo{}, false, err
	}
	ownedID := s.newID()
	if err = s.files.Save(ctx, ownedID, r.Image, prepared.Thumbnail); err != nil {
		return domain.Photo{}, false, err
	}
	var result domain.Photo
	created := false
	err = s.game.WithCapture(ctx, a.Session, a.ConnectionID, r, func(version string, check func() error) error {
		return s.repo.WithKey(ctx, a.Session.UserID, r.Key, func(store LockedStore) error {
			saved, exists, e := findReplay(ctx, store, fingerprint)
			if e != nil {
				return e
			}
			if exists {
				result = saved
				return nil
			}
			if e = check(); e != nil {
				return e
			}
			p := domain.Photo{ID: ownedID, UserID: a.Session.UserID, Key: r.Key, WorldVersion: version, RoomID: r.RoomID, Caption: r.Caption, Epoch: r.Epoch, Fingerprint: fingerprint, ContentType: prepared.ContentType, Width: prepared.Width, Height: prepared.Height, ImageBytes: int64(len(r.Image)), ThumbnailBytes: int64(len(prepared.Thumbnail)), CreatedAt: s.now().UTC()}
			if r.ExhibitID != "" {
				p.ExhibitID = &r.ExhibitID
			}
			result, e = store.Create(ctx, a.Session, p, check)
			if e == nil {
				created = true
			}
			return e
		})
	})
	if errors.Is(err, ErrUncertain) {
		// Never delete files on a failed COMMIT response alone. Reacquire the key
		// with a fresh deadline after releasing both the old connection and game gate.
		recoverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		var resolveErr error
		result, found, resolveErr = s.lookup(recoverCtx, a.Session.UserID, r.Key, fingerprint)
		cancel()
		if resolveErr != nil {
			return domain.Photo{}, false, auth.Unavailable
		}
		created = found && result.ID == ownedID
		err = auth.Unavailable
	} else if result.ID == "" && err != nil {
		// A concurrent winner may have committed and then changed rooms before this
		// attempt obtained the gate. Its durable replay still takes precedence.
		previous, exists, lookupErr := s.lookup(ctx, a.Session.UserID, r.Key, fingerprint)
		if lookupErr == nil && exists {
			result = previous
		} else if lookupErr != nil {
			err = lookupErr
		}
	}
	if result.ID != ownedID {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		cleanupErr := s.files.Delete(cleanupCtx, ownedID)
		cancel()
		if cleanupErr != nil {
			return domain.Photo{}, false, errors.Join(auth.Unavailable, cleanupErr)
		}
	}
	if result.ID != "" {
		return result, created, nil
	}
	return domain.Photo{}, false, err
}

func (s *Service) lookup(ctx context.Context, user, key string, fingerprint [32]byte) (domain.Photo, bool, error) {
	var result domain.Photo
	var found bool
	err := s.repo.WithKey(ctx, user, key, func(store LockedStore) error {
		var e error
		result, found, e = findReplay(ctx, store, fingerprint)
		return e
	})
	return result, found, err
}
func findReplay(ctx context.Context, store LockedStore, fingerprint [32]byte) (domain.Photo, bool, error) {
	p, err := store.Find(ctx)
	if errors.Is(err, auth.Code("NOT_FOUND")) {
		return domain.Photo{}, false, nil
	}
	if err != nil {
		return domain.Photo{}, false, err
	}
	if p.Fingerprint != fingerprint {
		return domain.Photo{}, false, auth.Code("IDEMPOTENCY_CONFLICT")
	}
	return p, true, nil
}
