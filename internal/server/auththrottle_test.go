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
	"sync"
	"testing"
	"time"
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
