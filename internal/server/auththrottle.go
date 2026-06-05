/*
 * Copyright 2026 by CoffeeMuse.
 *
 * This file is part of tn3270proxy.
 *
 * tn3270proxy is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * tn3270proxy is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with tn3270proxy. If not, see <https://www.gnu.org/licenses/>.
 */

package server

import (
	"strings"
	"sync"
	"time"
)

// authThrottle applies per-username linear backoff to failed authentication
// attempts (password + MFA), closing the brute-force gap left open by the MFA
// work (GH #48). It is keyed on the normalized attempted username, so it is
// immune to source-IP aggregation (e.g. a browser-based TN3270 client whose
// users share one IP) and to reconnect resets. In-memory and shared across all
// sessions by the handler; counts reset on success and decay after a window.
type authThrottle struct {
	mu        sync.Mutex
	entries   map[string]throttleEntry
	lastSweep time.Time
}

type throttleEntry struct {
	count int
	last  time.Time
}

func newAuthThrottle() *authThrottle {
	return &authThrottle{entries: make(map[string]throttleEntry)}
}

// throttleKey normalizes an attempted username to the map key, matching the
// store's canonical-uppercase convention so case/whitespace variants cannot
// bypass the counter.
func throttleKey(username string) string {
	return strings.ToUpper(strings.TrimSpace(username))
}

// Fail records one failed attempt for username and returns the running failure
// count within the decay window. A nil throttle returns 0 (throttling off). If
// the previous failure was more than window ago the count restarts at 1. Fail
// also prunes stale entries at most once per window so a distinct-username
// spray cannot grow the map unbounded.
func (t *authThrottle) Fail(username string, now time.Time, window time.Duration) int {
	if t == nil {
		return 0
	}
	key := throttleKey(username)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweepLocked(now, window)
	e := t.entries[key]
	if e.count > 0 && now.Sub(e.last) > window {
		e.count = 0 // decayed: start over
	}
	e.count++
	e.last = now
	t.entries[key] = e
	return e.count
}

// Reset clears any failure count for username (called on a successful auth). A
// nil throttle is a no-op.
func (t *authThrottle) Reset(username string) {
	if t == nil {
		return
	}
	key := throttleKey(username)
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, key)
}

// peek returns the current count for username (test helper).
func (t *authThrottle) peek(username string) (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[throttleKey(username)]
	return e.count, ok
}

// sweepLocked deletes entries whose last failure is older than window. It runs
// at most once per window (tracked by lastSweep) to bound cost. Caller holds mu.
func (t *authThrottle) sweepLocked(now time.Time, window time.Duration) {
	if window <= 0 {
		return
	}
	if !t.lastSweep.IsZero() && now.Sub(t.lastSweep) < window {
		return
	}
	t.lastSweep = now
	for k, e := range t.entries {
		if now.Sub(e.last) > window {
			delete(t.entries, k)
		}
	}
}
