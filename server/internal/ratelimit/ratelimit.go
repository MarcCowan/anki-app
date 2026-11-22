// Package ratelimit holds an in-memory rolling-window limiter.
package ratelimit

import (
	"sync"
	"time"
)

// Window allows at most Limit hits per key in any rolling Period. Allowed
// hits count; refused ones don't. The zero value of the unexported fields is
// ready to use; it is safe for concurrent use.
type Window struct {
	Limit  int
	Period time.Duration

	mu   sync.Mutex
	hits map[string][]time.Time
}

// Allow records a hit for key at now and reports whether it is within the limit.
func (w *Window) Allow(key string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hits == nil {
		w.hits = map[string][]time.Time{}
	}
	for k, hs := range w.hits {
		if kept := recent(hs, now, w.Period); len(kept) == 0 {
			delete(w.hits, k)
		} else {
			w.hits[k] = kept
		}
	}
	if len(w.hits[key]) >= w.Limit {
		return false
	}
	w.hits[key] = append(w.hits[key], now)
	return true
}

func recent(hs []time.Time, now time.Time, period time.Duration) []time.Time {
	kept := hs[:0]
	for _, at := range hs {
		if now.Sub(at) < period {
			kept = append(kept, at)
		}
	}
	return kept
}

func (w *Window) keys() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.hits)
}
