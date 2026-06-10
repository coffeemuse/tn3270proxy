# Unified Failed-Auth Throttling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-username linear backoff to failed authentication attempts (password + MFA), closing the brute-force gap left open by the MFA work (GH #48).

**Architecture:** An in-memory, mutex-guarded `authThrottle` keyed on the normalized attempted username, shared across all sessions by the connection handler. After each failed attempt the session sleeps `base × min(count, maxTries)` seconds before re-prompting; the count resets on success and decays after a window. Three live-tunable `sysconfig` params drive it.

**Tech Stack:** Go, `modernc.org/sqlite` (system_config table), existing `internal/server` session machine and `internal/sysconfig` catalog.

**Design doc:** `docs/superpowers/specs/2026-06-05-auth-throttling-design.md`

---

## File Structure

- **Create** `internal/server/auththrottle.go` — the `authThrottle` counter component **and** the `Session` glue methods (`loadThrottle`, `throttleDelay`, `failDelay`, `sleepFor`, `throttleDetail`). All throttle logic lives here, keeping `session.go` focused.
- **Create** `internal/server/auththrottle_test.go` — unit tests for the component and the pure helpers.
- **Modify** `internal/sysconfig/catalog.go` — three new `Key*`/`Default*` constants, three `Catalog` entries, two int validators.
- **Modify** `internal/sysconfig/catalog_test.go` — tests for the new entries and validators.
- **Modify** `internal/server/session.go` — add `Throttle`/`Sleep` fields to `Session`; integrate `failDelay`/`sleepFor`/`Reset` into `doLogin`, `mfaVerify`, `mfaEnroll`.
- **Modify** `internal/server/session_test.go` — integration tests for the three call sites.
- **Modify** `internal/server/server.go` — add `throttle` to `sessionHandler`, construct it in `NewSessionHandler`, inject it in `sessionFor`.
- **Modify** `internal/server/server_test.go` — test that all sessions from one handler share the throttle.
- **Modify** `CLAUDE.md` — note the new package members and the audit-detail format.

---

## Task 1: System parameters (sysconfig catalog)

**Files:**
- Modify: `internal/sysconfig/catalog.go`
- Test: `internal/sysconfig/catalog_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/sysconfig/catalog_test.go`:

```go
func TestCatalogThrottleParams(t *testing.T) {
	want := map[string]string{
		KeyAuthDelayBaseSecs:  "2",
		KeyAuthMaxTries:       "5",
		KeyAuthFailWindowMins: "15",
	}
	for key, def := range want {
		var found *Entry
		for i := range Catalog {
			if Catalog[i].Key == key {
				found = &Catalog[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("%s not in catalog", key)
		}
		if found.Label == "" {
			t.Errorf("%s label is empty", key)
		}
		if found.Default != def {
			t.Errorf("%s default = %q, want %q", key, found.Default, def)
		}
		if found.Validate == nil {
			t.Fatalf("%s Validate is nil", key)
		}
	}
}

func TestThrottleValidators(t *testing.T) {
	base := entryByKey(t, KeyAuthDelayBaseSecs)
	if msg := base.Validate("0"); msg != "" { // 0 is the off-switch, must be valid
		t.Errorf("base 0: got %q, want valid", msg)
	}
	if msg := base.Validate("-1"); msg == "" {
		t.Error("base -1: want error")
	}
	if msg := base.Validate("x"); msg == "" {
		t.Error("base x: want error")
	}
	win := entryByKey(t, KeyAuthFailWindowMins)
	if msg := win.Validate("0"); msg == "" { // window must be >= 1
		t.Error("window 0: want error")
	}
	if msg := win.Validate("1"); msg != "" {
		t.Errorf("window 1: got %q, want valid", msg)
	}
}

func entryByKey(t *testing.T, key string) *Entry {
	t.Helper()
	for i := range Catalog {
		if Catalog[i].Key == key {
			return &Catalog[i]
		}
	}
	t.Fatalf("%s not in catalog", key)
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sysconfig/ -run 'Throttle|ThrottleParams' -v`
Expected: FAIL — `undefined: KeyAuthDelayBaseSecs` (does not compile yet).

- [ ] **Step 3: Add the constants, entries, and validators**

In `internal/sysconfig/catalog.go`, change the import block to add `strconv`:

```go
import (
	"strconv"
	"strings"
)
```

Add after the `KeyMFAIssuer` const block:

