package postgres

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
)

func TestGuideContextRestartIsolationAndMemoryIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	owner := socialSession(ctx, t, pool, "context_owner")
	other := socialSession(ctx, t, pool, "context_other")
	store := NewGuideStore(pool)
	for i := range 6 {
		now := time.Now().UTC().Add(time.Duration(i) * time.Microsecond)
		g := question(owner.UserID, "c", now)
		g.UserMessage.Text = "вопрос " + strings.Repeat("я", 1500)
		var err error
		g, err = store.Create(ctx, owner, g)
		if err != nil {
			t.Fatal(err)
		}
		g.Status = "completed"
		g.FinishedAt = &now
		g.AssistantMessage = &domain.Message{ID: security.Crypto{}.NewID(), Text: "ответ " + strings.Repeat("ю", 7500), CreatedAt: now}
		if _, won, e := store.Finish(ctx, g); e != nil || !won {
			t.Fatal(won, e)
		}
	}
	// New adapter instances must reconstruct the context exclusively from durable rows.
	restarted := NewGuideStore(pool)
	data, err := restarted.ReadContext(ctx, owner.UserID, "guide")
	if err != nil || len(data.Completed) != 4 || data.Summary == "" || utf8.RuneCountInString(data.Summary) > 4000 {
		t.Fatal(len(data.Completed), err)
	}
	for _, g := range data.Completed {
		if g.UserID != owner.UserID || g.GuideID != "guide" || g.Status != "completed" {
			t.Fatal("foreign or failed context")
		}
	}
	for _, scope := range []struct{ u, g string }{{other.UserID, "guide"}, {owner.UserID, "other"}} {
		foreign, e := restarted.ReadContext(ctx, scope.u, scope.g)
		if e != nil || foreign.Summary != "" || len(foreign.Completed) != 0 {
			t.Fatal("private history leaked", e)
		}
	}
	before := data.Summary
	now := time.Now().UTC()
	g, err := store.Create(ctx, owner, question(owner.UserID, "c", now))
	if err != nil {
		t.Fatal(err)
	}
	g.Status = "failed"
	g.FinishedAt = &now
	g.Error = domain.Failure("AI_UNAVAILABLE")
	if _, _, err = store.Finish(ctx, g); err != nil {
		t.Fatal(err)
	}
	data, err = store.ReadContext(ctx, owner.UserID, "guide")
	if err != nil || data.Summary != before || len(data.Completed) != 4 {
		t.Fatal("failure changed memory", err)
	}
}
