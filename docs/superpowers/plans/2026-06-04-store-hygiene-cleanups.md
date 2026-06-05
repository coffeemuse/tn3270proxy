# Store Hygiene Cleanups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply three behavior-preserving clarity cleanups in `internal/store` (drop hand-rolled bool→int, simplify `deleteCascade`, trim the `AdminStore.GetService` interface method) per GH #35.

**Architecture:** Pure refactor. No schema, behavior, or test-expectation change. The SQLite driver (`modernc.org/sqlite`) binds Go `bool` to integer 0/1 itself, so the manual conversions are redundant. `deleteCascade` gains a narrower variadic signature matching every caller. The `GetService` method stays on the concrete `*store.Store` (tests use it) but leaves the `AdminStore` interface (no production caller).

**Tech Stack:** Go, `modernc.org/sqlite` (pure-Go, no cgo). Test with `go test ./... -race`.

---

### Task 1: Drop bool→int in `CreateService`

**Files:**
- Modify: `internal/store/store.go:300-321`
- Test: `internal/store/admin_test.go` (existing `TestListAllServicesAndGetService` and CreateService TLS coverage — no change)

- [ ] **Step 1: Confirm existing tests pass (baseline)**

Run: `go test ./internal/store/ -run 'Service' -v`
Expected: PASS (establishes green baseline before refactor)

- [ ] **Step 2: Replace the function body to pass bools directly**

In `internal/store/store.go`, replace the `CreateService` function (lines 300–321) with:

```go
func (s *Store) CreateService(ctx context.Context, name, description, host string, port int, tls, verify bool) (int64, error) {
	name, err := NormalizeServiceName(name)
	if err != nil {
		return 0, err
	}
	if err := ValidateDescription(description); err != nil {
		return 0, err
	}
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO services (name, description, host, port, tls, tls_verify) VALUES (?, ?, ?, ?, ?, ?)",
		[]any{name, description, host, port, tls, verify},
		"SELECT id FROM services WHERE name = ?",
		[]any{name})
}
```

- [ ] **Step 3: Run tests to verify behavior unchanged**

Run: `go test ./internal/store/ -run 'Service' -v`
Expected: PASS (TLS/verify flags round-trip identically — `queryServices` still reads `tlsInt != 0`)

- [ ] **Step 4: Commit**

```bash
git add internal/store/store.go
git commit -m "refactor(store): pass bools directly in CreateService"
```

---

### Task 2: Drop bool→int in `UpdateService`

**Files:**
- Modify: `internal/store/admin.go:167-185`

- [ ] **Step 1: Replace the function body to pass bools directly**

In `internal/store/admin.go`, replace the `UpdateService` function (lines 167–185) with:

```go
// UpdateService replaces every editable field of the service. Returns
// ErrNotFound for an unknown service id.
func (s *Store) UpdateService(ctx context.Context, id int64, name, description, host string, port int, tls, verify bool) error {
	name, err := NormalizeServiceName(name)
	if err != nil {
		return err
	}
	if err := ValidateDescription(description); err != nil {
		return err
	}
	return s.execExpectingRow(ctx,
		"UPDATE services SET name = ?, description = ?, host = ?, port = ?, tls = ?, tls_verify = ? WHERE id = ?",
		name, description, host, port, tls, verify, id)
}
```

- [ ] **Step 2: Run tests to verify behavior unchanged**

Run: `go test ./internal/store/ ./internal/server/ -run 'Service|Update' -v`
Expected: PASS (UpdateService TLS flags round-trip identically)

- [ ] **Step 3: Commit**

```bash
git add internal/store/admin.go
git commit -m "refactor(store): pass bools directly in UpdateService"
```

---

### Task 3: Simplify `deleteCascade` signature

**Files:**
- Modify: `internal/store/admin.go:202-244` (the three `Delete*` callers + the helper)

- [ ] **Step 1: Confirm existing delete tests pass (baseline)**

Run: `go test ./internal/store/ -run 'Delete' -v`
Expected: PASS

