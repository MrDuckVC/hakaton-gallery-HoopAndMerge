package game

import "time"

type windows struct {
	moveSecond                 int64
	violatingSeconds           int
	moves, commands, chat, ask []time.Time
	last                       time.Time
}

func allow(window *[]time.Time, now time.Time, duration time.Duration, limit int) time.Duration {
	entries := *window
	first := 0
	for first < len(entries) && !entries[first].After(now.Add(-duration)) {
		first++
	}
	entries = entries[first:]
	if len(entries) >= limit {
		*window = entries
		return entries[0].Add(duration).Sub(now)
	}
	entries = append(entries, now)
	*window = entries
	return 0
}
func rateFailure(wait time.Duration) Failure {
	return Failure{Code: "RATE_LIMITED", RetryAfterMS: int64((wait + time.Millisecond - 1) / time.Millisecond)}
}
