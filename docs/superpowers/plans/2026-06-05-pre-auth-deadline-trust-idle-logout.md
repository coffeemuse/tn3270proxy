# Pre-auth deadline + trust list + idle-logout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close #18 by giving pre-auth connections an absolute wall-clock deadline, add a config-file trust list that exempts dedicated devices from pre-auth timers and the per-IP cap, and convert post-auth idle from a hard disconnect into a logout-to-login.

**Architecture:** A connection moves through *idle regimes* enforced by `idleConn`; the session switches regime at each lifecycle transition (`armPreAuth`/`armPostAuth`/`armBridge`). Trust (a `[]netip.Prefix` from config) selects the exempt pre-auth regime and bypasses the per-IP cap. Post-auth idle at the menu/admin becomes a logout (reusing the PF3-logoff transition), re-arming the pre-auth regime.

**Tech Stack:** Go, stdlib `net`/`net/netip`/`time`, `modernc.org/sqlite`, go3270. TDD per CLAUDE.md; run `go test ./... -race`.

**Spec:** `docs/superpowers/specs/2026-06-05-pre-auth-deadline-trust-idle-logout-design.md`

---

## File map

- `internal/store/audit.go` — add `AuditLogout` constant.
- `internal/server/idleconn.go` — add `hard` ceiling; replace `SetIdle` with `setPreAuth`/`setWindow`; rewrite `arm()`.
- `internal/server/idleconn_test.go` — replace the `SetIdle` test with ceiling/window/exempt tests.
- `internal/server/trust.go` *(new)* — `trustList` + `Contains`.
- `internal/server/trust_test.go` *(new)*.
- `internal/config/config.go` — `Limits` fields `PreAuthMax`/`TrustedCIDRs`/`BridgeIdleExempt`; file keys, flag, parse, validate.
- `internal/config/config_test.go` — tests for the three new keys.
- `internal/server/limiter.go` — `admitIP`/`releaseIP` gain a `trusted bool`.
- `internal/server/limiter_test.go` — update calls + add trusted-bypass test.
- `internal/server/server.go` — `Server.Trust`; `Serve`/`handle` thread `trusted`; `sessionHandler` populates new `Session` fields; `newServers` sets `Trust`.
- `internal/server/session.go` — `Session` fields; `idleRegime` seam; `armPreAuth`/`armPostAuth`/`armBridge`; transitions; idle-logout at menu+admin; PF3-logoff audit. Remove `idleSetter`/`setIdle`.
- `internal/server/session_idle_test.go` — update `idleRecordingConn` + the two regime tests; add idle-logout / trusted / bridge-exempt tests.
- `cmd/tn3270proxy/main.go` — map new `cfg.Limits` fields into `server.Limits`.
- `CLAUDE.md` — document the regime model + new knobs.
- `.claude/skills/s3270-smoke-testing/smoke.sh` — assert idle-logout returns to login.

---

## Task 1: Add the `AuditLogout` audit kind

**Files:**
- Modify: `internal/store/audit.go:30-37`

- [ ] **Step 1: Add the constant**

In the `const (...)` block of audit kinds (after `AuditAdmin`), add:

```go
	AuditLogout      = "logout"       // session ended login (detail: "user logoff" | "idle logout")
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success (a new const has no behavior to break).

- [ ] **Step 3: Commit**

```bash
git add internal/store/audit.go
git commit -m "feat(store): add AuditLogout audit kind (GH #18)"
```

---

## Task 2: `idleConn` regime model — absolute ceiling + window setters

Replace the single sliding `idle` with `idle` + an optional absolute `hard` ceiling, and expose two regime setters. `SetIdle` is removed (no caller after Task 6; its test is rewritten here).

**Files:**
- Modify: `internal/server/idleconn.go`
- Test: `internal/server/idleconn_test.go`

- [ ] **Step 1: Write the failing tests**

Replace the existing `TestIdleConnSetIdleSwitchesWindow` (the test around `idleconn_test.go:127` that calls `ic.SetIdle`) with these. Keep all other tests in the file unchanged.

```go
func TestIdleConnHardCeilingClampsBelowIdleWindow(t *testing.T) {
	c, srv := net.Pipe()
	defer c.Close()
	defer srv.Close()
	ic := newIdleConn(c, 10*time.Second) // wide idle window
	ic.setPreAuth(10*time.Second, 50*time.Millisecond) // tight absolute ceiling

	go func() { srv.Read(make([]byte, 1)) }() // never writes; ic.Read must time out at the ceiling
	start := time.Now()
	ic.Read(make([]byte, 1))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("read blocked %v; hard ceiling should fire ~50ms", elapsed)
	}
}

