# User Settings Lock (`user_settings_locked`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-user `user_settings_locked` flag an admin can set that hides the self-service User Settings (`0`) menu entry and freezes the account's MFA state as admin-managed.

**Architecture:** A new boolean column on `users` (mirroring `mfa_required`), surfaced on the admin edit-user form and audited on toggle. Two enforcement points read it: `screens.MenuScreen` / the menu choice plumbing omit and reject `0` when locked, and `Session.mfaGate` skips the *enrollment* branch (the verify branch is untouched, so an already-enrolled secret is still required at login). Seed JSON gains the field for declarative guest provisioning.

**Tech Stack:** Go, `modernc.org/sqlite` (pure-Go), `racingmars/go3270`, internal `ui3270`/`screens`/`store`/`server`/`seed` packages. TDD with the in-repo fake presenter/terminal harness; `go test ./... -race`.

**Spec:** `docs/superpowers/specs/2026-06-08-user-settings-lock-design.md` · **Issue:** #86

---

## File Structure

- `internal/store/store.go` — schema column, `User` struct field, `GetUserByUsername` scan; `migrate()` `ensureColumn`.
- `internal/store/admin.go` — `ListUsers`/`ListUsersInGroup` SELECT + `queryUsers` scan; new `SetUserSettingsLocked`.
- `internal/store/audit.go` — two new audit-kind constants.
- `internal/store/usersettingslock_test.go` — **new**, store round-trip tests.
- `internal/screens/admin.go` — new `FieldUserSettingsLocked` field-name constant.
- `internal/screens/menu.go` — `MenuScreen` gains a `settingsLocked` param; meta-band omits `0` when locked.
- `internal/screens/menu_test.go` — update existing `MenuScreen(...)` call sites; new lock cases.
- `internal/server/presenter.go` — `classifyMenuSubmit` + `go3270Presenter.Menu` gain `settingsLocked`.
- `internal/server/session.go` — `Presenter.Menu` interface signature; load lock before the menu loop; pass it; `mfaGate` skip-enroll-when-locked; dispatch guard.
- `internal/server/session_test.go` — `fakePresenter.Menu` signature.
- `internal/server/session_mfa_test.go` — new locked-gate test.
- `internal/server/admin_users.go` — form field, hint, `applyLockEdit`, wired into `userSaveEdit`.
- `internal/server/admin_users_test.go` — `applyLockEdit` tests.
- `internal/seed/seed.go` — `SeedUser.UserSettingsLocked` + apply it.
- `internal/seed/seed_test.go` — seed-the-flag test.
- `CLAUDE.md` — package-map notes.

---

## Task 1: Store — column, struct field, scanners, setter

**Files:**
- Modify: `internal/store/store.go` (schema ~63-73, `migrate` ~157-160, `User` struct ~212-222, `GetUserByUsername` ~258-271)
- Modify: `internal/store/admin.go` (`ListUsers` ~42-46, `ListUsersInGroup` ~50-56, `queryUsers` ~58-76; add setter near `SetPassword` ~167)
- Test: `internal/store/usersettingslock_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/store/usersettingslock_test.go`:

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

package store

import (
	"context"
	"errors"
	"testing"
)

func TestUserSettingsLockedDefaultsOff(t *testing.T) {
	st, err := Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	st.CreateUser(ctx, "alice", "h")
	u, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.UserSettingsLocked {
		t.Fatal("new user must default to unlocked")
	}
}

func TestSetUserSettingsLockedRoundTrips(t *testing.T) {
	st, _ := Open(t.TempDir() + "/s.db")
	defer st.Close()
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "h")

	if err := st.SetUserSettingsLocked(ctx, uid, true); err != nil {
		t.Fatalf("set true: %v", err)
	}
	u, _ := st.GetUserByUsername(ctx, "alice")
	if !u.UserSettingsLocked {
		t.Fatal("expected locked after set true")
	}

	if err := st.SetUserSettingsLocked(ctx, uid, false); err != nil {
		t.Fatalf("set false: %v", err)
	}
	u, _ = st.GetUserByUsername(ctx, "alice")
	if u.UserSettingsLocked {
		t.Fatal("expected unlocked after set false")
	}

	// Also visible through the list path.
	users, _ := st.ListUsers(ctx)
	if len(users) != 1 || users[0].UserSettingsLocked {
		t.Fatalf("list path: %+v", users)
	}
}

