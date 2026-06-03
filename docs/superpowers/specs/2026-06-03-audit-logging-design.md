# Audit Logging — Design

**Date:** 2026-06-03
**Status:** Approved
**Roadmap item:** #5 (`docs/superpowers/ROADMAP.md`)

## Goal

A durable, queryable record of who connected, when, from where, which service
they reached, how each session ended, and what admins changed — the
accountability layer for a (eventually) public-facing access gateway.

## Scope decisions

- **Sink: a SQLite `audit` table** in the existing proxy DB, owned by `store`
  (no structured-log/slog sink in v1; the table is the single source of truth).
- **Events: roadmap baseline + admin CRUD.** Connect, auth success/failure,
  bridge start/end with cause, disconnect, and every admin-screen mutation.
  No logoff/menu-transition events (derivable noise).
- **Retention: an explicit `audit prune` CLI subcommand** (operator-run or
  cron'd). No background sweeper in `serve`.
- **Reading: an `audit list` CLI subcommand** with simple filters. No admin
  3270 audit-browser screen in v1 (possible later milestone on top of the
  same table).
- **Failure mode: best-effort.** An audit write failure is logged via stdlib
  `log` and the session continues. Availability over audit completeness at
  this stage; nothing in the session path can fail because of auditing.
- **Never log credentials** (existing hard rule): `auth_fail` records the
  attempted username and source IP, never the password.

## Data model

New table, created idempotently in `store.migrate()` like the rest:

```sql
CREATE TABLE IF NOT EXISTS audit (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    at          TEXT NOT NULL,              -- UTC, RFC3339
    session_id  TEXT NOT NULL,              -- correlates all events of one connection
    kind        TEXT NOT NULL,              -- event kind, see below
    username    TEXT NOT NULL DEFAULT '',   -- empty before/without login
    remote_addr TEXT NOT NULL DEFAULT '',
    service     TEXT NOT NULL DEFAULT '',   -- service name, for bridge events
    detail      TEXT NOT NULL DEFAULT ''    -- cause / admin-change description / error summary
);
CREATE INDEX IF NOT EXISTS audit_at ON audit(at);
CREATE INDEX IF NOT EXISTS audit_username ON audit(username);
```

**Event kinds** (string constants in `store`, alongside `AuditEvent`):

| Kind | When | Notable fields |
|---|---|---|
| `connect` | top of `Session.Run` (≈ accept) | remote_addr |
| `auth_ok` | login success | username |
| `auth_fail` | login failure | attempted username (never the password) |
| `bridge_start` | service selected, dial begins | service |
| `bridge_end` | bridge returns | service; detail = cause (`backend_closed` / `user_escaped` / `client_closed` / `error: …`) |
| `admin` | admin CRUD mutation commits | detail = e.g. `user create alice`, `group ZZADMIN add-member bob` |
| `disconnect` | connection ends (deferred) | detail = how the session ended |

Design notes:

- **`session_id`** is 8 random bytes (crypto/rand) hex-encoded, generated once
  per connection — it turns rows into a per-connection narrative.
- **`username` is a plain string, not a FK**: audit rows must survive user
  deletion, and `auth_fail` records usernames that may not exist.
- **Timestamps are RFC3339 TEXT in UTC**: human-readable in raw SQL and sort
  lexicographically.
- Service selection and bridge start are the same moment in the session loop,
  so they are one event (`bridge_start`), not two.
- Every event carries `session_id` and `remote_addr` (prefilled by the
  session helper); `username`/`service`/`detail` apply per kind.

## Architecture

### `Auditor` seam (chosen approach)

A consumer-defined interface in `server`, mirroring the `Presenter`/`Bridger`
seam pattern:

```go
type Auditor interface {
    Record(ctx context.Context, ev store.AuditEvent)
}
```

- `Record` returns **no error** — best-effort is baked into the contract.
- Rejected alternatives: direct `store` calls from `Session` (breaks the seam
  convention exactly where tests want a recording fake) and an async
  channel/writer goroutine (solves a latency problem we don't have; adds
  drop/backpressure/shutdown complexity — YAGNI).

### Real implementation

`storeAuditor` in `server` (next to `go3270Presenter` / `realBridger`),
wrapping `*store.Store`. It stamps `At = time.Now().UTC()`, calls
`store.RecordAudit`, and on error `log.Printf`s and moves on. All SQL stays
in `store`.

### Session wiring

- `Session` gains an `Auditor` field. An unexported helper `s.audit(ctx, ev)`
  no-ops when the field is nil (mirrors the nil-`AdminPresenter` pattern, so
  existing tests are untouched).
- `Run` generates the session ID and captures `conn.RemoteAddr()` once at the
  top; the helper stamps both into every event.
- Placement in `Run`:
  - `connect` — top of `Run` (no `Server.handle` signature churn).
  - `disconnect` — in a `defer`, firing on every exit path.
  - `auth_ok` / `auth_fail` — inside `doLogin`, next to `Authenticate`.
  - `bridge_start` — immediately before `Bridger.Bridge`; `bridge_end`
    immediately after, with the `bridge.Cause` rendered into `detail`.

### adminFlow wiring

`adminFlow` receives the session's prefilled audit helper (the way
`identity`/`term` already flow). Each successful store mutation in
`admin_users.go` / `admin_groups.go` / `admin_services.go` records one
`admin` event — **after** the store call succeeds, so the trail reflects what
actually happened.