func TestIdleConnSetWindowClearsCeiling(t *testing.T) {
	c, srv := net.Pipe()
	defer c.Close()
	defer srv.Close()
	ic := newIdleConn(c, 10*time.Second)
	ic.setPreAuth(10*time.Second, 50*time.Millisecond) // ceiling armed...
	ic.setWindow(10 * time.Second)                     // ...then post-auth clears it

	go func() {
		time.Sleep(120 * time.Millisecond) // past the old ceiling
		srv.Write([]byte{0x00})
	}()
	start := time.Now()
	if _, err := ic.Read(make([]byte, 1)); err != nil {
		t.Fatalf("read errored after ceiling cleared: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("read returned too early (%v); ceiling should be gone", elapsed)
	}
}

func TestIdleConnExemptWindowNeverFires(t *testing.T) {
	c, srv := net.Pipe()
	defer c.Close()
	defer srv.Close()
	ic := newIdleConn(c, 50*time.Millisecond)
	ic.setWindow(0) // exempt: no deadline

	go func() {
		time.Sleep(120 * time.Millisecond) // past the old 50ms idle
		srv.Write([]byte{0x00})
	}()
	start := time.Now()
	if _, err := ic.Read(make([]byte, 1)); err != nil {
		t.Fatalf("exempt read errored: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("exempt read returned too early (%v)", elapsed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run TestIdleConn -v`
Expected: compile failure — `ic.setPreAuth`/`ic.setWindow` undefined.

- [ ] **Step 3: Implement the regime model**

In `internal/server/idleconn.go`, add a `hard time.Time` field to the struct and update its doc comment:

```go
// idleConn wraps a net.Conn and arms a deadline before every Read and Write so
// a stalled peer cannot pin the connection (slowloris hardening, GH #1/#18).
//
// Two bounds compose: a sliding idle window (idle) re-armed on every byte, and
// an optional absolute ceiling (hard) that does NOT slide — used pre-auth so a
// trickle cannot hold a slot forever. The earlier of the two fires. idle<=0
// with no ceiling disables the deadline (trusted/exempt regimes).
//
// An explicit non-zero deadline set through the wrapper suspends auto-arming
// until a zero deadline clears it (the bridge teardown interrupt relies on
// this). mu serializes deadline decisions.
type idleConn struct {
	net.Conn

	mu     sync.Mutex
	idle   time.Duration
	hard   time.Time // absolute ceiling; zero = none
	manual bool      // explicit deadline in force; auto-arm suspended
}
```

Rewrite `arm()` and replace `SetIdle` with the two setters (delete the old `SetIdle` method):

```go
// arm sets the deadline to the earlier of (now+idle) and the absolute ceiling,
// unless an explicit deadline is in force. A zero result clears the deadline.
func (c *idleConn) arm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.manual {
		return
	}
	var d time.Time
	if c.idle > 0 {
		d = time.Now().Add(c.idle)
	}
	if !c.hard.IsZero() && (d.IsZero() || c.hard.Before(d)) {
		d = c.hard
	}
	c.Conn.SetDeadline(d)
}

// setPreAuth installs the pre-auth regime: a sliding idle window plus an
// absolute ceiling now+max by which authentication must complete. The ceiling
// does not slide with activity; re-call to re-arm it (e.g. at logoff).
func (c *idleConn) setPreAuth(idle, max time.Duration) {
	c.mu.Lock()
	c.idle = idle
	c.hard = time.Now().Add(max)
	c.mu.Unlock()
}

// setWindow installs a plain sliding idle window with no ceiling (post-auth and
// bridge regimes). idle<=0 disables the deadline entirely (trusted pre-auth
// exemption, or bridge_idle=exempt).
func (c *idleConn) setWindow(idle time.Duration) {
	c.mu.Lock()
	c.idle = idle
	c.hard = time.Time{}
	c.mu.Unlock()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run TestIdleConn -race -v`
Expected: PASS (new tests green; the existing `manual`/zero-deadline suspend tests still pass).

- [ ] **Step 5: Commit**

```bash
git add internal/server/idleconn.go internal/server/idleconn_test.go
git commit -m "feat(server): idleConn absolute pre-auth ceiling + window setters (GH #18)"
```

---

## Task 3: Trust list

**Files:**
- Create: `internal/server/trust.go`
- Create: `internal/server/trust_test.go`

- [ ] **Step 1: Write the failing test**

```go
package server

import (
	"net"
	"net/netip"
	"testing"
)

func tcp(addr string) net.Addr { return &net.TCPAddr{IP: net.ParseIP(addr), Port: 23} }

func TestTrustListContains(t *testing.T) {
	tl := trustList{
		netip.MustParsePrefix("10.0.0.0/24"),
		netip.MustParsePrefix("192.168.1.5/32"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	cases := []struct {
		addr net.Addr
		want bool
	}{
		{tcp("10.0.0.7"), true},
		{tcp("10.0.1.7"), false},
		{tcp("192.168.1.5"), true},
		{tcp("192.168.1.6"), false},
		{tcp("2001:db8::1"), true},
		{tcp("2001:dead::1"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := tl.Contains(c.addr); got != c.want {
			t.Errorf("Contains(%v) = %v, want %v", c.addr, got, c.want)
		}
	}
}

func TestTrustListEmptyIsAlwaysFalse(t *testing.T) {
	var tl trustList
	if tl.Contains(tcp("10.0.0.1")) {
		t.Error("empty trust list must trust nobody")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestTrustList -v`
Expected: compile failure — `trustList` undefined.

- [ ] **Step 3: Implement**

Create `internal/server/trust.go` (GPL header omitted here — copy it from any sibling file):

```go
package server

import (
	"net"
	"net/netip"
)

// trustList holds the operator-configured trusted client networks (GH #18).
// A trusted client is exempt from the pre-auth idle/ceiling timers and the
// per-IP connection cap (it is still counted toward the global cap). An empty
// list trusts nobody. It is a plain []netip.Prefix so config's already-parsed
// prefixes convert in for free.
type trustList []netip.Prefix

// Contains reports whether addr's host IP falls in any trusted network.
func (t trustList) Contains(addr net.Addr) bool {
	if len(t) == 0 {
		return false
	}
	host := ipKey(addr) // reuses limiter.go: host portion, or "" if unparseable
	if host == "" {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap() // normalize 4-in-6 so v4 prefixes match
	for _, p := range t {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestTrustList -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/trust.go internal/server/trust_test.go
git commit -m "feat(server): trustList for trusted client networks (GH #18)"
```

---

## Task 4: Config — `pre_auth_max`, `trusted_cidrs`, `bridge_idle`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go` (add `"net/netip"` and `"os"` to its imports if missing):

```go
func writeCfg(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/c.json"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaultsNewLimits(t *testing.T) {
	cfg, err := Load([]string{"-config", writeCfg(t, `{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.PreAuthMax != 5*time.Minute {
		t.Errorf("PreAuthMax default = %v, want 5m", cfg.Limits.PreAuthMax)
	}
	if len(cfg.Limits.TrustedCIDRs) != 0 {
		t.Errorf("TrustedCIDRs default = %v, want empty", cfg.Limits.TrustedCIDRs)
	}
	if cfg.Limits.BridgeIdleExempt {
		t.Error("BridgeIdleExempt default = true, want false")
	}
}

func TestLoadTrustedCIDRsParsesIPAndCIDR(t *testing.T) {
	path := writeCfg(t, `{"limits":{"trusted_cidrs":["10.0.0.0/24","192.168.1.5"]}}`)
	cfg, err := Load([]string{"-config", path})
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
		netip.MustParsePrefix("192.168.1.5/32"), // bare IP → /32
	}
	if len(cfg.Limits.TrustedCIDRs) != 2 ||
		cfg.Limits.TrustedCIDRs[0] != want[0] || cfg.Limits.TrustedCIDRs[1] != want[1] {
		t.Errorf("TrustedCIDRs = %v, want %v", cfg.Limits.TrustedCIDRs, want)
	}
}

func TestLoadTrustedCIDRsRejectsGarbage(t *testing.T) {
	path := writeCfg(t, `{"limits":{"trusted_cidrs":["not-an-ip"]}}`)
	if _, err := Load([]string{"-config", path}); err == nil {
		t.Fatal("expected error for malformed trusted_cidrs")
	}
}

func TestLoadBridgeIdle(t *testing.T) {
	for _, c := range []struct {
		val        string
		wantExempt bool
		wantErr    bool
	}{
		{`"disconnect"`, false, false},
		{`"exempt"`, true, false},
		{`"bogus"`, false, true},
	} {
		path := writeCfg(t, `{"limits":{"bridge_idle":`+c.val+`}}`)
		cfg, err := Load([]string{"-config", path})
		if c.wantErr {
			if err == nil {
				t.Errorf("bridge_idle=%s: expected error", c.val)
			}
			continue
		}
		if err != nil {
			t.Fatalf("bridge_idle=%s: %v", c.val, err)
		}
		if cfg.Limits.BridgeIdleExempt != c.wantExempt {
			t.Errorf("bridge_idle=%s: exempt=%v, want %v", c.val, cfg.Limits.BridgeIdleExempt, c.wantExempt)
		}
	}
}

func TestLoadPreAuthMaxFlagOverrides(t *testing.T) {
	path := writeCfg(t, `{"limits":{"pre_auth_max":"5m"}}`)
	cfg, err := Load([]string{"-config", path, "-pre-auth-max", "90s"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.PreAuthMax != 90*time.Second {
		t.Errorf("PreAuthMax = %v, want 90s (flag overrides file)", cfg.Limits.PreAuthMax)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestLoad(DefaultsNewLimits|TrustedCIDRs|BridgeIdle|PreAuthMax)' -v`
Expected: compile failure — `cfg.Limits.PreAuthMax` etc. undefined.

- [ ] **Step 3: Implement — struct + defaults**

In `internal/config/config.go`, add `"net/netip"` to imports. Extend `Limits` (after `MaxPerIP`):

```go
	PreAuthMax       time.Duration  // absolute deadline to authenticate (GH #18)
	TrustedCIDRs     []netip.Prefix // clients exempt from pre-auth timers + per-IP cap
	BridgeIdleExempt bool           // true → no idle timeout during an active bridge
```

Extend the `fileConfig` `Limits` struct (after `MaxPerIP`):

```go
		PreAuthMax   *string  `json:"pre_auth_max"`  // Go duration string, e.g. "5m"
		TrustedCIDRs []string `json:"trusted_cidrs"` // IPs or CIDRs
		BridgeIdle   *string  `json:"bridge_idle"`   // "disconnect" (default) | "exempt"
```

Add a default constant (in the `const (...)` block) and set it in `defaults()`:

```go
	defaultPreAuthMax  = 5 * time.Minute
```

```go
		Limits: Limits{
			PreAuthIdle: defaultPreAuthIdle,
			Idle:        defaultIdle,
			MaxConns:    defaultMaxConns,
			MaxPerIP:    defaultMaxPerIP,
			PreAuthMax:  defaultPreAuthMax,
		},
```

- [ ] **Step 4: Implement — flag, file merge, parse helper, validate**

Add the flag in `Load` (next to the other limits flags):

```go
	preAuthMax := fs.Duration("pre-auth-max", 0, "absolute deadline to authenticate (overrides config)")
```

Apply it after the `pre-auth-idle` block:

```go
	if set["pre-auth-max"] {
		cfg.Limits.PreAuthMax = *preAuthMax
	}
```

In `mergeFile`, inside `if l := fc.Limits; l != nil {`, after the `MaxPerIP` handling add:

```go
		if l.PreAuthMax != nil {
			d, err := time.ParseDuration(*l.PreAuthMax)
			if err != nil {
				return fmt.Errorf("config: limits.pre_auth_max: %w", err)
			}
			cfg.Limits.PreAuthMax = d
		}
		if l.TrustedCIDRs != nil {
			prefixes, err := parseTrusted(l.TrustedCIDRs)
			if err != nil {
				return err
			}
			cfg.Limits.TrustedCIDRs = prefixes
		}
		if l.BridgeIdle != nil {
			switch *l.BridgeIdle {
			case "disconnect":
				cfg.Limits.BridgeIdleExempt = false
			case "exempt":
				cfg.Limits.BridgeIdleExempt = true
			default:
				return fmt.Errorf("config: limits.bridge_idle: %q (want \"disconnect\" or \"exempt\")", *l.BridgeIdle)
			}
		}
```

Add the parse helper (package-level):

```go
// parseTrusted converts trusted_cidrs entries (IP or CIDR) to prefixes; a bare
// IP becomes a host route (/32 or /128).
func parseTrusted(entries []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(entries))
	for _, s := range entries {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
			continue
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("config: limits.trusted_cidrs: %q is not an IP or CIDR", s)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}
```

In `validate`, after the `PreAuthIdle` check add:

```go
	if cfg.Limits.PreAuthMax <= 0 {
		return errors.New("config: limits.pre_auth_max must be positive")
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (new + existing).

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): pre_auth_max, trusted_cidrs, bridge_idle limits (GH #18)"
```

---

## Task 5: Plumb new limits into `server.Limits` and `cmd`

This only widens structs and the mapping; behavior lands in later tasks. Keeps the build green.

**Files:**
- Modify: `internal/server/session.go:36-41` (the `Limits` struct)
- Modify: `cmd/tn3270proxy/main.go:85-90`

- [ ] **Step 1: Extend `server.Limits`**

In `internal/server/session.go`, add `"net/netip"` to imports and extend `Limits` (after `MaxPerIP`):

```go
	PreAuthMax       time.Duration  // absolute deadline to authenticate (GH #18)
	TrustedCIDRs     []netip.Prefix // trusted client networks
	BridgeIdleExempt bool           // no idle timeout during an active bridge
```

- [ ] **Step 2: Map in `cmd`**

In `cmd/tn3270proxy/main.go`, extend the `server.Limits{...}` literal:

```go
	limits := server.Limits{
		PreAuthIdle:      cfg.Limits.PreAuthIdle,
		Idle:             cfg.Limits.Idle,
		MaxConns:         cfg.Limits.MaxConns,
		MaxPerIP:         cfg.Limits.MaxPerIP,
		PreAuthMax:       cfg.Limits.PreAuthMax,
		TrustedCIDRs:     cfg.Limits.TrustedCIDRs,
		BridgeIdleExempt: cfg.Limits.BridgeIdleExempt,
	}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/server/session.go cmd/tn3270proxy/main.go
git commit -m "chore(server): carry pre-auth-max/trust/bridge-idle into server.Limits (GH #18)"
```

---

## Task 6: Session state machine — regime seam, transitions, idle-logout

The heart of the change. Replace the `idleSetter`/`setIdle` seam with `idleRegime` + three `arm*` helpers; wire transitions; convert post-auth idle (menu + admin) to logout; audit PF3-logoff.

**Files:**
- Modify: `internal/server/session.go`
- Test: `internal/server/session_idle_test.go`

- [ ] **Step 1: Update the test fake + rewrite the two regime tests**

In `internal/server/session_idle_test.go`, replace `idleRecordingConn` and its `SetIdle` method with a transcript recorder implementing the new seam:

```go
// idleRecordingConn stands in for the idleConn wrapper, recording the regime
// transitions the session drives.
type idleRecordingConn struct {
	net.Conn
	calls []string
}

func (c *idleRecordingConn) setPreAuth(idle, max time.Duration) {
	c.calls = append(c.calls, "preauth:"+idle.String()+"/"+max.String())
}
func (c *idleRecordingConn) setWindow(idle time.Duration) {
	c.calls = append(c.calls, "window:"+idle.String())
}
```

Replace `TestSessionSwitchesIdleAfterAuthAndBackOnLogoff` and `TestSessionZeroIdleConfigLeavesConnAlone` with:

```go
func TestSessionRegimeTransitions(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true},
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.PreAuthIdle = 2 * time.Minute
	s.Idle = 30 * time.Minute
	s.PreAuthMax = 5 * time.Minute

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	want := []string{
		"preauth:2m0s/5m0s", // connect
		"window:30m0s",      // auth ok → post-auth
		"preauth:2m0s/5m0s", // PF3 logoff → back to pre-auth
	}
	if !equalStrings(client.calls, want) {
		t.Errorf("regime calls = %v, want %v", client.calls, want)
	}
}

func TestSessionZeroIdleConfigLeavesConnAlone(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{}) // PreAuthIdle/Idle/PreAuthMax left zero

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	if len(client.calls) != 0 {
		t.Errorf("regime calls = %v, want none when idle config is zero", client.calls)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/ -run 'TestSessionRegimeTransitions|TestSessionZeroIdleConfig' -v`
Expected: compile failure — `setPreAuth`/`setWindow` not used by session yet; `armPreAuth` undefined.

- [ ] **Step 3: Replace the idle seam in `session.go`**

Delete the `idleSetter` interface and `setIdle` helper (`session.go:88-102`). Add the new seam + fields. First, extend the `Session` struct (after the `Idle` field at ~`session.go:85`):

```go
	// PreAuthMax bounds total time to authenticate (absolute, re-armed at every
	// return to the login screen). Trusted skips all pre-auth timers; the
	// per-connection trust decision is made by the handler. BridgeIdleExempt
	// disables the idle deadline during an active bridge. (GH #18)
	PreAuthMax       time.Duration
	Trusted          bool
	BridgeIdleExempt bool
```

Add the seam and helpers (replacing the deleted `idleSetter`/`setIdle`):

```go
// idleRegime is implemented by *idleConn (and test fakes); the session switches
// the connection's idle regime at each lifecycle transition. A connection that
// doesn't implement it (idle hardening disabled) is left untouched.
type idleRegime interface {
	setPreAuth(idle, max time.Duration)
	setWindow(idle time.Duration)
}

// armPreAuth enters the pre-auth regime: trusted connections are exempt; others
// get the idle window plus a fresh absolute ceiling. No-op when idle hardening
// is off (PreAuthIdle<=0) and the client isn't trusted.
func (s *Session) armPreAuth(conn net.Conn) {
	r, ok := conn.(idleRegime)
	if !ok {
		return
	}
	if s.Trusted {
		r.setWindow(0) // exempt: park at login indefinitely
		return
	}
	if s.PreAuthIdle <= 0 {
		return
	}
	r.setPreAuth(s.PreAuthIdle, s.PreAuthMax)
}

// armPostAuth enters the post-auth regime (menu/admin): a plain idle window.
func (s *Session) armPostAuth(conn net.Conn) {
	if s.Idle <= 0 {
		return
	}
	if r, ok := conn.(idleRegime); ok {
		r.setWindow(s.Idle)
	}
}

// armBridge enters the bridge regime: the post-auth idle window, or no deadline
// when bridge idle is exempt.
func (s *Session) armBridge(conn net.Conn) {
	r, ok := conn.(idleRegime)
	if !ok {
		return
	}
	if s.BridgeIdleExempt {
		r.setWindow(0)
		return
	}
	if s.Idle <= 0 {
		return
	}
	r.setWindow(s.Idle)
}
```

- [ ] **Step 4: Wire the transitions in `Run`**

Make these edits in `Run` / its menu loop:

1. At the top of `Run`, right after `aud := s.newAuditTrail(conn)`:

```go
	s.armPreAuth(conn)
```

2. Replace `setIdle(conn, s.Idle)` (auth success, ~`session.go:146`) with:

```go
		s.armPostAuth(conn) // authenticated: post-auth idle window
```

3. Replace the menu-quit (PF3-logoff) block (~`session.go:166-170`):

```go
			if quit {
				aud.record(ctx, store.AuditEvent{
					Kind: store.AuditLogout, Username: identity.Username, Detail: "user logoff"})
				currentUser = ""
				s.armPreAuth(conn) // logoff: back to the pre-auth regime
				break menu
			}
```

4. Replace the menu-render error block (~`session.go:158-165`) so a post-auth idle timeout logs out instead of disconnecting:

```go
			selected, adminSel, quit, err := s.Presenter.Menu(conn, term, services, isAdmin, errMsg)
			if err != nil {
				if isTimeoutErr(err) {
					aud.record(ctx, store.AuditEvent{
						Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
					currentUser = ""
					s.armPreAuth(conn)
					break menu
				}
				endDetail = "menu render error"
				return
			}
```

5. In the admin-flow error block (~`session.go:182-189`), treat a timeout as idle-logout:

```go
				if aerr := flow.Run(ctx, conn); aerr != nil {
					if isTimeoutErr(aerr) {
						aud.record(ctx, store.AuditEvent{
							Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
						currentUser = ""
						s.armPreAuth(conn)
						break menu
					}
					log.Printf("admin flow for %s ended: %v", identity.Username, aerr)
					endDetail = "admin flow error"
					return
				}
```

6. Before the `Bridge` call (~`session.go:200`), enter the bridge regime; restore post-auth after the switch. Replace the bridge block:

```go
			s.armBridge(conn)
			cause, berr := s.Bridger.Bridge(conn, addr, term.Type, s.EscapeAID, btls)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditBridgeEnd, Username: identity.Username,
				Service: selected.Name, Detail: causeDetail(cause, berr)})
			switch cause {
			case bridge.CauseClientClosed:
				endDetail = "client closed during bridge"
				return
			case bridge.CauseError:
				log.Printf("bridge error to %s (%s): %v", selected.Name, addr, berr)
				errMsg = "Could not connect to " + selected.Name
				if berr == nil {
					errMsg = "Session error on " + selected.Name
				}
			default:
				// CauseBackendClosed or CauseUserEscaped → back to the menu.
			}
			s.armPostAuth(conn) // back to the menu: restore the post-auth window
```

(The `AuditBridgeStart` record just above the `s.armBridge` line is unchanged.)

- [ ] **Step 5: Run the regime tests**

Run: `go test ./internal/server/ -run 'TestSessionRegimeTransitions|TestSessionZeroIdleConfig' -race -v`
Expected: PASS.

- [ ] **Step 6: Fix the now-stale idle-timeout-at-menu test + add coverage**

`TestSessionAuditsIdleTimeoutAtMenu` (in `session_idle_test.go`) asserts a *disconnect* on post-auth idle — that behavior is now a logout. Replace it, and add the new behavior tests:

```go
func TestSessionIdleAtMenuLogsOutToLogin(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // re-presented login after idle-logout
		},
		menuPicks: []menuResult{{err: os.ErrDeadlineExceeded}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	// Login must be presented twice: initial + after idle-logout.
	if len(p.logins) != 0 {
		t.Fatalf("expected both login renders consumed, %d left (login not re-presented)", len(p.logins))
	}
	var logout *store.AuditEvent
	for i := range rec.events {
		if rec.events[i].Kind == store.AuditLogout {
			logout = &rec.events[i]
		}
	}
	if logout == nil || logout.Detail != "idle logout" || logout.Username != "alice" {
		t.Errorf("want AuditLogout{idle logout, alice}, got %+v", logout)
	}
}

func TestSessionPF3LogoffAuditsLogout(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec
	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	found := false
	for _, e := range rec.events {
		if e.Kind == store.AuditLogout && e.Detail == "user logoff" {
			found = true
		}
	}
	if !found {
		t.Errorf("PF3 logoff did not emit AuditLogout{user logoff}; events=%v", rec.kinds())
	}
}

func TestSessionTrustedIsExemptPreAuth(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.PreAuthIdle = 2 * time.Minute
	s.Idle = 30 * time.Minute
	s.PreAuthMax = 5 * time.Minute
	s.Trusted = true

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	want := []string{"window:0s", "window:30m0s", "window:0s"} // exempt pre-auth, post-auth, exempt again
	if !equalStrings(client.calls, want) {
		t.Errorf("trusted regime calls = %v, want %v", client.calls, want)
	}
}

func TestSessionBridgeIdleExemptDisablesTimeout(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}},
			{quit: true},
		},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseUserEscaped}}
	s := newTestSession(t, p, b)
	s.PreAuthIdle = 2 * time.Minute
	s.Idle = 30 * time.Minute
	s.PreAuthMax = 5 * time.Minute
	s.BridgeIdleExempt = true

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	// Bridge regime must disable the deadline; menu restores 30m afterward.
	if !containsStr(client.calls, "window:0s") {
		t.Errorf("bridge-exempt did not disable idle; calls=%v", client.calls)
	}
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
```

- [ ] **Step 7: Run the full server package with the race detector**

Run: `go test ./internal/server/ -race`
Expected: PASS. (If `TestSessionAuditsIdleTimeoutAtMenu` still exists, delete it — it is superseded by `TestSessionIdleAtMenuLogsOutToLogin`.)

- [ ] **Step 8: Commit**

```bash
git add internal/server/session.go internal/server/session_idle_test.go
git commit -m "feat(server): pre-auth ceiling + idle-logout + trusted/bridge regimes (GH #18)"
```

---

## Task 7: Per-IP cap exemption for trusted clients

**Files:**
- Modify: `internal/server/limiter.go:88-121`
- Modify: `internal/server/server.go` (`Server` struct, `Serve`, `handle`, `newServers`, `sessionHandler.Handle`)
- Test: `internal/server/limiter_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/server/limiter_test.go`:

```go
func TestAdmitIPTrustedBypassesCap(t *testing.T) {
	l := newConnLimiter(10, 1) // per-IP cap of 1
	addr := &net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 5000}

	if !l.admitIP(addr, false) {
		t.Fatal("first untrusted admit should succeed")
	}
	if l.admitIP(addr, false) {
		t.Fatal("second untrusted admit should hit the per-IP cap")
	}
	// Trusted bypasses the cap and does not consume a per-IP slot.
	if !l.admitIP(addr, true) {
		t.Fatal("trusted admit should bypass the per-IP cap")
	}
	l.releaseIP(addr, true) // must be a no-op (never admitted a slot)
	// The original untrusted slot is still held → still at cap.
	if l.admitIP(addr, false) {
		t.Fatal("untrusted slot should still be held after trusted release")
	}
}
```

If existing tests in this file call `l.admitIP(addr)` / `l.releaseIP(addr)`, update them to pass `false`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/ -run TestAdmitIP -v`
Expected: compile failure — `admitIP` takes one arg.

- [ ] **Step 3: Make `admitIP`/`releaseIP` trust-aware**

In `internal/server/limiter.go`, change the signatures and short-circuit trusted:

```go
func (l *connLimiter) admitIP(addr net.Addr, trusted bool) bool {
	if l == nil || l.maxPerIP <= 0 || trusted {
		return true
	}
	// ... unchanged body ...
}

func (l *connLimiter) releaseIP(addr net.Addr, trusted bool) {
	if l == nil || l.maxPerIP <= 0 || trusted {
		return
	}
	// ... unchanged body ...
}
```

- [ ] **Step 4: Thread trust through the accept path**

In `internal/server/server.go`, add a `Trust` field to `Server`:

```go
type Server struct {
	Listener net.Listener
	Handler  connHandler
	Limiter  *connLimiter
	// Trust exempts matching client IPs from the per-IP cap (GH #18).
	Trust trustList
}
```

Update `Serve` to compute trust once and pass it down:

```go
func (s *Server) Serve() error {
	for {
		s.Limiter.acquire()
		conn, err := s.Listener.Accept()
		if err != nil {
			s.Limiter.releaseGlobal()
			return err
		}
		trusted := s.Trust.Contains(conn.RemoteAddr())
		if !s.Limiter.admitIP(conn.RemoteAddr(), trusted) {
			log.Printf("per-ip connection cap reached; rejecting %s", conn.RemoteAddr())
			conn.Close()
			s.Limiter.releaseGlobal()
			continue
		}
		log.Printf("accepted connection from %s", conn.RemoteAddr())
		go s.handle(conn, trusted)
	}
}
```

Update `handle` to release the per-IP slot only when one was taken:

```go
func (s *Server) handle(conn net.Conn, trusted bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("session panic from %s: %v", conn.RemoteAddr(), r)
		}
		conn.Close()
		s.Limiter.releaseIP(conn.RemoteAddr(), trusted)
		s.Limiter.releaseGlobal()
	}()
	s.Handler.Handle(conn)
}
```

In `newServers`, build the trust list once and set it on each server:

```go
func newServers(listeners []net.Listener, handler connHandler, limits Limits) []*Server {
	var limiter *connLimiter
	if limits.MaxConns > 0 {
		limiter = newConnLimiter(limits.MaxConns, limits.MaxPerIP)
	}
	trust := trustList(limits.TrustedCIDRs)
	servers := make([]*Server, len(listeners))
	for i, ln := range listeners {
		servers[i] = &Server{Listener: ln, Handler: handler, Limiter: limiter, Trust: trust}
	}
	return servers
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/server/ -run 'TestAdmitIP|TestServe|TestServeAll' -race -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/limiter.go internal/server/limiter_test.go internal/server/server.go
git commit -m "feat(server): trusted clients bypass the per-IP cap (GH #18)"
```

---

## Task 8: Populate the session's trust + regime fields from the handler

**Files:**
- Modify: `internal/server/server.go` (`sessionHandler.Handle`)
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/server/server_test.go` (it already imports `net`/`testing`; add `"net/netip"` and `"time"` if absent):

```go
func TestSessionHandlerSetsTrustAndRegimeFields(t *testing.T) {
	st := openTestStore(t) // existing helper in this package's tests
	limits := Limits{
		PreAuthIdle:      2 * time.Minute,
		Idle:             30 * time.Minute,
		PreAuthMax:       5 * time.Minute,
		BridgeIdleExempt: true,
		TrustedCIDRs:     []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	}
	h := NewSessionHandler(st, 0x6B, limits).(sessionHandler)

	got := h.sessionFor(&net.TCPAddr{IP: net.ParseIP("10.0.0.9"), Port: 1})
	if !got.Trusted {
		t.Error("client in trusted CIDR should yield Trusted session")
	}
	if got.PreAuthMax != 5*time.Minute || !got.BridgeIdleExempt {
		t.Errorf("regime fields not propagated: %+v", got)
	}
	untrusted := h.sessionFor(&net.TCPAddr{IP: net.ParseIP("10.9.9.9"), Port: 1})
	if untrusted.Trusted {
		t.Error("client outside trusted CIDRs must not be Trusted")
	}
}
```

> If `openTestStore` doesn't exist in `server_test.go`, use the same `store.Open(t.TempDir()+"/s.db")` pattern as `newTestSession` (close via `t.Cleanup`).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/ -run TestSessionHandlerSetsTrust -v`
Expected: compile failure — `sessionFor` undefined; `Trusted` not set.

- [ ] **Step 3: Extract a `sessionFor` builder and populate fields**

In `internal/server/server.go`, refactor `sessionHandler.Handle` to build the session via a testable helper that takes the remote address:

```go
// sessionFor builds the Session for a connection from addr, deciding trust and
// carrying the regime knobs from limits.
func (h sessionHandler) sessionFor(addr net.Addr) *Session {
	return &Session{
		Store:            h.store,
		Authenticate:     auth.Authenticate,
		Presenter:        go3270Presenter{},
		Bridger:          realBridger{},
		EscapeAID:        h.escapeAID,
		AdminPresenter:   go3270Presenter{},
		Auditor:          storeAuditor{store: h.store},
		PreAuthIdle:      h.limits.PreAuthIdle,
		Idle:             h.limits.Idle,
		PreAuthMax:       h.limits.PreAuthMax,
		Trusted:          trustList(h.limits.TrustedCIDRs).Contains(addr),
		BridgeIdleExempt: h.limits.BridgeIdleExempt,
	}
}

func (h sessionHandler) Handle(conn net.Conn) {
	s := h.sessionFor(conn.RemoteAddr())
	s.Run(wrapIdle(conn, h.limits.PreAuthIdle))
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/server/ -run TestSessionHandler -v`
Expected: PASS.

- [ ] **Step 5: Full package + race**

Run: `go test ./... -race`
Expected: PASS across all packages.

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): derive trust + regime knobs per connection (GH #18)"
```

---

## Task 9: Documentation

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update the hardening notes**

In `CLAUDE.md`, in the `internal/server` package summary, extend the Hardening paragraph to describe the regime model. Replace the `idleConn wraps every conn ...` sentence with:

```
Hardening (GH #1, #18): idleConn enforces an idle regime that the session
switches at each transition — pre-auth (idle window + an absolute now+pre_auth_max
ceiling that does NOT slide, so a 1-byte/2min trickle can't hold a slot),
post-auth (plain idle window), and bridge (idle window, or none when
bridge_idle=exempt). Trusted clients (limits.trusted_cidrs) are exempt from the
pre-auth timers and the per-IP cap but still count toward the global cap.
Post-auth idle at the menu/admin LOGS OUT to the login screen (re-arming pre-auth)
rather than disconnecting — audited as logout {idle logout}; PF3-logoff audits as
logout {user logoff}. connLimiter claims a global slot before Accept and enforces
the per-IP cap after Accept by closing.
```

In the `internal/config` summary, add `pre_auth_max`, `trusted_cidrs`, and `bridge_idle` to the `limits` description, e.g.:

```
limits section (pre_auth_idle/idle/pre_auth_max as Go durations, trusted_cidrs
[]IP-or-CIDR, bridge_idle "disconnect"|"exempt", max_conns, max_per_ip; defaults
2m/30m/5m, [], disconnect, 512, 16).
```

Add a one-line note to the Gotchas/Conventions about trust being config-file-only:

```
- **Trust list is config-file-only** (limits.trusted_cidrs): it bypasses DoS
  controls, so it lives on the deployment surface, not the admin UI. A follow-up
  issue tracks optional DB/admin-UI management.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document idle regimes, trust list, idle-logout (GH #18)"
```

---

## Task 10: s3270 smoke — idle-logout returns to login

Protocol guard: verify that a post-auth idle deadline firing mid-`HandleScreen` cleanly re-renders the login screen (the one behavior unit tests can't prove — CLAUDE.md "learned the hard way").

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`
- Possibly modify: the smoke config it generates (short `idle`)

- [ ] **Step 1: Read the smoke script to find its config + assertion helpers**

Run: `sed -n '1,80p' .claude/skills/s3270-smoke-testing/smoke.sh`
Expected: locate where it writes the throwaway config (the `"tls":{"enabled":false}` one) and where it asserts screen content/cursor.

- [ ] **Step 2: Configure a short post-auth idle for the run**

In the generated smoke config, set `"limits": {"idle": "3s", "pre_auth_idle": "3s", "pre_auth_max": "10s"}` so the windows fire fast enough to test. (Match the JSON shape the script already emits.)

- [ ] **Step 3: Add the idle-logout assertion**

After the step that reaches the menu (post-login), add a wait + assertion that the screen returns to the login screen. Using the script's existing s3270 helper style:

```sh
# Post-auth idle must LOG OUT to the login screen (GH #18), not hang or disconnect.
sleep 4   # exceed the 3s post-auth idle window
expect_screen_contains "Userid" "idle-logout returns to login"
assert_cursor_at "$LOGIN_USERID_ROW" "$LOGIN_USERID_COL" "cursor homes to userid after idle-logout"
```

(Use the same field label and cursor coordinates the script already asserts for the login screen earlier in the run; reuse those variables rather than hardcoding.)

- [ ] **Step 4: Run the smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all assertions pass, including the new idle-logout one. If s3270 isn't installed, note it and run the smoke test manually per the skill before declaring the task done.

- [ ] **Step 5: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "test(smoke): assert idle-logout returns to login screen (GH #18)"
```

---

## Task 11: Final verification + issue housekeeping

- [ ] **Step 1: Full build + race suite**

Run: `go build ./... && go test ./... -race`
Expected: all green.

- [ ] **Step 2: Sanity-run the binary with a trust + bridge-idle config**

```bash
go build -o bin/tn3270proxy ./cmd/tn3270proxy
cat > /tmp/trust.json <<'JSON'
{"listeners":{"plain":{"enabled":true,"addr":":2399"},"tls":{"enabled":false}},
 "limits":{"pre_auth_max":"10s","trusted_cidrs":["127.0.0.1/32"],"bridge_idle":"exempt"}}
JSON
./bin/tn3270proxy serve -db /tmp/smoke.db -config /tmp/trust.json &
```

Connect with `c3270 127.0.0.1:2399`, confirm: (a) as 127.0.0.1 (trusted) the login screen does NOT time out; (b) post-auth idle at the menu returns to login. Stop the server when done. (Manual; record the result.)

- [ ] **Step 3: Post follow-up issues / comments** *(human or `gh`)*

- Comment on #19 that `bridge_idle` now offers `disconnect`/`exempt`; true *user-idle* during bridge remains its open item.
- Open a follow-up issue: "DB/admin-UI-managed trust list (deferred from #18)".
- Note in #18 that Hole B (accept-loop rework / shutdown observability) is deferred to its own issue.

> These are repo-management actions, not code — do them via `gh` only after the PR is up, and **never** write the `@`-mention that dispatches the bot unless intending to dispatch it.

---

## Self-review notes

- **Spec coverage:** regime model (Task 2/6), trust list (Task 3/4/7/8), config keys (Task 4/5), idle-logout menu+admin (Task 6), AuditLogout incl. PF3 retrofit (Task 1/6), per-IP exemption + global-cap accounting (Task 7), smoke guard (Task 10), docs (Task 9). Bridge-idle exempt (Task 4/6). All spec sections map to a task.
- **Out of scope (per spec):** accept-loop rework, true user-idle during bridge, DB/admin-UI trust — noted in Task 11, not implemented.
- **Type consistency:** seam methods `setPreAuth(idle, max)` / `setWindow(idle)` used identically in `idleconn.go`, the `idleRegime` interface, the test fake, and the `arm*` helpers; `admitIP(addr, trusted)` / `releaseIP(addr, trusted)` consistent across limiter, server, and tests; `trustList` is `[]netip.Prefix` so `trustList(limits.TrustedCIDRs)` is a zero-cost conversion everywhere.