func TestSetUserSettingsLockedUnknownUser(t *testing.T) {
	st, _ := Open(t.TempDir() + "/s.db")
	defer st.Close()
	if err := st.SetUserSettingsLocked(context.Background(), 999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run UserSettingsLocked -v`
Expected: FAIL — `u.UserSettingsLocked undefined` and `st.SetUserSettingsLocked undefined` (compile error).

- [ ] **Step 3: Add the schema column and struct field**

In `internal/store/store.go`, add the column to the `users` CREATE TABLE (after `mfa_last_step`):

```go
	mfa_last_step   INTEGER NOT NULL DEFAULT 0,
	user_settings_locked INTEGER NOT NULL DEFAULT 0
);
```

In `migrate()`, after the `mfa_last_step` `ensureColumn` block (~line 160), add:

```go
	if err := s.ensureColumn("users", "user_settings_locked",
		"ALTER TABLE users ADD COLUMN user_settings_locked INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
```

In the `User` struct, add the field after `MFALastStep`:

```go
	MFALastStep        int64  // replay floor: highest accepted TOTP step
	UserSettingsLocked bool   // admin lock: hides self-service User Settings; freezes MFA as admin-managed
```

- [ ] **Step 4: Update the single-user scan**

In `GetUserByUsername` (`internal/store/store.go`), extend the SELECT and Scan:

```go
	var reqInt, lockInt int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, full_name, email,
		        mfa_required, mfa_secret, mfa_enrolled_at, mfa_last_step, user_settings_locked
		 FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Email,
			&reqInt, &u.MFASecret, &u.MFAEnrolledAt, &u.MFALastStep, &lockInt)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.MFARequired = reqInt != 0
	u.UserSettingsLocked = lockInt != 0
	return u, nil
```

- [ ] **Step 5: Update the list scans and add the setter**

In `internal/store/admin.go`, add the column to both `ListUsers` and `ListUsersInGroup` SELECT lists (append `, user_settings_locked` / `, u.user_settings_locked` after `mfa_last_step`):

```go
	// ListUsers:
	`SELECT id, username, password_hash, full_name, email,
	        mfa_required, mfa_secret, mfa_enrolled_at, mfa_last_step, user_settings_locked
	 FROM users ORDER BY username`)

	// ListUsersInGroup:
	`SELECT u.id, u.username, u.password_hash, u.full_name, u.email,
	        u.mfa_required, u.mfa_secret, u.mfa_enrolled_at, u.mfa_last_step, u.user_settings_locked
	 FROM users u
	 JOIN user_groups ug ON ug.user_id = u.id
	 WHERE ug.group_id = ? ORDER BY u.username`, groupID)
```

Update `queryUsers` to scan the extra column:

```go
	for rows.Next() {
		var u User
		var reqInt, lockInt int
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Email,
			&reqInt, &u.MFASecret, &u.MFAEnrolledAt, &u.MFALastStep, &lockInt); err != nil {
			return nil, err
		}
		u.MFARequired = reqInt != 0
		u.UserSettingsLocked = lockInt != 0
		out = append(out, u)
	}
