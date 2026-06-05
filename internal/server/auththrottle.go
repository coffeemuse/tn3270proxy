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
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
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
	t.sweepLocked(now, window) // may delete key itself if stale; zero-value below handles that
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

// maxThrottleDelay caps the computed backoff. Any delay this large is already an
// effective hard stop for an interactive login (far beyond the default pre-auth
// ceiling), and the cap makes the seconds*time.Second math overflow-proof for
// pathological admin values — without it an overflow could yield a negative
// duration that sleepFor would silently skip, disabling throttling entirely.
const maxThrottleDelay = time.Hour

// throttleConfig is a snapshot of the three throttle params, read live per
// failure so admin edits take effect without a restart.
type throttleConfig struct {
	baseSecs int
	maxTries int
	window   time.Duration
}

// loadThrottle reads the throttle params from system_config, falling back to the
// catalog defaults on a missing key or parse error (mirrors mfaIssuer).
func (s *Session) loadThrottle(ctx context.Context) throttleConfig {
	mins := s.throttleInt(ctx, sysconfig.KeyAuthFailWindowMins, sysconfig.DefaultAuthFailWindowMins)
	if mins < 1 {
		mins = sysconfig.DefaultAuthFailWindowMins // floor: the form validates >=1; guard a hand-edited DB
	}
	return throttleConfig{
		baseSecs: s.throttleInt(ctx, sysconfig.KeyAuthDelayBaseSecs, sysconfig.DefaultAuthDelayBaseSecs),
		maxTries: s.throttleInt(ctx, sysconfig.KeyAuthMaxTries, sysconfig.DefaultAuthMaxTries),
		window:   time.Duration(mins) * time.Minute,
	}
}

func (s *Session) throttleInt(ctx context.Context, key string, def int) int {
	v, err := s.Store.GetConfig(ctx, key)
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return def
	}
	return n
}

// throttleDelay implements delay = baseSecs * min(count, maxTries) seconds. A
// base or max-tries of 0 yields 0 (throttling disabled). The result is capped
// at maxThrottleDelay to prevent int64 overflow for absurd admin values — an
// overflow without the cap would produce a negative duration that sleepFor
// would silently skip, effectively disabling throttling.
func (s *Session) throttleDelay(count int, cfg throttleConfig) time.Duration {
	if cfg.baseSecs <= 0 {
		return 0
	}
	mult := count
	if mult > cfg.maxTries {
		mult = cfg.maxTries
	}
	if mult <= 0 {
		return 0
	}
	secs := int64(cfg.baseSecs) * int64(mult)
	if secs > int64(maxThrottleDelay/time.Second) {
		return maxThrottleDelay
	}
	d := time.Duration(secs) * time.Second
	if d > maxThrottleDelay {
		return maxThrottleDelay
	}
	return d
}

// failDelay records a failed attempt for username and returns how long to delay
// before re-prompting, plus the running count for the audit detail. Returns
// (0, 0) when throttling is off (nil throttle or base/max-tries 0).
func (s *Session) failDelay(ctx context.Context, username string) (time.Duration, int) {
	if s.Throttle == nil {
		return 0, 0
	}
	cfg := s.loadThrottle(ctx)
	count := s.Throttle.Fail(username, s.now(), cfg.window)
	return s.throttleDelay(count, cfg), count
}

// sleepFor delays by d using the Sleep seam (nil → time.Sleep). A non-positive
// d is a no-op.
func (s *Session) sleepFor(d time.Duration) {
	if d <= 0 {
		return
	}
	if s.Sleep != nil {
		s.Sleep(d)
		return
	}
	time.Sleep(d)
}

// throttleDetail formats the audit Detail for a failed attempt, appending the
// applied backoff when throttling fired. base is the existing context label
// ("" for password, "login"/"enroll" for MFA).
func throttleDetail(base string, delay time.Duration, count int) string {
	if delay <= 0 {
		return base
	}
	if base == "" {
		return fmt.Sprintf("delay=%s count=%d", delay, count)
	}
	return fmt.Sprintf("%s delay=%s count=%d", base, delay, count)
}
