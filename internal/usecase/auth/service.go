package auth

import (
	"context"
	"errors"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
)

// Service owns authentication and restoration rules. It holds no request or SQL state.
type Service struct {
	store    Store
	security Security
	world    World
	events   Events
	now      func() time.Time
}

// New wires the application through consumer-owned ports.
func New(store Store, security Security, world World, events Events, now func() time.Time) *Service {
	return &Service{store: store, security: security, world: world, events: events, now: now}
}

// LoginResult carries the newly issued plaintext token only as far as the cookie boundary.
type LoginResult struct {
	User  domain.User
	Token string
}

// MeResult is the authenticated preliminary pose, before the final game snapshot.
type MeResult struct {
	User  domain.User
	State player.State
}

// Register creates account, initial pose and session atomically.
func (s *Service) Register(ctx context.Context, username, displayName, password, previous string) (LoginResult, error) {
	username, displayName, err := domain.Credentials(username, displayName, password, true)
	if err != nil {
		return LoginResult{}, err
	}
	state, err := s.world.Start(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	encoded, err := s.security.HashPassword(ctx, password)
	if err != nil {
		return LoginResult{}, err
	}
	account := domain.Account{User: domain.User{ID: s.security.NewID(), Username: username, DisplayName: displayName, AvatarColor: s.security.AvatarColor()}, PasswordHash: encoded}
	return s.createLogin(ctx, account.User, &Registration{Account: account, State: state}, previous)
}

// Login verifies credentials before replacing only the presented cookie session.
func (s *Service) Login(ctx context.Context, username, password, previous string) (LoginResult, error) {
	username, _, err := domain.Credentials(username, "", password, false)
	if err != nil {
		return LoginResult{}, err
	}
	account, err := s.store.FindAccount(ctx, username)
	if err != nil && !errors.Is(err, domain.InvalidCredentials) {
		return LoginResult{}, err
	}
	// The security adapter performs equivalent KDF work for an absent account.
	valid, verifyErr := s.security.VerifyPassword(ctx, password, account.PasswordHash)
	if verifyErr != nil {
		return LoginResult{}, verifyErr
	}
	if !valid || err != nil {
		return LoginResult{}, domain.InvalidCredentials
	}
	return s.createLogin(ctx, account.User, nil, previous)
}

func (s *Service) createLogin(ctx context.Context, user domain.User, registration *Registration, previous string) (LoginResult, error) {
	token, hash, err := s.security.NewToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.now().UTC()
	session := domain.Session{ID: s.security.NewID(), UserID: user.ID, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	var old *[32]byte
	if previous != "" {
		value := s.security.TokenHash(previous)
		old = &value
	}
	revoked, err := s.store.CreateLogin(ctx, registration, session, old, now)
	if err != nil {
		return LoginResult{}, err
	}
	if revoked != "" {
		s.events.Revoke(revoked)
	}
	return LoginResult{User: user, Token: token}, nil
}

// Authenticate checks persisted validity without extending the session lifetime.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.Session, domain.User, error) {
	if token == "" {
		return domain.Session{}, domain.User{}, domain.Unauthenticated
	}
	session, user, err := s.store.FindSession(ctx, s.security.TokenHash(token))
	if err != nil {
		return domain.Session{}, domain.User{}, err
	}
	if !session.Valid(s.now()) {
		return domain.Session{}, domain.User{}, domain.Unauthenticated
	}
	return session, user, nil
}

// Logout is idempotent; observers are notified only after successful persistence.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	id, err := s.store.Revoke(ctx, s.security.TokenHash(token), s.now().UTC())
	if err != nil {
		return err
	}
	if id != "" {
		s.events.Revoke(id)
	}
	return nil
}

// Me checks the current world and durably repairs invalid saved positions.
func (s *Service) Me(ctx context.Context, token string) (MeResult, error) {
	_, user, err := s.Authenticate(ctx, token)
	if err != nil {
		return MeResult{}, err
	}
	for range 3 {
		state, readErr := s.store.PlayerState(ctx, user.ID)
		if readErr != nil {
			return MeResult{}, readErr
		}
		valid, worldErr := s.world.Valid(ctx, state)
		if worldErr != nil {
			return MeResult{}, worldErr
		}
		if valid {
			return MeResult{User: user, State: state}, nil
		}
		next, startErr := s.world.Start(ctx)
		if startErr != nil {
			return MeResult{}, startErr
		}
		session, _, authErr := s.Authenticate(ctx, token)
		if authErr != nil {
			return MeResult{}, authErr
		}
		saved, saveErr := s.store.RepairState(ctx, session, state, next)
		if saveErr != nil {
			return MeResult{}, saveErr
		}
		if saved {
			return MeResult{User: user, State: next}, nil
		}
	}
	return MeResult{}, domain.Unavailable
}

// Observe registers before rechecking validity, preventing a lost logout during attachment.
// The future game adapter maps end reasons to close 4001 and guards its connection ID.
func (s *Service) Observe(ctx context.Context, session domain.Session) (<-chan EndReason, func(), error) {
	events, cancel := s.events.Observe(session)
	stop := context.AfterFunc(ctx, cancel)
	cleanup := func() { stop(); cancel() }
	current, err := s.store.SessionByID(ctx, session.ID)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if !current.Valid(s.now()) {
		if current.RevokedAt != nil {
			s.events.Revoke(session.ID)
		}
		cleanup()
		return nil, nil, domain.Unauthenticated
	}
	return events, cleanup, nil
}
