package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
)

func TestAuthTransactionsIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewAuthStore(pool)
	crypto := security.Crypto{}
	now := time.Now().UTC()
	account := domain.Account{User: domain.User{ID: crypto.NewID(), Username: "alice", DisplayName: "Алиса", AvatarColor: "#AABBCC"}, PasswordHash: "encoded"}
	state := player.State{WorldVersion: "v1", RoomID: "start"}
	_, hash, _ := crypto.NewToken()
	session := domain.Session{ID: crypto.NewID(), UserID: account.User.ID, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	if _, err := store.CreateLogin(ctx, &auth.Registration{Account: account, State: state}, session, nil, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.FindAccount(ctx, "alice")
	if err != nil || loaded.User != account.User {
		t.Fatal(loaded, err)
	}
	got, user, err := store.FindSession(ctx, hash)
	if err != nil || !got.Valid(now) || user != account.User {
		t.Fatal(got, user, err)
	}
	other := session
	other.ID = crypto.NewID()
	_, other.TokenHash, _ = crypto.NewToken()
	if _, err = store.CreateLogin(ctx, nil, other, nil, now); err != nil {
		t.Fatal(err)
	}
	replacement := other
	replacement.ID = crypto.NewID()
	_, replacement.TokenHash, _ = crypto.NewToken()
	revoked, err := store.CreateLogin(ctx, nil, replacement, &hash, now)
	if err != nil || revoked != session.ID {
		t.Fatal(revoked, err)
	}
	got, err = store.SessionByID(ctx, session.ID)
	if err != nil || got.RevokedAt == nil {
		t.Fatal(got, err)
	}
	got, err = store.SessionByID(ctx, other.ID)
	if err != nil || !got.Valid(now) {
		t.Fatal("other session lost", err)
	}
	saved, err := store.PlayerState(ctx, account.User.ID)
	if err != nil || saved.RoomID != "start" {
		t.Fatal(saved, err)
	}
	next := state
	next.RoomID = "repaired"
	changed, err := store.RepairState(ctx, replacement, saved, next)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	changed, err = store.RepairState(ctx, replacement, saved, state)
	if err != nil || changed {
		t.Fatal("stale repair overwrote newer position", err)
	}
	if id, revokeErr := store.Revoke(ctx, other.TokenHash, now); revokeErr != nil || id != other.ID {
		t.Fatal(id, revokeErr)
	}
	if id, revokeErr := store.Revoke(ctx, other.TokenHash, now); revokeErr != nil || id != "" {
		t.Fatal("logout not idempotent", id, revokeErr)
	}
	if _, _, lookupErr := store.FindSession(ctx, [32]byte{}); !errors.Is(lookupErr, domain.Unauthenticated) {
		t.Fatal(lookupErr)
	}
	if _, lookupErr := store.FindAccount(ctx, "missing"); !errors.Is(lookupErr, domain.InvalidCredentials) {
		t.Fatal(lookupErr)
	}
	// A failed account insert must not revoke the previous valid cookie session.
	duplicate := account
	duplicate.User.ID = crypto.NewID()
	failed := session
	failed.ID = crypto.NewID()
	failed.UserID = duplicate.User.ID
	_, failed.TokenHash, _ = crypto.NewToken()
	_, err = store.CreateLogin(ctx, &auth.Registration{Account: duplicate, State: state}, failed, &replacement.TokenHash, now)
	if !errors.Is(err, domain.UsernameTaken) {
		t.Fatal(err)
	}
	got, err = store.SessionByID(ctx, replacement.ID)
	if err != nil || !got.Valid(now) {
		t.Fatal("rollback revoked old session", err)
	}
	// A failure after account/state insertion rolls both back.
	broken := account
	broken.User.ID = crypto.NewID()
	broken.User.Username = "rollback"
	badSession := session
	badSession.ID = crypto.NewID()
	badSession.UserID = broken.User.ID // existing hash triggers unique failure
	if _, createErr := store.CreateLogin(ctx, &auth.Registration{Account: broken, State: state}, badSession, &replacement.TokenHash, now); createErr == nil {
		t.Fatal("duplicate token accepted")
	}
	if _, lookupErr := store.FindAccount(ctx, "rollback"); !errors.Is(lookupErr, domain.InvalidCredentials) {
		t.Fatal("partial account persisted", lookupErr)
	}

	// Switching accounts still revokes the presented cookie, never all sessions of its owner.
	spare := session
	spare.ID = crypto.NewID()
	_, spare.TokenHash, _ = crypto.NewToken()
	if _, createErr := store.CreateLogin(ctx, nil, spare, nil, now); createErr != nil {
		t.Fatal(createErr)
	}
	bob := account
	bob.User.ID = crypto.NewID()
	bob.User.Username = "bob"
	bobSession := session
	bobSession.ID = crypto.NewID()
	bobSession.UserID = bob.User.ID
	_, bobSession.TokenHash, _ = crypto.NewToken()
	revoked, err = store.CreateLogin(ctx, &auth.Registration{Account: bob, State: state}, bobSession, &replacement.TokenHash, now)
	if err != nil || revoked != replacement.ID {
		t.Fatal("account switch did not revoke previous cookie", revoked, err)
	}
	got, err = store.SessionByID(ctx, spare.ID)
	if err != nil || !got.Valid(now) {
		t.Fatal("account switch revoked unrelated session", err)
	}

}

func TestConcurrentUsernameIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewAuthStore(pool)
	crypto := security.Crypto{}
	now := time.Now().UTC()
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			account := domain.Account{User: domain.User{ID: crypto.NewID(), Username: "alice", DisplayName: "Alice", AvatarColor: "#123456"}, PasswordHash: "encoded"}
			_, hash, _ := crypto.NewToken()
			_, err := store.CreateLogin(ctx, &auth.Registration{Account: account, State: player.State{WorldVersion: "v1", RoomID: "start"}}, domain.Session{ID: crypto.NewID(), UserID: account.User.ID, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, nil, now)
			results <- err
		})
	}
	group.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, domain.UsernameTaken):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
}

func TestRepairSessionValidityIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewAuthStore(pool)
	crypto := security.Crypto{}
	now := time.Now().UTC()
	account := domain.Account{User: domain.User{ID: crypto.NewID(), Username: "repair", DisplayName: "Repair", AvatarColor: "#123456"}, PasswordHash: "encoded"}
	initial := player.State{WorldVersion: "v1", RoomID: "initial"}
	_, hash, _ := crypto.NewToken()
	session := domain.Session{ID: crypto.NewID(), UserID: account.User.ID, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if _, err := store.CreateLogin(ctx, &auth.Registration{Account: account, State: initial}, session, nil, now); err != nil {
		t.Fatal(err)
	}
	saved, err := store.PlayerState(ctx, account.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := initial
	next.RoomID = "repaired"
	if _, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, repairErr := store.RepairState(ctx, session, saved, next); !errors.Is(repairErr, domain.Unauthenticated) {
		t.Fatal("revoked session repaired state", repairErr)
	}
	if _, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=NULL,created_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, repairErr := store.RepairState(ctx, session, saved, next); !errors.Is(repairErr, domain.Unauthenticated) {
		t.Fatal("expired session repaired state", repairErr)
	}
	current, err := store.PlayerState(ctx, account.User.ID)
	if err != nil || current.RoomID != "initial" {
		t.Fatal("unauthorized repair persisted", current, err)
	}
	// Restore a live session and exercise the absent-state insert path.
	if _, err = pool.Exec(ctx, `UPDATE sessions SET created_at=$2,expires_at=$3 WHERE id=$1`, session.ID, session.CreatedAt, session.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM player_states WHERE user_id=$1`, account.User.ID); err != nil {
		t.Fatal(err)
	}
	missing, err := store.PlayerState(ctx, account.User.ID)
	if err != nil || !missing.UpdatedAt.IsZero() {
		t.Fatal(missing, err)
	}
	if changed, repairErr := store.RepairState(ctx, session, missing, next); repairErr != nil || !changed {
		t.Fatal(changed, repairErr)
	}
	if changed, repairErr := store.RepairState(ctx, session, missing, initial); repairErr != nil || changed {
		t.Fatal("concurrent insert replaced", changed, repairErr)
	}
}
