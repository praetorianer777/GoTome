package auth

import (
	"sync"
	"time"
)

// Limiter counts failed attempts per key and refuses further ones for a while
// after too many. It lives in memory: GOtome is one process, and a restart
// forgetting the counts costs an attacker nothing worth having.
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[string][]time.Time
}

// NewLimiter allows max failures per key within window.
func NewLimiter(max int, window time.Duration, now func() time.Time) *Limiter {
	return &Limiter{max: max, window: window, now: now, failures: map[string][]time.Time{}}
}

// RetryAfter is how long the key is still blocked; zero when it is not.
func (l *Limiter) RetryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.prune(key)
	if len(recent) < l.max {
		return 0
	}
	// Blocked until the oldest of the counted failures leaves the window.
	return recent[len(recent)-l.max].Add(l.window).Sub(l.now())
}

// Fail records a failed attempt.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[key] = append(l.prune(key), l.now())
	// Keys nobody asks about again would otherwise stay for good.
	if len(l.failures) > 10000 {
		for k := range l.failures {
			l.prune(k)
		}
	}
}

// Reset forgets a key's failures, after a success.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// prune drops what has left the window and returns what remains.
func (l *Limiter) prune(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	all := l.failures[key]
	i := 0
	for i < len(all) && !all[i].After(cutoff) {
		i++
	}
	recent := all[i:]
	if len(recent) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = recent
	return recent
}
