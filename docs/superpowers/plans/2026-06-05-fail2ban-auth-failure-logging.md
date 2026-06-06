# fail2ban-friendly auth-failure logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Emit a stable, fail2ban-friendly slog line on every authentication failure (password and TOTP) carrying the client IP, a trusted/untrusted marker, and a coarse reason.

**Architecture:** A single `(*Session).logAuthFailure(lg, reason)` helper emits the documented line at the three failure sites in `internal/server/session.go` (password, MFA verify, MFA enroll-confirm). The client IP (port stripped) is parsed once in `sessionFor` and held on `Session.RemoteHost`; the trusted marker reuses the existing `Session.Trusted`. `auth.Authenticate` is unchanged (coarse reason needs no enumeration leak). We reuse the existing dual slog sinks (text→stderr always, JSON→file when `-log-file` is set); no new logging config. README gains a Logging section with a sample fail2ban filter, marked as an example.

**Tech Stack:** Go, `log/slog`, the repo's `internal/logging` package, `net.SplitHostPort`. Tests use the existing `bufLogger` (JSON) harness in `internal/server`.

**Spec:** `docs/superpowers/specs/2026-06-05-fail2ban-auth-failure-logging-design.md`

---

## File Structure

- **Modify `internal/server/server.go`** — add `hostOnly(addr net.Addr) string`; set `RemoteHost` in `sessionFor`.
- **Modify `internal/server/session.go`** — add `RemoteHost string` field to `Session`; add `logAuthFailure` helper; call it at the three fail sites.
- **Modify `internal/server/server_test.go`** — `TestHostOnly` table test.
- **Modify `internal/server/session_logging_test.go`** — `findLogRecord` helper; field/shape/MFA tests.
- **Modify `README.md`** — new `## Logging` section (sinks + fail2ban example).

All `internal/server` changes stay in package `server`; no other package is touched.

---

### Task 1: `hostOnly` helper + `RemoteHost` plumbing

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/session.go` (add struct field)
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/server_test.go`:

```go
func TestHostOnly(t *testing.T) {
	cases := []struct {
		name string
		addr net.Addr
		want string
	}{
		{"ipv4 host:port", &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51324}, "203.0.113.7"},
		{"ipv6 host:port", &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 23}, "2001:db8::1"},
		{"nil addr", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostOnly(tc.addr); got != tc.want {
				t.Errorf("hostOnly(%v) = %q, want %q", tc.addr, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestHostOnly -v`
Expected: FAIL — `undefined: hostOnly`.

- [ ] **Step 3: Add the `hostOnly` helper**

Add to `internal/server/server.go` (near the other small helpers, e.g. just below `wrapIdle`):

```go
// hostOnly returns the IP/host portion of addr without the port — the bare
// <HOST> a fail2ban filter binds to on auth-failure log lines (see
// logAuthFailure). Falls back to the full address string when addr is nil or
// has no host:port shape.
func hostOnly(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr.String()); err == nil {
		return host
	}
	return addr.String()
}
```

`net` is already imported in `server.go`.

- [ ] **Step 4: Add the `RemoteHost` field to `Session`**

In `internal/server/session.go`, add the field to the `Session` struct, right after the `Trusted` / `BridgeIdleExempt` block:

```go
	// RemoteHost is the client IP (port stripped) for the fail2ban <HOST> on
	// auth-failure log lines (see logAuthFailure). Set by the handler from the
	// connection's RemoteAddr; empty in unit tests that construct Session
	// directly unless set explicitly.
	RemoteHost string
```

- [ ] **Step 5: Set `RemoteHost` in `sessionFor`**

In `internal/server/server.go`, in `sessionFor`, add the field to the returned `&Session{...}` (alongside `Trusted: trusted,`):

```go
		RemoteHost:       hostOnly(addr),
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/server/ -run TestHostOnly -v`
Expected: PASS (all three sub-cases).

- [ ] **Step 7: Build to confirm wiring compiles**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 8: Commit**

```bash
git add internal/server/server.go internal/server/session.go internal/server/server_test.go
git commit -m "feat(server): parse client host for fail2ban auth-failure lines (GH #51)"
```

---

### Task 2: `logAuthFailure` helper + password-fail emit

**Files:**
- Modify: `internal/server/session.go`
- Test: `internal/server/session_logging_test.go`

