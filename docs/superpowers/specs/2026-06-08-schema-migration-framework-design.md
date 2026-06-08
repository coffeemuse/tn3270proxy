# Versioned, forward-only schema migration framework with auto-backup

- **Status:** Approved (design)
- **Date:** 2026-06-08
- **Issue:** [#88](https://github.com/coffeemuse/TN3270Proxy/issues/88)
- **Package:** `internal/store`

## Problem

Schema changes today are **additive-only and version-less**. `store.migrate()` runs
`CREATE TABLE IF NOT EXISTS` plus a series of `ensureColumn()` probes
(`PRAGMA table_info` → `ALTER TABLE ADD COLUMN`). It is idempotent and forward-only,
and it has been adequate pre-prod because every change so far has been "add a defaulted
column." CLAUDE.md still records *"Pre-prod: schema edited directly, no data migration
(closed #9)."*

As we approach release quality this is no longer sufficient. Once operators hold live
`proxy.db` files we can no longer:

- rename or drop a column, or change a constraint (SQLite cannot `ALTER` most constraints —
  it needs the 12-step table-rebuild dance);
- backfill or transform existing rows (no "run exactly once" guarantee in the
  introspection-guarded style);
- know what schema version a given DB is at (`PRAGMA user_version` is unused, sits at 0);
- protect an operator who points an **older** binary at a **newer** DB;
- recover from a bad upgrade (no backup-before-migrate).

## Decision

Adopt a **forward-only migration framework with auto-backup**: numbered Go migrations keyed
on `PRAGMA user_version`, a snapshot taken before any pending migration runs, and a
fail-closed refusal when the DB is newer than the binary understands.

**Forward-only (no down-migrations) is deliberate.** This is a single-file SQLite appliance
(Docker, typically one operator). The whole database is one file, so a snapshot plus the old
binary is a cheaper, more reliable rollback than maintaining reversible steps against SQLite's
weak `ALTER`. The pre-migration snapshot *is* the downgrade story.

**Rejected alternatives:**
- *Embedded `.sql` files via a library (golang-migrate / goose)* — adds a dependency and a
  tracking table, and pushes transforms toward raw SQL; heavier than this appliance needs and
  against the repo's minimal-deps / pure-Go ethos.
- *Status quo + ad-hoc introspection-guarded transforms* — no version record, no guaranteed
  ordering, re-pays the "what's already there?" probe every boot, and data transforms have no
  run-once guarantee.

## Architecture

### File layout

New `internal/store/migrate.go` owns all migration logic, pulled out of `store.go` (which
keeps `Open` / `Store` / CRUD). The `schema` const and `ensureColumn` move into `migrate.go`.
`Open` calls one entry point: `runMigrations(ctx, db, dbPath)`.

New `internal/store/maintenance.go` owns reusable database-maintenance primitives. Only
`Backup` is built now; it is shaped to sit alongside future `Vacuum` / `IntegrityCheck` /
`SchemaVersion` helpers (see Future work).

### The migration value

```go
type migration struct {
    version int
    name    string
    fn      func(ctx context.Context, tx *sql.Tx) error
}

var migrations = []migration{
    {1, "baseline schema", migrateV1Baseline},
    // {2, "rename foo→bar", migrateV2…},  ← future steps appended here, monotonically
}
```

`maxKnown` is the highest `version` in `migrations`.

### Runner flow

All work happens on **one dedicated `*sql.Conn`** (`db.Conn(ctx)`) so PRAGMAs do not leak
across the pool.

1. Read `PRAGMA user_version` → `cur`.
2. `cur > maxKnown` → **refuse to start**: return `ErrSchemaNewer` wrapped with an actionable
   message. No writes.
3. `cur == maxKnown` → no-op; return.
4. `cur < maxKnown` → pending work:
   a. If the DB is **not fresh** (has app tables — see below), take an auto-backup
      (Backup section). A backup failure is **fatal**: no snapshot, no migration.
   b. Disable foreign keys on the dedicated connection for the duration of the run
      (see FK handling).
   c. For each migration with `version > cur`, in ascending order: `BEGIN`; run `fn(tx)`;
      `PRAGMA user_version = <version>` (built from the trusted int literal — PRAGMA values
      cannot be bound); `COMMIT`. A step error rolls back **that step's** tx and returns
      `migrate: step %d (%s): %w`. The DB is left at the last successfully committed version
      with the backup intact.
   d. After all steps, run `PRAGMA foreign_key_check`; if it reports rows, fail. Re-enable
      foreign keys and close the dedicated connection (pool connections keep FK on via the
      DSN).

"Fresh" detection: count `sqlite_master` rows where `type='table'` and name not like
`sqlite_%`; zero → fresh (nothing to back up).

### Foreign-key handling (SQLite gotcha, baked in once)

Table rebuilds — the only way SQLite renames/drops columns or changes constraints — require
foreign keys **off**, and `PRAGMA foreign_keys` is a **no-op inside a transaction**. The
runner therefore toggles `foreign_keys = OFF` on the dedicated migration connection for the
whole run, validates with `PRAGMA foreign_key_check` at the end, and restores it afterward.
Future rebuild migrations work without each one rediscovering the trap. (The DSN sets
`foreign_keys(1)` per pooled connection, so normal operation is unaffected.)

## The v1 baseline & the seeding boundary

**v1 = "the schema as it exists today."** `migrateV1Baseline` is today's *structural*
`migrate()` body: exec the `schema` const (`CREATE … IF NOT EXISTS`) and the `ensureColumn`
ALTERs. On a fresh DB it creates everything in one shot; on a legacy `user_version=0` DB
(every existing dev / early DB) it harmlessly fills gaps, then `user_version` is stamped to 1.
From v2 onward migrations are strict, run-once forward steps. The `ensureColumn` calls are the
v0→v1 bridge only; once no v0 DBs remain in the wild they may eventually be retired (future
cleanup, not now).

**Code-defined defaults stay out of the version ledger — deliberately.** Two of today's
`migrate()` steps reconcile *code-defined data*, not schema:

- ensuring the `ZZADMIN` group exists, and
- seeding `sysconfig.Catalog` defaults (`INSERT OR IGNORE`).

These must keep running **every `Open`** (idempotently), because a new `Catalog` entry added
in code must reach already-migrated DBs — a one-time v1 migration would never deliver it. The
design splits responsibilities explicitly:

- **Versioned migrations** own structure and data transforms.
- **A separate idempotent `reconcileDefaults(ctx)` step**, run every boot *after* migrations,
  owns code-defined seed rows (ZZADMIN group + `sysconfig.Catalog` defaults).

This boundary is documented in CLAUDE.md so the next change lands in the right place: a new
**catalog default** → `reconcileDefaults`; a **structural or data change** → a new migration.

## Backup (generic primitive)

`Backup` is a first-class, reusable store method — not logic trapped inside the runner.

```go
// Backup writes a consistent single-file snapshot of the database to dest using
// VACUUM INTO (safe under WAL; no .wal/.shm side files). dest must not already
// exist. Callable at any time, including while serving.
func (s *Store) Backup(ctx context.Context, dest string) error
```

- A shared `vacuumInto(ctx, conn, dest)` helper performs the actual `VACUUM INTO` so both
  `Store.Backup` (on a pooled connection) and the migration runner (on its dedicated FK-off
  connection during `Open`) use one code path. Prefer a bound parameter (`VACUUM INTO ?`,
  supported by modernc/SQLite ≥ 3.27); if the driver rejects a bound filename, fall back to a
  single-quote-escaped literal.
- `Backup` refuses to clobber an existing `dest` (returns an error) — callers choose unique
  names.
- It takes only a `dest` path and makes no migration-specific assumptions, so it is drop-in
  for the future admin "Backup now" action.

**Migration's use of it.** Before any pending step, the runner:

- **skips backup on a fresh/empty DB** (nothing to lose);
- otherwise writes a never-clobbering snapshot next to the DB:
  `<dbPath>.pre-migrate-v<cur>-<unixepoch>.bak` (encodes the from-version, unique per run);
- treats a backup failure as **fatal**.

`*.bak` is added to `.gitignore`. Automatic retention/pruning of old snapshots is out of
scope; documented as an operator concern.

## Errors & logging

- `ErrSchemaNewer` sentinel; `Open` returns it wrapped: *"database schema is v%d but this
  build supports up to v%d — upgrade the binary or restore a backup."* Fail closed.
- Step failure: rollback that step's tx, return `migrate: step %d (%s): %w`; DB left at last
  committed version, backup present.
- Logging via `slog.Default()` (the store's nil→Default convention): one `Info` when work is
  pending (`from`/`to` versions + backup path), per-step at `Debug`, one `Info` on completion.
  **No DB content is ever logged.**

## Testing (TDD, `migrate_test.go` + `maintenance_test.go`)

- **Fresh DB:** `Open` → `user_version == maxKnown`, full schema present, **no `.bak`
  created**.
- **Canonical-convergence golden test** (headline guarantee): build a DB two ways —
  (a) fresh `Open`; (b) construct a legacy `user_version=0` shape (the old
  pre-`tls_verify` / pre-`description` / pre-MFA schemas already exercised in today's
  `store_test.go`) then `Open` — and assert the resulting schema is **identical** (normalized
  `sqlite_master.sql` / `PRAGMA table_info` per table). Today's legacy-ALTER tests migrate
  into this.
- **Idempotency:** `Open` twice → second run is a no-op, `user_version` stable, no new `.bak`.
- **Newer DB refused:** stamp `user_version = maxKnown+1`, `Open` → `errors.Is(err,
  ErrSchemaNewer)`.
- **Backup on upgrade:** a legacy DB *with data* produces a `.bak`; opening the `.bak` shows
  the pre-migration rows intact (proves restorability).
- **FK integrity:** after migration, `PRAGMA foreign_key_check` is empty.
- **`Backup` unit test:** standalone call writes a valid snapshot; refuses an existing `dest`.
- **Future-step pattern:** an `openAtVersion(t, v)` fixture helper so each new migration ships
  with a "given v−1 data, after `Open` the transform is correct" up-test.

Run with `go test ./... -race`.

## Out of scope / future work

- **Reversible (down) migrations.**
- **Third-party migration library / embedded `.sql` files.**
- **Admin maintenance screen** (a later enhancement) — backup / vacuum / health checks in the
  3270 admin UI. `maintenance.go` is shaped to host the primitives it will need, sketched here
  as the intended shape but **not implemented now**:
  - `Vacuum(ctx) error` — compact / reclaim (`VACUUM`).
  - `IntegrityCheck(ctx) ([]string, error)` — `PRAGMA quick_check` + `foreign_key_check`,
    returning any problems for a health panel.
  - `SchemaVersion(ctx) (int, error)` — read `user_version` for an about / health display.
- **Automatic backup retention/pruning.**
- **Retiring the v0→v1 `ensureColumn` bridge** once no v0 DBs remain.