- [ ] **Step 2: Rewrite the three callers and the helper**

In `internal/store/admin.go`, replace the block from `DeleteUser` through the end of `deleteCascade` (lines 202–244) with:

```go
// DeleteUser removes the user and its group memberships in one transaction.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	return s.deleteCascade(ctx, userID,
		"DELETE FROM user_groups WHERE user_id = ?",
		"DELETE FROM users WHERE id = ?")
}

// DeleteGroup removes the group, its memberships, and its service links in one
// transaction. Callers enforce the ZZ* reservation; the store stays mechanical.
func (s *Store) DeleteGroup(ctx context.Context, groupID int64) error {
	return s.deleteCascade(ctx, groupID,
		"DELETE FROM user_groups WHERE group_id = ?",
		"DELETE FROM group_services WHERE group_id = ?",
		"DELETE FROM groups WHERE id = ?")
}

// DeleteService removes the service and its group links in one transaction.
func (s *Store) DeleteService(ctx context.Context, serviceID int64) error {
	return s.deleteCascade(ctx, serviceID,
		"DELETE FROM group_services WHERE service_id = ?",
		"DELETE FROM services WHERE id = ?")
}

// deleteCascade runs each query in a single transaction, binding the same id to
// each. Deleting an absent id is a silent no-op (unlike SetPassword/
// UpdateService): admin callers always delete rows they just listed, so a
// missing id means concurrent removal, not a caller bug.
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

- [ ] **Step 3: Build and run delete tests**

Run: `go build ./... && go test ./internal/store/ -run 'Delete' -v`
Expected: PASS (cascade behavior unchanged; the `st[0].(string)` assertion is gone)

- [ ] **Step 4: Commit**

```bash
git add internal/store/admin.go
git commit -m "refactor(store): simplify deleteCascade to variadic queries"
```

---

### Task 4: Trim `GetService` from the `AdminStore` interface

**Files:**
- Modify: `internal/server/admin.go:52`

- [ ] **Step 1: Remove the interface line**

In `internal/server/admin.go`, delete this line from the `AdminStore` interface (line 52):

```go
	GetService(ctx context.Context, id int64) (store.Service, error)
```

The lines above (`ListAllServices`) and below (`CreateService`) remain.

- [ ] **Step 2: Build to confirm nothing in production needs it**

Run: `go build ./...`
Expected: PASS (no production caller invokes `GetService` through the interface)

- [ ] **Step 3: Run the server admin tests**

Run: `go test ./internal/server/ -run 'Admin' -v`
Expected: PASS (admin tests call `f.store.GetService` on the concrete `*store.Store`, which still has the method)

- [ ] **Step 4: Commit**

```bash
git add internal/server/admin.go
git commit -m "refactor(server): drop unused GetService from AdminStore interface"
```

---

### Task 5: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full race suite**

Run: `go test ./... -race`
Expected: PASS across all packages (bridge is concurrent — the race detector is required per project convention)

- [ ] **Step 2: Confirm no stray bool→int helpers remain**

Run: `grep -rn 'tlsInt\|verifyInt' internal/store/`
Expected: only `queryServices` scan-side usage in `internal/store/admin.go` (reading `tlsInt`/`verifyInt` from `rows.Scan` is the read path and is intentionally kept). No write-side conversions in `CreateService`/`UpdateService`.

---

## Notes for the implementer

- This is a **rigid behavior-preserving refactor**: do not change any test expectations. If a test fails, the refactor is wrong — fix the code, not the test.
- The driver's bool binding is verified at `modernc.org/sqlite@v1.51.0/conn.go:458` (`case bool: v := 0; if x { v = 1 }`).
- `queryServices` (read path) still scans into `int` and compares `!= 0` — that is correct and out of scope; only the **write-side** conversions are removed.
- The whole plan can optionally be squashed into one commit (`refactor(store): drop boolToInt, simplify deleteCascade, trim AdminStore`) when finishing the branch; per-task commits above keep the steps reviewable.
