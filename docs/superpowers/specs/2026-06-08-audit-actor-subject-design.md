# Audit trail: separate actor from subject (GH #73)

**Date:** 2026-06-08
**Issue:** #73 — *Audit trail conflates actor and subject*
**Status:** design approved, ready for implementation plan

## Problem

The `audit` table has a single `username` column that two code paths fill with
**different meanings**:

- **Generic admin CRUD** (`recordAdmin`, `internal/server/admin.go`) writes the
  **acting admin** into `username` and describes the target in `Detail`.
- **MFA enforce/clear and settings lock/unlock** (`internal/server/admin_users.go`)
  write the **target user** into `username`; the acting admin is dropped (recoverable
  only by joining `session_id` back to that connection's `auth_ok` row).

So `audit list -kind mfa_cleared` shows only the affected account, never the admin who
pulled the secret — and these are exactly the security-sensitive "who authorized this?"
events where actor attribution matters most.

### Scope correction vs. the issue text

When #73 was filed it named two offending kinds (`mfa_enforced`, `mfa_cleared`). Since
then the User Settings Lock work (#86) added two more that follow the **same pattern**:
`settings_locked` and `settings_unlocked` (`admin_users.go`). All **four** admin-on-other
events lose the actor; the fix covers all four.

The issue's note that "the schema can be edited directly, no migration (see #9)" is also
**stale**: the migration framework landed in #88. Schema changes now go through the
append-only ledger in `internal/store/migrate.go`, and editing the v1 baseline
`CREATE TABLE` only reaches *fresh* DBs. This work therefore adds a migration step.

## Goal

Make actor attribution a first-class, queryable field so the trail answers both lenses:

- **Actor-centric** — "what did admin X do?" → filter on `actor`.
- **Subject-centric** — "what happened to account Y?" → filter on `username` (unchanged).

## Decisions (settled in brainstorming)

1. **Option 1 — add an `actor` column** (vs. encoding actor in `Detail`, or documenting
   the split). First-class and queryable.
2. **Uniform population** — `actor` carries the authenticated session principal on
   *every* row, auto-filled at the per-connection choke point. For login / bridge /
   self-service rows it simply equals `username`.
3. **Migration + backfill** — add the column via a new ledger step (v2), backfilling
   `actor = username` on existing rows.
4. **Generic CRUD `username` → empty** (Option A) — once `actor` auto-fills with the
   admin, `username` *always* means "the user account this row is about," and is empty
   when there is no single user subject (group/service ops). The target stays in `Detail`.
5. **Actor set at `auth_ok`** (Option A) — flipped on the moment login succeeds, so
   login-phase `mfa_success` / `mfa_failed` rows carry it; cleared on logout / idle-logout.

## Semantics

| Field      | Meaning                                                                |
|------------|-----------------------------------------------------------------------|
| `actor`    | The authenticated session principal who performed the action. Empty pre-auth (`connect`, `auth_fail`). |
| `username` | The subject — the user account the row is *about*. Empty when there is no single user subject (e.g. generic group/service CRUD). |

For self-service and ordinary session events (`auth_ok`, `mfa_success`, `bridge_*`,
`logout`, `password_self`, self `mfa_cleared`, …) `actor == username`.

## Design

### 1. Store layer (`internal/store/audit.go`, `migrate.go`)

- Add `Actor string` to `AuditEvent`.
- `RecordAudit` includes `actor` in the INSERT; `ListAudit` selects it.
- `AuditFilter` gains `Actor string` → `WHERE actor = ?`. The existing `Username`
  constraint (subject lens) is retained.
- **Migration v2** (`migrations` ledger, version exactly one above the current head v1):
  - `ALTER TABLE audit ADD COLUMN actor TEXT NOT NULL DEFAULT ''` (via the existing
    idempotent `ensureColumnTx` helper).
  - Backfill `UPDATE audit SET actor = username` (runs exactly once during the v1→v2
    upgrade; a no-op on a fresh DB whose `audit` table is empty).
  - `CREATE INDEX IF NOT EXISTS audit_actor ON audit(actor)`.
  - The v1 baseline `schema` const is **not** edited; v2 supplies the column for both
    fresh and existing DBs (v1 then v2 run in sequence on a fresh open).

**Backfill caveat (accepted):** for the four historical admin-on-other rows the backfill
sets `actor = username = subject`; the real admin is unrecoverable from existing data.
This is pre-prod historical data, so the approximation is acceptable and noted here.

### 2. Auto-fill at the choke point (`internal/server/auditor.go`)

- `auditTrail` gains a mutable `actor` field alongside `sessionID` / `remoteAddr`.
- `record()` sets `ev.Actor = a.actor` **only when the caller left `ev.Actor` empty**
  (explicit override still allowed).
- The session sets `a.actor` at the point `auth_ok` is recorded and clears it on logout /
  idle-logout (returning to the pre-auth regime). `connect` and `auth_fail` naturally get
  an empty actor because no principal is set yet.

### 3. Call-site changes

- **The four buggy sites** (`mfa_enforced`, `mfa_cleared`, `settings_locked`,
  `settings_unlocked` in `admin_users.go`): keep `Username: u.Username` (subject). They
  become correct for free — `actor` auto-fills to the admin.
- **`recordAdmin`** (`admin.go`): drop `Username: f.identity.Username`. Subject stays in
  `Detail`; `actor` auto-fills. (Decision 4 — `username` empty for generic CRUD.)
- **Login / bridge / self-service rows**: unchanged; auto-fill makes `actor == username`.

### 4. Display surfaces

- **CLI** (`cmd/tn3270proxy/audit.go`): add an `-actor` filter flag wired to
  `AuditFilter.Actor`, and an `actor` column to `printAuditEvents`.
- **Admin audit screen** (`internal/server/admin_audit.go`): surface `actor` on the
  detail line.

## Testing (TDD)

**store**
- Round-trip `Actor` through `RecordAudit` / `ListAudit`.
- `AuditFilter.Actor` narrows correctly; combines (AND) with `Username` / `Kind` / `Since`.
- Migration v2: column added, existing rows backfilled `actor = username`, `audit_actor`
  index present; convergence (legacy→v2 and fresh→v2 produce the same schema); no-op on
  re-open.

**server**
- `actor` auto-fills from the session principal once set; empty for `connect` / `auth_fail`.
- Actor is present across the login→MFA boundary (`mfa_success` / `mfa_failed` at login).
- Actor cleared on logout and on idle-logout (next pre-auth event has empty actor).
- Admin-on-other rows: `actor = admin`, `username = subject`.
- Generic CRUD rows: `username` empty, `actor = admin`.
- Explicit `ev.Actor` override is respected (auto-fill does not clobber it).

**cmd**
- `-actor` filter passes through to `AuditFilter.Actor`.
- `actor` column renders in `printAuditEvents`.

## Out of scope

- The self-service audit kinds themselves (owned by #63, already shipped).
- Any rework of `session_id` correlation or audit retention/pruning.
