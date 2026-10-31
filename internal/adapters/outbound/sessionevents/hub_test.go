package sessionevents

import (
	"sync"
	"testing"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
)

func TestRevocationIsSessionScoped(t *testing.T) {
	h := New()
	a, cancelA := h.Observe(domain.Session{ID: "a", ExpiresAt: time.Now().Add(time.Hour)})
	defer cancelA()
	b, cancelB := h.Observe(domain.Session{ID: "b", ExpiresAt: time.Now().Add(time.Hour)})
	defer cancelB()
	h.Revoke("a")
	h.Revoke("a")
	if reason := <-a; reason != auth.Revoked {
		t.Fatal(reason)
	}
	select {
	case <-b:
		t.Fatal("revoked another session")
	default:
	}
}
func TestExpirationAndCancel(t *testing.T) {
	h := New()
	a, cancel := h.Observe(domain.Session{ID: "a", ExpiresAt: time.Now().Add(-time.Second)})
	defer cancel()
	select {
	case reason := <-a:
		if reason != auth.Expired {
			t.Fatal(reason)
		}
	case <-time.After(time.Second):
		t.Fatal("expiry not delivered")
	}
	_, stop := h.Observe(domain.Session{ID: "b", ExpiresAt: time.Now().Add(time.Hour)})
	stop()
	stop()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.observers) != 0 {
		t.Fatal("observers leaked")
	}
}
func TestConcurrentExpiryRevocationAndCancel(t *testing.T) {
	h := New()
	for range 100 {
		ch, cancel := h.Observe(domain.Session{ID: "a", ExpiresAt: time.Now().Add(time.Millisecond)})
		var group sync.WaitGroup
		group.Go(func() { h.Revoke("a") })
		group.Go(cancel)
		group.Wait()
		cancel()
		select {
		case <-ch:
		default:
		}
	}
}