```go
// Throttle params (GH #48): per-username failed-auth backoff. After each failed
// password or MFA attempt the session delays the next prompt by
// AUTH_DELAY_BASE_SECS * min(failcount, AUTH_MAX_TRIES) seconds; the per-username
// count decays after AUTH_FAIL_WINDOW_MINS of no failures. Setting the base to 0
// disables throttling entirely.
const (
	KeyAuthDelayBaseSecs  = "AUTH_DELAY_BASE_SECS"
	KeyAuthMaxTries       = "AUTH_MAX_TRIES"
	KeyAuthFailWindowMins = "AUTH_FAIL_WINDOW_MINS"

	DefaultAuthDelayBaseSecs  = 2
	DefaultAuthMaxTries       = 5
	DefaultAuthFailWindowMins = 15
)
```

Add these three entries inside the `Catalog` slice literal (after the `KeyMFAIssuer` entry, before the closing `}`):

```go
	{
		Key:      KeyAuthDelayBaseSecs,
		Label:    "Auth Delay Base (sec):",
		Default:  strconv.Itoa(DefaultAuthDelayBaseSecs),
		Validate: nonNegativeInt,
	},
	{
		Key:      KeyAuthMaxTries,
		Label:    "Max Auth Tries:",
		Default:  strconv.Itoa(DefaultAuthMaxTries),
		Validate: nonNegativeInt,
	},
	{
		Key:      KeyAuthFailWindowMins,
		Label:    "Auth Fail Window (min):",
		Default:  strconv.Itoa(DefaultAuthFailWindowMins),
		Validate: positiveInt,
	},
```

Add the two validator helpers at the end of the file:

```go
// nonNegativeInt accepts "0" and positive integers (used by the delay base and
// max-tries params; 0 disables their effect).
func nonNegativeInt(v string) string {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return "MUST BE A NON-NEGATIVE INTEGER"
	}
	return ""
}

// positiveInt requires an integer >= 1 (used by the fail-window param).
func positiveInt(v string) string {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return "MUST BE A POSITIVE INTEGER"
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sysconfig/ -v`
Expected: PASS (including the existing `TestCatalogKeysAreUppercase`, which now also covers the new keys).

- [ ] **Step 5: Commit**

```bash
git add internal/sysconfig/catalog.go internal/sysconfig/catalog_test.go
git commit -m "feat(sysconfig): auth-throttle params (base/max-tries/window) — GH #48"
```

---

## Task 2: `authThrottle` component

**Files:**
- Create: `internal/server/auththrottle.go`
- Test: `internal/server/auththrottle_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/server/auththrottle_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run AuthThrottle -v`
Expected: FAIL — `undefined: newAuthThrottle`.

- [ ] **Step 3: Write the component**

Create `internal/server/auththrottle.go` (start with the standard GPL header copied from `internal/server/limiter.go`, then):

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run AuthThrottle -race -v`
Expected: PASS (all seven tests, no data race).

- [ ] **Step 5: Commit**

```bash
git add internal/server/auththrottle.go internal/server/auththrottle_test.go
git commit -m "feat(server): per-username authThrottle counter with decay — GH #48"
```

---

## Task 3: Session throttle glue (fields + helpers)

**Files:**
- Modify: `internal/server/session.go` (add two `Session` fields)
- Modify: `internal/server/auththrottle.go` (add the `Session` helper methods)
- Test: `internal/server/auththrottle_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/auththrottle_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run 'ThrottleDelay|SleepFor|ThrottleDetail' -v`
Expected: FAIL — `undefined: throttleConfig` / `s.throttleDelay undefined`.

- [ ] **Step 3a: Add the two Session fields**

In `internal/server/session.go`, add to the `Session` struct (after the `MFAGenerate` field, before the closing `}`):

```go
	// Throttle applies per-username backoff to failed auth attempts (GH #48);
	// nil disables throttling (no delay). Shared across sessions by the handler.
	Throttle *authThrottle
	// Sleep delays the next prompt after a failed attempt; nil → time.Sleep.
	// Tests inject a recorder to assert the computed delay without waiting.
	Sleep func(time.Duration)
