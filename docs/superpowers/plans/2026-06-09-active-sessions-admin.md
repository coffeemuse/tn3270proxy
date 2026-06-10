# Active Sessions Admin View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an admin screen (`A` menu → option `7`) that lists every live client connection (id, client IP:port, connect time, session length, logged-in user, bridged service) and lets an admin disconnect any session but their own.

**Architecture:** A new in-memory `sessionRegistry` (created once per process in `NewSessionHandler`, shared across listeners like `authThrottle`) tracks each live connection. The session goroutine registers at accept, updates its entry at login/bridge/logoff transitions, and deregisters at teardown; disconnect hard-closes the connection via an idempotent `sync.Once`-wrapped close. The admin screen reuses the `RunSnapshotList` driver (extended with a confirm-capable line command and a full-width row variant).

**Tech Stack:** Go 1.25 (stdlib `sync`/`sort`/`time`/`net`), go3270, the existing `internal/ui3270` snapshot-list machinery, `internal/store` audit trail.

**Spec:** `docs/superpowers/specs/2026-06-09-active-sessions-admin-design.md`

**Conventions:** TDD (test first, watch it fail, minimal impl, watch it pass, commit). `go test ./... -race` (the registry is concurrent). Commit after every task. `internal/screens`/`internal/ui3270` stay pure render; all concurrency lives in `internal/server`; no SQL outside `internal/store`.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/server/registry.go` (new) | `SessionView`, `sessionEntry`, `sessionRegistry` (register/deregister/Snapshot/mutators/Disconnect), `SessionRegistry` seam |
| `internal/server/registry_test.go` (new) | registry unit tests (concurrent, snapshot copies, disconnect semantics) |
| `internal/server/session.go` (modify) | `Registry`/`SessionID` fields + nil-safe `reg*` helpers + 4 lifecycle hooks in `Run` + thread registry into `adminFlow` |
| `internal/server/server.go` (modify) | `registry` field on `sessionHandler`; build it in `NewSessionHandler`; register/deregister + `sync.Once` close in `Handle` |
| `internal/server/admin.go` (modify) | `sessions`/`selfSessionID` fields on `adminFlow`; `case 7` dispatch |
| `internal/server/admin_sessions.go` (new) | `activeSessions` flow: snapshot → format → confirm/veto/disconnect/audit |
| `internal/server/admin_sessions_test.go` (new) | flow tests through a fake `SessionRegistry` |
| `internal/server/presenter_admin.go` (modify) | accept `"7"` in `AdminMenu` parse |
| `internal/screens/admin.go` (modify) | add option `7 Sessions` to the tri-color grid |
| `internal/ui3270/types.go` (modify) | `Wide bool` on `SnapshotView` |
| `internal/ui3270/snapshotscreen.go` (modify) | full-width row variant when `Wide` |
| `internal/ui3270/snapshotlist.go` (modify) | `ActCmd`/`Confirm`/`OnAct`/`Wide` on `SnapshotConfig` + confirm dance |
| `internal/ui3270/snapshotlist_test.go` (modify) | driver extension tests |
| `internal/store/audit.go` (modify) | `AuditSessionDisconnect` kind constant |
| `internal/server/admin_audit.go` (modify) | colour the new kind yellow in `auditEventColor` |
| `.claude/skills/s3270-smoke-testing/` (modify) | smoke assertions for the new screen + disconnect |

---

## Task 1: Registry core — register / deregister / Snapshot

**Files:**
- Create: `internal/server/registry.go`
- Test: `internal/server/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
package server

import (
	"testing"
	"time"
)

func TestRegistry_RegisterAssignsIncreasingIDs(t *testing.T) {
	r := newSessionRegistry()
	id1 := r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})
	id2 := r.register("2.2.2.2:5000", time.Unix(200, 0), func() {})
	if id1 == 0 || id2 <= id1 {
		t.Fatalf("ids must be nonzero and increasing: id1=%d id2=%d", id1, id2)
	}
}

func TestRegistry_SnapshotReflectsEntriesSortedByID(t *testing.T) {
	r := newSessionRegistry()
	r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})
	r.register("2.2.2.2:5000", time.Unix(200, 0), func() {})
	got := r.Snapshot()
	if len(got) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(got))
	}
	if got[0].ID >= got[1].ID {
		t.Errorf("snapshot not sorted by id: %+v", got)
	}
	if got[0].RemoteAddr != "1.1.1.1:5000" || !got[0].ConnectedAt.Equal(time.Unix(100, 0)) {
		t.Errorf("entry 0 = %+v, want addr/connectedAt populated", got[0])
	}
}

func TestRegistry_SnapshotReturnsCopies(t *testing.T) {
	r := newSessionRegistry()
	id := r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})
	snap := r.Snapshot()
	snap[0].Username = "MUTATED" // mutate the returned copy
	r.setLogin(id, "REAL", time.Unix(150, 0))
	if again := r.Snapshot(); again[0].Username != "REAL" {
		t.Errorf("Snapshot must return copies; registry state leaked: %q", again[0].Username)
	}
}

func TestRegistry_DeregisterRemoves(t *testing.T) {
	r := newSessionRegistry()
	id := r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})
	r.deregister(id)
	if got := r.Snapshot(); len(got) != 0 {
		t.Fatalf("snapshot len = %d after deregister, want 0", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestRegistry -v`
Expected: FAIL — `undefined: newSessionRegistry` (and `setLogin`, referenced by a Task-1 test but implemented in Task 2; that test will compile-fail until Task 2 — acceptable, the suite won't build yet).

> Note: `TestRegistry_SnapshotReturnsCopies` calls `setLogin`, added in Task 2. If you want a green Task 1 in isolation, temporarily drop that one assertion; otherwise implement Task 1 + Task 2 together before running. Recommended: implement both, then run.

- [ ] **Step 3: Write minimal implementation**

```go
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
	"sort"
	"sync"
	"time"
)

// SessionView is a point-in-time, copied snapshot of one live session. It holds
// no net.Conn and no close func — only display/audit data, safe to hand to the
// admin screen layer.
type SessionView struct {
	ID          uint64
	RemoteAddr  string
	ConnectedAt time.Time
	LoggedInAt  time.Time // zero ⇒ not logged in
	Username    string
	Service     string
}

// sessionEntry is the registry's live record for one connection.
type sessionEntry struct {
	view  SessionView
	close func() // hard-closes the connection; idempotent (wrapped in sync.Once by the handler)
}

// sessionRegistry tracks every live connection. One instance is created per
// process (NewSessionHandler) and shared across all listeners, mirroring how
// authThrottle is wired. All methods are safe for concurrent use.
type sessionRegistry struct {
	mu     sync.Mutex
	nextID uint64
	byID   map[uint64]*sessionEntry
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{byID: make(map[uint64]*sessionEntry)}
}

// register adds a live session and returns its id. close hard-closes the
// connection when called (the caller wraps it in sync.Once for idempotency).
func (r *sessionRegistry) register(remoteAddr string, connectedAt time.Time, close func()) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.byID[id] = &sessionEntry{
		view:  SessionView{ID: id, RemoteAddr: remoteAddr, ConnectedAt: connectedAt},
		close: close,
	}
	return id
}

