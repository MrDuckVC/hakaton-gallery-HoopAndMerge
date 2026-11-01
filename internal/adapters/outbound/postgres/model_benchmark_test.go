package postgres

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/ollama"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
)

type benchmarkEvents struct {
	t     *testing.T
	mu    sync.Mutex
	first map[string]time.Time
}

func (e *benchmarkEvents) Emit(_, _ string, event guide.Event) bool {
	if event.Type == "guide.delta" {
		e.mu.Lock()
		if e.first[event.GenerationID].IsZero() {
			e.first[event.GenerationID] = time.Now()
		}
		e.mu.Unlock()
	}
	return true
}
func (e *benchmarkEvents) CloseGuideConnection(_, _ string, _ int) {
	e.t.Error("unexpected connection failure")
}

// TestModelBenchmarkQueue is opt-in: normal tests never require a GPU.
// It exercises the existing SQL FIFO and worker with three synthetic owners.
func TestModelBenchmarkQueue(t *testing.T) {
	model := os.Getenv("A01_OLLAMA_MODEL")
	if model == "" {
		t.Skip("A01_OLLAMA_MODEL not set; manual hardware benchmark")
	}
	_, dsn, pool := testDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	store := NewGuideStore(pool)
	events := &benchmarkEvents{t: t, first: map[string]time.Time{}}
	base := os.Getenv("A01_OLLAMA_URL")
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	service := guide.New(store, guideCatalog{}, nil, events, ollama.New(base, model), guide.Clock{Now: time.Now, After: time.After}, security.Crypto{}.NewID)
	service.ConfigureContext(guideCatalog{}, store)
	if err := service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 3)
	users := make([]string, 0, 3)
	for _, name := range []string{"a01_first", "a01_second", "a01_third"} {
		session := socialSession(ctx, t, pool, name)
		g := question(session.UserID, name, time.Now().UTC())
		g.UserMessage.Text = "Объясни по-русски, как свет и тень передают объём картины. Предложи короткое упражнение, до 100 слов."
		if _, err := store.Create(ctx, session, g); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, g.ID)
		users = append(users, session.UserID)
	}
	done := make(chan struct{})
	go func() { defer close(done); service.Run(ctx) }()
	defer func() { cancel(); <-done }()
	var previous time.Time
	for i, id := range ids {
		for {
			g, err := store.Get(ctx, users[i], id)
			if err != nil {
				t.Fatal(err)
			}
			if g.Terminal() {
				if g.Status != "completed" || g.StartedAt == nil || g.FinishedAt == nil || g.AssistantMessage == nil {
					t.Fatalf("queue item %d failed: status=%s error=%+v", i, g.Status, g.Error)
				}
				if !previous.IsZero() && g.StartedAt.Before(previous) {
					t.Fatal("FIFO model calls overlap")
				}
				previous = *g.FinishedAt
				events.mu.Lock()
				first := events.first[id]
				events.mu.Unlock()
				if first.IsZero() {
					t.Fatal("completed without visible delta")
				}
				memory, e := store.ReadContext(ctx, users[i], "guide")
				if e != nil || len(memory.Completed) != 1 || memory.Summary == "" {
					t.Fatal("completion/memory not persisted", e)
				}
				row := map[string]any{"model": model, "item": i, "queue_seconds": g.StartedAt.Sub(g.CreatedAt).Seconds(), "run_seconds": g.FinishedAt.Sub(*g.StartedAt).Seconds(), "ttft_seconds": first.Sub(*g.StartedAt).Seconds()}
				b, e := json.Marshal(row)
				if e != nil {
					t.Fatal(e)
				}
				t.Log(string(b))
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
}
