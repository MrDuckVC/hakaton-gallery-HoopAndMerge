package httpapi

import (
	"sync"
	"time"
)

type authLimiter struct {
	mu          sync.Mutex
	now         func() time.Time
	attempts    map[string][]time.Time
	lastCleanup time.Time
}

func newAuthLimiter(now func() time.Time) *authLimiter {
	return &authLimiter{now: now, attempts: make(map[string][]time.Time)}
}
func (l *authLimiter) allow(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-time.Minute)
	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= time.Minute {
		for key, attempts := range l.attempts {
			if len(attempts) == 0 || !attempts[len(attempts)-1].After(cutoff) {
				delete(l.attempts, key)
			}
		}
		l.lastCleanup = now
	}
	attempts := l.attempts[ip]
	first := 0
	for first < len(attempts) && !attempts[first].After(cutoff) {
		first++
	}
	attempts = attempts[first:]
	if len(attempts) >= 10 {
		l.attempts[ip] = attempts
		return attempts[0].Add(time.Minute).Sub(now)
	}
	l.attempts[ip] = append(attempts, now)
	return 0
}