// deregister removes a session (called from the session's teardown defer).
func (r *sessionRegistry) deregister(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, id)
}

// Snapshot returns a copied, id-sorted slice of the live sessions. The copies
// never alias the live entries, so the caller can format them freely.
func (r *sessionRegistry) Snapshot() []SessionView {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SessionView, 0, len(r.byID))
	for _, e := range r.byID {
		out = append(out, e.view) // value copy
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
```

- [ ] **Step 4: Run test to verify it passes** (after Task 2 is also in)

Run: `go test ./internal/server/ -run TestRegistry -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/registry.go internal/server/registry_test.go
git commit -m "feat(server): session registry core (register/deregister/Snapshot) (#91)"
```

---

## Task 2: Registry mutators — setLogin / clearLogin / setService / clearService

**Files:**
- Modify: `internal/server/registry.go`
- Test: `internal/server/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRegistry_LoginAndServiceMutators(t *testing.T) {
	r := newSessionRegistry()
	id := r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})

	r.setLogin(id, "ALICE", time.Unix(150, 0))
	v := r.Snapshot()[0]
	if v.Username != "ALICE" || !v.LoggedInAt.Equal(time.Unix(150, 0)) {
		t.Fatalf("after setLogin: %+v", v)
	}

	r.setService(id, "PROD")
	if v = r.Snapshot()[0]; v.Service != "PROD" {
		t.Fatalf("after setService: service = %q", v.Service)
	}

	r.clearService(id)
	if v = r.Snapshot()[0]; v.Service != "" {
		t.Fatalf("after clearService: service = %q", v.Service)
	}

	r.clearLogin(id)
	if v = r.Snapshot()[0]; v.Username != "" || !v.LoggedInAt.IsZero() {
		t.Fatalf("after clearLogin: %+v", v)
	}
}

func TestRegistry_MutatorsOnMissingIDAreNoOps(t *testing.T) {
	r := newSessionRegistry()
	// Must not panic on an unknown id.
	r.setLogin(999, "X", time.Unix(1, 0))
	r.clearLogin(999)
	r.setService(999, "Y")
	r.clearService(999)
}

func TestRegistry_ConcurrentAccessIsRaceFree(t *testing.T) {
	r := newSessionRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := r.register("9.9.9.9:1", time.Unix(1, 0), func() {})
			r.setLogin(id, "U", time.Unix(2, 0))
			r.setService(id, "S")
			_ = r.Snapshot()
			r.clearService(id)
			r.clearLogin(id)
			r.deregister(id)
		}()
	}
	wg.Wait()
	if got := len(r.Snapshot()); got != 0 {
		t.Fatalf("all sessions deregistered, want 0, got %d", got)
	}
}
```

(`sync` is already imported via the implementation file but the test file needs its own import — add `"sync"` to `registry_test.go` imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestRegistry -v`
Expected: FAIL — `r.setLogin undefined` etc.

- [ ] **Step 3: Write minimal implementation** (append to `registry.go`)

```go
// setLogin records a successful login on the entry (username + login time).
func (r *sessionRegistry) setLogin(id uint64, username string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.byID[id]; e != nil {
		e.view.Username = username
		e.view.LoggedInAt = at
	}
}

// clearLogin reverts the entry to a pre-auth state (logoff / idle logout). It
// also clears any service, defensively — a logged-off session is never bridged.
func (r *sessionRegistry) clearLogin(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.byID[id]; e != nil {
		e.view.Username = ""
		e.view.LoggedInAt = time.Time{}
		e.view.Service = ""
	}
}

// setService records the service NAME an entry is actively bridged to.
func (r *sessionRegistry) setService(id uint64, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.byID[id]; e != nil {
		e.view.Service = name
	}
}

// clearService records that an entry's bridge has ended (back to the menu).
func (r *sessionRegistry) clearService(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.byID[id]; e != nil {
		e.view.Service = ""
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestRegistry -race -v`
Expected: PASS (including the race test under `-race`)

- [ ] **Step 5: Commit**

```bash
git add internal/server/registry.go internal/server/registry_test.go
git commit -m "feat(server): session registry login/service mutators (#91)"
```

---

## Task 3: Registry Disconnect + SessionRegistry seam

**Files:**
- Modify: `internal/server/registry.go`
- Test: `internal/server/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRegistry_DisconnectClosesAndReturnsView(t *testing.T) {
	r := newSessionRegistry()
	closes := 0
	id := r.register("3.3.3.3:7000", time.Unix(100, 0), func() { closes++ })
	r.setLogin(id, "BOB", time.Unix(120, 0))

	booted, ok := r.Disconnect(id)
	if !ok {
		t.Fatal("Disconnect on a live session must return ok=true")
	}
	if closes != 1 {
		t.Errorf("close called %d times, want 1", closes)
	}
	if booted.Username != "BOB" || booted.RemoteAddr != "3.3.3.3:7000" {
		t.Errorf("booted view = %+v, want BOB@3.3.3.3:7000", booted)
	}
}

func TestRegistry_DisconnectMissingIDIsBenign(t *testing.T) {
	r := newSessionRegistry()
	_, ok := r.Disconnect(404)
	if ok {
		t.Error("Disconnect on an unknown id must return ok=false")
	}
}

// The SessionRegistry seam is what adminFlow depends on.
func TestRegistry_SatisfiesSeam(t *testing.T) {
	var _ SessionRegistry = newSessionRegistry()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestRegistry -v`
Expected: FAIL — `r.Disconnect undefined`, `undefined: SessionRegistry`

- [ ] **Step 3: Write minimal implementation** (append to `registry.go`)

```go
// SessionRegistry is the read/act seam the admin flow depends on (so admin
// tests use a fake and the screen layer never sees concurrency internals).
// *sessionRegistry satisfies it.
type SessionRegistry interface {
	Snapshot() []SessionView
	Disconnect(id uint64) (SessionView, bool)
}

var _ SessionRegistry = (*sessionRegistry)(nil)

// Disconnect hard-closes the session's connection and returns its last-known
// view for the audit record. ok=false means the id was already gone (it raced a
// natural disconnect). The entry is NOT removed here — the session goroutine's
// own teardown defer deregisters once its blocked Read unblocks. close() is
// invoked outside the lock (it is a syscall) and is idempotent.
func (r *sessionRegistry) Disconnect(id uint64) (SessionView, bool) {
	r.mu.Lock()
	e := r.byID[id]
	if e == nil {
		r.mu.Unlock()
		return SessionView{}, false
	}
	view, closeFn := e.view, e.close
	r.mu.Unlock()
	if closeFn != nil {
		closeFn()
	}
	return view, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestRegistry -race -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/registry.go internal/server/registry_test.go
git commit -m "feat(server): session registry Disconnect + SessionRegistry seam (#91)"
```

