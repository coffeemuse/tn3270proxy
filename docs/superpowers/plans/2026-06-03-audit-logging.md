# Audit Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A SQLite-backed audit trail of connect/auth/bridge/admin/disconnect events, recorded best-effort through an `Auditor` seam in the session, with `audit list` / `audit prune` CLI verbs.

**Architecture:** A new `audit` table owned by `store` (spec: `docs/superpowers/specs/2026-06-03-audit-logging-design.md`). `server` defines an `Auditor` interface (mirroring `Presenter`/`Bridger`); a per-connection `auditTrail` helper prefills session ID + remote addr into every event; `adminFlow` gets the same hook for admin CRUD events. `cmd/tn3270proxy` gains an `audit` subcommand with `list` and `prune` verbs.

**Tech Stack:** Go, modernc.org/sqlite (pure Go), stdlib `flag`/`crypto/rand`. TDD with `go test ./... -race` throughout.

**Conventions that apply to every task:** never log or store credentials; all SQL stays in `internal/store`; commits use conventional-ish prefixes; run `go test ./... -race` before every commit (the bridge is concurrent).

---

### Task 1: store — `audit` table, `AuditEvent`, `RecordAudit`, `ListAudit` (no filters)

**Files:**
- Modify: `internal/store/store.go` (the `schema` const, lines 39–67)
- Create: `internal/store/audit.go`
- Create: `internal/store/audit_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/store/audit_test.go`:

```go
package store

import (
	"context"
	"testing"
	"time"
)

func TestRecordAndListAudit(t *testing.T) {
	st := newTestStore(t) // helper from store_test.go
	ctx := context.Background()
	t0 := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	events := []AuditEvent{
		{At: t0, SessionID: "s1", Kind: AuditConnect, RemoteAddr: "10.0.0.5:40000"},
		{At: t0.Add(time.Second), SessionID: "s1", Kind: AuditAuthOK, Username: "alice", RemoteAddr: "10.0.0.5:40000"},
		{At: t0.Add(2 * time.Second), SessionID: "s1", Kind: AuditDisconnect, RemoteAddr: "10.0.0.5:40000", Detail: "client disconnected"},
	}
	for _, ev := range events {
		if err := st.RecordAudit(ctx, ev); err != nil {
			t.Fatalf("RecordAudit(%s): %v", ev.Kind, err)
		}
	}

	got, err := st.ListAudit(ctx, AuditFilter{})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Newest first (id DESC — RFC3339 is second-granular, id preserves insert order).
	if got[0].Kind != AuditDisconnect || got[1].Kind != AuditAuthOK || got[2].Kind != AuditConnect {
		t.Errorf("order = %s,%s,%s, want disconnect,auth_ok,connect", got[0].Kind, got[1].Kind, got[2].Kind)
	}
	if !got[2].At.Equal(t0) {
		t.Errorf("At round-trip = %v, want %v", got[2].At, t0)
	}
	if got[1].Username != "alice" || got[0].Detail != "client disconnected" || got[2].RemoteAddr != "10.0.0.5:40000" {
		t.Errorf("field round-trip failed: %+v", got)
	}
	if got[0].SessionID != "s1" || got[0].ID == 0 {
		t.Errorf("SessionID/ID round-trip failed: %+v", got[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestRecordAndListAudit -v`
Expected: FAIL (compile error: `AuditEvent`, `AuditConnect`, etc. undefined)

- [ ] **Step 3: Add the table to the schema and write the implementation**

In `internal/store/store.go`, append to the `schema` const (inside the backticks, after the `group_services` table):

```sql
CREATE TABLE IF NOT EXISTS audit (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	at          TEXT NOT NULL,
	session_id  TEXT NOT NULL,
	kind        TEXT NOT NULL,
	username    TEXT NOT NULL DEFAULT '',
	remote_addr TEXT NOT NULL DEFAULT '',
	service     TEXT NOT NULL DEFAULT '',
	detail      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS audit_at ON audit(at);
CREATE INDEX IF NOT EXISTS audit_username ON audit(username);
```

Create `internal/store/audit.go`:

```go
package store

import (
	"context"
	"time"
)

// Audit event kinds. One session_id ties a connection's events together.
const (
	AuditConnect     = "connect"      // TCP session began
	AuditAuthOK      = "auth_ok"      // login success
	AuditAuthFail    = "auth_fail"    // login failure (attempted username, never the password)
	AuditBridgeStart = "bridge_start" // service selected, backend dial begins
	AuditBridgeEnd   = "bridge_end"   // bridge returned (detail = cause)
	AuditAdmin       = "admin"        // admin CRUD mutation (detail = change description)
	AuditDisconnect  = "disconnect"   // connection ended (detail = how)
)

// AuditEvent is one audit-trail row. Username is a plain string, not a user
// FK: rows must survive user deletion, and auth_fail records usernames that
// may not exist.
type AuditEvent struct {
	ID         int64
	At         time.Time
	SessionID  string
	Kind       string
	Username   string
	RemoteAddr string
	Service    string
	Detail     string
}

// RecordAudit inserts ev. At is stored as UTC RFC3339 (second resolution;
// the autoincrement id preserves insert order within a second).
func (s *Store) RecordAudit(ctx context.Context, ev AuditEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit (at, session_id, kind, username, remote_addr, service, detail)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.At.UTC().Format(time.RFC3339), ev.SessionID, ev.Kind,
		ev.Username, ev.RemoteAddr, ev.Service, ev.Detail)
	return err
}

// AuditFilter narrows ListAudit. Zero values mean "no constraint";
// Limit <= 0 means the default of 100 rows.
type AuditFilter struct {
	Username string
	Kind     string
	Since    time.Time
	Limit    int
}

// ListAudit returns matching events, newest first.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEvent, error) {
	query := `SELECT id, at, session_id, kind, username, remote_addr, service, detail
		FROM audit ORDER BY id DESC LIMIT ?`
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var ev AuditEvent
		var at string
		if err := rows.Scan(&ev.ID, &at, &ev.SessionID, &ev.Kind,
			&ev.Username, &ev.RemoteAddr, &ev.Service, &ev.Detail); err != nil {
			return nil, err
		}
		if ev.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
```