```

- [ ] **Step 3b: Add the helper methods**

Append to `internal/server/auththrottle.go`. First extend the import block to:

```go
import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
)
```

Then add:

```go
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
	return throttleConfig{
		baseSecs: s.throttleInt(ctx, sysconfig.KeyAuthDelayBaseSecs, sysconfig.DefaultAuthDelayBaseSecs),
		maxTries: s.throttleInt(ctx, sysconfig.KeyAuthMaxTries, sysconfig.DefaultAuthMaxTries),
		window:   time.Duration(s.throttleInt(ctx, sysconfig.KeyAuthFailWindowMins, sysconfig.DefaultAuthFailWindowMins)) * time.Minute,
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
// base or max-tries of 0 yields 0 (throttling disabled).
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
	return time.Duration(cfg.baseSecs*mult) * time.Second
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'ThrottleDelay|SleepFor|ThrottleDetail' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/auththrottle.go internal/server/auththrottle_test.go
git commit -m "feat(server): session throttle helpers (delay/sleep/detail) — GH #48"
```

---

## Task 4: Integrate throttling into `doLogin`

**Files:**
- Modify: `internal/server/session.go` (the `doLogin` auth-fail block and the success return)
- Test: `internal/server/session_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/session_test.go`:

```go
// fixedNow returns a deterministic clock for throttle tests.
func fixedNow() time.Time { return time.Unix(1_700_000_000, 0) }

func TestLoginThrottleBacksOffAndResets(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "bad"},  // fail 1 → 2s
			{user: "alice", pass: "bad"},  // fail 2 → 4s
			{user: "alice", pass: "good"}, // success → reset
			{quit: true},                  // logoff at next login
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Throttle = newAuthThrottle()
	s.Now = fixedNow
	var slept []time.Duration
	s.Sleep = func(d time.Duration) { slept = append(slept, d) }

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []time.Duration{2 * time.Second, 4 * time.Second}
	if len(slept) != len(want) || slept[0] != want[0] || slept[1] != want[1] {
		t.Fatalf("delays = %v, want %v", slept, want)
	}
	if n, ok := s.Throttle.peek("alice"); ok {
		t.Errorf("counter not reset on success: count=%d", n)
	}
}

func TestLoginThrottleDisabledWhenBaseZero(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "bad"}, {quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Throttle = newAuthThrottle()
	s.Now = fixedNow
	var slept []time.Duration
	s.Sleep = func(d time.Duration) { slept = append(slept, d) }
	if err := s.Store.SetConfig(context.Background(), "AUTH_DELAY_BASE_SECS", "0"); err != nil {
		t.Fatal(err)
	}

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(slept) != 0 {
		t.Errorf("base=0 should disable throttling, slept=%v", slept)
	}
}

func TestLoginThrottleEnumerationSafe(t *testing.T) {
	// A first failure for an unknown username and for a known one must produce
	// the same delay — the throttle must not reveal whether a user exists.
	delayFor := func(user string) time.Duration {
		p := &fakePresenter{
			termType: "IBM-3278-2-E",
			logins:   []loginResult{{user: user, pass: "bad"}, {quit: true}},
		}
		s := newTestSession(t, p, &fakeBridger{})
		s.Throttle = newAuthThrottle()
		s.Now = fixedNow
		var slept []time.Duration
		s.Sleep = func(d time.Duration) { slept = append(slept, d) }
		client, _ := net.Pipe()
		defer client.Close()
		s.Run(client)
		if len(slept) != 1 {
			t.Fatalf("%s: expected one delay, got %v", user, slept)
		}
		return slept[0]
	}
	if delayFor("ghost") != delayFor("alice") {
		t.Error("unknown vs known username produced different delays")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run 'LoginThrottle' -v`
Expected: FAIL — delays are empty (no integration yet).

- [ ] **Step 3: Integrate into `doLogin`**

In `internal/server/session.go`, in `doLogin`, replace the success block:

```go
		identity, err := s.Authenticate(ctx, s.Store, user, pass)
		if err == nil {
			aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthOK, Username: identity.Username})
			return identity, true, ""
		}
```

with (add the `Reset`):

```go
		identity, err := s.Authenticate(ctx, s.Store, user, pass)
		if err == nil {
			s.Throttle.Reset(user)
			aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthOK, Username: identity.Username})
			return identity, true, ""
		}
```

Then replace the auth-fail tail of the loop:

```go
		// Auth fail: log the attempted username only — never the password.
		s.log().Warn("auth failed", "user", user)
		// Attempted username only — never the password (CLAUDE.md hard rule).
		aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthFail, Username: user})
		// Generic message — never reveals whether the username exists (spec §7).
		errMsg = "Invalid userid or password"