---

## Task 4: Session registry fields + nil-safe helpers

**Files:**
- Modify: `internal/server/session.go` (add fields to `Session` struct ~line 88-143; add helper methods near `now()` ~line 145)
- Test: `internal/server/registry_test.go` (or a new `session_registry_test.go`)

- [ ] **Step 1: Write the failing test**

```go
func TestSessionRegistryHelpers_NilRegistryIsNoOp(t *testing.T) {
	s := &Session{} // no Registry
	// Must not panic.
	s.regSetLogin("ALICE")
	s.regClearLogin()
	s.regSetService("PROD")
	s.regClearService()
}

func TestSessionRegistryHelpers_UpdateEntry(t *testing.T) {
	r := newSessionRegistry()
	id := r.register("1.2.3.4:9", time.Unix(100, 0), func() {})
	s := &Session{Registry: r, SessionID: id, Now: func() time.Time { return time.Unix(150, 0) }}

	s.regSetLogin("ALICE")
	if v := r.Snapshot()[0]; v.Username != "ALICE" || !v.LoggedInAt.Equal(time.Unix(150, 0)) {
		t.Fatalf("regSetLogin: %+v", v)
	}
	s.regSetService("PROD")
	if v := r.Snapshot()[0]; v.Service != "PROD" {
		t.Fatalf("regSetService: %q", v.Service)
	}
	s.regClearService()
	s.regClearLogin()
	if v := r.Snapshot()[0]; v.Username != "" || v.Service != "" {
		t.Fatalf("clear helpers: %+v", v)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestSessionRegistryHelpers -v`
Expected: FAIL — `s.Registry undefined`, `s.regSetLogin undefined`

- [ ] **Step 3: Write minimal implementation**

In the `Session` struct (after the `Sleep func(time.Duration)` field near line 143), add:

```go
	// Registry tracks this connection in the process-wide live-session set
	// (GH #91); nil disables tracking (unit tests that build a Session directly).
	// SessionID is this connection's registry id, assigned by the handler.
	Registry  *sessionRegistry
	SessionID uint64
```

After the `now()` method (line ~150), add the nil-safe helpers:

```go
// reg* helpers mirror the session's lifecycle into the live-session registry.
// They are nil-safe so a Session built without a Registry (unit tests) is a no-op.

func (s *Session) regSetLogin(username string) {
	if s.Registry != nil {
		s.Registry.setLogin(s.SessionID, username, s.now())
	}
}

func (s *Session) regClearLogin() {
	if s.Registry != nil {
		s.Registry.clearLogin(s.SessionID)
	}
}

func (s *Session) regSetService(name string) {
	if s.Registry != nil {
		s.Registry.setService(s.SessionID, name)
	}
}

func (s *Session) regClearService() {
	if s.Registry != nil {
		s.Registry.clearService(s.SessionID)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestSessionRegistryHelpers -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/registry_test.go
git commit -m "feat(server): Session registry fields + nil-safe lifecycle helpers (#91)"
```

---

## Task 5: Wire the four lifecycle hooks into Session.Run

**Files:**
- Modify: `internal/server/session.go` (`Run`, lines ~317-477)
- Test: `internal/server/session_registry_test.go` (new)

Hook placement (only 4 edits — clearing login at the top of the outer loop covers every logoff/idle-logout path uniformly):
1. **First statement inside `for {`** (line ~317): `s.regClearLogin()` — covers the initial pre-auth state and every return-to-login.
2. **After `currentUser = identity.Username`** (line ~323): `s.regSetLogin(identity.Username)`.
3. **At bridge start**, right after the `AuditBridgeStart` record / before `s.armBridge(conn)` (line ~459): `s.regSetService(selected.Name)`.
4. **After the bridge returns**, at `s.armPostAuth(conn)` (line ~477): `s.regClearService()`.

- [ ] **Step 1: Write the failing test**

```go
package server

import (
	"net"
	"testing"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/bridge"
	"github.com/coffeemuse/tn3270proxy/internal/store"
)

// snapBridger captures the registry snapshot while a session is bridged, so the
// test can observe the mid-bridge state (username + service set).
type snapBridger struct {
	reg     *sessionRegistry
	during  []SessionView
	cause   bridge.Cause
}

func (sb *snapBridger) Bridge(conn net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error) {
	sb.during = sb.reg.Snapshot()
	return sb.cause, nil
}

func TestSessionRun_RegistryTracksLifecycle(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService},
			{quit: true},
		},
	}
	reg := newSessionRegistry()
	sb := &snapBridger{reg: reg, cause: bridge.CauseUserEscaped}
	s := newTestSession(t, p, sb)
	id := reg.register("203.0.113.5:5555", time.Unix(1000, 0), func() {})
	s.Registry = reg
	s.SessionID = id

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	// During the bridge: logged in as alice, bridged to PROD.
	if len(sb.during) != 1 {
		t.Fatalf("mid-bridge snapshot len = %d, want 1", len(sb.during))
	}
	if sb.during[0].Username != "alice" || sb.during[0].Service != "PROD" {
		t.Errorf("mid-bridge view = %+v, want alice/PROD", sb.during[0])
	}
	// After logoff: entry still present (handler owns deregister), but cleared.
	final := reg.Snapshot()
	if len(final) != 1 {
		t.Fatalf("final snapshot len = %d, want 1 (Run must not deregister)", len(final))
	}
	if final[0].Username != "" || final[0].Service != "" {
		t.Errorf("final view = %+v, want cleared username/service", final[0])
	}
}
```