(Filters are added in Task 2 — this version only honors Limit's default.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestRecordAndListAudit|TestMigrateIdempotent' -v`
Expected: PASS (both — migration must stay idempotent with the new DDL)

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS

```bash
git add internal/store/store.go internal/store/audit.go internal/store/audit_test.go
git commit -m "feat: audit table with RecordAudit/ListAudit in store"
```

---

### Task 2: store — `ListAudit` filters (username / kind / since / limit)

**Files:**
- Modify: `internal/store/audit.go` (`ListAudit`)
- Modify: `internal/store/audit_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/audit_test.go`:

```go
func TestListAuditFilters(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	seed := []AuditEvent{
		{At: t0, SessionID: "s1", Kind: AuditAuthFail, Username: "mallory"},
		{At: t0.Add(time.Minute), SessionID: "s2", Kind: AuditAuthOK, Username: "alice"},
		{At: t0.Add(2 * time.Minute), SessionID: "s2", Kind: AuditBridgeStart, Username: "alice", Service: "PROD"},
		{At: t0.Add(time.Hour), SessionID: "s3", Kind: AuditAuthFail, Username: "mallory"},
	}
	for _, ev := range seed {
		if err := st.RecordAudit(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name      string
		f         AuditFilter
		wantKinds []string
	}{
		{"by username", AuditFilter{Username: "alice"}, []string{AuditBridgeStart, AuditAuthOK}},
		{"by kind", AuditFilter{Kind: AuditAuthFail}, []string{AuditAuthFail, AuditAuthFail}},
		{"since cuts older", AuditFilter{Since: t0.Add(30 * time.Minute)}, []string{AuditAuthFail}},
		{"combined AND", AuditFilter{Username: "mallory", Since: t0.Add(30 * time.Minute)}, []string{AuditAuthFail}},
		{"limit", AuditFilter{Limit: 2}, []string{AuditAuthFail, AuditBridgeStart}},
		{"no match", AuditFilter{Username: "nobody"}, nil},
	}
	for _, c := range cases {
		got, err := st.ListAudit(ctx, c.f)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		kinds := make([]string, 0, len(got))
		for _, ev := range got {
			kinds = append(kinds, ev.Kind)
		}
		if len(kinds) != len(c.wantKinds) {
			t.Errorf("%s: kinds = %v, want %v", c.name, kinds, c.wantKinds)
			continue
		}
		for i := range kinds {
			if kinds[i] != c.wantKinds[i] {
				t.Errorf("%s: kinds = %v, want %v", c.name, kinds, c.wantKinds)
				break
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestListAuditFilters -v`
Expected: FAIL ("by username" returns all 4 kinds — filters not applied yet)

- [ ] **Step 3: Implement the filters**

Replace `ListAudit` in `internal/store/audit.go` (add `"strings"` to imports):

```go
// ListAudit returns matching events, newest first. Filters AND-combine.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEvent, error) {
	var where []string
	var args []any
	if f.Username != "" {
		where = append(where, "username = ?")
		args = append(args, f.Username)
	}
	if f.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, f.Kind)
	}
	if !f.Since.IsZero() {
		where = append(where, "at >= ?")
		args = append(args, f.Since.UTC().Format(time.RFC3339))
	}
	query := `SELECT id, at, session_id, kind, username, remote_addr, service, detail FROM audit`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var ev AuditEvent
		var at string
		if err := rows.Scan(&ev.ID, &at, &ev.SessionID, &ev.Kind,
			&ev.Username, &ev.RemoteAddr, &ev.Service, &ev.Detail); err != nil {
			return nil, err
		}
		if ev.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestRecordAndListAudit|TestListAuditFilters' -v`
Expected: PASS (both)

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS

```bash
git add internal/store/audit.go internal/store/audit_test.go
git commit -m "feat: ListAudit username/kind/since/limit filters"
```

---

### Task 3: store — `PruneAudit`

**Files:**
- Modify: `internal/store/audit.go`
- Modify: `internal/store/audit_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/audit_test.go`:

```go
func TestPruneAudit(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	for _, ev := range []AuditEvent{
		{At: t0.Add(-100 * 24 * time.Hour), SessionID: "old", Kind: AuditConnect},
		{At: t0.Add(-91 * 24 * time.Hour), SessionID: "old2", Kind: AuditDisconnect},
		{At: t0, SessionID: "new", Kind: AuditConnect},
	} {
		if err := st.RecordAudit(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	n, err := st.PruneAudit(ctx, t0.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatalf("PruneAudit: %v", err)
	}
	if n != 2 {
		t.Errorf("pruned = %d, want 2", n)
	}
	got, err := st.ListAudit(ctx, AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != "new" {
		t.Errorf("remaining = %+v, want only session 'new'", got)
	}

	// Pruning again is a no-op.
	n, err = st.PruneAudit(ctx, t0.Add(-90*24*time.Hour))
	if err != nil || n != 0 {
		t.Errorf("second prune = (%d, %v), want (0, nil)", n, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestPruneAudit -v`
Expected: FAIL (compile error: `PruneAudit` undefined)

- [ ] **Step 3: Implement**

Append to `internal/store/audit.go`:

```go
// PruneAudit deletes events strictly older than before; returns rows deleted.
func (s *Store) PruneAudit(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM audit WHERE at < ?", before.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestPruneAudit -v`
Expected: PASS

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS

```bash
git add internal/store/audit.go internal/store/audit_test.go
git commit -m "feat: PruneAudit deletes audit rows older than a cutoff"
```

---

### Task 4: server — `Auditor` seam, per-connection trail, connect/disconnect events

**Files:**
- Create: `internal/server/auditor.go`
- Create: `internal/server/auditor_test.go`
- Modify: `internal/server/session.go` (`Session` struct, `Run`)
- Modify: `internal/server/session_test.go` (add `recordingAuditor` fake + test)

- [ ] **Step 1: Write the failing tests**

Append to `internal/server/session_test.go` (the fakes section):

```go
// recordingAuditor captures every audit event for sequence assertions.
type recordingAuditor struct {
	events []store.AuditEvent
}

func (r *recordingAuditor) Record(_ context.Context, ev store.AuditEvent) {
	r.events = append(r.events, ev)
}

func (r *recordingAuditor) kinds() []string {
	out := make([]string, len(r.events))
	for i, ev := range r.events {
		out[i] = ev.Kind
	}
	return out
}
```

Append the test:

```go
func TestSessionAuditsConnectAndDisconnect(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{quit: true}}, // user quits at login
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	kinds := rec.kinds()
	if len(kinds) != 2 || kinds[0] != store.AuditConnect || kinds[1] != store.AuditDisconnect {
		t.Fatalf("kinds = %v, want [connect disconnect]", kinds)
	}
	for _, ev := range rec.events {
		if len(ev.SessionID) != 16 {
			t.Errorf("%s: session id %q, want 16 hex chars", ev.Kind, ev.SessionID)
		}
		if ev.RemoteAddr == "" {
			t.Errorf("%s: empty remote addr", ev.Kind)
		}
	}
	if rec.events[0].SessionID != rec.events[1].SessionID {
		t.Error("session ids differ within one connection")
	}
}
```

Create `internal/server/auditor_test.go` (the real impl writes to the store and stamps the time):

```go
package server

import (
	"context"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

func TestStoreAuditorRecordsAndStampsTime(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := storeAuditor{store: st}

	a.Record(context.Background(), store.AuditEvent{
		Kind: store.AuditConnect, SessionID: "abcd", RemoteAddr: "10.0.0.5:40000"})

	evs, err := st.ListAudit(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != store.AuditConnect || evs[0].At.IsZero() {
		t.Errorf("events = %+v, want one connect with a stamped time", evs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestSessionAuditsConnectAndDisconnect|TestStoreAuditor' -v`
Expected: FAIL (compile error: `Auditor`, `storeAuditor` undefined; `s.Auditor` unknown field)

- [ ] **Step 3: Implement**

Create `internal/server/auditor.go`:

```go
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

// Auditor records audit events. Recording is best-effort by contract: Record
// returns no error and implementations must never block or fail the session.
type Auditor interface {
	Record(ctx context.Context, ev store.AuditEvent)
}

// storeAuditor writes audit events to the store, stamping the time. Failures
// are logged and swallowed (best-effort — a DB hiccup must not kick users off).
type storeAuditor struct {
	store *store.Store
}

func (a storeAuditor) Record(ctx context.Context, ev store.AuditEvent) {
	ev.At = time.Now().UTC()
	if err := a.store.RecordAudit(ctx, ev); err != nil {
		log.Printf("audit: recording %s failed: %v", ev.Kind, err)
	}
}

// auditTrail prefills one connection's identity (session id + remote addr)
// into every event, so call sites only supply Kind/Username/Service/Detail.
type auditTrail struct {
	auditor    Auditor
	sessionID  string
	remoteAddr string
}

// newAuditTrail builds the connection's trail. A nil Session.Auditor yields a
// trail whose record() is a no-op (mirrors the nil-AdminPresenter pattern).
func (s *Session) newAuditTrail(conn net.Conn) *auditTrail {
	addr := ""
	if ra := conn.RemoteAddr(); ra != nil {
		addr = ra.String()
	}
	return &auditTrail{auditor: s.Auditor, sessionID: newSessionID(), remoteAddr: addr}
}

func (a *auditTrail) record(ctx context.Context, ev store.AuditEvent) {
	if a.auditor == nil {
		return
	}
	ev.SessionID = a.sessionID
	ev.RemoteAddr = a.remoteAddr
	a.auditor.Record(ctx, ev)
}

// newSessionID returns 8 random bytes hex-encoded (16 chars).
func newSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown" // crypto/rand does not fail in practice
	}
	return hex.EncodeToString(b)
}
```

In `internal/server/session.go`, add the field to `Session` (after `AdminPresenter`):

```go
	// Auditor records the session's audit trail; nil disables auditing.
	Auditor Auditor
```

In `Run`, replace the opening (everything before the `for {` outer loop):

```go
func (s *Session) Run(conn net.Conn) {
	ctx := context.Background()
	aud := s.newAuditTrail(conn)

	aud.record(ctx, store.AuditEvent{Kind: store.AuditConnect})
	endDetail := "client disconnected"
	currentUser := ""
	defer func() {
		aud.record(ctx, store.AuditEvent{
			Kind: store.AuditDisconnect, Username: currentUser, Detail: endDetail})
	}()

	term, err := s.Presenter.Negotiate(conn)
	if err != nil {
		log.Printf("telnet negotiation failed: %v", err)
		endDetail = "negotiation failed"
		return
	}
```

And set `currentUser`/`endDetail` on the remaining paths inside `Run`:
- after `identity, ok := s.doLogin(...)`: the `!ok` branch sets `endDetail = "quit at login"` before `return`; the success path sets `currentUser = identity.Username`.
- the `break menu` logoff path (`if quit {`) adds `currentUser = ""` before the `break menu` (logged off — the disconnect happens at the login screen).
- the `Menu` error return (`if err != nil { return }`) sets `endDetail = "menu render error"` before `return`.
- the admin-flow error return (after `log.Printf("admin flow for %s ended: ...")`) sets `endDetail = "admin flow error"` before `return`.
- the `case bridge.CauseClientClosed:` return sets `endDetail = "client closed during bridge"` before `return`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -v`
Expected: PASS — including all pre-existing session tests (they leave `Auditor` nil, exercising the no-op path)

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS

```bash
git add internal/server/auditor.go internal/server/auditor_test.go internal/server/session.go internal/server/session_test.go
git commit -m "feat: Auditor seam; session records connect/disconnect"
```

---

### Task 5: server — auth events + credential-leak test

**Files:**
- Modify: `internal/server/session.go` (`doLogin`)
- Modify: `internal/server/session_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/server/session_test.go` (add `"slices"` and `"strings"` to its imports):

```go
func TestSessionAuditsAuthEvents(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "sw0rdf1sh-wrong"},
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []string{store.AuditConnect, store.AuditAuthFail, store.AuditAuthOK, store.AuditDisconnect}
	if !slices.Equal(rec.kinds(), want) {
		t.Fatalf("kinds = %v, want %v", rec.kinds(), want)
	}
	fail := rec.events[1]
	if fail.Username != "alice" {
		t.Errorf("auth_fail username = %q, want the attempted username", fail.Username)
	}
	// The password must not appear in ANY field of ANY event.
	for _, ev := range rec.events {
		for _, field := range []string{ev.SessionID, ev.Kind, ev.Username, ev.RemoteAddr, ev.Service, ev.Detail} {
			if strings.Contains(field, "sw0rdf1sh-wrong") || strings.Contains(field, "good") {
				t.Errorf("credential leaked into audit event %+v", ev)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestSessionAuditsAuthEvents -v`
Expected: FAIL — kinds are `[connect disconnect]`, missing the auth events

- [ ] **Step 3: Implement**

In `internal/server/session.go`, thread the trail into `doLogin`. Change the call in `Run`:

```go
		identity, ok := s.doLogin(ctx, conn, term, aud)
```

Change `doLogin` to record both outcomes:

```go
// doLogin loops the login screen until success, or returns ok=false if the
// user quits.
func (s *Session) doLogin(ctx context.Context, conn net.Conn, term Term, aud *auditTrail) (auth.Identity, bool) {
	errMsg := ""
	for {
		user, pass, quit, err := s.Presenter.Login(conn, term, errMsg)
		if err != nil || quit {
			return auth.Identity{}, false
		}
		identity, err := s.Authenticate(ctx, s.Store, user, pass)
		if err == nil {
			aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthOK, Username: identity.Username})
			return identity, true
		}
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			return auth.Identity{}, false
		}
		// Attempted username only — never the password (CLAUDE.md hard rule).
		aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthFail, Username: user})
		// Generic message — never reveals whether the username exists (spec §7).
		errMsg = "Invalid userid or password"
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -v`
Expected: PASS

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS

```bash
git add internal/server/session.go internal/server/session_test.go
git commit -m "feat: audit auth_ok/auth_fail; test no credential leakage"
```

---

### Task 6: server — bridge events with cause detail

**Files:**
- Modify: `internal/server/session.go` (bridge call site; add `causeDetail`)
- Modify: `internal/server/session_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/server/session_test.go`:

```go
func TestSessionAuditsBridgeLifecycle(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}},
			{quit: true},
		},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseUserEscaped}}
	s := newTestSession(t, p, b)
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []string{store.AuditConnect, store.AuditAuthOK,
		store.AuditBridgeStart, store.AuditBridgeEnd, store.AuditDisconnect}
	if !slices.Equal(rec.kinds(), want) {
		t.Fatalf("kinds = %v, want %v", rec.kinds(), want)
	}
	start, end := rec.events[2], rec.events[3]
	if start.Service != "PROD" || start.Username != "alice" {
		t.Errorf("bridge_start = %+v, want service PROD by alice", start)
	}
	if end.Service != "PROD" || end.Detail != "user_escaped" {
		t.Errorf("bridge_end = %+v, want service PROD detail user_escaped", end)
	}
}

