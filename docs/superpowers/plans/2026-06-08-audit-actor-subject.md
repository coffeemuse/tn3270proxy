# Audit actor/subject separation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a first-class, queryable `actor` (who performed the action) column to the audit trail, distinct from `username` (the subject the row is about), so security-sensitive admin events attribute the acting admin (GH #73).

**Architecture:** A new migration step adds and backfills the `audit.actor` column. `AuditEvent`/`RecordAudit`/`ListAudit`/`AuditFilter` gain `actor`. The per-connection `auditTrail` auto-fills `actor` from the authenticated session principal (set at `auth_ok`, cleared on return to login), so the four buggy admin sites become correct for free and generic CRUD drops the admin from `username`. CLI and the admin audit screen surface the new field.

**Tech Stack:** Go, `modernc.org/sqlite` (pure-Go), go3270, the existing append-only migration ledger.

**Spec:** `docs/superpowers/specs/2026-06-08-audit-actor-subject-design.md`

**Conventions:** TDD throughout. Run `go test ./... -race` (the server/bridge are concurrent). Copyright header: every new `.go` file must start with the GPLv3 header block used across the repo (copy it verbatim from any existing file in the same package). All commit messages must end with the trailer:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```

---

## File structure

| File | Change | Responsibility |
|------|--------|----------------|
| `internal/store/migrate.go` | Modify | Append migration v2 (`migrateV2AuditActor`) + ledger entry |
| `internal/store/migrate_test.go` | Modify | `legacyAuditDB` helper + backfill/column/index test |
| `internal/store/audit.go` | Modify | `AuditEvent.Actor`, `AuditFilter.Actor`, INSERT/SELECT |
| `internal/store/audit_test.go` | Modify | Actor round-trip + actor-lens filter test |
| `internal/server/auditor.go` | Modify | `auditTrail.actor` field + `setActor` + record auto-fill |
| `internal/server/auditor_test.go` | Modify | Auto-fill / override / clear unit test |
| `internal/server/session.go` | Modify | Set actor at `auth_ok`, clear at `doLogin` entry |
| `internal/server/session_test.go` | Modify | Full-session actor attribution test |
| `internal/server/admin.go` | Modify | `recordAdmin` drops `Username` (Option A) |
| `internal/server/admin_test.go` | Modify | Admin-on-other + generic-CRUD attribution test |
| `cmd/tn3270proxy/audit.go` | Modify | `-actor` flag + actor column |
| `cmd/tn3270proxy/audit_test.go` | Modify | `-actor` filter + column render test |
| `internal/server/admin_audit.go` | Modify | Actor on the detail view |
| `internal/server/admin_audit_test.go` | Modify | Detail-view actor field test |

---

## Task 1: Migration v2 — add and backfill `audit.actor`

**Files:**
- Modify: `internal/store/migrate.go` (ledger at `:106`, new fn after `migrateV1Baseline` ~`:136`)
- Test: `internal/store/migrate_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/store/migrate_test.go`. The helper builds a `user_version=0` DB whose `audit` table predates the `actor` column (v1 baseline's `CREATE TABLE IF NOT EXISTS audit` then skips it, leaving it actor-less), with one row to prove backfill:

```go
// legacyAuditDB creates a user_version=0 DB whose audit table predates the actor
// column (GH #73), with one row, mimicking a real pre-v2 DB that needs migrating.
func legacyAuditDB(t *testing.T, path string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`CREATE TABLE audit (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		at          TEXT NOT NULL,
		session_id  TEXT NOT NULL,
		kind        TEXT NOT NULL,
		username    TEXT NOT NULL DEFAULT '',
		remote_addr TEXT NOT NULL DEFAULT '',
		service     TEXT NOT NULL DEFAULT '',
		detail      TEXT NOT NULL DEFAULT ''
	);`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`INSERT INTO audit (at, session_id, kind, username)
		 VALUES ('2026-06-01T00:00:00Z','s0','mfa_cleared','BOB')`); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationAddsAndBackfillsAuditActor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-audit.db")
	legacyAuditDB(t, path)

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if _, ok := columnSet(t, st.db, "audit")["actor"]; !ok {
		t.Fatal("audit.actor column missing after migrate")
	}
	var actor string
	if err := st.db.QueryRow(
		"SELECT actor FROM audit WHERE username='BOB'").Scan(&actor); err != nil {
		t.Fatalf("query actor: %v", err)
	}
	if actor != "BOB" {
		t.Errorf("backfilled actor = %q, want BOB", actor)
	}
	var n int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='audit_actor'").Scan(&n); err != nil {
		t.Fatalf("query index: %v", err)
	}
	if n != 1 {
		t.Errorf("audit_actor index count = %d, want 1", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestMigrationAddsAndBackfillsAuditActor -v`
Expected: FAIL — `audit.actor column missing after migrate` (v2 doesn't exist yet).

- [ ] **Step 3: Add the migration function and ledger entry**

In `internal/store/migrate.go`, append the entry to the `migrations` slice (`:106`):

```go
var migrations = []migration{
	{1, "baseline schema", migrateV1Baseline},
	{2, "audit actor column", migrateV2AuditActor},
}
```

Add the function immediately after `migrateV1Baseline` (after `:136`):

```go
// migrateV2AuditActor adds the audit.actor column (actor vs subject split, GH #73):
// actor = who performed the action, distinct from username = the subject. Existing
// rows are backfilled actor=username (best available identity for historical rows;
// the true admin behind a pre-v2 admin-on-other row is unrecoverable). Indexed for
// the actor-centric query lens. Runs once during the v1->v2 upgrade.
func migrateV2AuditActor(ctx context.Context, tx *sql.Tx) error {
	if err := ensureColumnTx(ctx, tx, "audit", "actor",
		"ALTER TABLE audit ADD COLUMN actor TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE audit SET actor = username"); err != nil {
		return fmt.Errorf("backfill audit.actor: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"CREATE INDEX IF NOT EXISTS audit_actor ON audit(actor)"); err != nil {
		return fmt.Errorf("create audit_actor index: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestMigration|TestFreshDB|TestLegacyConverges|TestSecondOpen' -v`
Expected: PASS — the new test passes, and the existing convergence/fresh/no-op tests still pass (fresh DBs now stamp `user_version=2`).

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrate.go internal/store/migrate_test.go
git commit -m "feat(store): migration v2 adds and backfills audit.actor (#73)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Store — `Actor` field on AuditEvent / Record / List / Filter

**Files:**
- Modify: `internal/store/audit.go` (`:52` struct, `:65` Record, `:76` Filter, `:84` List)
- Test: `internal/store/audit_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/store/audit_test.go`:

```go
func TestAuditActorRoundTripAndFilter(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	events := []AuditEvent{
		{At: t0, SessionID: "s1", Kind: AuditMFACleared, Username: "BOB", Actor: "ADMIN"},
		{At: t0.Add(time.Second), SessionID: "s2", Kind: AuditAuthOK, Username: "ALICE", Actor: "ALICE"},
	}
	for _, ev := range events {
		if err := st.RecordAudit(ctx, ev); err != nil {
			t.Fatalf("RecordAudit(%s): %v", ev.Kind, err)
		}
	}
	// Subject lens still works AND actor round-trips.
	bySubject, err := st.ListAudit(ctx, AuditFilter{Username: "BOB"})
	if err != nil {
		t.Fatalf("ListAudit subject: %v", err)
	}
	if len(bySubject) != 1 || bySubject[0].Actor != "ADMIN" {
		t.Fatalf("subject lens BOB: got %+v, want one row actor=ADMIN", bySubject)
	}
	// Actor-centric lens.
	byActor, err := st.ListAudit(ctx, AuditFilter{Actor: "ADMIN"})
	if err != nil {
		t.Fatalf("ListAudit actor: %v", err)
	}
	if len(byActor) != 1 || byActor[0].Username != "BOB" {
		t.Fatalf("actor lens ADMIN: got %+v, want one row username=BOB", byActor)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestAuditActorRoundTripAndFilter -v`
Expected: FAIL — compile error: `unknown field Actor in struct literal`.

- [ ] **Step 3: Add the Actor field and wire it through SQL**

In `internal/store/audit.go`, add `Actor` to the struct (after `Username`, `:57`):

```go
type AuditEvent struct {
	ID         int64
	At         time.Time
	SessionID  string
	Kind       string
	Username   string // the subject — the account this row is about ("" if none)
	Actor      string // the authenticated principal who performed the action ("" pre-auth)
	RemoteAddr string
	Service    string
	Detail     string
}
```

Update `RecordAudit` (`:66-70`) to include `actor`:

```go
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit (at, session_id, kind, username, actor, remote_addr, service, detail)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.At.UTC().Format(time.RFC3339), ev.SessionID, ev.Kind,
		ev.Username, ev.Actor, ev.RemoteAddr, ev.Service, ev.Detail)
```

Add `Actor` to `AuditFilter` (`:76`):

```go
type AuditFilter struct {
	Username string // subject lens
	Actor    string // actor lens
	Kind     string
	Since    time.Time
	Limit    int
}
```

In `ListAudit`, add the actor predicate (after the `Username` block, `:90`):

```go
	if f.Actor != "" {
		where = append(where, "actor = ?")
		args = append(args, f.Actor)
	}
```

Update the SELECT column list (`:99`) and the Scan (`:119-120`):

```go
	query := `SELECT id, at, session_id, kind, username, actor, remote_addr, service, detail FROM audit`
```
```go
		if err := rows.Scan(&ev.ID, &at, &ev.SessionID, &ev.Kind,
			&ev.Username, &ev.Actor, &ev.RemoteAddr, &ev.Service, &ev.Detail); err != nil {
			return nil, err
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestAudit|TestRecordAndListAudit' -v`
Expected: PASS — round-trip, actor lens, and the pre-existing audit tests all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/store/audit.go internal/store/audit_test.go
git commit -m "feat(store): add actor field to AuditEvent and AuditFilter (#73)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Server — `auditTrail` auto-fills actor

**Files:**
- Modify: `internal/server/auditor.go` (`:62` struct, `:78` record)
- Test: `internal/server/auditor_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/auditor_test.go` (the `recordingAuditor` test double lives in `session_test.go`, same package):

```go
func TestAuditTrailAutoFillsActor(t *testing.T) {
	rec := &recordingAuditor{}
	tr := &auditTrail{auditor: rec, sessionID: "sid", remoteAddr: "1.2.3.4:5"}
	ctx := context.Background()

	tr.record(ctx, store.AuditEvent{Kind: store.AuditConnect})                 // pre-auth: no actor
	tr.setActor("ALICE")                                                       // login success
	tr.record(ctx, store.AuditEvent{Kind: store.AuditAuthOK, Username: "ALICE"})
	tr.record(ctx, store.AuditEvent{Kind: store.AuditAdmin, Actor: "ADMIN"})   // explicit override kept
	tr.setActor("")                                                            // logout / back to login
	tr.record(ctx, store.AuditEvent{Kind: store.AuditDisconnect})

	if got := rec.events[0].Actor; got != "" {
		t.Errorf("connect actor = %q, want empty", got)
	}
	if got := rec.events[1].Actor; got != "ALICE" {
		t.Errorf("auth_ok actor = %q, want ALICE", got)
	}
	if got := rec.events[2].Actor; got != "ADMIN" {
		t.Errorf("admin actor = %q, want ADMIN (explicit, not clobbered)", got)
	}
	if got := rec.events[3].Actor; got != "" {
		t.Errorf("disconnect actor = %q, want empty after clear", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestAuditTrailAutoFillsActor -v`
Expected: FAIL — compile error: `tr.setActor undefined` and `actor` not a field.

- [ ] **Step 3: Add the actor field, setter, and auto-fill**

In `internal/server/auditor.go`, add the field to `auditTrail` (`:62`):

```go
type auditTrail struct {
	auditor    Auditor
	sessionID  string
	remoteAddr string
	actor      string // authenticated principal; stamped onto actor-less events
}
```

Add the setter (after `newAuditTrail`, `:76`):

```go
// setActor records the authenticated principal whose actions this connection's
// events should be attributed to; record() stamps it onto any event the caller
// left actor-less. Pass "" to clear it (logout / return to the login screen).
func (a *auditTrail) setActor(name string) { a.actor = name }
```

Update `record` (`:78`) to auto-fill (do not clobber an explicit actor):

```go
func (a *auditTrail) record(ctx context.Context, ev store.AuditEvent) {
	if a.auditor == nil {
		return
	}
	ev.SessionID = a.sessionID
	ev.RemoteAddr = a.remoteAddr
	if ev.Actor == "" {
		ev.Actor = a.actor
	}
	a.auditor.Record(ctx, ev)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestAuditTrailAutoFillsActor -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/auditor.go internal/server/auditor_test.go
git commit -m "feat(server): auditTrail auto-fills actor from session principal (#73)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Server — set actor at login, clear on return to login

**Files:**
- Modify: `internal/server/session.go` (`doLogin` entry `:677`, `auth_ok` block `:693-696`)
- Test: `internal/server/session_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/session_test.go` (modeled on `TestSessionAuditsAuthEvents` at `:673`). It drives connect → auth_fail → auth_ok → logout → disconnect and asserts the actor on each:

```go
func TestSessionAuditActorAttribution(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "sw0rdf1sh-wrong"},
			{user: "alice", pass: "good"},
			{quit: true}, // login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []string{store.AuditConnect, store.AuditAuthFail, store.AuditAuthOK, store.AuditLogout, store.AuditDisconnect}
	if !slices.Equal(rec.kinds(), want) {
		t.Fatalf("kinds = %v, want %v", rec.kinds(), want)
	}
	if a := rec.events[0].Actor; a != "" { // connect: pre-auth
		t.Errorf("connect actor = %q, want empty", a)
	}
	if a := rec.events[1].Actor; a != "" { // auth_fail: never authenticated
		t.Errorf("auth_fail actor = %q, want empty", a)
	}
	// authStub returns identity.Username verbatim ("alice"), so actor == "alice".
	if a := rec.events[2].Actor; a != "alice" { // auth_ok
		t.Errorf("auth_ok actor = %q, want alice", a)
	}
	if a := rec.events[3].Actor; a != "alice" { // logout while still attributed
		t.Errorf("logout actor = %q, want alice", a)
	}
	if a := rec.events[4].Actor; a != "" { // disconnect from the login screen post-logoff
		t.Errorf("disconnect actor = %q, want empty after logoff", a)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestSessionAuditActorAttribution -v`
Expected: FAIL — `auth_ok actor = "" want ALICE` (wiring not added yet).

- [ ] **Step 3: Wire actor set/clear into the session**

In `internal/server/session.go`, clear actor at the start of `doLogin` (after `:677`), so any return to the login screen (logoff, idle-logout) drops the prior principal before pre-auth events (`auth_fail`/`auth_error`) are recorded:

```go
func (s *Session) doLogin(ctx context.Context, conn net.Conn, term Term, aud *auditTrail) (auth.Identity, bool, string) {
	aud.setActor("") // returning to the login screen drops any prior principal
	errMsg := ""
```

Set actor on login success, in the `err == nil` block (`:693-696`), before recording `auth_ok` so that row (and the subsequent login-phase MFA rows) carry it:

```go
		if err == nil {
			s.Throttle.Reset(user)
			aud.setActor(identity.Username)
			aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthOK, Username: identity.Username})
			return identity, true, ""
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestSessionAudit' -v`
Expected: PASS — the new attribution test and the existing `TestSessionAuditsAuthEvents` / `TestSessionAuditsBridgeLifecycle` all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/session_test.go
git commit -m "feat(server): attribute audit rows to the logged-in actor (#73)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Server — generic admin CRUD drops admin from `username` (Option A)

**Files:**
- Modify: `internal/server/admin.go` (`recordAdmin`, `:162-168`)
- Test: `internal/server/admin_test.go`

**Context:** The four admin-on-other sites (`mfa_enforced`, `mfa_cleared`, `settings_locked`, `settings_unlocked` in `admin_users.go`) need **no code change** — they already set `Username: u.Username` (the subject), and Task 3/4 auto-fill `actor` with the admin. This task only fixes generic CRUD, which currently mis-stores the admin in `username`.

- [ ] **Step 1: Write the failing test**

Add to `internal/server/admin_test.go`. This verifies generic CRUD leaves `username` empty (actor carries the admin) and that an MFA-clear row puts the subject in `username` with the admin in `actor`. Build the `adminFlow` the same way the existing admin tests in this file do (reuse the established harness — find a nearby test such as the ones around `:1134`/`:1388` that capture `[]store.AuditEvent` and copy its setup, including how `f.audit` is wired to capture events with the actor pre-stamped):

```go
func TestAdminAuditActorSubjectSplit(t *testing.T) {
	var got []store.AuditEvent
	// auditFn simulates the auditTrail's auto-fill: the acting admin is the
	// session principal, stamped onto any actor-less event.
	const admin = "ADMIN"
	auditFn := func(_ context.Context, ev store.AuditEvent) {
		if ev.Actor == "" {
			ev.Actor = admin
		}
		got = append(got, ev)
	}

	// Generic CRUD: recordAdmin must NOT put the admin in Username.
	f := &adminFlow{identity: auth.Identity{Username: admin}, audit: auditFn}
	f.recordAdmin(context.Background(), "created service PROD")

	if len(got) != 1 {
		t.Fatalf("recordAdmin emitted %d events, want 1", len(got))
	}
	if got[0].Username != "" {
		t.Errorf("generic CRUD username = %q, want empty (subject lives in Detail)", got[0].Username)
	}
	if got[0].Actor != admin {
		t.Errorf("generic CRUD actor = %q, want %q", got[0].Actor, admin)
	}
	if got[0].Detail != "created service PROD" {
		t.Errorf("generic CRUD detail = %q, want the change description", got[0].Detail)
	}
}
```

If `auth.Identity` is not already imported in `admin_test.go`, add `"github.com/coffeemuse/tn3270proxy/internal/auth"` to its imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestAdminAuditActorSubjectSplit -v`
Expected: FAIL — `generic CRUD username = "ADMIN", want empty` (recordAdmin still sets Username).

- [ ] **Step 3: Drop Username from recordAdmin**

In `internal/server/admin.go`, update `recordAdmin` (`:162-168`):

```go
// recordAdmin emits one generic admin-CRUD audit event. The acting admin is
// auto-filled as the actor by auditTrail.record (GH #73); Username is left empty
// because a generic mutation's subject (a user, group, or service) is described
// in Detail, not necessarily a single user account. Call it only after the store
// mutation has succeeded, so the trail reflects reality.
func (f *adminFlow) recordAdmin(ctx context.Context, detail string) {
	if f.audit == nil {
		return
	}
	f.audit(ctx, store.AuditEvent{Kind: store.AuditAdmin, Detail: detail})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestAdmin' -v`
Expected: PASS — the new test passes. If any pre-existing admin test asserted that a generic-CRUD row's `Username` equals the admin, update that assertion to expect an empty `Username` and a populated `Actor` (this is the intended behavior change; note it in the commit body).

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin.go internal/server/admin_test.go
git commit -m "feat(server): generic admin CRUD stores subject in detail, actor in actor (#73)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: CLI — `-actor` filter and column

**Files:**
- Modify: `cmd/tn3270proxy/audit.go` (`runAuditList` `:72-82`, `printAuditEvents` `:105-115`)
- Test: `cmd/tn3270proxy/audit_test.go`

- [ ] **Step 1: Write the failing test**

Add to `cmd/tn3270proxy/audit_test.go` (reuse the existing test's setup style around `:85`):

```go
func TestPrintAuditEventsIncludesActor(t *testing.T) {
	var buf bytes.Buffer
	printAuditEvents(&buf, []store.AuditEvent{
		{At: time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC),
			Kind: store.AuditMFACleared, SessionID: "s1",
			Username: "BOB", Actor: "ADMIN", RemoteAddr: "10.0.0.5:40000"},
	})
	out := buf.String()
	if !strings.Contains(out, "ADMIN") {
		t.Errorf("printed output missing actor; got:\n%s", out)
	}
	if !strings.Contains(out, "BOB") {
		t.Errorf("printed output missing subject; got:\n%s", out)
	}
}
```

Ensure `cmd/tn3270proxy/audit_test.go` imports `bytes`, `strings`, `time`, and the `store` package (add any that are missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tn3270proxy/ -run TestPrintAuditEventsIncludesActor -v`
Expected: FAIL — output contains `BOB` but not `ADMIN` (actor not printed yet).

- [ ] **Step 3: Add the column and the filter flag**

In `cmd/tn3270proxy/audit.go`, update `printAuditEvents` (`:110-114`) to print actor (place it right after the subject username):

```go
	for _, ev := range events {
		fmt.Fprintf(w, "%s  %-12s %s  %-12s %-12s %-21s %-12s %s\n",
			ev.At.Format(time.RFC3339), ev.Kind, ev.SessionID,
			ev.Username, ev.Actor, ev.RemoteAddr, ev.Service, ev.Detail)
	}
```

Update the doc comment above it (`:103-104`) to mention actor:

```go
// printAuditEvents writes one event per line: time, kind, session, subject user,
// actor, remote, service, detail.
```

Add the `-actor` flag in `runAuditList` (after the `user` flag, `:75`) and wire it into the filter (`:82`):

```go
	user := fs.String("user", "", "filter by subject username")
	actor := fs.String("actor", "", "filter by acting principal (who performed the action)")
```
```go
	f := store.AuditFilter{Username: *user, Actor: *actor, Kind: *kind, Limit: *limit}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/tn3270proxy/ -run 'TestPrintAuditEvents|TestAudit' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tn3270proxy/audit.go cmd/tn3270proxy/audit_test.go
git commit -m "feat(cmd): audit list -actor filter and actor column (#73)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Admin audit screen — actor on the detail view

**Files:**
- Modify: `internal/server/admin_audit.go` (`auditDetail`, `:145-174`)
- Test: `internal/server/admin_audit_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/admin_audit_test.go`. Build an `adminFlow` and call `auditDetail` for an admin-on-other event, asserting an Actor field is present. Reuse the file's existing setup (it already constructs events and an `adminFlow`; mirror the nearest test):

```go
func TestAuditDetailShowsActor(t *testing.T) {
	f := &adminFlow{} // auditDetail only needs ctx + the event for the actor field
	dv := f.auditDetail(context.Background(), store.AuditEvent{
		Kind: store.AuditMFACleared, SessionID: "s1",
		Username: "BOB", Actor: "ADMIN"})

	var found bool
	for _, fld := range dv.Fields {
		if fld.Label == "Actor" && fld.Value == "ADMIN" {
			found = true
		}
	}
	if !found {
		t.Errorf("auditDetail fields missing Actor=ADMIN; got %+v", dv.Fields)
	}
}
```

If `auditDetail` dereferences `f.resolver`/config only when `RemoteAddr != ""`, the empty-RemoteAddr event above avoids that path; keep `RemoteAddr` empty so a bare `adminFlow{}` is sufficient. Confirm by reading `auditDetail` before finalizing; if it needs more wiring, copy the harness from an existing `admin_audit_test.go` test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestAuditDetailShowsActor -v`
Expected: FAIL — no `Actor` field in the detail view.

- [ ] **Step 3: Add the Actor field to the detail view**

In `internal/server/admin_audit.go`, in `auditDetail`, add an Actor field right after the Username field (`:150`):

```go
		{Label: "Username", Value: ev.Username},
		{Label: "Actor", Value: ev.Actor},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run 'TestAuditDetail|TestAudit' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin_audit.go internal/server/admin_audit_test.go
git commit -m "feat(server): show actor on the admin audit detail view (#73)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Full verification, docs, and CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (internal/store + audit description), `docs/superpowers/ROADMAP.md` if it tracks #73

- [ ] **Step 1: Run the full suite with the race detector**

Run: `go test ./... -race`
Expected: PASS across all packages.

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: clean build, no errors.

- [ ] **Step 3: Update CLAUDE.md**

In `CLAUDE.md`, update the `internal/store` audit description to note the actor/subject split. Change the audit-trail line to read approximately:

```
Audit trail: `audit` table (UTC RFC3339, session-correlated) with both an
`actor` (who performed the action) and `username` (the subject affected) column
+ RecordAudit/ListAudit/PruneAudit; AuditFilter narrows by either lens.
```

If `docs/superpowers/ROADMAP.md` lists #73, mark it done.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/superpowers/ROADMAP.md
git commit -m "docs: note audit actor/subject split (#73)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 5: Optional protocol smoke (only if a 3270-facing change is suspected)**

This change is data-model/CLI only — no screen layout, cursor, or negotiation change — so the s3270 smoke test is not required. The admin audit *detail view* gains a field; if you want visual confirmation, run the s3270-smoke-testing skill or eyeball the audit detail screen in c3270. Otherwise skip.

---

## Self-review notes (verify during execution)

- **Spec coverage:** migration+backfill+index (Task 1), store fields+filter (Task 2), auto-fill (Task 3), lifecycle set/clear (Task 4), generic-CRUD Option A (Task 5), the four admin-on-other sites (Task 5 context — no change needed), CLI (Task 6), admin screen (Task 7), docs (Task 8). All spec sections mapped.
- **Type consistency:** `setActor` (single setter; `""` clears) is used identically in Tasks 3 and 4. `AuditFilter.Actor`, `AuditEvent.Actor` names match across store/server/cmd.
- **Behavior-change watchpoints:** Task 5 may break a pre-existing admin test that asserted generic-CRUD `Username == admin`; the step calls this out. Task 4's expected actor is `"alice"` (confirmed: `authStub` in `session_test.go` returns `Username: "alice"` verbatim, no normalization).
