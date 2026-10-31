package postgres

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
)

func question(user, connection string, now time.Time) guide.Generation {
	crypto := security.Crypto{}
	p, _ := guide.Normalize("guide", "question", "")
	return guide.Generation{ID: crypto.NewID(), UserID: user, RequestID: crypto.NewID(), GuideID: p.GuideID, Fingerprint: p.Fingerprint(), Status: "queued", ConnectionID: connection, UserMessage: guide.Message{ID: crypto.NewID(), Text: p.Text, CreatedAt: now}, CreatedAt: now}
}
func TestGuideDurabilityAndTerminalRaceIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewGuideStore(pool)
	session := socialSession(ctx, t, pool, "guide_user")
	other := socialSession(ctx, t, pool, "other_user")
	g := question(session.UserID, "connection", time.Now().UTC())
	saved, err := store.Create(ctx, session, g)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != g.ID {
		t.Fatal(saved)
	}
	replay, err := store.Create(ctx, session, g)
	if err != nil || replay.ID != g.ID {
		t.Fatal(replay, err)
	}
	if !replay.UserMessage.CreatedAt.Equal(saved.UserMessage.CreatedAt) {
		t.Fatal("accepted question timestamp differs from saved replay")
	}

	conflict := g
	conflict.Fingerprint = "different"
	if _, err = store.Create(ctx, session, conflict); !errors.Is(err, auth.Code("IDEMPOTENCY_CONFLICT")) {
		t.Fatal(err)
	}
	if _, err = store.Create(ctx, session, question(session.UserID, "c", time.Now())); !errors.Is(err, auth.Code("GUIDE_BUSY")) {
		t.Fatal(err)
	}
	if _, err = store.Get(ctx, other.UserID, g.ID); !errors.Is(err, auth.Code("NOT_FOUND")) {
		t.Fatal("foreign get", err)
	}
	claimed, err := store.Claim(ctx, time.Now())
	if err != nil || claimed.ID != g.ID || claimed.Status != "running" {
		t.Fatal(claimed, err)
	}
	// Finish and cancel compete in SQL, with exactly one winner and matching memory.
	var wg sync.WaitGroup
	wins := make(chan guide.Generation, 2)
	for _, status := range []string{"completed", "failed"} {
		wg.Go(func() {
			result := claimed
			result.Status = status
			now := time.Now()
			result.FinishedAt = &now
			if status == "completed" {
				result.AssistantMessage = &guide.Message{ID: security.Crypto{}.NewID(), Text: "answer", CreatedAt: now}
			} else {
				result.Error = guide.Failure("CANCELLED")
			}
			current, won, e := store.Finish(ctx, result)
			if e != nil {
				t.Error(e)
			}
			if won {
				wins <- current
			}
		})
	}
	wg.Wait()
	close(wins)
	if len(wins) != 1 {
		t.Fatal("terminal winners", len(wins))
	}
	final := <-wins
	var memories int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM guide_memories WHERE user_id=$1`, session.UserID).Scan(&memories); err != nil {
		t.Fatal(err)
	}
	if (final.Status == "completed") != (memories == 1) {
		t.Fatal(final.Status, memories)
	}
	items, cursor, err := store.History(ctx, session.UserID, "guide", 20, "")
	if err != nil || len(items) != 1 || cursor != "" || items[0].ID != g.ID {
		t.Fatal(items, cursor, err)
	}
	if _, err = store.ByRequest(ctx, other.UserID, g.RequestID); !errors.Is(err, auth.Code("NOT_FOUND")) {
		t.Fatal(err)
	}
	next := question(session.UserID, "c2", time.Now())
	if _, err = store.Create(ctx, session, next); err != nil {
		t.Fatal(err)
	}
	if err = store.Recover(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Get(ctx, session.UserID, next.ID)
	if err != nil || recovered.Error.Code != "SERVER_RESTARTED" || recovered.AssistantMessage != nil {
		t.Fatal(recovered, err)
	}
}

func TestGuideConcurrentCapacityFIFOAndMemoryRollbackIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewGuideStore(pool)
	sessions := make([]auth.Session, 16)
	for i := range sessions {
		sessions[i] = socialSession(ctx, t, pool, fmt.Sprintf("queue_%d", i))
	}
	var wg sync.WaitGroup
	accepted := make(chan guide.Generation, len(sessions))
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, session := range sessions {
		wg.Go(func() {
			g, err := store.Create(ctx, session, question(session.UserID, "connection", now))
			if err == nil {
				accepted <- g
			} else if !errors.Is(err, auth.Code("GUIDE_QUEUE_FULL")) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(accepted)
	if len(accepted) != 8 {
		t.Fatal("queued count", len(accepted))
	}
	oldest := ""
	for g := range accepted {
		if oldest == "" || g.ID < oldest {
			oldest = g.ID
		}
	}
	running, err := store.Claim(ctx, time.Now())
	if err != nil || running.ID != oldest {
		t.Fatal("fifo", running, err, oldest)
	}
	for range 5 {
		wg.Go(func() {
			if _, claimErr := store.Claim(ctx, time.Now()); !errors.Is(claimErr, auth.Code("NOT_FOUND")) {
				t.Error("second running", claimErr)
			}
		})
	}
	wg.Wait()
	extra := socialSession(ctx, t, pool, "extra")
	if _, err = store.Create(ctx, extra, question(extra.UserID, "c", now)); err != nil {
		t.Fatal("one running plus eight queued", err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE guide_memories ADD CONSTRAINT reject_memory CHECK(summary='never')`); err != nil {
		t.Fatal(err)
	}
	finished := time.Now()
	running.Status = "completed"
	running.FinishedAt = &finished
	running.AssistantMessage = &guide.Message{ID: security.Crypto{}.NewID(), Text: "answer", CreatedAt: finished}
	if _, _, err = store.Finish(ctx, running); err == nil {
		t.Fatal("memory outage success")
	}
	saved, err := store.Get(ctx, running.UserID, running.ID)
	if err != nil || saved.Status != "running" || saved.AssistantMessage != nil {
		t.Fatal("partial completion", saved, err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE guide_memories DROP CONSTRAINT reject_memory`); err != nil {
		t.Fatal(err)
	}
	final, won, err := store.Finish(ctx, running)
	if err != nil || !won || final.Status != "completed" {
		t.Fatal(final, won, err)
	}
	running.Status = "failed"
	running.AssistantMessage = nil
	running.Error = guide.Failure("CANCELLED")
	final, won, err = store.Finish(ctx, running)
	if err != nil || won || final.Status != "completed" {
		t.Fatal("mutated terminal", final, won, err)
	}
}

func TestGuideHistoryKeysetOwnershipAndSessionIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewGuideStore(pool)
	session := socialSession(ctx, t, pool, "history")
	other := socialSession(ctx, t, pool, "stranger")
	now := time.Now().UTC().Truncate(time.Microsecond)
	for range 5 {
		g, err := store.Create(ctx, session, question(session.UserID, "c", now))
		if err != nil {
			t.Fatal(err)
		}
		g.Status = "failed"
		g.Error = guide.Failure("CANCELLED")
		g.FinishedAt = &now
		if _, _, err = store.Finish(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	first, cursor, err := store.History(ctx, session.UserID, "guide", 2, "")
	if err != nil || len(first) != 2 || cursor == "" || first[0].ID >= first[1].ID {
		t.Fatal(first, cursor, err)
	}
	second, next, err := store.History(ctx, session.UserID, "guide", 2, cursor)
	if err != nil || len(second) != 2 || next == "" || second[1].ID >= first[0].ID {
		t.Fatal(second, next, err)
	}
	last, end, err := store.History(ctx, session.UserID, "guide", 2, next)
	if err != nil || len(last) != 1 || end != "" || last[0].ID >= second[0].ID {
		t.Fatal(last, end, err)
	}
	for _, tc := range []struct{ user, guide, cursor string }{{other.UserID, "guide", cursor}, {session.UserID, "other", cursor}, {session.UserID, "guide", "malformed"}} {
		if _, _, err = store.History(ctx, tc.user, tc.guide, 2, tc.cursor); !errors.Is(err, auth.Code("INVALID_CURSOR")) {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Create(ctx, session, question(session.UserID, "c", now)); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("revoked session", err)
	}
}