```

with (record the delay, then sleep before re-prompting):

```go
		// Auth fail: per-username backoff (GH #48). Compute before auditing so
		// the audit detail records the applied delay; never log the password.
		delay, count := s.failDelay(ctx, user)
		s.log().Warn("auth failed", "user", user)
		// Attempted username only — never the password (CLAUDE.md hard rule).
		aud.record(ctx, store.AuditEvent{
			Kind: store.AuditAuthFail, Username: user, Detail: throttleDetail("", delay, count)})
		s.sleepFor(delay)
		// Generic message — never reveals whether the username exists (spec §7).
		errMsg = "Invalid userid or password"
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'LoginThrottle|TestSession' -v`
Expected: PASS — the new throttle tests pass and the existing `TestSession*` login tests still pass (nil throttle in `newTestSession` → no delay).

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/session_test.go
git commit -m "feat(server): apply per-username backoff in doLogin — GH #48"
```

---

## Task 5: Integrate throttling into `mfaVerify` and `mfaEnroll`

**Files:**
- Modify: `internal/server/session.go` (the `mfaVerify` and `mfaEnroll` wrong-code blocks + success resets)
- Test: `internal/server/session_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/session_test.go`:

```go
func TestMFAVerifyThrottleBacksOff(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	good := codeForServer(t, secret, step)

	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		verifies: []mfaResult{
			{code: "000000"}, // wrong → 2s
			{code: "111111"}, // wrong → 4s
			{code: good},     // correct → reset
		},
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s, st := newMFATestSession(t, p, &fakeBridger{})
	s.Throttle = newAuthThrottle() // Now is already fixed by newMFATestSession
	var slept []time.Duration
	s.Sleep = func(d time.Duration) { slept = append(slept, d) }
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "x")
	st.SetMFARequired(ctx, uid, true)
	enc, _ := s.MFA.Seal([]byte(secret))
	st.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []time.Duration{2 * time.Second, 4 * time.Second}
	if len(slept) != len(want) || slept[0] != want[0] || slept[1] != want[1] {
		t.Fatalf("mfa delays = %v, want %v", slept, want)
	}
	if n, ok := s.Throttle.peek("alice"); ok {
		t.Errorf("counter not reset after correct code: count=%d", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run 'MFAVerifyThrottle' -v`
Expected: FAIL — `mfa delays = [] want [2s 4s]`.

- [ ] **Step 3a: Integrate into `mfaVerify`**

In `internal/server/session.go`, in `mfaVerify`, replace the wrong-code block:

```go
		if !ok {
			aud.record(ctx, store.AuditEvent{Kind: store.AuditMFAFailed, Username: u.Username, Detail: "login"})
			errMsg = "Code incorrect - try again"
			continue
		}
```

with:

```go
		if !ok {
			delay, count := s.failDelay(ctx, u.Username)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditMFAFailed, Username: u.Username, Detail: throttleDetail("login", delay, count)})
			s.sleepFor(delay)
			errMsg = "Code incorrect - try again"
			continue
		}
```

Then, in the same function, add a `Reset` on the success path. Replace:

```go
		if err := s.Store.UpdateMFAStep(ctx, u.ID, int64(step)); err != nil {
			return false, "mfa store error", err
		}
		aud.record(ctx, store.AuditEvent{Kind: store.AuditMFASuccess, Username: u.Username})
		return true, "", nil
```

with:

```go
		if err := s.Store.UpdateMFAStep(ctx, u.ID, int64(step)); err != nil {
			return false, "mfa store error", err
		}
		s.Throttle.Reset(u.Username)
		aud.record(ctx, store.AuditEvent{Kind: store.AuditMFASuccess, Username: u.Username})
		return true, "", nil
```

- [ ] **Step 3b: Integrate into `mfaEnroll`**

In `mfaEnroll`, replace the wrong-code block:

```go
		if !ok {
			aud.record(ctx, store.AuditEvent{Kind: store.AuditMFAFailed, Username: u.Username, Detail: "enroll"})
			errMsg = "Code incorrect - check the key and try again"
			continue
		}
```

with:

```go
		if !ok {
			delay, count := s.failDelay(ctx, u.Username)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditMFAFailed, Username: u.Username, Detail: throttleDetail("enroll", delay, count)})
			s.sleepFor(delay)
			errMsg = "Code incorrect - check the key and try again"
			continue
		}
```

Then add a `Reset` on the enroll success path. Replace:

```go
		aud.record(ctx, store.AuditEvent{Kind: store.AuditMFAEnrolled, Username: u.Username})
		return true, "", nil
```

with:

```go
		s.Throttle.Reset(u.Username)
		aud.record(ctx, store.AuditEvent{Kind: store.AuditMFAEnrolled, Username: u.Username})
		return true, "", nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'MFA' -v`
