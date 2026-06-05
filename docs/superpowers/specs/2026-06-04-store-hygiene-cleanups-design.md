# Store hygiene cleanups (GH #35)

Sub-issue of #15 (item 3). Three low-risk, behavior-preserving clarity cleanups in
`internal/store`, plus one interface trim in `internal/server`. No schema or behavior
change; existing tests already cover every touched path.

## Motivation

`internal/store` carries three small bits of incidental complexity worth removing while
the code is cold and pre-1.0:

1. Hand-rolled `bool`→`int` conversions before binding, which the SQLite driver already
   does for us.
2. A `deleteCascade` helper whose `[][2]any` parameter shape forces a runtime type
   assertion, despite every caller binding exactly one id to every statement.
3. A `GetService` method on the `AdminStore` interface with no production caller.

## Changes

### 1. Drop inline `bool`→`int` conversions

`modernc.org/sqlite` binds a Go `bool` directly to an integer 0/1 — verified at
`conn.go:458` (`@v1.51.0`):

```go
case bool:
    v := 0
    if x { v = 1 }
```

The stored value is byte-identical to the manual conversion, so:

- **`store.go` `CreateService`**: delete the `tlsInt`/`verifyInt` block; pass `tls, verify`
  straight into the `[]any{…}` insert args.
- **`admin.go` `UpdateService`**: delete the matching block; pass `tls, verify` into the
  `execExpectingRow` args.

### 2. Simplify `deleteCascade`

Replace the `[][2]any` shape (and its `st[0].(string)` runtime assertion) with a variadic
query list bound to a single id — the shape every caller already uses:

```go
func (s *Store) deleteCascade(ctx context.Context, id int64, queries ...string) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()
    for _, q := range queries {
        if _, err := tx.ExecContext(ctx, q, id); err != nil {
            return err
        }
    }
    return tx.Commit()
}
```

Callers (`DeleteUser`, `DeleteGroup`, `DeleteService`) pass their single id followed by the
delete statements, e.g.:

```go
s.deleteCascade(ctx, userID,
    "DELETE FROM user_groups WHERE user_id = ?",
    "DELETE FROM users WHERE id = ?")
```

This is narrower than the old shape (one id for all statements vs. arbitrary per-statement
args), but matches every real caller. YAGNI; revisit only if a caller needs heterogeneous
args.

### 3. Trim `AdminStore.GetService`

Remove `GetService(ctx, id) (store.Service, error)` from the `AdminStore` interface
(`internal/server/admin.go:52`). It has no production caller in the admin flow. The
concrete `*store.Store.GetService` stays — store-level and admin-level tests call it
directly on the concrete type, not through the interface.

## Out of scope (deferred, per #35)

- N+1 render folds (`GetUserGroups` per row; the two `COUNT`s per group row).
- Indexed single-row name-uniqueness lookups (`checkServiceNameFree` / `groupIDByName`).

These are correctness-neutral perf on a cold, human-cadence path with tiny admin-managed
data volumes. Reconsider only if list sizes grow.

## Testing

No new tests. All three paths are exercised by existing tests:

- `CreateService` / `UpdateService` TLS flags — `internal/store` + `internal/server` admin tests.
- All three cascade deletes — existing delete tests.
- `GetService` — store and admin tests (on the concrete type, unaffected by the interface trim).

Verify behavior is unchanged with `go test ./... -race`.

## Delivery

One focused commit: `refactor(store): drop boolToInt, simplify deleteCascade, trim AdminStore`.