(`newTestSession` accepts any `Bridger`; `snapBridger` satisfies the interface.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestSessionRun_RegistryTracksLifecycle -v`
Expected: FAIL — mid-bridge `Username`/`Service` empty (hooks not wired yet).

- [ ] **Step 3: Write minimal implementation** — add the 4 hooks.

Edit 1 — top of the outer loop (the `for {` at line ~317), make `s.regClearLogin()` the first statement:

```go
	for {
		s.regClearLogin() // returning to the login screen drops any prior login
		identity, ok, loginDetail := s.doLogin(ctx, conn, term, aud)
```

Edit 2 — after `currentUser = identity.Username` (line ~323):

```go
		currentUser = identity.Username
		s.regSetLogin(identity.Username)
		s.Logger = baseLog.With("user", identity.Username) // enrich with user
```

Edit 3 — at bridge start (line ~457-459), after the `AuditBridgeStart` record:

```go
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditBridgeStart, Username: identity.Username, Service: selected.Name})
			s.regSetService(selected.Name)
			s.armBridge(conn)
```

Edit 4 — after the bridge returns, at the `s.armPostAuth(conn)` line (line ~477):

```go
			s.regClearService()
			s.armPostAuth(conn) // back to the menu: restore the post-auth window
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestSessionRun_RegistryTracksLifecycle -race -v`
Expected: PASS

Then run the full server suite to confirm no regression in existing session tests:
Run: `go test ./internal/server/ -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/session_registry_test.go
git commit -m "feat(server): mirror session lifecycle into the registry (#91)"
```

---

## Task 6: Handler wiring — build the registry, register/deregister per connection

**Files:**
- Modify: `internal/server/server.go` (`sessionHandler` struct ~line 117; `NewSessionHandler` ~line 200; `Handle` ~line 191)
- Test: `internal/server/server_test.go` (or `serveall_test.go`)

- [ ] **Step 1: Write the failing test**

```go
func TestSessionHandler_RegistersAndDeregisters(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/h.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	h := NewSessionHandler(st, 0x6B, Limits{}, nil, "v-test", nil).(sessionHandler)
	if h.registry == nil {
		t.Fatal("NewSessionHandler must create a registry")
	}

	c1, c2 := net.Pipe()
	c2.Close() // peer closed → Negotiate fails fast, Run returns immediately
	h.Handle(c1)

	if got := len(h.registry.Snapshot()); got != 0 {
		t.Fatalf("expected 0 sessions after Handle returns, got %d (deregister missing?)", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestSessionHandler_RegistersAndDeregisters -v`
Expected: FAIL — `h.registry undefined`

- [ ] **Step 3: Write minimal implementation**

Add the field to `sessionHandler` (after `throttle  *authThrottle`):

```go
	throttle  *authThrottle
	registry  *sessionRegistry
```

In `NewSessionHandler`, set it on the returned handler:

```go
	return sessionHandler{store: st, escapeAID: escapeAID, limits: limits, logger: logger, release: release, mfaCipher: mfaCipher, throttle: newAuthThrottle(), registry: newSessionRegistry()}
```

Rewrite `Handle` (currently lines ~191-195) to register/deregister and inject the registry + id:

```go
func (h sessionHandler) Handle(conn net.Conn) {
	connLog := h.logger.With("remote", conn.RemoteAddr().String())
	s := h.sessionFor(conn.RemoteAddr(), connLog)
	if h.registry != nil {
		var once sync.Once
		id := h.registry.register(conn.RemoteAddr().String(), time.Now(), func() {
			once.Do(func() { _ = conn.Close() }) // hard close, idempotent; unblocks the session's Read
		})
		s.Registry = h.registry
		s.SessionID = id
		defer h.registry.deregister(id)
	}
	s.Run(wrapIdle(conn, h.limits.PreAuthIdle))
}
```

`sync` and `time` are already imported in `server.go` (verify the import block; both are present).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestSessionHandler_RegistersAndDeregisters -race -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): per-connection registry register/deregister + idempotent close (#91)"
```

---

## Task 7: ui3270 — full-width row variant for the snapshot screen

**Files:**
- Modify: `internal/ui3270/types.go` (`SnapshotView` struct)
- Modify: `internal/ui3270/snapshotscreen.go` (`buildSnapshotScreen`)
- Test: `internal/ui3270/snapshotscreen_test.go` (new or existing)

- [ ] **Step 1: Write the failing test**

```go
package ui3270

import "testing"

func fieldByContent(s []struct{ /* placeholder */ }) {} // remove if unused

func TestBuildSnapshotScreen_WideRendersFullWidthRows(t *testing.T) {
	v := SnapshotView{
		Title: "ACTIVE SESSIONS",
		AsOf:  "AS OF X",
		Wide:  true,
		Head:  SnapshotRow{Left: "ID    CLIENT"},
		Rows:  []SnapshotRow{{Left: "12    1.2.3.4:5"}},
	}
	screen, _ := buildSnapshotScreen(24, v)

	// The wide row content must appear as a single field at the left content
	// column (snapLeftAttr), and there must be NO field at the Mid attr column
	// (that attr byte would otherwise chop the packed row).
	var sawWide, sawMid bool
	for _, f := range screen {
		if f.Row == 4 && f.Col == snapLeftAttr && f.Content == "12    1.2.3.4:5" {
			sawWide = true
		}
		if f.Row == 4 && f.Col == snapMidAttr {
			sawMid = true
		}
	}
	if !sawWide {
		t.Error("wide data row not rendered as a single left-anchored field")
	}
	if sawMid {
		t.Error("wide mode must not emit a Mid-segment field (it would chop the row)")
	}
}
```

(Delete the stray `fieldByContent` placeholder line — it is not needed; included only to remind you the test file's package is `ui3270`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui3270/ -run TestBuildSnapshotScreen_Wide -v`
Expected: FAIL — `v.Wide` undefined.

- [ ] **Step 3: Write minimal implementation**

In `types.go`, add `Wide` to `SnapshotView`:

```go
type SnapshotView struct {
	Title, RowInfo, AsOf, Legend, ErrMsg, PFHelp, Empty string
	Head                                                SnapshotRow
	Rows                                                []SnapshotRow
	// Wide renders each row (and the heading) as one full-width field spanning
	// cols 8–79 instead of the three colour-segmented fields. Used by screens
	// with many plain columns and no per-segment colour (GH #91 active sessions).
	Wide bool
}
```

In `snapshotscreen.go`, branch the heading and the per-row rendering on `v.Wide`.

Replace the heading lines (currently the three `bodyTopRow()` Blue fields) with:

```go
	if v.Wide {
		screen = append(screen, go3270.Field{Row: bodyTopRow(), Col: snapLeftAttr, Color: go3270.Blue, Content: v.Head.Left})
	} else {
		screen = append(screen,
			go3270.Field{Row: bodyTopRow(), Col: snapLeftAttr, Color: go3270.Blue, Content: v.Head.Left},
			go3270.Field{Row: bodyTopRow(), Col: snapMidAttr, Color: go3270.Blue, Content: v.Head.Mid},
			go3270.Field{Row: bodyTopRow(), Col: snapRightAttr, Color: go3270.Blue, Content: v.Head.Right},
		)
	}
```

In the data-row loop, replace `screen = append(screen, snapSegments(row, r)...)` with:

```go
		if v.Wide {
			screen = append(screen, go3270.Field{Row: row, Col: snapLeftAttr, Color: go3270.Green, Content: r.Left})
		} else {
			screen = append(screen, snapSegments(row, r)...)
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui3270/ -run TestBuildSnapshotScreen_Wide -v`
Expected: PASS

Then confirm the audit viewer (non-wide) is unaffected:
Run: `go test ./internal/ui3270/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/types.go internal/ui3270/snapshotscreen.go internal/ui3270/snapshotscreen_test.go
git commit -m "feat(ui3270): full-width row variant for snapshot screens (#91)"
```

---

## Task 8: ui3270 — RunSnapshotList confirm-capable action command

**Files:**
- Modify: `internal/ui3270/snapshotlist.go` (`SnapshotConfig` + `RunSnapshotList`)
- Test: `internal/ui3270/snapshotlist_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRunSnapshotList_ActCmdConfirmThenCommit(t *testing.T) {
	acted := -1
	cfg := SnapshotConfig[int]{
		Rows:   24,
		Fetch:  func(context.Context) ([]SnapshotEntry[int], string, string) { return entries(3), "X", "" },
		ActCmd: 'D',
		Confirm: func(item int) (string, string) { return "CONFIRM - PRESS D AGAIN", "" },
		OnAct: func(_ context.Context, item int) (bool, string) {
			acted = item
			return true, ""
		},
	}
	// First D arms the confirm; second D commits row 1's item (==1); then PF3.
	r := &scriptRenderer{acts: []ListAction{{Cmd: 'D', Row: 1}, {Cmd: 'D', Row: 1}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if acted != 1 {
		t.Errorf("OnAct item = %d, want 1", acted)
	}
	// The prompt must have been shown on the message line after the first D.
	if got := r.views[1].ErrMsg; got != "CONFIRM - PRESS D AGAIN" {
		t.Errorf("confirm prompt = %q, want the prompt on the message line", got)
	}
}

func TestRunSnapshotList_ActCmdBlockedVetoes(t *testing.T) {
	acted := false
	cfg := SnapshotConfig[int]{
		Rows:    24,
		Fetch:   func(context.Context) ([]SnapshotEntry[int], string, string) { return entries(2), "X", "" },
		ActCmd:  'D',
		Confirm: func(item int) (string, string) { return "", "BLOCKED" },
		OnAct:   func(_ context.Context, item int) (bool, string) { acted = true; return true, "" },
	}
	r := &scriptRenderer{acts: []ListAction{{Cmd: 'D', Row: 0}, {Cmd: 'D', Row: 0}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if acted {
		t.Error("a blocked Confirm must veto: OnAct must not run")
	}
	if got := r.views[1].ErrMsg; got != "BLOCKED" {
		t.Errorf("veto message = %q, want BLOCKED on the message line", got)
	}
}

func TestRunSnapshotList_OtherKeyCancelsPendingConfirm(t *testing.T) {
	acted := false
	cfg := SnapshotConfig[int]{
		Rows:    24,
		Fetch:   func(context.Context) ([]SnapshotEntry[int], string, string) { return entries(2), "X", "" },
		ActCmd:  'D',
		Confirm: func(item int) (string, string) { return "CONFIRM", "" },
		OnAct:   func(_ context.Context, item int) (bool, string) { acted = true; return true, "" },
	}
	// D (arm) → PF8 (cancel) → PF3 (exit). OnAct must never run.
	r := &scriptRenderer{acts: []ListAction{{Cmd: 'D', Row: 0}, {PF: 8}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if acted {
		t.Error("a non-D action must cancel the pending confirm")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui3270/ -run TestRunSnapshotList_ActCmd -v`
Expected: FAIL — `ActCmd`/`Confirm`/`OnAct` undefined fields.

- [ ] **Step 3: Write minimal implementation**

Extend `SnapshotConfig` in `snapshotlist.go`:

```go
type SnapshotConfig[T any] struct {
	Title, Legend, PFHelp, Empty string
	Head                         SnapshotRow
	Rows                         int // terminal row count → page-size math
	Wide                         bool
	Fetch                        func(ctx context.Context) (rows []SnapshotEntry[T], asOf, errMsg string)
	OnSelect                     func(ctx context.Context, r Renderer, item T) error
	// ActCmd is a confirm-gated mutating line command (e.g. 'D'). 0 disables it.
	// Confirm is consulted on first keypress (blocked != "" vetoes with that
	// message; otherwise prompt is shown and the action is held pending). The
	// pending action commits via OnAct when ActCmd is re-issued, and is cancelled
	// by any other action. OnAct returns (refresh, errMsg): refresh re-fetches the
	// snapshot. All messages render on the row-2 message line.
	ActCmd  byte
	Confirm func(item T) (prompt, blocked string)
	OnAct   func(ctx context.Context, item T) (refresh bool, errMsg string)
}
```

Rewrite `RunSnapshotList` to thread `Wide` and handle the pending confirm:

```go
func RunSnapshotList[T any](ctx context.Context, r Renderer, cfg SnapshotConfig[T]) error {
	var (
		rows    []SnapshotEntry[T]
		asOf    string
		errMsg  string
		page    int
		loaded  bool
		pending *T // ActCmd target awaiting confirmation
	)
	for {
		if !loaded {
			rows, asOf, errMsg = cfg.Fetch(ctx)
			if errMsg != "" {
				rows = nil
			}
			loaded = true
		}
		page, start, end, rowInfo := pageBounds(page, len(rows), cfg.Rows)
		pageRows := rows[start:end]
		display := make([]SnapshotRow, len(pageRows))
		for i, e := range pageRows {
			display[i] = e.Row
		}
		act, err := r.Snapshot(SnapshotView{
			Title: cfg.Title, RowInfo: rowInfo, AsOf: asOf, Head: cfg.Head,
			Rows: display, Legend: cfg.Legend, ErrMsg: errMsg, PFHelp: cfg.PFHelp,
			Empty: cfg.Empty, Wide: cfg.Wide,
		})
		if err != nil {
			return err
		}
		errMsg = ""

		switch {
		case act.PF == 3:
			return nil
		case act.PF == 7:
			pending = nil
			page--
		case act.PF == 8:
			pending = nil
			if end < len(rows) {
				page++
			}
		case cfg.ActCmd != 0 && act.Cmd == cfg.ActCmd:
			if act.Row >= len(pageRows) {
				pending = nil
				break
			}
			if pending != nil { // second ActCmd = confirm
				target := *pending
				pending = nil
				if cfg.OnAct != nil {
					refresh, msg := cfg.OnAct(ctx, target)
					errMsg = msg
					if refresh {
						loaded = false
						page = 0
					}
				}
			} else if cfg.Confirm != nil { // first ActCmd = consult Confirm
				item := pageRows[act.Row].Item
				prompt, blocked := cfg.Confirm(item)
				if blocked != "" {
					errMsg = blocked
				} else {
					pending = &item
					errMsg = prompt
				}
			}
		case act.Cmd == 'S':
			pending = nil
			if cfg.OnSelect != nil && act.Row < len(pageRows) {
				if ferr := cfg.OnSelect(ctx, r, pageRows[act.Row].Item); ferr != nil {
					return ferr
				}
			}
		case act.Cmd == 0 && act.PF == 0: // plain Enter = refresh
			pending = nil
			loaded = false
			page = 0
		default:
			pending = nil
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui3270/ -run TestRunSnapshotList -v`
Expected: PASS (the new ActCmd tests plus the existing stable-paging / Enter-refetch / OnSelect tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/snapshotlist.go internal/ui3270/snapshotlist_test.go
git commit -m "feat(ui3270): confirm-capable action command on RunSnapshotList (#91)"
```

---

## Task 9: store — AuditSessionDisconnect kind + colour

**Files:**
- Modify: `internal/store/audit.go` (kind constants ~line 30-46)
- Modify: `internal/server/admin_audit.go` (`auditEventColor`)
- Test: `internal/server/admin_audit_test.go`

- [ ] **Step 1: Write the failing test** (append to `admin_audit_test.go`)

```go
func TestAuditEventColor_SessionDisconnectIsYellow(t *testing.T) {
	if got := auditEventColor(store.AuditSessionDisconnect); got != go3270.Yellow {
		t.Errorf("auditEventColor(session_disconnect) = %v, want Yellow", got)
	}
}
```

(Ensure the test file imports `"github.com/racingmars/go3270"` and `".../internal/store"` — both are already used in the package's tests; add if missing.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestAuditEventColor_SessionDisconnect -v`
Expected: FAIL — `undefined: store.AuditSessionDisconnect`

- [ ] **Step 3: Write minimal implementation**

In `internal/store/audit.go`, add the constant alongside the others:

```go
	AuditSessionDisconnect = "session_disconnect" // admin disconnected a live session (GH #91)
```

In `internal/server/admin_audit.go`, add it to the Yellow tier of `auditEventColor`:

```go
	case store.AuditAdmin, store.AuditMFACleared, store.AuditMFAEnforced, store.AuditMFAEnrolled, store.AuditSessionDisconnect:
		return go3270.Yellow
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestAuditEventColor -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/audit.go internal/server/admin_audit.go internal/server/admin_audit_test.go
git commit -m "feat(store): session_disconnect audit kind + yellow colour (#91)"
```

---

## Task 10: screens + presenter — admin menu option 7

**Files:**
- Modify: `internal/screens/admin.go` (`AdminMenuScreen` opts)
- Modify: `internal/server/presenter_admin.go` (`AdminMenu` parse)
- Test: `internal/screens/admin_test.go` (existing) + `internal/server/admin_test.go` (existing)

- [ ] **Step 1: Write the failing test**

In `internal/screens/admin_test.go` (append; mirror the existing admin-menu test style — assert by field content):

```go
func TestAdminMenuScreen_HasSessionsOption(t *testing.T) {
	screen, _ := AdminMenuScreen(Geometry{Rows: 24, Cols: 80}, "")
	var sawKey, sawName bool
	for _, f := range screen {
		if f.Content == "  7" {
			sawKey = true
		}
		if f.Content == "Sessions" {
			sawName = true
		}
	}
	if !sawKey || !sawName {
		t.Errorf("admin menu missing option 7 Sessions (key=%v name=%v)", sawKey, sawName)
	}
}
```

> Check the existing admin-menu screen test for the exact `Geometry` literal it uses (it may be a helper). Match it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestAdminMenuScreen_HasSessionsOption -v`
Expected: FAIL — no `"Sessions"` field.

- [ ] **Step 3: Write minimal implementation**

In `internal/screens/admin.go`, add the row to `opts`:

```go
		{"5", "Networks", "Trusted networks (DoS allow-list)"},
		{"6", "Audit", "Browse the audit trail"},
		{"7", "Sessions", "Active client sessions"},
```

In `internal/server/presenter_admin.go`, accept `"7"` in the switch (after `case "6"`):

```go
		case "6":
			return 6, false, nil
		case "7":
			return 7, false, nil
```

Update the `AdminMenu` doc comment range from `1-6` to `1-7`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/screens/ -run TestAdminMenuScreen_HasSessionsOption -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/screens/admin.go internal/server/presenter_admin.go internal/screens/admin_test.go
git commit -m "feat(screens): admin menu option 7 Sessions (#91)"
```

---

## Task 11: admin flow — activeSessions screen + dispatch

**Files:**
- Modify: `internal/server/admin.go` (`adminFlow` struct + `Run` dispatch)
- Create: `internal/server/admin_sessions.go`
- Test: `internal/server/admin_sessions_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package server

import (
	"context"
	"testing"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
)

// fakeRegistry is a scripted SessionRegistry.
type fakeRegistry struct {
	views       []SessionView
	disconnect  []uint64 // ids passed to Disconnect, in order
	disconnects map[uint64]SessionView
	okFor       map[uint64]bool
}

func (f *fakeRegistry) Snapshot() []SessionView { return f.views }
func (f *fakeRegistry) Disconnect(id uint64) (SessionView, bool) {
	f.disconnect = append(f.disconnect, id)
	return f.disconnects[id], f.okFor[id]
}

// sessRenderer scripts Snapshot actions for the activeSessions flow.
type sessRenderer struct {
	acts  []ui3270.ListAction
	views []ui3270.SnapshotView
}

func (r *sessRenderer) List(ui3270.ListView) (ui3270.ListAction, error) { panic("unused") }
func (r *sessRenderer) Form(ui3270.FormView) (ui3270.FormAction, error) { panic("unused") }
func (r *sessRenderer) Detail(ui3270.DetailView) error                  { return nil }
func (r *sessRenderer) Snapshot(v ui3270.SnapshotView) (ui3270.ListAction, error) {
	r.views = append(r.views, v)
	a := r.acts[0]
	r.acts = r.acts[1:]
	return a, nil
}

func newSessionsFlow(reg SessionRegistry, selfID uint64, r ui3270.Renderer, audit *[]store.AuditEvent) *adminFlow {
	return &adminFlow{
		term:          Term{Rows: 24, Cols: 80},
		renderer:      func(_ net.Conn) ui3270.Renderer { return r },
		sessions:      reg,
		selfSessionID: selfID,
		now:           func() time.Time { return time.Unix(1_000_000, 0).UTC() },
		audit:         func(_ context.Context, ev store.AuditEvent) { *audit = append(*audit, ev) },
	}
}

func TestActiveSessions_DisconnectAuditsSubject(t *testing.T) {
	reg := &fakeRegistry{
		views: []SessionView{
			{ID: 1, RemoteAddr: "10.0.0.9:5050", ConnectedAt: time.Unix(999_000, 0), LoggedInAt: time.Unix(999_500, 0), Username: "BOB"},
			{ID: 7, RemoteAddr: "10.0.0.4:6060", ConnectedAt: time.Unix(998_000, 0)}, // pre-auth
		},
		disconnects: map[uint64]SessionView{1: {ID: 1, RemoteAddr: "10.0.0.9:5050", Username: "BOB"}},
		okFor:       map[uint64]bool{1: true},
	}
	var audits []store.AuditEvent
	// D on row 0 (id 1) → D again (confirm) → PF3.
	r := &sessRenderer{acts: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {Cmd: 'D', Row: 0}, {PF: 3}}}
	f := newSessionsFlow(reg, 7 /*self is id 7*/, r, &audits)

	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 1 || reg.disconnect[0] != 1 {
		t.Fatalf("Disconnect calls = %v, want [1]", reg.disconnect)
	}
	if len(audits) != 1 || audits[0].Kind != store.AuditSessionDisconnect || audits[0].Username != "BOB" {
		t.Fatalf("audit = %+v, want one session_disconnect for BOB", audits)
	}
}

func TestActiveSessions_SelfDisconnectVetoed(t *testing.T) {
	reg := &fakeRegistry{
		views:       []SessionView{{ID: 7, RemoteAddr: "10.0.0.4:6060", LoggedInAt: time.Unix(1, 0), Username: "ADMIN"}},
		disconnects: map[uint64]SessionView{},
		okFor:       map[uint64]bool{},
	}
	var audits []store.AuditEvent
	// D on row 0 (id 7 == self) → D again → PF3. Must never disconnect.
	r := &sessRenderer{acts: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {Cmd: 'D', Row: 0}, {PF: 3}}}
	f := newSessionsFlow(reg, 7, r, &audits)

	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 0 {
		t.Errorf("self-session must not be disconnected; got calls %v", reg.disconnect)
	}
	if len(audits) != 0 {
		t.Errorf("no audit on a vetoed self-disconnect; got %+v", audits)
	}
	// The veto message must show on the message line.
	if r.views[1].ErrMsg != "CANNOT DISCONNECT YOUR OWN SESSION" {
		t.Errorf("veto message = %q", r.views[1].ErrMsg)
	}
}

func TestActiveSessions_RowFormatting(t *testing.T) {
	reg := &fakeRegistry{
		views: []SessionView{
			{ID: 1, RemoteAddr: "10.0.0.9:5050", ConnectedAt: time.Unix(999_000, 0), LoggedInAt: time.Unix(999_500, 0), Username: "BOB", Service: "PROD"},
			{ID: 2, RemoteAddr: "10.0.0.8:5051", ConnectedAt: time.Unix(999_900, 0)}, // pre-auth, no service
		},
	}
	var audits []store.AuditEvent
	r := &sessRenderer{acts: []ui3270.ListAction{{PF: 3}}}
	f := newSessionsFlow(reg, 99 /*self not present*/, r, &audits)
	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	row0 := r.views[0].Rows[0].Left
	row1 := r.views[0].Rows[1].Left
	if !strings.Contains(row0, "BOB") || !strings.Contains(row0, "PROD") || !strings.Contains(row0, "10.0.0.9:5050") {
		t.Errorf("row0 = %q, want BOB/PROD/addr", row0)
	}
	if !strings.Contains(row1, "(login)") || !strings.Contains(row1, "-") {
		t.Errorf("row1 = %q, want (login) and '-' service", row1)
	}
}
```

(Add imports `"net"` and `"strings"` to the test file as used above.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestActiveSessions -v`
Expected: FAIL — `f.activeSessions undefined`, `adminFlow.sessions`/`selfSessionID` undefined.

- [ ] **Step 3: Write minimal implementation**

Add the fields to `adminFlow` in `admin.go` (after `now func() time.Time`):

```go
	now func() time.Time
	// sessions is the live-session registry seam (GH #91); nil disables the
	// Active Sessions screen. selfSessionID is the acting admin's own session id,
	// guarded against self-disconnect.
	sessions      SessionRegistry
	selfSessionID uint64
```

Add the dispatch in `adminFlow.Run`'s switch (after `case 6`):

```go
		case 6:
			err = f.auditLog(ctx, conn)
		case 7:
			err = f.activeSessions(ctx, conn)
```

Create `internal/server/admin_sessions.go`:

```go
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
	"net"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
)

// Active-sessions column widths (full-width row, cols 8–79 = 72 chars):
// ID 5 + CLIENT 21 + CONNECTED 9 + SESSION 8 + USER 8 + SERVICE 16, single
// spaces between (5+1+21+1+9+1+8+1+8+1+16 = 72).
const sessionRowFmt = "%-5d %-21s %-9s %-8s %-8s %-16s"

func sessionHeader() string {
	return fmt.Sprintf("%-5s %-21s %-9s %-8s %-8s %-16s",
		"ID", "CLIENT", "CONNECTED", "SESSION", "USER", "SERVICE")
}

// clipField clips s to width w, marking truncation with a trailing '>' (ASCII;
// '…' is unsafe on a 3270 screen). Shorter strings are returned unchanged (the
// row formatter pads via the width verb).
func clipField(s string, w int) string {
	if len(s) <= w {
		return s
	}
	return s[:w-1] + ">"
}

// hhmmss formats a non-negative duration as HH:MM:SS (hours may exceed 99).
func hhmmss(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", secs/3600, (secs%3600)/60, secs%60)
}

// fmtSessionRow packs one SessionView into a full-width snapshot row.
func fmtSessionRow(v SessionView, now time.Time, selfID uint64) ui3270.SnapshotRow {
	user := v.Username
	if v.LoggedInAt.IsZero() {
		user = "(login)"
	}
	if v.ID == selfID {
		user = "*YOU*"
	}
	service := v.Service
	if service == "" {
		service = "-"
	}
	left := fmt.Sprintf(sessionRowFmt,
		v.ID,
		clipField(v.RemoteAddr, 21),
		v.ConnectedAt.UTC().Format("15:04:05"),
		hhmmss(now.Sub(v.ConnectedAt)),
		clipField(user, 8),
		clipField(service, 16),
	)
	return ui3270.SnapshotRow{Left: left}
}

// activeSessions drives the read-only live-session viewer with a confirm-gated
// Disconnect ('D'). PF3 returns to the admin menu. The acting admin's own
// session is marked *YOU* and cannot be disconnected.
func (f *adminFlow) activeSessions(ctx context.Context, conn net.Conn) error {
	if f.sessions == nil {
		return nil // no registry wired (direct unit tests without one)
	}
	r := f.renderer(conn)
	cfg := ui3270.SnapshotConfig[SessionView]{
		Title:  "ACTIVE SESSIONS",
		Wide:   true,
		Head:   ui3270.SnapshotRow{Left: sessionHeader()},
		Legend: "D=Disconnect",
		PFHelp: "PF3=Admin Menu  PF7=Up  PF8=Down  Enter=Refresh",
		Empty:  "(no active sessions)",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.SnapshotEntry[SessionView], string, string) {
			now := f.clock()
			views := f.sessions.Snapshot()
			rows := make([]ui3270.SnapshotEntry[SessionView], len(views))
			for i, v := range views {
				rows[i] = ui3270.SnapshotEntry[SessionView]{Row: fmtSessionRow(v, now, f.selfSessionID), Item: v}
			}
			asOf := "AS OF " + julianStamp(now) + "  " + now.Format("15:04") + " UTC"
			return rows, asOf, ""
		},
		ActCmd: 'D',
		Confirm: func(v SessionView) (string, string) {
			if v.ID == f.selfSessionID {
				return "", "CANNOT DISCONNECT YOUR OWN SESSION"
			}
			who := v.Username
			if who == "" {
				who = v.RemoteAddr
			}
			return "CONFIRM DISCONNECT " + who + " - PRESS D AGAIN", ""
		},
		OnAct: func(ctx context.Context, v SessionView) (bool, string) {
			booted, ok := f.sessions.Disconnect(v.ID)
			if !ok {
				return true, "SESSION ALREADY ENDED"
			}
			if f.audit != nil {
				f.audit(ctx, store.AuditEvent{
					Kind:     store.AuditSessionDisconnect,
					Username: booted.Username,
					Detail:   fmt.Sprintf("disconnected session %d (%s)", booted.ID, booted.RemoteAddr),
				})
			}
			return true, ""
		},
	}
	return ui3270.RunSnapshotList(ctx, r, cfg)
}
```

> Note: `julianStamp` already exists in `admin_audit.go` (same package) — reuse it; do not redefine.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestActiveSessions -race -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin.go internal/server/admin_sessions.go internal/server/admin_sessions_test.go
git commit -m "feat(server): admin Active Sessions screen with confirm-gated Disconnect (#91)"
```

---

## Task 12: thread the registry from Session.Run into adminFlow

**Files:**
- Modify: `internal/server/session.go` (`adminFlow` construction ~line 410)
- Test: `internal/server/admin_test.go` (existing admin-selection test will still pass; add a focused assertion)

- [ ] **Step 1: Write the failing test**

This wiring is best verified by an end-to-end-ish check that selecting option 7 as an admin runs the flow against the session's registry. Add to `internal/server/admin_test.go` (adjust the fake admin presenter to return choice 7 once, then back):

```go
func TestSessionAdminSessionsUsesRegistry(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "root", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{choice: menuAdmin}, {quit: true}},
	}
	// adminPresenter: pick option 7 once, then PF3 back to the service menu.
	ap := &fakeAdminPresenter{choices: []adminChoice{{choice: 7}, {back: true}}}
	reg := newSessionRegistry()
	id := reg.register("203.0.113.1:5000", time.Unix(1000, 0), func() {})

	s := newTestSession(t, p, &fakeBridger{})
	s.AdminPresenter = ap
	s.Registry = reg
	s.SessionID = id
	// Inject a fake admin renderer that immediately PF3s out of the snapshot list.
	s.AdminRenderer = func(conn net.Conn, term Term) ui3270.Renderer {
		return &sessRenderer{acts: []ui3270.ListAction{{PF: 3}}}
	}

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	// The flow rendered the snapshot from the session's own registry: the one
	// live session (self) must have been listed and marked *YOU*.
	// (sessRenderer recorded the view; assert it saw exactly the self row.)
	if got := len(ap.choices); got != 0 {
		t.Errorf("admin choices unconsumed: %d left", got)
	}
}
```

> Inspect the existing admin tests for the real fake admin-presenter type name and its scripting struct (the plan assumes `fakeAdminPresenter`/`adminChoice`; use whatever `admin_test.go` already defines). The point of the test: with `s.Registry`/`s.SessionID` set and option 7 selected, `activeSessions` runs without panic and consumes the scripted actions. If the existing fakes differ, adapt the scaffolding — the assertion that matters is "option 7 runs the registry-backed flow."

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestSessionAdminSessionsUsesRegistry -v`
Expected: FAIL — `adminFlow` has no `sessions`/`selfSessionID` wired from the session, so `activeSessions` returns early (nil registry) — adjust assertion to detect the early return, or it panics if mis-wired. (If `activeSessions` returns nil immediately because `f.sessions` is nil, the scripted `{PF:3}` action is never consumed — assert the renderer's `acts` is unconsumed to prove the gap.)

- [ ] **Step 3: Write minimal implementation**

In `session.go`, where `adminFlow` is constructed (line ~410), set the registry fields only when a registry is present (avoid the nil-interface-holding-nil-pointer trap):

```go
				flow := &adminFlow{store: s.Store, presenter: s.AdminPresenter,
					renderer: renderer,
					identity: identity, term: term, audit: aud.record,
					logger: s.log(), now: s.now}
				if s.Registry != nil {
					flow.sessions = s.Registry
					flow.selfSessionID = s.SessionID
				}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestSessionAdminSessionsUsesRegistry -race -v`
Expected: PASS

Then the whole server package:
Run: `go test ./internal/server/ -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/admin_test.go
git commit -m "feat(server): wire live-session registry into the admin flow (#91)"
```

---

## Task 13: protocol smoke test + final verification

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh` (add Active Sessions assertions)

- [ ] **Step 1: Add smoke assertions**

Extend the smoke script (study the existing audit-viewer assertions for the exact helper functions it uses) to, as `ZZADMIN`:
1. Navigate `A` → `7` and assert the title row contains `ACTIVE SESSIONS` and the heading row contains `CLIENT` / `CONNECTED` / `SERVICE`.
2. Assert the cursor homes to the first command field (row 4, col 3) — or `{0,0}` if the harness opens only the one session and it is `*YOU*` (still a row, so the command field).
3. With a **second** s3270 connection open and logged in, assert that connection appears as a second row, then issue `D` + `D` on it and assert the second connection drops (its session ends) while the admin's own row remains and shows `*YOU*` (and `D` on the `*YOU*` row shows `CANNOT DISCONNECT YOUR OWN SESSION` on the message line).
4. Assert `PF3` returns to the admin menu.

- [ ] **Step 2: Run the smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all assertions pass (screen content, cursor, the second-connection disconnect).

- [ ] **Step 3: Full build + race suite**

Run: `go build ./... && go test ./... -race`
Expected: PASS

- [ ] **Step 4: Manual emulator pass (final word on visual polish)**

Connect with `c3270 127.0.0.1:2323`, log in as an admin, open `A` → `7`, open a second `c3270` session, confirm the row layout/alignment within 80 cols, disconnect the second session, and confirm the `*YOU*` guard.

- [ ] **Step 5: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "test(smoke): active sessions screen + disconnect assertions (#91)"
```

---

## Self-Review (completed by plan author)

**Spec coverage:** registry (§Architecture/§1 → T1–T3), lifecycle integration (§2 → T4–T6), seam + adminFlow (§3 → T11–T12), RunSnapshotList confirm extension (§4 → T8), full-width row (§4 → T7), screen layout (§5 → T11 formatting + T10 menu), disconnect flow + self-veto (§6 → T8/T11), auditing (§7 → T9/T11), CLIENT truncation (§5 → T11 `clipField` + test), testing matrix (§Testing → tests in every task + T13 smoke). All sections mapped.

**Placeholder scan:** no TBD/TODO; every code step shows complete code. (Two explicit "inspect the existing fake" notes in T12/T10 are deliberate — they point at pre-existing test scaffolding whose exact names must be matched, not invented.)

**Type consistency:** `SessionView`, `sessionRegistry`, `SessionRegistry`, `register/deregister/Snapshot/Disconnect/setLogin/clearLogin/setService/clearService`, `reg*` helpers, `SnapshotConfig{ActCmd,Confirm,OnAct,Wide}`, `SnapshotView.Wide`, `AuditSessionDisconnect`, `activeSessions`, `fmtSessionRow`, `clipField`, `hhmmss`, `sessionHeader` — all names used consistently across tasks.
