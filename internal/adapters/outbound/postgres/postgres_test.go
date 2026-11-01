package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOpenRejectsInvalidDSNWithoutLeakingIt(t *testing.T) {
	for _, dsn := range []string{"invalid-private-password", "postgres://user:private-password@localhost/db?connect_timeout=bad"} {
		pool, err := Open(context.Background(), dsn)
		if pool != nil || err == nil || strings.Contains(err.Error(), "private-password") {
			t.Fatalf("expected sanitized config error: %v", err)
		}
	}
}

func TestOpenHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	pool, err := Open(ctx, "postgres://user:private-password@127.0.0.1:1/db?sslmode=disable")
	if pool != nil || err == nil || strings.Contains(err.Error(), "private-password") || time.Since(started) > time.Second {
		t.Fatalf("expected prompt safe failure: %v", err)
	}
}

func TestExactMigrationSet(t *testing.T) {
	for _, tc := range []struct {
		versions []int64
		valid    bool
	}{
		{[]int64{0, 1, 2, 3, 4, 5, 6, 7, 8}, true}, {nil, false}, {[]int64{0}, false}, {[]int64{0, 2}, false}, {[]int64{0, 1}, false}, {[]int64{0, 1, 2, 3, 4, 5}, false}, {[]int64{0, 1, 2, 3, 4, 5, 6}, false}, {[]int64{0, 1, 2, 3, 4, 5, 6, 7}, false}, {[]int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, false},
	} {
		err := compatibleVersions(tc.versions)
		if (err == nil) != tc.valid {
			t.Fatalf("versions %v: %v", tc.versions, err)
		}
	}
}

type waitingLocker struct{}

func (waitingLocker) SessionLock(context.Context, *sql.Conn) error { return nil }
func (waitingLocker) SessionUnlock(ctx context.Context, _ *sql.Conn) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestMigrationUnlockKeepsOriginalDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- (migrationLocker{SessionLocker: waitingLocker{}, ctx: ctx}).SessionUnlock(context.WithoutCancel(ctx), nil)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unlock lost migration deadline")
	}
}
