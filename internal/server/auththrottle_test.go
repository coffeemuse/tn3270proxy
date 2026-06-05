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
	"sync"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

var throttleEpoch = time.Unix(1_700_000_000, 0)

const testWindow = 15 * time.Minute

func TestAuthThrottleIncrements(t *testing.T) {
	tr := newAuthThrottle()
	if n := tr.Fail("bob", throttleEpoch, testWindow); n != 1 {
		t.Errorf("first fail = %d, want 1", n)
	}
	if n := tr.Fail("bob", throttleEpoch, testWindow); n != 2 {
		t.Errorf("second fail = %d, want 2", n)
	}
}

func TestAuthThrottleResetClears(t *testing.T) {
	tr := newAuthThrottle()
	tr.Fail("bob", throttleEpoch, testWindow)
	tr.Fail("bob", throttleEpoch, testWindow)
	tr.Reset("bob")
	if n := tr.Fail("bob", throttleEpoch, testWindow); n != 1 {
		t.Errorf("after reset = %d, want 1", n)
	}
}

func TestAuthThrottleDecaysAfterWindow(t *testing.T) {
	tr := newAuthThrottle()
	tr.Fail("bob", throttleEpoch, testWindow)
	tr.Fail("bob", throttleEpoch, testWindow) // count 2
	later := throttleEpoch.Add(testWindow + time.Second)
	if n := tr.Fail("bob", later, testWindow); n != 1 {
		t.Errorf("after window = %d, want 1 (decayed)", n)
	}
}

func TestAuthThrottleNormalizesKey(t *testing.T) {
	tr := newAuthThrottle()
	tr.Fail(" Bob ", throttleEpoch, testWindow)
	if n := tr.Fail("BOB", throttleEpoch, testWindow); n != 2 {
		t.Errorf("case/space variant counted separately: got %d, want 2", n)
	}
}

func TestAuthThrottleSweepPrunesStale(t *testing.T) {
	tr := newAuthThrottle()
	tr.Fail("stale", throttleEpoch, testWindow)
	// A fresh failure for a different key, a full window later, triggers a sweep
	// that must drop "stale".
	later := throttleEpoch.Add(testWindow + time.Second)
	tr.Fail("fresh", later, testWindow)
	if _, ok := tr.peek("stale"); ok {
		t.Error("stale entry should have been swept")
	}
}

func TestAuthThrottleNilSafe(t *testing.T) {
	var tr *authThrottle
	if n := tr.Fail("bob", throttleEpoch, testWindow); n != 0 {
		t.Errorf("nil Fail = %d, want 0", n)
	}
	tr.Reset("bob") // must not panic
}

func TestAuthThrottleConcurrent(t *testing.T) {
	tr := newAuthThrottle()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.Fail("bob", throttleEpoch, testWindow)
		}()
	}
	wg.Wait()
	if n, _ := tr.peek("bob"); n != 50 {
		t.Errorf("concurrent count = %d, want 50", n)
	}
}

func TestThrottleDelayFormula(t *testing.T) {
	s := &Session{}
	cfg := throttleConfig{baseSecs: 2, maxTries: 5, window: testWindow}
	cases := []struct {
		count int
		want  time.Duration
	}{
		{0, 0},
		{1, 2 * time.Second},
		{3, 6 * time.Second},
		{5, 10 * time.Second},
		{9, 10 * time.Second}, // capped at maxTries
	}
	for _, c := range cases {
		if got := s.throttleDelay(c.count, cfg); got != c.want {
			t.Errorf("count %d: delay = %s, want %s", c.count, got, c.want)
		}
	}
}

func TestThrottleDelayDisabled(t *testing.T) {
	s := &Session{}
	if got := s.throttleDelay(3, throttleConfig{baseSecs: 0, maxTries: 5}); got != 0 {
		t.Errorf("base 0: delay = %s, want 0 (disabled)", got)
	}
	if got := s.throttleDelay(3, throttleConfig{baseSecs: 2, maxTries: 0}); got != 0 {
		t.Errorf("maxTries 0: delay = %s, want 0 (disabled)", got)
	}
}

func TestSleepForUsesSeam(t *testing.T) {
	var slept []time.Duration
	s := &Session{Sleep: func(d time.Duration) { slept = append(slept, d) }}
	s.sleepFor(6 * time.Second)
	s.sleepFor(0) // zero must be a no-op
	if len(slept) != 1 || slept[0] != 6*time.Second {
		t.Errorf("slept = %v, want [6s]", slept)
	}
}

func TestThrottleDetail(t *testing.T) {
	if d := throttleDetail("", 0, 0); d != "" {
		t.Errorf("disabled detail = %q, want empty", d)
	}
	if d := throttleDetail("", 6*time.Second, 3); d != "delay=6s count=3" {
		t.Errorf("detail = %q", d)
	}
	if d := throttleDetail("login", 6*time.Second, 3); d != "login delay=6s count=3" {
		t.Errorf("detail = %q", d)
	}
	if d := throttleDetail("login", 0, 0); d != "login" {
		t.Errorf("base-only detail = %q, want \"login\"", d)
	}
}

func TestThrottleDelayCapsAtCeiling(t *testing.T) {
	s := &Session{}
	// Absurd values that would overflow time.Duration if multiplied naively.
	cfg := throttleConfig{baseSecs: 2_000_000_000, maxTries: 2_000_000_000, window: testWindow}
	got := s.throttleDelay(5, cfg)
	if got != time.Hour {
		t.Errorf("absurd config delay = %s, want 1h ceiling (never negative/zero)", got)
	}
	if got <= 0 {
		t.Fatalf("delay must never be <= 0 for a real failure (got %s) — that would silently disable throttling", got)
	}
}

func TestLoadThrottleFloorsWindow(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	// Simulate a hand-edited DB with a 0 window (the admin form would reject this).
	if err := st.SetConfig(ctx, "AUTH_FAIL_WINDOW_MINS", "0"); err != nil {
		t.Fatal(err)
	}
	s := &Session{Store: st}
	cfg := s.loadThrottle(ctx)
	if cfg.window < time.Minute {
		t.Errorf("window = %s, want floored to >= 1m", cfg.window)
	}
}