## CLI

New `audit` subcommand in `cmd/tn3270proxy` with two verbs, following the
`serve`/`seed` pattern:

```bash
tn3270proxy audit list  -db proxy.db [-user alice] [-kind auth_fail] [-since 24h] [-limit 100]
tn3270proxy audit prune -db proxy.db -older-than 90d
```

- **`list`**: one event per line (timestamp, kind, session, user, remote,
  service, detail), newest first, default limit 100, filters AND-combined.
- **`prune`**: deletes rows older than the cutoff, reports the count.
  `-older-than` is **required** — no default that silently deletes.
- **Durations**: `-since` / `-older-than` accept Go durations plus a `d`
  suffix (`90d`) via a small parse helper (`time.ParseDuration` stops at
  hours; `2160h` is operator-hostile).

Backing `store` methods (all SQL stays in `store`):

```go
func (s *Store) RecordAudit(ctx context.Context, ev AuditEvent) error
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEvent, error) // Username/Kind/Since/Limit
func (s *Store) PruneAudit(ctx context.Context, before time.Time) (int64, error)    // rows deleted
```

## Error handling

- `storeAuditor` logs and continues on write failure; the session never
  blocks or dies because of auditing.
- CLI verbs exit non-zero with a clear message on bad flags or DB errors.
- `prune` with a missing/unparseable `-older-than` is an error, not a
  default.

## Testing (TDD, per repo convention)

1. **`store`** first: record → list → prune roundtrips; filter combinations;
   prune cutoff boundaries; idempotent migration.
2. **`server`**: a `recordingAuditor` fake asserting event sequences per
   scenario — failed login (`auth_fail` with username + remote addr), full
   bridge lifecycle (`connect`, `auth_ok`, `bridge_start`, `bridge_end`,
   `disconnect`), PA3 escape (`bridge_end` detail `user_escaped`), admin
   mutation (`admin` event with change description). Nil-`Auditor` sessions
   keep working.
3. **Credential-leak test**: explicit assertion that the password string
   appears in no recorded event after a failed login.
4. **CLI verbs** against a temp DB.
5. `go test ./... -race` throughout (bridge is concurrent).

Audit is not protocol-surface (no screens/negotiation changes), so no live
emulator gate — one c3270 session to eyeball real rows is a cheap optional
sanity check.

## Docs on completion

- Mark #5 done in `docs/superpowers/ROADMAP.md`.
- Add the `audit` table, `Auditor` seam, and `audit` CLI verbs to
  `CLAUDE.md`'s package map.
