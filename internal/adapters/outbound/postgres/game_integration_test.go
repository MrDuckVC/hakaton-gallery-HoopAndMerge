package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/security"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/auth"
)

func TestGamePositionTransactionsIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	crypto := security.Crypto{}
	now := time.Now().UTC()
	account := domain.Account{User: domain.User{ID: crypto.NewID(), Username: "position", DisplayName: "Игрок", AvatarColor: "#AABBCC"}, PasswordHash: "encoded"}
	_, hash, err := crypto.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	session := domain.Session{ID: crypto.NewID(), UserID: account.User.ID, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	authStore := NewAuthStore(pool)
	if _, err = authStore.CreateLogin(ctx, &auth.Registration{Account: account, State: player.State{WorldVersion: "v1", RoomID: "start"}}, session, nil, now); err != nil {
		t.Fatal(err)
	}
	store := NewGameStore(pool)
	old, err := store.PlayerState(ctx, account.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := old
	next.RoomID = "target"
	next.Position.X = 2
	next.Rotation.Yaw = 1
	saved, err := store.SaveState(ctx, session, old, next, true)
	if err != nil || saved.RoomID != "target" || saved.Position.X != 2 || saved.Rotation.Yaw != 1 || !saved.UpdatedAt.After(old.UpdatedAt) {
		t.Fatalf("save: state=%+v err=%v", saved, err)
	}
	loaded, err := store.PlayerState(ctx, account.User.ID)
	if err != nil || loaded != saved {
		t.Fatalf("round trip: state=%+v err=%v", loaded, err)
	}
	if _, err = store.SaveState(ctx, session, old, old, true); !errors.Is(err, domain.Unavailable) {
		t.Fatalf("stale save accepted: %v", err)
	}
	loaded, err = store.PlayerState(ctx, account.User.ID)
	if err != nil || loaded != saved {
		t.Fatalf("conflict changed position: state=%+v err=%v", loaded, err)
	}
	bad := saved
	bad.Position.Y = 1
	if _, err = store.SaveState(ctx, session, saved, bad, true); !errors.Is(err, domain.Unavailable) {
		t.Fatalf("invalid position accepted: %v", err)
	}
	loaded, err = store.PlayerState(ctx, account.User.ID)
	if err != nil || loaded != saved {
		t.Fatalf("failed transaction changed position: state=%+v err=%v", loaded, err)
	}
	foreign := session
	foreign.UserID = crypto.NewID()
	if _, err = store.SaveState(ctx, foreign, saved, next, true); !errors.Is(err, domain.Unauthenticated) {
		t.Fatalf("session of another user authorized save: %v", err)
	}
	expired := session
	expired.ID = crypto.NewID()
	expired.CreatedAt = now.Add(-2 * time.Hour)
	expired.ExpiresAt = now.Add(-time.Hour)
	_, expired.TokenHash, err = crypto.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = authStore.CreateLogin(ctx, nil, expired, nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveState(ctx, expired, saved, next, true); !errors.Is(err, domain.Unauthenticated) {
		t.Fatalf("expired session authorized save: %v", err)
	}
	if _, err = authStore.Revoke(ctx, hash, now); err != nil {
		t.Fatal(err)
	}
	next = saved
	next.Position.Z = 3
	if _, err = store.SaveState(ctx, session, saved, next, true); !errors.Is(err, domain.Unauthenticated) {
		t.Fatalf("revoked session authorized transition: %v", err)
	}
	flushed, err := store.SaveState(ctx, session, saved, next, false)
	if err != nil || flushed.Position.Z != 3 || !flushed.UpdatedAt.After(saved.UpdatedAt) {
		t.Fatalf("accepted pose could not flush after revoke: state=%+v err=%v", flushed, err)
	}
	loaded, err = store.PlayerState(ctx, account.User.ID)
	if err != nil || loaded != flushed {
		t.Fatalf("flush round trip: state=%+v err=%v", loaded, err)
	}
	results := make(chan error, 2)
	for _, x := range []float64{4, 5} {
		go func() {
			pose := flushed
			pose.Position.X = x
			_, saveErr := store.SaveState(ctx, session, flushed, pose, false)
			results <- saveErr
		}()
	}
	winners := 0
	for range 2 {
		if saveErr := <-results; saveErr == nil {
			winners++
		} else if !errors.Is(saveErr, domain.Unavailable) {
			t.Fatalf("unexpected concurrent save error: %v", saveErr)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent revision accepted %d writers", winners)
	}
	loaded, err = store.PlayerState(ctx, account.User.ID)
	if err != nil || (loaded.Position.X != 4 && loaded.Position.X != 5) || loaded.Position.Z != 3 || !loaded.UpdatedAt.After(flushed.UpdatedAt) {
		t.Fatalf("concurrent save persisted invalid result: state=%+v err=%v", loaded, err)
	}
}