```

Add the setter after `SetPassword` (`internal/store/admin.go`):

```go
// SetUserSettingsLocked toggles the admin lock that hides a user's self-service
// User Settings and freezes their MFA state as admin-managed. Returns
// ErrNotFound for an unknown user id.
func (s *Store) SetUserSettingsLocked(ctx context.Context, userID int64, locked bool) error {
	return s.execExpectingRow(ctx,
		"UPDATE users SET user_settings_locked = ? WHERE id = ?", boolToInt(locked), userID)
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/store/ -run UserSettingsLocked -v`
Expected: PASS (all three tests).

- [ ] **Step 7: Run the full store package to catch scan regressions**

Run: `go test ./internal/store/`
Expected: PASS (the extra scan column must not break existing user tests).

- [ ] **Step 8: Commit**

```bash
git add internal/store/
git commit -m "feat(store): user_settings_locked column + SetUserSettingsLocked (#86)"
```

---

## Task 2: Audit kinds

**Files:**
- Modify: `internal/store/audit.go` (constant block ~37-44)
- Test: covered indirectly by Task 5; add a trivial value assertion here.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/audit_test.go` (create the file if it does not exist, using the standard GPL header from another store test file):

```go
func TestSettingsLockAuditKindValues(t *testing.T) {
	if AuditSettingsLocked != "settings_locked" || AuditSettingsUnlocked != "settings_unlocked" {
		t.Fatalf("kinds = %q/%q", AuditSettingsLocked, AuditSettingsUnlocked)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run SettingsLockAuditKind -v`
Expected: FAIL — `AuditSettingsLocked undefined`.

- [ ] **Step 3: Add the constants**

In `internal/store/audit.go`, after `AuditPasswordSelf`:

```go
	AuditPasswordSelf     = "password_self"     // user changed their own password (self-service)
	AuditSettingsLocked   = "settings_locked"   // admin locked a user out of self-service
	AuditSettingsUnlocked = "settings_unlocked" // admin restored a user's self-service
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/store/ -run SettingsLockAuditKind -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/audit.go internal/store/audit_test.go
git commit -m "feat(store): settings_locked/unlocked audit kinds (#86)"
```

---

## Task 3: `screens.MenuScreen` — omit `0` row when locked

**Files:**
- Modify: `internal/screens/menu.go` (`MenuScreen` signature ~109; meta band ~146-164)
- Modify: `internal/screens/menu_test.go` (existing `MenuScreen(...)` call sites + new cases)

> The signature gains `settingsLocked bool` immediately after `admin bool`. Every existing caller must be updated. Find them first.

- [ ] **Step 1: Write the failing test**

Append to `internal/screens/menu_test.go`:

```go
func TestMenuScreenLockedHidesUserSettings(t *testing.T) {
	// non-admin, locked: neither "0 User Settings" nor "Administration".
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, true, MenuStatus{}, "", 0)
	if screenContains(screen, "User Settings") {
		t.Error("locked non-admin must not see User Settings entry")
	}
	if screenContains(screen, "Administration") {
		t.Error("non-admin must never see Administration")
	}

	// non-admin, unlocked: User Settings present (regression).
	screen, _, _ = MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "User Settings") {
		t.Error("unlocked non-admin must see User Settings entry")
	}

	// admin, locked: Administration present, User Settings hidden.
	screen, _, _ = MenuScreen(DefaultGeometry, nil, true, true, MenuStatus{}, "", 0)
	if screenContains(screen, "User Settings") {
		t.Error("locked admin must not see User Settings entry")
	}
	if !screenContains(screen, "Administration") {
		t.Error("locked admin must still see Administration")
	}

	// admin, unlocked: both present.
	screen, _, _ = MenuScreen(DefaultGeometry, nil, true, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "User Settings") || !screenContains(screen, "Administration") {
		t.Error("unlocked admin must see both entries")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/screens/ -run MenuScreenLocked -v`
Expected: FAIL — too many arguments to `MenuScreen` (signature is still 6-arg).

- [ ] **Step 3: Change the `MenuScreen` signature and meta-band logic**

In `internal/screens/menu.go`, change the signature:

```go
func MenuScreen(geom Geometry, services []store.Service, admin bool, settingsLocked bool, status MenuStatus, errMsg string, page int) (go3270.Screen, map[string]store.Service, Cursor) {
```

Replace the meta-band block (currently lines ~150-164) with:

```go
	// Meta band: bottom-anchored. Non-admin unlocked: "0" on menuBottomRow.
	// Admin: "A" on menuBottomRow, "0" one row above. A locked user has no "0"
	// row at all (self-service is hidden). The page window is capped at
	// MenuCapacity, so service rows never reach the meta band.
	metaRow := geom.menuBottomRow()
	if admin {
		metaRow-- // leave the bottom row for the "A" entry
	}
	if !settingsLocked {
		screen = append(screen,
			go3270.Field{Row: metaRow, Col: 0, Intense: true, Content: "  0"},
			go3270.Field{Row: metaRow, Col: 17, Color: go3270.Green, Content: "User Settings"},
		)
	}
	if admin {
		adminRow := metaRow + 1
		screen = append(screen,
			go3270.Field{Row: adminRow, Col: 0, Intense: true, Content: "  A"},
			go3270.Field{Row: adminRow, Col: 17, Color: go3270.Green, Content: "Administration"},
		)
	}
```

> Note: `MenuCapacity(admin)` is intentionally unchanged — a locked user simply leaves the reserved `0` row blank. This keeps paging math identical and is harmless (one blank row above the PF legend).

- [ ] **Step 4: Update existing `MenuScreen` call sites in the test file**

Run: `grep -rn "MenuScreen(" internal/screens/menu_test.go`
For every existing call (they pass 6 args today), insert `false` after the `admin` argument. For example:

```go
// before: MenuScreen(DefaultGeometry, nil, true, MenuStatus{}, "", 0)
// after:  MenuScreen(DefaultGeometry, nil, true, false, MenuStatus{}, "", 0)
```

Apply to all matches in `menu_test.go` (TestMenuScreenMapping, TestMenuScreenEmpty, TestMenuScreenShowsError, TestMenuScreenAdminEntry — both calls, TestMenuScreenAdminEntryClampedWithManyServices, TestMenuScreenHelpSaysLogoff, TestMenuScreenTopBand, TestMenuScreenBandsAcrossGeometries, TestMenuScreenMappingCoversAllServices, TestMenuRendersISPFStyleAndHidesHostPort, TestMenuScreenCursor, and any others the grep surfaces).

- [ ] **Step 5: Run the screens package tests**

Run: `go test ./internal/screens/`
Expected: PASS (new lock test + all updated existing tests). The `internal/server` package will NOT compile yet — that's Task 3b/4 (presenter still calls the old `MenuScreen` arity). Do not run `./...` until the server changes land.

- [ ] **Step 6: Commit**

```bash
git add internal/screens/menu.go internal/screens/menu_test.go
git commit -m "feat(screens): MenuScreen hides 0 entry when settings locked (#86)"
```

---

## Task 3b: Menu choice plumbing — reject `0` when locked, thread the flag

**Files:**
- Modify: `internal/server/presenter.go` (`classifyMenuSubmit` ~50-66; `go3270Presenter.Menu` ~101-146)
- Modify: `internal/server/session.go` (`Presenter.Menu` interface ~53; menu loop ~351-366; dispatch guard ~416)
- Modify: `internal/server/session_test.go` (`fakePresenter.Menu` ~108)
- Test: new test in `internal/server/presenter_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/server/presenter_test.go`:

```go
func TestClassifyMenuSubmitLockedRejectsZero(t *testing.T) {
	mapping := map[string]store.Service{"1": {Name: "DEMO"}}

	// Unlocked: "0" → user settings.
	if ch, _ := classifyMenuSubmit("0", mapping, false, false); ch != menuUserSettings {
		t.Errorf("unlocked '0' = %v, want menuUserSettings", ch)
	}
	// Locked: "0" is not special — it falls through to a normal (invalid) key.
	if ch, _ := classifyMenuSubmit("0", mapping, false, true); ch != menuReprompt {
		t.Errorf("locked '0' = %v, want menuReprompt", ch)
	}
	// Locked admin still reaches admin on "A".
	if ch, _ := classifyMenuSubmit("A", mapping, true, true); ch != menuAdmin {
		t.Errorf("locked admin 'A' = %v, want menuAdmin", ch)
	}
}
```

> The exact import block of `presenter_test.go` already pulls in `store` and `testing`; if not, add them.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run ClassifyMenuSubmitLocked -v`
Expected: FAIL — `classifyMenuSubmit` takes 3 args, not 4 (compile error).

- [ ] **Step 3: Add `settingsLocked` to `classifyMenuSubmit`**

In `internal/server/presenter.go`:

```go
func classifyMenuSubmit(key string, mapping map[string]store.Service, admin bool, settingsLocked bool) (menuChoice, store.Service) {
	if admin && key == "A" {
		return menuAdmin, store.Service{}
	}
	if !settingsLocked && key == "0" {
		return menuUserSettings, store.Service{}
	}
	if svc, ok := mapping[key]; ok {
		return menuService, svc
	}
	if len(mapping) == 0 {
		return menuRequery, store.Service{}
	}
	return menuReprompt, store.Service{}
}
```

- [ ] **Step 4: Thread `settingsLocked` through `go3270Presenter.Menu`**

In `internal/server/presenter.go`, change the method signature and the two internal calls:

```go
func (go3270Presenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, settingsLocked bool, status screens.MenuStatus, errMsg string) (*store.Service, menuChoice, error) {
```

Inside, update the render call:

```go
		screen, mapping, cur := screens.MenuScreen(geom, svcs, admin, settingsLocked, status, errMsg, page)
```

and the classify call:

```go
		switch choice, svc := classifyMenuSubmit(key, mapping, admin, settingsLocked); choice {
```

- [ ] **Step 5: Update the `Presenter` interface and the menu loop**

In `internal/server/session.go`, update the interface method (~line 53):

```go
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, settingsLocked bool, status screens.MenuStatus, errMsg string) (selected *store.Service, choice menuChoice, err error)
```

In the run loop, after `isAdmin := ...` (~line 351), load the lock once (applies at next login/menu render, per repo convention):

```go
		isAdmin := s.AdminPresenter != nil && slices.Contains(identity.Groups, store.AdminGroup)
		settingsLocked := false
		if lu, lerr := s.Store.GetUserByUsername(ctx, identity.Username); lerr != nil {
			s.log().Warn("load user-settings-lock failed; defaulting unlocked", "error", lerr)
		} else {
			settingsLocked = lu.UserSettingsLocked
		}
		errMsg := ""
```

Update the `Presenter.Menu` call (~line 366):

```go
			selected, choice, err := s.Presenter.Menu(conn, term, services, isAdmin, settingsLocked, status, errMsg)
```

Add a defense-in-depth guard in the `menuUserSettings` case (~line 416), mirroring the admin guard:

```go
			case menuUserSettings:
				if settingsLocked {
					continue // guard: a buggy presenter can't open self-service for a locked user
				}
				if uerr := s.userSettings(ctx, conn, term, identity, aud); uerr != nil {
```

- [ ] **Step 6: Update the fake presenter**

In `internal/server/session_test.go`, change `fakePresenter.Menu`'s signature to match (add `settingsLocked bool` after `admin bool`); the body is unchanged:

```go
func (f *fakePresenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, settingsLocked bool, status screens.MenuStatus, errMsg string) (*store.Service, menuChoice, error) {
```

- [ ] **Step 7: Update the existing `classifyMenuSubmit` callers in tests**

`internal/server/presenter_test.go` already calls `classifyMenuSubmit` with 3 args in two places — both must gain the new `settingsLocked` argument (`false`, preserving today's behavior):

In `TestClassifyMenuSubmit` (table-driven, ~line 60):

```go
			gotChoice, gotSvc := classifyMenuSubmit(tc.key, tc.mapping, tc.admin, false)
```

In `TestClassifyMenuSubmitUserSettings` (~lines 73, 76, 79), add `, false` to each call:

```go
	if c, _ := classifyMenuSubmit("0", mapping, false, false); c != menuUserSettings {
		t.Errorf(`classify "0" (non-admin) = %v, want menuUserSettings`, c)
	}
	if c, _ := classifyMenuSubmit("0", mapping, true, false); c != menuUserSettings {
		t.Errorf(`classify "0" (admin) = %v, want menuUserSettings`, c)
	}
	if c, _ := classifyMenuSubmit("0", map[string]store.Service{}, false, false); c != menuUserSettings {
		t.Errorf(`classify "0" (empty mapping) = %v, want menuUserSettings`, c)
	}
```

> Verify with `grep -rn "classifyMenuSubmit(" internal/server/` that the only remaining 3-arg call is none — all callers now pass 4 args.

- [ ] **Step 8: Run the server package tests**

Run: `go test ./internal/server/ -run ClassifyMenuSubmitLocked -v`
Expected: PASS.

Run: `go test ./internal/server/`
Expected: PASS (the signature change compiles everywhere; existing menu tests still pass — `settingsLocked=false` preserves behavior).

- [ ] **Step 9: Commit**

```bash
git add internal/server/presenter.go internal/server/session.go internal/server/session_test.go internal/server/presenter_test.go
git commit -m "feat(server): reject/hide menu option 0 for locked users (#86)"
```

---

## Task 4: `mfaGate` — skip enrollment when locked

**Files:**
- Modify: `internal/server/session.go` (`mfaGate` ~518-538)
- Test: `internal/server/session_mfa_test.go` (new test)

- [ ] **Step 1: Write the failing test**

Append to `internal/server/session_mfa_test.go`:

```go
// TestMFAGateLockedSkipsEnrollment asserts that a user_settings_locked account
// never enters the enrollment branch (forced or otherwise), but a stored secret
// is still verified at login.
func TestMFAGateLockedSkipsEnrollment(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	good, err := hotp.GenerateCodeCustom(secret, step, hotp.ValidateOpts{
		Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("locked_required_no_secret_no_enroll", func(t *testing.T) {
		p := &fakePresenter{
			termType:  "IBM-3278-2-E",
			logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
			menuPicks: []menuResult{{quit: true}},
		}
		b := &fakeBridger{}
		s, st := newMFATestSession(t, p, b)
		ctx := context.Background()
		uid, _ := st.CreateUser(ctx, "alice", "x")
		st.SetMFARequired(ctx, uid, true)
		st.SetUserSettingsLocked(ctx, uid, true)

		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		s.Run(client)

		if len(p.enrollErrors) > 0 {
			t.Error("EnrollMFA must not be called for a locked account")
		}
		if len(p.menuPicks) != 0 {
			t.Error("session should have reached the menu (no MFA prompt)")
		}
	})

	t.Run("locked_with_secret_still_verifies", func(t *testing.T) {
		p := &fakePresenter{
			termType:  "IBM-3278-2-E",
			verifies:  []mfaResult{{code: good}},
			logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
			menuPicks: []menuResult{{quit: true}},
		}
		b := &fakeBridger{}
		s, st := newMFATestSession(t, p, b)
		ctx := context.Background()
		uid, _ := st.CreateUser(ctx, "alice", "x")
		st.SetUserSettingsLocked(ctx, uid, true)
		enc, err := s.MFA.Seal([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		st.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0)

		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		s.Run(client)

		if len(p.verifyErrors) == 0 {
			t.Error("VerifyMFA must still be called for a locked, enrolled account")
		}
		if len(p.menuPicks) != 0 {
			t.Error("session should have reached the menu after verify")
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run MFAGateLockedSkipsEnrollment -v`
Expected: FAIL on `locked_required_no_secret_no_enroll` — today the gate calls `mfaEnroll` (EnrollMFA), so the session blocks on an enroll screen with no queued code and the menu is never reached.

- [ ] **Step 3: Add the locked check to `mfaGate`**

In `internal/server/session.go`, change the `mfaEnroll` branch:

```go
	if u.MFASecret != "" {
		// Enrolled by ANY path (admin-required OR voluntary opt-in) → always
		// verify. Enforcement is secret-first, not mfa_required-gated.
		return s.mfaVerify(ctx, conn, term, u, aud)
	}
	if u.MFARequired && !u.UserSettingsLocked {
		// Required but not yet enrolled → force one-time enrollment. A locked
		// account is never force-enrolled: its MFA state is admin-managed, so
		// mfa_required is inert until an admin enrolls a secret pre-lock.
		return s.mfaEnroll(ctx, conn, term, u, aud)
	}
	return true, "", nil // no secret, or locked-and-unenrolled → no MFA
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/server/ -run MFAGateLockedSkipsEnrollment -v`
Expected: PASS (both subtests).

- [ ] **Step 5: Run the existing gate test for regression**

Run: `go test ./internal/server/ -run MFAGateSecretFirst -v`
Expected: PASS (unlocked behavior unchanged — all four original cases).

- [ ] **Step 6: Commit**

```bash
git add internal/server/session.go internal/server/session_mfa_test.go
git commit -m "feat(server): locked accounts skip MFA enrollment branch (#86)"
```

---

## Task 5: Admin edit-user form — lock toggle + audit

**Files:**
- Modify: `internal/screens/admin.go` (field-name constants ~40-42)
- Modify: `internal/server/admin_users.go` (form fields ~169-188; `userSaveEdit` ~247-271; new `applyLockEdit`)
- Test: `internal/server/admin_users_test.go` (new tests)

- [ ] **Step 1: Write the failing test**

Append to `internal/server/admin_users_test.go`:

```go
func TestApplyLockEditTogglesAndAudits(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	st.CreateUser(ctx, "guest", "h")
	u, _ := st.GetUserByUsername(ctx, "guest")

	var kinds []string
	f := &adminFlow{
		store:    st,
		identity: auth.Identity{UserID: 99, Username: "admin"},
		audit:    func(_ context.Context, e store.AuditEvent) { kinds = append(kinds, e.Kind) },
	}

	// Lock.
	if msg, err := f.applyLockEdit(ctx, u, map[string]string{
		screens.FieldUserSettingsLocked: "Y",
	}); err != nil || msg != "" {
		t.Fatalf("lock: msg=%q err=%v", msg, err)
	}
	u, _ = st.GetUserByUsername(ctx, "guest")
	if !u.UserSettingsLocked {
		t.Fatal("expected locked")
	}

	// Unlock.
	if msg, err := f.applyLockEdit(ctx, u, map[string]string{
		screens.FieldUserSettingsLocked: "N",
	}); err != nil || msg != "" {
		t.Fatalf("unlock: msg=%q err=%v", msg, err)
	}
	u, _ = st.GetUserByUsername(ctx, "guest")
	if u.UserSettingsLocked {
		t.Fatal("expected unlocked")
	}

	if len(kinds) != 2 || kinds[0] != store.AuditSettingsLocked || kinds[1] != store.AuditSettingsUnlocked {
		t.Fatalf("audit kinds = %v", kinds)
	}
}

func TestApplyLockEditRejectsBadValue(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/s.db")
	defer st.Close()
	ctx := context.Background()
	st.CreateUser(ctx, "guest", "h")
	u, _ := st.GetUserByUsername(ctx, "guest")
	f := &adminFlow{store: st, identity: auth.Identity{UserID: 99, Username: "admin"}}
	if msg, err := f.applyLockEdit(ctx, u, map[string]string{
		screens.FieldUserSettingsLocked: "x",
	}); err != nil || msg == "" {
		t.Fatalf("want rejection message, got msg=%q err=%v", msg, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run ApplyLockEdit -v`
Expected: FAIL — `screens.FieldUserSettingsLocked` and `f.applyLockEdit` undefined (compile error).

- [ ] **Step 3: Add the field-name constant**

In `internal/screens/admin.go`, after `FieldMFAClear`:

```go
	FieldMFAClear           = "mfaclear"        // edit-user form: Y wipes the secret
	FieldUserSettingsLocked = "usettingslocked" // edit-user form: Y locks self-service
```

- [ ] **Step 4: Add `applyLockEdit` and wire it into `userSaveEdit`**

In `internal/server/admin_users.go`, add the method (next to `applyMFAEdit`):

```go
// applyLockEdit applies the "User Settings Locked" toggle from the edit form. It
// audits the transition (locked/unlocked) and returns an error-line message ("" on
// success).
func (f *adminFlow) applyLockEdit(ctx context.Context, u store.User, vals map[string]string) (string, error) {
	var locked bool
	switch strings.ToUpper(strings.TrimSpace(vals[screens.FieldUserSettingsLocked])) {
	case "Y":
		locked = true
	case "N", "":
		locked = false
	default:
		return "LOCK SELF MUST BE Y OR N", nil
	}
	if locked == u.UserSettingsLocked {
		return "", nil
	}
	if err := f.store.SetUserSettingsLocked(ctx, u.ID, locked); err != nil {
		return f.storeErr("set settings lock", err), nil
	}
	if f.audit != nil {
		kind := store.AuditSettingsUnlocked
		if locked {
			kind = store.AuditSettingsLocked
		}
		f.audit(ctx, store.AuditEvent{Kind: kind, Username: u.Username})
	}
	return "", nil
}
```

In `userSaveEdit`, after the `applyMFAEdit` block (~line 268) and before `recordAdmin`:

```go
	if msg, err := f.applyMFAEdit(ctx, u, vals); err != nil {
		return f.storeErr("apply mfa", err), nil
	} else if msg != "" {
		return msg, nil
	}
	if msg, err := f.applyLockEdit(ctx, u, vals); err != nil {
		return f.storeErr("apply lock", err), nil
	} else if msg != "" {
		return msg, nil
	}
	f.recordAdmin(ctx, "user edit "+u.Username)
	return "", nil
```

- [ ] **Step 5: Add the form field and the inert-MFA hint**

In `internal/server/admin_users.go`, inside the `if !create {` block, after the MFA fields append (~line 187), add:

```go
		lock := "N"
		if u.UserSettingsLocked {
			lock = "Y"
		}
		fields = append(fields,
			ui3270.FormField{Name: screens.FieldUserSettingsLocked, Label: "Lock self Y.", Length: 1, Value: lock},
		)
		// Hint: a locked account with no secret makes mfa_required a no-op.
		if u.UserSettingsLocked && u.MFASecret == "" {
			fields = append(fields,
				ui3270.FormField{Name: "lockhint", Label: "Note . . . .", Length: 40,
					Value: "MFA REQ INERT WHILE LOCKED W/O SECRET", ReadOnly: true},
			)
		}
```

> `lockhint` is a display-only field (`ReadOnly`); its `Name` is never read back from `vals`. Exact on-screen wording/placement is finalized at the emulator pass.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/server/ -run ApplyLockEdit -v`
Expected: PASS (both tests).

Run: `go test ./internal/server/`
Expected: PASS (existing admin/user tests unaffected).

- [ ] **Step 7: Commit**

```bash
git add internal/screens/admin.go internal/server/admin_users.go internal/server/admin_users_test.go
git commit -m "feat(server): admin edit-user lock toggle + audit (#86)"
```

---

## Task 6: Seed — declarative `user_settings_locked`

**Files:**
- Modify: `internal/seed/seed.go` (`SeedUser` ~34-38; user-create loop ~109-127)
- Test: `internal/seed/seed_test.go` (new test)

- [ ] **Step 1: Write the failing test**

Append to `internal/seed/seed_test.go`:

```go
func TestApplySeedsUserSettingsLocked(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	data := SeedData{
		Users: []SeedUser{
			{Username: "guest", Password: "guestpassword", UserSettingsLocked: true},
			{Username: "normal", Password: "normalpassword"},
		},
	}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatalf("apply: %v", err)
	}

	g, _ := st.GetUserByUsername(ctx, "guest")
	if !g.UserSettingsLocked {
		t.Error("guest should be locked")
	}
	n, _ := st.GetUserByUsername(ctx, "normal")
	if n.UserSettingsLocked {
		t.Error("normal user should default unlocked")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/seed/ -run UserSettingsLocked -v`
Expected: FAIL — `SeedUser` has no field `UserSettingsLocked` (compile error).

- [ ] **Step 3: Add the field and apply it**

In `internal/seed/seed.go`, add to `SeedUser`:

```go
type SeedUser struct {
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	Groups             []string `json:"groups"`
	UserSettingsLocked bool     `json:"user_settings_locked"` // omitted → unlocked
}
```

In the user-create loop, after the group memberships are linked (after the inner `for _, g := range u.Groups` block, ~line 126), apply the flag:

```go
		for _, g := range u.Groups {
			gid, err := ensureGroup(g)
			if err != nil {
				return err
			}
			if err := st.AddUserToGroup(ctx, uid, gid); err != nil {
				return fmt.Errorf("add %q to %q: %w", u.Username, g, err)
			}
		}
		if u.UserSettingsLocked {
			if err := st.SetUserSettingsLocked(ctx, uid, true); err != nil {
				return fmt.Errorf("lock %q: %w", u.Username, err)
			}
		}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/seed/ -run UserSettingsLocked -v`
Expected: PASS.

Run: `go test ./internal/seed/`
Expected: PASS (existing seed tests unaffected; the new field defaults false).

- [ ] **Step 5: Commit**

```bash
git add internal/seed/seed.go internal/seed/seed_test.go
git commit -m "feat(seed): user_settings_locked field for declarative guest provisioning (#86)"
```

---

## Task 7: Docs — CLAUDE.md package-map notes

**Files:**
- Modify: `CLAUDE.md` (intro paragraph; `internal/store`, `internal/screens`, `internal/server` package-map entries)

- [ ] **Step 1: Update the intro and package map**

In `CLAUDE.md`, in the opening "What this is" paragraph, after the User Settings sentence, add:

```
An admin can set a per-user **User Settings Locked** flag (`user_settings_locked`) on a
user (e.g. a shared/guest account): it hides the `0` self-service entry and freezes the
account's MFA as admin-managed — a stored secret is still verified at login, but the
account is never force-enrolled (so a shared login can't be hijacked into holding the only
TOTP).
```

In the `internal/store` entry, extend the MFA/users line to mention the new column and setter:

```
users also carry user_settings_locked (admin lock on self-service);
SetUserSettingsLocked toggles it.
```

In the `internal/screens` entry (MenuScreen description), note the new parameter:

```
MenuScreen takes settingsLocked: when set, the bottom-anchored `0 User Settings`
meta-row is omitted (the `A` admin row is unaffected).
```

In the `internal/server` entry (mfaGate description), add:

```
A user_settings_locked account skips the enrollment branch entirely (forced or
self-service); the verify branch is unchanged, and the menu/dispatch hide and reject `0`.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: user_settings_locked in CLAUDE.md package map (#86)"
```

---

## Task 8: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 2: Full test suite with the race detector**

Run: `go test ./... -race`
Expected: all packages PASS (the bridge/session are concurrent — race must be clean).

- [ ] **Step 3: Protocol smoke (s3270)**

Invoke the `s3270-smoke-testing` skill. Beyond the standard checklist, confirm:
- a normal (unlocked) user still sees the `0 User Settings` row and can open it;
- a locked user (set one via the admin form or seed) sees **no** `0` row and the menu/meta band still renders cleanly (cursor on the selection field; PF3=Logoff line intact);
- a locked **and MFA-enrolled** user is prompted for the code at login (verify branch) and still has no `0` row at the menu.

Expected: smoke script passes; the three lock-specific observations hold. Visual polish of the empty/`A`-only meta band is the human emulator-pass detail, not a blocker.

- [ ] **Step 4: Final branch state**

Run: `git status`
Expected: clean working tree; all task commits present. Ready to open the PR against `main` referencing #86.

---

## Self-Review notes (author)

- **Spec coverage:** data model (T1), audit (T2), menu hide (T3), server-side reject + thread (T3b), mfaGate freeze (T4), admin form + hint (T5), seed (T6), docs (T7), smoke (T8). All spec sections map to a task.
- **Type consistency:** `UserSettingsLocked` (struct), `SetUserSettingsLocked` (store), `FieldUserSettingsLocked` (screens), `settingsLocked` (param), `AuditSettingsLocked`/`AuditSettingsUnlocked` (kinds), `applyLockEdit` (admin), `SeedUser.UserSettingsLocked` (seed) — names used identically across tasks.
- **Signature ripple:** `MenuScreen` (Task 3) and `Presenter.Menu`/`classifyMenuSubmit` (Task 3b) are breaking arity changes; each task includes the call-site updates (test files + fake presenter) so each commit compiles.
- **No silent caps:** `MenuCapacity` is deliberately left unchanged for locked users (one blank reserved row) — noted in Task 3, not a hidden truncation.