Expected: PASS — the new `TestMFAVerifyThrottleBacksOff` and all existing `TestMFA*` tests (nil throttle in those → no delay).

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/session_test.go
git commit -m "feat(server): apply shared per-username backoff to MFA verify/enroll — GH #48"
```

---

## Task 6: Wire the shared throttle through the handler

**Files:**
- Modify: `internal/server/server.go` (`sessionHandler` field, `NewSessionHandler`, `sessionFor`)
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/server_test.go`:

```go
func TestHandlerSharesThrottleAcrossSessions(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	h := NewSessionHandler(st, 0x6B, Limits{}, nil, "", nil).(sessionHandler)
	a1 := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	a2 := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 2}
	s1 := h.sessionFor(a1, slog.Default())
	s2 := h.sessionFor(a2, slog.Default())

	if s1.Throttle == nil {
		t.Fatal("session throttle is nil")
	}
	if s1.Throttle != s2.Throttle {
		t.Error("sessions from one handler must share the throttle")
	}
}
```

If `server_test.go` does not already import `log/slog`, add it to that file's import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run 'HandlerSharesThrottle' -v`
Expected: FAIL — `s1.Throttle is nil`.

- [ ] **Step 3: Wire it**

In `internal/server/server.go`, add a field to `sessionHandler` (after `mfaCipher`):

```go
	throttle  *authThrottle
```

In `NewSessionHandler`, construct it. Replace:

```go
	return sessionHandler{store: st, escapeAID: escapeAID, limits: limits, logger: logger, release: release, mfaCipher: mfaCipher}
```

with:

```go
	return sessionHandler{store: st, escapeAID: escapeAID, limits: limits, logger: logger, release: release, mfaCipher: mfaCipher, throttle: newAuthThrottle()}
```

In `sessionFor`, inject it into the `Session` literal (add after `MFA: h.mfaCipher,`):

```go
		Throttle:         h.throttle,
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'HandlerSharesThrottle' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): share one authThrottle across all sessions of a handler — GH #48"
```

---

## Task 7: Full verification + docs

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Run the whole suite with the race detector**

Run: `go build ./... && go test ./... -race`
Expected: PASS across all packages (the bridge/session are concurrent — race must be clean).

- [ ] **Step 2: Update CLAUDE.md**

In the `internal/server` package-map entry, add a sentence after the Hardening paragraph:

```
                  Failed-auth throttling (GH #48): authThrottle (auththrottle.go)
                  applies per-username linear backoff (delay = AUTH_DELAY_BASE_SECS
                  * min(failcount, AUTH_MAX_TRIES)) shared across password + MFA
                  failures; one instance per handler, keyed on the normalized
                  username (IP-agnostic, so the web client's shared IP is fine),
                  counts reset on success and decay after AUTH_FAIL_WINDOW_MINS.
                  The applied delay is recorded in the auth_fail/mfa_failed audit
                  Detail (delay=Xs count=N). base=0 disables it.
```

In the `internal/sysconfig` reference (Conventions or package map, wherever sysconfig params are described), note the three new keys: `AUTH_DELAY_BASE_SECS`, `AUTH_MAX_TRIES`, `AUTH_FAIL_WINDOW_MINS`.

- [ ] **Step 3: Manual check of the admin form (no automated coverage)**

The three params now render on the System Parameters admin screen (five entries total). Confirm in a real emulator — or note in the PR — that all five fields fit and edit correctly. There is no screen/cursor change in the session flow itself, so the s3270 smoke script is not required for this change.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document auth-throttling (GH #48)"
```

---

## Self-review notes (for the implementer)

- **Nil-safety is load-bearing:** `newTestSession` leaves `Throttle`/`Sleep` nil, so every existing session test must stay green. `Fail`/`Reset` are nil-safe and `failDelay` short-circuits on a nil throttle → zero delay. Do not remove these guards.
- **Enumeration invariant:** the delay is a pure function of `count`; the on-screen `errMsg` is unchanged for unknown-user / wrong-password / wrong-MFA. `TestLoginThrottleEnumerationSafe` guards this — keep it passing.
- **Shared key:** `doLogin` keys on the raw `user` input and `mfaVerify`/`mfaEnroll` on `u.Username`; both pass through `throttleKey` (upper+trim), so password and MFA failures for the same person share one counter (the unified path #48 requires).
- **Out of scope (do not implement here):** per-IP spray defense + aggregator exemption, PROXY-protocol real-client-IP recovery, hard lockout / admin-unlock / DB-persisted counters. These are follow-up issues per the design doc.