- [ ] **Step 1: Add the `findLogRecord` test helper and the field + shape tests**

In `internal/server/session_logging_test.go`, add `"encoding/json"` to the import block, then append:

```go
// findLogRecord scans the JSON log lines in buf and returns the first record
// whose "msg" equals want. Fails the test if none is found.
func findLogRecord(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["msg"] == want {
			return rec
		}
	}
	t.Fatalf("no log record with msg=%q found in:\n%s", want, buf.String())
	return nil
}

// TestAuthFailLineCarriesFail2banFields asserts the password-failure line
// carries the documented fail2ban fields: src (bare IP), trusted, coarse
// reason, and the attempted user.
func TestAuthFailLineCarriesFail2banFields(t *testing.T) {
	var buf bytes.Buffer
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "wrong"}, // auth fail
			{quit: true},
		},
		menuPicks: []menuResult{},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Logger = bufLogger(&buf)
	s.RemoteHost = "203.0.113.7"
	s.Trusted = false

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	rec := findLogRecord(t, &buf, "auth failed")
	if rec["src"] != "203.0.113.7" {
		t.Errorf("src = %v, want 203.0.113.7", rec["src"])
	}
	if rec["trusted"] != false {
		t.Errorf("trusted = %v, want false", rec["trusted"])
	}
	if rec["reason"] != "invalid_credentials" {
		t.Errorf("reason = %v, want invalid_credentials", rec["reason"])
	}
	if rec["user"] != "alice" {
		t.Errorf("user = %v, want alice", rec["user"])
	}
}

// TestAuthFailLineTrustedMarker asserts the trusted marker reflects a trusted
// session (so an operator filter can ban untrusted sources only).
func TestAuthFailLineTrustedMarker(t *testing.T) {
	var buf bytes.Buffer
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "wrong"}, {quit: true}},
		menuPicks: []menuResult{},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Logger = bufLogger(&buf)
	s.RemoteHost = "10.0.0.5"
	s.Trusted = true

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	rec := findLogRecord(t, &buf, "auth failed")
	if rec["trusted"] != true {
		t.Errorf("trusted = %v, want true", rec["trusted"])
	}
}

// TestAuthFailLineShapeIsStable pins the auth-failure field set so the fail2ban
// contract cannot silently drift (extra or missing fields fail the test).
func TestAuthFailLineShapeIsStable(t *testing.T) {
	var buf bytes.Buffer
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "wrong"}, {quit: true}},
		menuPicks: []menuResult{},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Logger = bufLogger(&buf)
	s.RemoteHost = "203.0.113.7"

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	rec := findLogRecord(t, &buf, "auth failed")
	delete(rec, "time")
	delete(rec, "level")
	delete(rec, "msg")
	want := map[string]bool{"user": true, "src": true, "trusted": true, "reason": true}
	for k := range rec {
		if !want[k] {
			t.Errorf("unexpected field %q on auth-failure line (stable contract)", k)
		}
	}
	for k := range want {
		if _, ok := rec[k]; !ok {
			t.Errorf("missing required field %q on auth-failure line (stable contract)", k)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestAuthFailLine' -v`
Expected: FAIL — the line currently emits only `user` (no `src`/`trusted`/`reason`), so the field and shape assertions fail.

- [ ] **Step 3: Add the `logAuthFailure` helper**

In `internal/server/session.go`, add near the `log()` helper:

```go
// logAuthFailure emits the stable, fail2ban-friendly auth-failure line. The
// field set — msg="auth failed" plus src, trusted, reason, and the user carried
// by lg — is a DOCUMENTED STABLE CONTRACT (operators build log filters against
// it); do not rename or drop fields without updating README and the shape test.
// lg must already carry the attempted username as "user": pre-auth callers pass
// s.log().With("user", user); post-auth callers pass s.log() (already enriched).
// reason is coarse ("invalid_credentials" or "bad_mfa") so it never reveals
// whether a username exists. Never pass credential content (password / TOTP
// code / MFA secret).
func (s *Session) logAuthFailure(lg *slog.Logger, reason string) {
	lg.Warn("auth failed",
		"src", s.RemoteHost,
		"trusted", s.Trusted,
		"reason", reason,
	)
}
```

- [ ] **Step 4: Emit it at the password-fail site**

