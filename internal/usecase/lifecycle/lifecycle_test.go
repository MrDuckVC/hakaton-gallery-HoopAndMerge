package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type participant struct {
	calls     *[]string
	mu        *sync.Mutex
	completed chan struct{}
	fail      string
	block     string
}

func (p participant) step(ctx context.Context, name string) error {
	p.mu.Lock()
	*p.calls = append(*p.calls, name)
	p.mu.Unlock()
	if p.completed != nil {
		p.completed <- struct{}{}
	}
	if p.block == name {
		<-ctx.Done()
		return ctx.Err()
	}
	if p.fail == name {
		return errors.New("failed " + name)
	}
	return nil
}
func (p participant) StopAccepting(ctx context.Context) error     { return p.step(ctx, "admission") }
func (p participant) CancelGenerations(ctx context.Context) error { return p.step(ctx, "generations") }
func (p participant) SavePositions(ctx context.Context) error     { return p.step(ctx, "positions") }
func (p participant) CloseConnections(ctx context.Context) error  { return p.step(ctx, "connections") }

func TestShutdownOrderAndContinuesAfterEachFailure(t *testing.T) {
	for _, fail := range []string{"", "admission", "generations", "positions", "connections"} {
		t.Run(fail, func(t *testing.T) {
			var calls []string
			p := participant{calls: &calls, fail: fail, mu: &sync.Mutex{}}
			c := Coordinator{Admission: p, Generations: p, Positions: p, Connections: p}
			err := c.Shutdown(context.Background())
			if (err != nil) != (fail != "") {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(calls, []string{"admission", "generations", "positions", "connections"}) {
				t.Fatalf("wrong order: %v", calls)
			}
		})
	}
}

func TestShutdownDeadlineStillAttemptsCleanup(t *testing.T) {
	var calls []string
	completed := make(chan struct{}, 4)
	p := participant{calls: &calls, block: "generations", mu: &sync.Mutex{}, completed: completed}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := (Coordinator{Admission: p, Generations: p, Positions: p, Connections: p}).Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline: %v", err)
	}
	for range 4 {
		select {
		case <-completed:
		case <-time.After(time.Second):
			t.Fatal("cleanup not attempted")
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(calls) != 4 {
		t.Fatalf("cleanup skipped: %v", calls)
	}
}

func TestEmptyCoordinator(t *testing.T) {
	if err := (Coordinator{}).Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