func TestCauseDetail(t *testing.T) {
	cases := []struct {
		c    bridge.Cause
		err  error
		want string
	}{
		{bridge.CauseBackendClosed, nil, "backend_closed"},
		{bridge.CauseClientClosed, nil, "client_closed"},
		{bridge.CauseUserEscaped, nil, "user_escaped"},
		{bridge.CauseError, errors.New("connection refused"), "error: connection refused"},
		{bridge.CauseError, nil, "error"},
	}
	for _, c := range cases {
		if got := causeDetail(c.c, c.err); got != c.want {
			t.Errorf("causeDetail(%v, %v) = %q, want %q", c.c, c.err, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestSessionAuditsBridgeLifecycle|TestCauseDetail' -v`
Expected: FAIL (compile error: `causeDetail` undefined)

- [ ] **Step 3: Implement**

In `internal/server/session.go`, wrap the bridge call (currently `cause, berr := s.Bridger.Bridge(...)`):

```go
			addr := net.JoinHostPort(selected.Host, strconv.Itoa(selected.Port))
			btls := BackendTLS{Enabled: selected.TLS, Verify: selected.TLSVerify}
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditBridgeStart, Username: identity.Username, Service: selected.Name})
			cause, berr := s.Bridger.Bridge(conn, addr, term.Type, s.EscapeAID, btls)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditBridgeEnd, Username: identity.Username,
				Service: selected.Name, Detail: causeDetail(cause, berr)})
```

Append to `internal/server/session.go`:

```go
// causeDetail renders a bridge outcome for the audit trail.
func causeDetail(c bridge.Cause, err error) string {
	switch c {
	case bridge.CauseBackendClosed:
		return "backend_closed"
	case bridge.CauseClientClosed:
		return "client_closed"
	case bridge.CauseUserEscaped:
		return "user_escaped"
	case bridge.CauseError:
		if err != nil {
			return "error: " + err.Error()
		}
		return "error"
	}
	return "unknown"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -v`
Expected: PASS

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS

```bash
git add internal/server/session.go internal/server/session_test.go
git commit -m "feat: audit bridge_start/bridge_end with cause detail"
```

---

### Task 7: server — adminFlow audit hook + all mutation call sites

**Files:**
- Modify: `internal/server/admin.go` (`adminFlow` struct + `recordAdmin` helper)
- Modify: `internal/server/session.go` (adminFlow construction, ~line 97)
- Modify: `internal/server/admin_users.go`, `internal/server/admin_groups.go`, `internal/server/admin_services.go`
- Modify: `internal/server/admin_test.go`, `internal/server/session_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/server/admin_test.go` (uses the fixture's identity "root"):

```go
func TestAdminAuditUserCreate(t *testing.T) {
	p := &fakeAdminPresenter{forms: []AdminFormAction{{Values: map[string]string{
		screens.FieldUsername: "newbie",
		screens.FieldPassword: "pw",
		screens.FieldRetype:   "pw",
	}}}}
	f, _ := newAdminFixture(t, p)
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }

	if err := f.userAdd(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != store.AuditAdmin ||
		got[0].Detail != "user create newbie" || got[0].Username != "root" {
		t.Errorf("audit = %+v, want one admin 'user create newbie' by root", got)
	}
}

func TestAdminAuditValidationFailureRecordsNothing(t *testing.T) {
	// A rejected form (duplicate user) must not produce an audit event.
	p := &fakeAdminPresenter{forms: []AdminFormAction{
		{Values: map[string]string{
			screens.FieldUsername: "alice", // already exists in the fixture
			screens.FieldPassword: "pw",
			screens.FieldRetype:   "pw",
		}},
		{Cancel: true},
	}}
	f, _ := newAdminFixture(t, p)
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }

	if err := f.userAdd(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("audit = %+v, want no events for a rejected create", got)
	}
}
```

Append to `internal/server/session_test.go` (session threads its trail into the flow):

```go
func TestSessionThreadsAuditIntoAdminFlow(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "root", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{admin: true}, {quit: true}},
	}
	ap := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}}, // groups list: PF4 add, then back
		forms: []AdminFormAction{{Values: map[string]string{screens.FieldName: "newgrp"}}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.AdminPresenter = ap
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	var admins []store.AuditEvent
	for _, ev := range rec.events {
		if ev.Kind == store.AuditAdmin {
			admins = append(admins, ev)
		}
	}
	if len(admins) != 1 || admins[0].Detail != "group create newgrp" ||
		admins[0].Username != "root" || admins[0].SessionID == "" {
		t.Errorf("admin events = %+v, want one 'group create newgrp' by root with a session id", admins)
	}
}
```

(`screens.FieldName` (`"name"`, `internal/screens/admin.go:14`) is the group-name form field read by `adminFlow.groupAdd`. Add `"github.com/coffeemuse/tn3270proxy/internal/screens"` to session_test.go's imports. Step trace: admin menu choice 2 → groups list `{PF:4}` → groupAdd form creates `newgrp` and returns → groups list re-renders `{PF:3}` → admin menu `{back}` → service menu `{quit}` → login `{quit}`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestAdminAudit|TestSessionThreadsAuditIntoAdminFlow' -v`
Expected: FAIL (compile error: `f.audit` undefined)

- [ ] **Step 3: Implement the hook**

In `internal/server/admin.go`, add a field to `adminFlow`:

```go
type adminFlow struct {
	store     AdminStore
	presenter AdminPresenter
	identity  auth.Identity
	term      Term // negotiated client terminal; drives page size + screen rendering
	// audit records admin CRUD events; nil (direct tests) disables auditing.
	audit func(ctx context.Context, ev store.AuditEvent)
}
```

Append to `internal/server/admin.go`:

```go
// recordAdmin emits one admin audit event ("who changed what"). Call it only
// after the store mutation has succeeded, so the trail reflects reality.
func (f *adminFlow) recordAdmin(ctx context.Context, detail string) {
	if f.audit == nil {
		return
	}
	f.audit(ctx, store.AuditEvent{
		Kind: store.AuditAdmin, Username: f.identity.Username, Detail: detail})
}
```

In `internal/server/session.go`, pass the trail when constructing the flow:

```go
				flow := &adminFlow{store: s.Store, presenter: s.AdminPresenter,
					identity: identity, term: term, audit: aud.record}
```

- [ ] **Step 4: Wire every mutation call site**

Insert `f.recordAdmin(ctx, ...)` in the success path of each mutation. The files follow two patterns: (a) `if err := f.store.X(...); err != nil { msg = logStoreErr(...) }` — add an `else { f.recordAdmin(...) }` branch or place the call after the error return; (b) `deleteUser`-style helpers that `return ""` on success — place the call just before the success return.

| File | Mutation (anchor) | Detail string |
|---|---|---|
| `admin_users.go` `deleteUser` | after `DeleteUser` succeeds, before `return ""` | `"user delete "+u.Username` |
| `admin_users.go` `userAdd` | after `CreateUser` succeeds, before `return nil` | `"user create "+username` |
| `admin_users.go` `setPassword` | after `SetPassword` succeeds, before `return nil` | `"user set-password "+u.Username` |
| `admin_users.go` `userGroups` case `'A'` | `else` branch of `AddUserToGroup` | `"user "+u.Username+" add-group "+g.Name` |
| `admin_users.go` `userGroups` case `'R'` | `else` branch of `RemoveUserFromGroup` | `"user "+u.Username+" remove-group "+g.Name` |
| `admin_groups.go` (delete, ~line 62) | `else` branch of `DeleteGroup` | `"group delete "+target.Name` |
| `admin_groups.go` (~line 169) | `else` branch of `AddUserToGroup` | `"group "+g.Name+" add-member "+u.Username` |
| `admin_groups.go` (~line 179) | `else` branch of `RemoveUserFromGroup` | `"group "+g.Name+" remove-member "+u.Username` |
| `admin_groups.go` (create, ~line 221) | after `CreateGroup` succeeds | `"group create "+name` |
| `admin_services.go` (delete, ~line 71) | `else` branch of `DeleteService` | `"service delete "+target.Name` |
| `admin_services.go` (create, ~line 170) | after `CreateService` succeeds | `"service create "+name` |
| `admin_services.go` (update, ~line 174) | after `UpdateService` succeeds | `"service update "+name` |
| `admin_services.go` (~line 259) | `else` branch of `LinkGroupService` | `"service "+svc.Name+" grant "+g.Name` |
| `admin_services.go` (~line 263) | `else` branch of `UnlinkGroupService` | `"service "+svc.Name+" revoke "+g.Name` |

Note: setPassword's detail names the user but **never** the password — keep it exactly as in the table. Where guardrails block a mutation (self-delete, last ZZADMIN member) no event is recorded, because nothing changed.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/ -v`
Expected: PASS — including all pre-existing admin tests (fixture leaves `f.audit` nil)

- [ ] **Step 6: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS

```bash
git add internal/server/admin.go internal/server/admin_users.go internal/server/admin_groups.go internal/server/admin_services.go internal/server/session.go internal/server/admin_test.go internal/server/session_test.go
git commit -m "feat: audit admin CRUD mutations through adminFlow hook"
```

---

### Task 8: server — wire storeAuditor into the real session handler

**Files:**
- Modify: `internal/server/server.go` (`sessionHandler.Handle`, ~line 53)

- [ ] **Step 1: Wire it**

In `internal/server/server.go`, `sessionHandler.Handle`, add the field:

```go
	s := &Session{
		Store:          h.store,
		Authenticate:   auth.Authenticate,
		Presenter:      go3270Presenter{},
		Bridger:        realBridger{},
		EscapeAID:      h.escapeAID,
		AdminPresenter: go3270Presenter{},
		Auditor:        storeAuditor{store: h.store},
	}
```

(No new test: `Handle` is the existing thin untested wiring layer — `storeAuditor` itself is covered by `auditor_test.go`, the session events by `session_test.go`, and the live path by the smoke check in Task 11.)

- [ ] **Step 2: Build and run the full suite**

Run: `go build ./... && go test ./... -race`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/server/server.go
git commit -m "feat: real sessions audit to the store"
```

---

### Task 9: cmd — `parseDuration` helper (`90d` support)

**Files:**
- Create: `cmd/tn3270proxy/audit.go`
- Create: `cmd/tn3270proxy/audit_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/tn3270proxy/audit_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"90d", 90 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"0d", 0, false},
		{"", 0, true},
		{"d", 0, true},
		{"-5d", 0, true},
		{"1d12h", 0, true}, // mixed day form not supported
		{"ninety", 0, true},
	}
	for _, c := range cases {
		got, err := parseDuration(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseDuration(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("parseDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tn3270proxy/ -run TestParseDuration -v`
Expected: FAIL (compile error: `parseDuration` undefined)

- [ ] **Step 3: Implement**

Create `cmd/tn3270proxy/audit.go`:

```go
package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseDuration is time.ParseDuration plus a "d" suffix (days, 24h each),
// e.g. "90d" or "36h". Mixed forms like "1d12h" are not supported.
// time.ParseDuration stops at hours, and "2160h" is operator-hostile.
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid duration %q (want e.g. 90d or 24h)", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q (want e.g. 90d or 24h)", s)
	}
	return d, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tn3270proxy/ -run TestParseDuration -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/tn3270proxy/audit.go cmd/tn3270proxy/audit_test.go
git commit -m "feat: parseDuration with day suffix for audit CLI"
```

---

### Task 10: cmd — `audit list` verb + dispatch

**Files:**
- Modify: `cmd/tn3270proxy/audit.go`
- Modify: `cmd/tn3270proxy/audit_test.go`
- Modify: `cmd/tn3270proxy/main.go` (`run`, lines 25–33)

- [ ] **Step 1: Write the failing tests**

Append to `cmd/tn3270proxy/audit_test.go`:

```go
func TestRunAuditDispatch(t *testing.T) {
	if err := runAudit(nil); err == nil {
		t.Error("no verb: want usage error")
	}
	if err := runAudit([]string{"bogus"}); err == nil {
		t.Error("unknown verb: want error")
	}
}

func TestRunAuditListBadSince(t *testing.T) {
	if err := runAuditList([]string{"-since", "ninety"}); err == nil {
		t.Error("invalid -since: want error")
	}
}

func TestPrintAuditEvents(t *testing.T) {
	at := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	events := []store.AuditEvent{
		{At: at, SessionID: "deadbeef00000000", Kind: store.AuditAuthOK,
			Username: "alice", RemoteAddr: "10.0.0.5:40000"},
		{At: at, SessionID: "deadbeef00000000", Kind: store.AuditBridgeEnd,
			Username: "alice", RemoteAddr: "10.0.0.5:40000", Service: "PROD", Detail: "user_escaped"},
	}
	var buf bytes.Buffer
	printAuditEvents(&buf, events)
	out := buf.String()
	for _, want := range []string{"2026-06-03T10:00:00Z", "auth_ok", "deadbeef00000000",
		"alice", "10.0.0.5:40000", "PROD", "user_escaped"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "\n"); n != 2 {
		t.Errorf("output lines = %d, want 2:\n%s", n, out)
	}
}
```

Add to audit_test.go imports: `"bytes"`, `"strings"`, `"github.com/coffeemuse/tn3270proxy/internal/store"`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/tn3270proxy/ -run 'TestRunAudit|TestPrintAuditEvents' -v`
Expected: FAIL (compile error: `runAudit`, `runAuditList`, `printAuditEvents` undefined)

- [ ] **Step 3: Implement**

Append to `cmd/tn3270proxy/audit.go` (extend imports with `"context"`, `"flag"`, `"io"`, `"os"`, and the `store` package):

```go
// runAudit dispatches the audit verbs.
func runAudit(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("audit: usage: audit list|prune [flags]")
	}
	switch args[0] {
	case "list":
		return runAuditList(args[1:])
	case "prune":
		return runAuditPrune(args[1:])
	default:
		return fmt.Errorf("audit: unknown subcommand %q (want list or prune)", args[0])
	}
}

func runAuditList(args []string) error {
	fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	user := fs.String("user", "", "filter by username")
	kind := fs.String("kind", "", "filter by event kind (e.g. auth_fail)")
	since := fs.String("since", "", "only events newer than this age (e.g. 24h, 7d)")
	limit := fs.Int("limit", 100, "maximum rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	f := store.AuditFilter{Username: *user, Kind: *kind, Limit: *limit}
	if *since != "" {
		d, err := parseDuration(*since)
		if err != nil {
			return fmt.Errorf("audit list: -since: %w", err)
		}
		f.Since = time.Now().Add(-d)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	events, err := st.ListAudit(context.Background(), f)
	if err != nil {
		return err
	}
	printAuditEvents(os.Stdout, events)
	return nil
}

// printAuditEvents writes one event per line: time, kind, session, user,
// remote, service, detail.
func printAuditEvents(w io.Writer, events []store.AuditEvent) {
	for _, ev := range events {
		fmt.Fprintf(w, "%s  %-12s %s  %-12s %-21s %-12s %s\n",
			ev.At.Format(time.RFC3339), ev.Kind, ev.SessionID,
			ev.Username, ev.RemoteAddr, ev.Service, ev.Detail)
	}
}
```

(`runAuditPrune` doesn't exist yet — add a temporary stub so Task 10 compiles, replaced in Task 11:)

```go
func runAuditPrune(args []string) error {
	return fmt.Errorf("audit prune: not implemented")
}
```

In `cmd/tn3270proxy/main.go` `run()`, add the dispatch before the `serve` fallthrough:

```go
	if len(args) > 0 && args[0] == "audit" {
		return runAudit(args[1:])
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/tn3270proxy/ -v`
Expected: PASS

Note: `TestRunAuditListBadSince` must fail on the bad duration **before** touching the DB (the flag check precedes `store.Open`), so no DB file is created.

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS

```bash
git add cmd/tn3270proxy/audit.go cmd/tn3270proxy/audit_test.go cmd/tn3270proxy/main.go
git commit -m "feat: tn3270proxy audit list subcommand"
```

---

### Task 11: cmd — `audit prune` verb

**Files:**
- Modify: `cmd/tn3270proxy/audit.go` (replace the stub)
- Modify: `cmd/tn3270proxy/audit_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/tn3270proxy/audit_test.go` (add imports `"context"`, `"path/filepath"`):

```go
func TestRunAuditPruneRequiresOlderThan(t *testing.T) {
	db := filepath.Join(t.TempDir(), "p.db")
	if err := runAuditPrune([]string{"-db", db}); err == nil {
		t.Error("missing -older-than: want error (no default that silently deletes)")
	}
}

func TestRunAuditPruneDeletesOldRows(t *testing.T) {
	db := filepath.Join(t.TempDir(), "p.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	old := store.AuditEvent{At: time.Now().Add(-100 * 24 * time.Hour), SessionID: "old", Kind: store.AuditConnect}
	fresh := store.AuditEvent{At: time.Now(), SessionID: "new", Kind: store.AuditConnect}
	if err := st.RecordAudit(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordAudit(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if err := runAuditPrune([]string{"-db", db, "-older-than", "90d"}); err != nil {
		t.Fatalf("runAuditPrune: %v", err)
	}

	st2, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	got, err := st2.ListAudit(ctx, store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != "new" {
		t.Errorf("remaining = %+v, want only session 'new'", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/tn3270proxy/ -run TestRunAuditPrune -v`
Expected: FAIL (`TestRunAuditPruneDeletesOldRows` hits the "not implemented" stub)

- [ ] **Step 3: Implement**

Replace the `runAuditPrune` stub in `cmd/tn3270proxy/audit.go`:

```go
func runAuditPrune(args []string) error {
	fs := flag.NewFlagSet("audit prune", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	olderThan := fs.String("older-than", "", "delete events older than this age (e.g. 90d; required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *olderThan == "" {
		return fmt.Errorf("audit prune: -older-than is required")
	}
	d, err := parseDuration(*olderThan)
	if err != nil {
		return fmt.Errorf("audit prune: -older-than: %w", err)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := st.PruneAudit(context.Background(), time.Now().Add(-d))
	if err != nil {
		return err
	}
	fmt.Printf("pruned %d audit rows\n", n)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/tn3270proxy/ -v`
Expected: PASS

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS

```bash
git add cmd/tn3270proxy/audit.go cmd/tn3270proxy/audit_test.go
git commit -m "feat: tn3270proxy audit prune subcommand"
```

---

### Task 12: docs + final verification

**Files:**
- Modify: `docs/superpowers/ROADMAP.md` (item 5 + the "Next up" line at the top)
- Modify: `CLAUDE.md` (package map + commands)

- [ ] **Step 1: Final full verification**

```bash
go build ./... && go test ./... -race && go vet ./...
```
Expected: clean build, all tests PASS, no vet findings.

- [ ] **Step 2: Optional live sanity check (recommended, not gating)**

Audit logging is not protocol-surface, so no emulator gate — but a quick eyeball is cheap:

```bash
go build -o bin/tn3270proxy ./cmd/tn3270proxy
./bin/tn3270proxy seed -db /tmp/audit-smoke.db -file seed.example.json
./bin/tn3270proxy serve -db /tmp/audit-smoke.db -listen :2323 &
# in another terminal: c3270 127.0.0.1:2323 — log in, pick a service or fail a login, disconnect
./bin/tn3270proxy audit list -db /tmp/audit-smoke.db
kill %1
```
Expected: connect / auth / bridge / disconnect rows with one shared session id.

- [ ] **Step 3: Update ROADMAP.md**

Mark item 5 done in the established style (see items 1 and 7 for the pattern): retitle to
`## 5. Audit logging  ✅ **DONE** *(merged)*`, add a `> **Completed.**` block naming the spec
(`docs/superpowers/specs/2026-06-03-audit-logging-design.md`), this plan, and what was
delivered (audit table + Auditor seam + auditTrail, admin CRUD events, `audit list`/`prune`
CLI). Update the `**Next up:**` line at the top of the file to the next open item (#4 TN3270E,
per the saved ordering 5 → 4 → 6).

- [ ] **Step 4: Update CLAUDE.md**

- In the package map: extend the `internal/store` line with the audit table/methods; extend
  `internal/server` with the `Auditor` seam; extend `cmd/tn3270proxy` with the `audit` subcommand.
- In Commands: add `./bin/tn3270proxy audit list -db proxy.db` and
  `./bin/tn3270proxy audit prune -db proxy.db -older-than 90d`.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/ROADMAP.md CLAUDE.md
git commit -m "docs: roadmap #5 audit logging done; document audit table/seam/CLI"
```