In `internal/server/session.go`, in `doLogin`, replace:

```go
		// Auth fail: log the attempted username only — never the password.
		s.log().Warn("auth failed", "user", user)
		// Attempted username only — never the password (CLAUDE.md hard rule).
```

with:

```go
		// Auth fail: stable fail2ban line — attempted username only, never the
		// password (CLAUDE.md hard rule). Coarse reason: no enumeration leak.
		s.logAuthFailure(s.log().With("user", user), "invalid_credentials")
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestAuthFailLine' -v`
Expected: PASS (fields, trusted marker, shape).

- [ ] **Step 6: Run the existing credential-rule test to confirm no regression**

Run: `go test ./internal/server/ -run TestSessionAuthFailLogsUserNotPassword -v`
Expected: PASS (line still contains `alice` and `auth failed`, never the password).

- [ ] **Step 7: Commit**

```bash
git add internal/server/session.go internal/server/session_logging_test.go
git commit -m "feat(server): stable fail2ban line on password auth failure (GH #51)"
```

---

### Task 3: Emit the line at the MFA failure sites

**Files:**
- Modify: `internal/server/session.go`
- Test: `internal/server/session_logging_test.go`

- [ ] **Step 1: Write the failing MFA-fail test**

Append to `internal/server/session_logging_test.go`:

```go
// TestMFAFailLineReasonBadMFA asserts a wrong TOTP code emits the stable
// auth-failure line with reason=bad_mfa, and that the MFA secret never leaks.
func TestMFAFailLineReasonBadMFA(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	var buf bytes.Buffer
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		verifies:  []mfaResult{{code: "000000"}, {quit: true}}, // wrong code, then PF3
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{},
	}
	s, st := newMFATestSession(t, p, &fakeBridger{})
	s.Logger = bufLogger(&buf)
	s.RemoteHost = "198.51.100.9"

	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "x")
	st.SetMFARequired(ctx, uid, true)
	enc, _ := s.MFA.Seal([]byte(secret))
	st.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	rec := findLogRecord(t, &buf, "auth failed")
	if rec["reason"] != "bad_mfa" {
		t.Errorf("reason = %v, want bad_mfa", rec["reason"])
	}
	if rec["src"] != "198.51.100.9" {
		t.Errorf("src = %v, want 198.51.100.9", rec["src"])
	}
	if rec["user"] != "alice" {
		t.Errorf("user = %v, want alice", rec["user"])
	}
	if strings.Contains(buf.String(), secret) {
		t.Errorf("MFA secret leaked into log output:\n%s", buf.String())
	}
}
```

This test needs `context` — confirm `"context"` is in the file's import block (add it if missing).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run TestMFAFailLineReasonBadMFA -v`
Expected: FAIL — the MFA verify site emits no log line today, so `findLogRecord` finds no `auth failed` record and fails.

- [ ] **Step 3: Emit at the MFA verify fail site**

In `internal/server/session.go`, in `mfaVerify`, the `if !ok {` block, add the log call as the first statement in the block:

```go
		if !ok {
			s.logAuthFailure(s.log(), "bad_mfa")
			delay, count := s.failDelay(ctx, u.Username)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditMFAFailed, Username: u.Username, Detail: throttleDetail("login", delay, count)})
			s.sleepFor(delay)
			errMsg = "Code incorrect - try again"
			continue
		}
```

- [ ] **Step 4: Emit at the MFA enroll-confirm fail site**

In `internal/server/session.go`, in `mfaEnroll`, the `if !ok {` block, add the same first statement:

```go
		if !ok {
			s.logAuthFailure(s.log(), "bad_mfa")
			delay, count := s.failDelay(ctx, u.Username)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditMFAFailed, Username: u.Username, Detail: throttleDetail("enroll", delay, count)})
			s.sleepFor(delay)
			errMsg = "Code incorrect - check the key and try again"
			continue
		}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/server/ -run TestMFAFailLineReasonBadMFA -v`
Expected: PASS.

- [ ] **Step 6: Run the full server package with the race detector**

Run: `go test ./internal/server/ -race`
Expected: PASS (no regressions in the existing MFA / logging tests).

- [ ] **Step 7: Commit**

```bash
git add internal/server/session.go internal/server/session_logging_test.go
git commit -m "feat(server): stable fail2ban line on MFA verify/enroll failure (GH #51)"
```

---

### Task 4: README Logging + fail2ban documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add the Logging section**

Insert a new `## Logging` section immediately **before** the `## Message of the Day (MOTD / NEWS)` heading in `README.md`:

```markdown
## Logging

The proxy logs to two concurrent sinks via slog:

- **stderr** — human-readable text, always on (the friendlier console/journald stream).
- **a JSON file** — machine-readable, enabled with `-log-file /path/to/file.json`
  (or `log.file` in the config). Both sinks share the level (`-log-level`,
  default `info`) and carry identical fields.

### Auth-failure lines (fail2ban-friendly)

Every authentication failure — wrong password or wrong TOTP code — emits a
**stable** line you can wire an external scanner (e.g. fail2ban) to. The proxy
does not ship or run fail2ban; it only provides the log surface.

JSON (file sink):

    {"time":"...","level":"WARN","msg":"auth failed","remote":"203.0.113.7:51324","user":"alice","src":"203.0.113.7","trusted":false,"reason":"invalid_credentials"}

Text (stderr sink):

    level=WARN msg="auth failed" remote=203.0.113.7:51324 user=alice src=203.0.113.7 trusted=false reason=invalid_credentials

`src` is the bare client IP (the fail2ban `<HOST>`), `trusted` reflects your
trusted-network config (`limits.trusted_cidrs` plus the admin trusted-network
list), and `reason` is coarse (`invalid_credentials` or `bad_mfa`) — the
on-screen message stays uniform, so the reason never reveals whether a username
exists. The field set is a **stable contract**: filters depend on it.

> Auth throttling is per-username backoff, not a hard lockout, so repeated
> failures keep emitting these lines; let your scanner's own retry counter
> (fail2ban `maxretry` / `findtime`) decide when to ban. Match `trusted=false`
> so trusted sources are never banned.

### Example fail2ban filter (adapt to your deployment)

Point fail2ban at the JSON log file:

    # /etc/fail2ban/filter.d/tn3270proxy.conf
    [Definition]
    failregex = "msg":"auth failed".*"src":"<HOST>".*"trusted":false
    ignoreregex =

Or, tailing the stderr/journald text stream:

    failregex = msg="auth failed" .*\bsrc=<HOST>\b.*\btrusted=false\b

with a jail like:

    # /etc/fail2ban/jail.d/tn3270proxy.conf
    [tn3270proxy]
    enabled  = true
    filter   = tn3270proxy
    logpath  = /var/log/tn3270proxy/auth.json
    maxretry = 5
    findtime = 10m
    bantime  = 1h
```

- [ ] **Step 2: Verify the full suite is still green**

Run: `go test ./... -race`
Expected: PASS across all packages.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document logging sinks + sample fail2ban filter (GH #51)"
```

---

## Self-Review

**1. Spec coverage:**
- Stable line with src/trusted/reason → Task 2 (password) + Task 3 (MFA). ✅
- On-screen messages unchanged / `auth.Authenticate` untouched → no task modifies them; `errMsg` strings left as-is. ✅
- Credentials never logged → coarse reason only; Task 3 asserts the secret never leaks; existing password test re-run in Task 2 Step 6. ✅
- README sample filter (JSON primary + text alt), example-only, untrusted-only → Task 4. ✅
- Shape-pinning test → Task 2 (`TestAuthFailLineShapeIsStable`). ✅
- Dropped "max attempts" line → not implemented (no event); documented in the README note (Task 4) and spec. ✅
- `trusted` from combined signal → reuses `Session.Trusted` (set in `sessionFor`), Task 1. ✅

**2. Placeholder scan:** No TBD/TODO; every code step shows full code and exact run commands. ✅

**3. Type consistency:** `hostOnly(net.Addr) string`, `Session.RemoteHost string`, `logAuthFailure(*slog.Logger, string)`, `findLogRecord(*testing.T, *bytes.Buffer, string) map[string]any` — names/signatures match across Tasks 1–3. The helper takes a logger already carrying `user`; password caller adds it via `.With`, MFA callers rely on the post-auth enriched logger — consistent with the spec's "no duplicate user attr" rule. ✅

**4. Import check:** Task 2 adds `encoding/json` to the test file; Task 3 confirms `context` present. `net`/`slog` already imported in the modified non-test files. ✅
