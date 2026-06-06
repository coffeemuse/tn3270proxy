# Self-Service User Settings (menu option `0`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a self-service `0 User Settings` menu entry letting any authenticated user change their own password and manage their own MFA (enroll / re-enroll / disable), and make login MFA enforcement secret-first so opt-in MFA is actually challenged.

**Architecture:** Two PRs. **PR1** reorders `Session.mfaGate` to enforce on secret presence (not the `mfa_required` flag) so a voluntarily-enrolled user is verified at login. **PR2** adds a `0` menu entry routed through a new `menuChoice`, a self-scoped `userSettings` flow (Session methods, testable with the existing fakes) rendering an adaptive menu, a self change-password form reusing `ui3270.RunForm`/`passwordFromForm`, and MFA enroll/re-enroll/disable that reuse the factored enrollment loop. All failure paths fold into the existing #48 throttle. Audit per Option B: new `password_self`; reused `mfa_enrolled`; reused `mfa_cleared`+`Detail="self-service"`.

**Tech Stack:** Go, `racingmars/go3270`, `internal/server` session state machine, `internal/screens` pure builders, `internal/ui3270` form/list renderer, `internal/store` (modernc SQLite), `internal/mfa` (TOTP + AES-GCM).

**Spec:** `docs/superpowers/specs/2026-06-06-self-service-user-settings-design.md`
**Issue:** #63. **Follow-up spun off:** #73 (audit actor/subject conflation).

---

## File Structure

**PR1 — secret-first gate**
- Modify: `internal/server/session.go` (`mfaGate`, lines ~492-508).
- Test: `internal/server/session_mfa_test.go` (add gate-routing tests; create if absent — check first).
- Modify: `CLAUDE.md` (one-line invariant in the MFA section).

**PR2 — User Settings**
- Modify: `internal/server/presenter.go` — extend `menuChoice`; teach `classifyMenuSubmit` about `"0"`; change `Menu` return to `(*store.Service, menuChoice, error)`; add `go3270Presenter.UserSettings`.
- Modify: `internal/server/session.go` — `Presenter` interface (`Menu` sig + new `UserSettings`); menu dispatch switch; factor `confirmEnroll` out of `mfaEnroll`; add `userSettings`, `changePassword`, `selfMFAEnroll`, `selfMFADisable`, `stepUpPassword`, `userSettingsActions` and the renderer helper.
- Create: `internal/screens/usersettings.go` — `UserSettingsRow`, `UserSettingsScreen`, `FieldUSOption`, `FieldCurrentPassword`.
- Test: `internal/screens/usersettings_test.go`.
- Modify: `internal/screens/menu.go` — render the `0 User Settings` meta-row.
- Modify: `internal/screens/geometry.go` — `MenuCapacity` reserves the always-present `0` row.
- Modify: `internal/store/audit.go` — add `AuditPasswordSelf`.
- Modify: `internal/server/session_test.go` — update `fakePresenter.Menu`/`menuResult` for the new return; add `UserSettings` to the fake.
- Test: `internal/server/usersettings_test.go` (new) — flow tests.
- Modify: `CLAUDE.md` package map (note the new flow + secret-first gate).

---

# PR1 — Secret-first MFA gate

### Task 1: Reorder `mfaGate` to enforce on secret presence

**Files:**
- Modify: `internal/server/session.go:492-508` (`mfaGate`)
- Test: `internal/server/session_mfa_test.go`

The current gate returns early on `!u.MFARequired`, so an opt-in secret is never verified. Make it secret-first.

- [ ] **Step 1: Check for an existing gate test file and read existing gate tests**

Run: `ls internal/server/session_mfa_test.go 2>/dev/null && grep -rn "mfaGate\|TestMFAGate\|func.*mfaGate" internal/server/*_test.go`
If a gate test exists, add the new cases there; otherwise create `internal/server/session_mfa_test.go`. Note the helper used to build a `*Session` with fakes (reuse the existing pattern — look at how other `session_test.go` tests construct `Session` with `fakePresenter`, an in-memory `*store.Store`, `MFA`, `Now`, `Throttle`).

- [ ] **Step 2: Write the failing test — secret + not-required must verify**

Add to `internal/server/session_mfa_test.go`. This is the headline regression: today this case wrongly passes the gate.

```go
func TestMFAGateSecretFirst(t *testing.T) {
	// table: each row sets (required, enrolled) and asserts which presenter
	// method the gate drives. "enrolled" means a real sealed secret is stored.
	cases := []struct {
		name           string
		required       bool
		enrolled       bool
		wantVerify     bool // expect VerifyMFA called
		wantEnroll     bool // expect EnrollMFA called
		wantProceedNow bool // expect gate to proceed with no MFA screen
	}{
		{"optin_secret_not_required", false, true, true, false, false},
		{"required_and_enrolled", true, true, true, false, false},
		{"required_pending", true, false, false, true, false},
		{"neither", false, false, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, fake := newMFAGateSession(t, tc.required, tc.enrolled) // helper: builds Session+fakePresenter, stores user
			// queue a correct code so verify/enroll succeeds if reached
			fake.verifies = []mfaResult{{code: "000000"}}
			fake.enrolls = []mfaResult{{code: "000000"}}
			aud := newAuditTrail(nil, "", "") // nil auditor → no-op; match the constructor used elsewhere
			proceed, _, err := s.mfaGate(context.Background(), nil, Term{Rows: 24, Cols: 80}, identityFor("ALICE"), aud)
			if err != nil {
				t.Fatalf("mfaGate err: %v", err)
			}
			if !proceed {
				t.Fatalf("expected proceed=true")
			}
			gotVerify := len(fake.verifyErrors) > 0
			gotEnroll := len(fake.enrollErrors) > 0
			if gotVerify != tc.wantVerify {
				t.Errorf("verify called=%v want %v", gotVerify, tc.wantVerify)
			}
			if gotEnroll != tc.wantEnroll {
				t.Errorf("enroll called=%v want %v", gotEnroll, tc.wantEnroll)
			}
			if tc.wantProceedNow && (gotVerify || gotEnroll) {
				t.Errorf("expected no MFA screen, got verify=%v enroll=%v", gotVerify, gotEnroll)
			}
		})
	}
}
```

> Implementer note: `newMFAGateSession`, `identityFor`, and the exact `newAuditTrail` constructor are repo helpers — reuse the real ones from `session_test.go`/`session_mfa_test.go`. If a helper does not exist, write a minimal one mirroring existing gate/login tests (deterministic `Now`, a TOTP secret sealed with the test `*mfa.Cipher`, a stored user with `MFARequired`/`MFASecret` set per the case). For the `enrolled` cases, generate a code valid at `Now` with `mfa.Validate`-compatible generation already used by other MFA tests.

- [ ] **Step 3: Run it to confirm the opt-in case fails**

Run: `go test ./internal/server/ -run TestMFAGateSecretFirst -v`
Expected: FAIL on `optin_secret_not_required` (today the gate skips verify when `!required`), others pass.

- [ ] **Step 4: Reorder the gate**

In `internal/server/session.go`, replace the body of `mfaGate` after the user load:

```go
	if !u.MFARequired {
		return true, "", nil
	}
	if u.MFASecret == "" {
		return s.mfaEnroll(ctx, conn, term, u, aud)
	}
	return s.mfaVerify(ctx, conn, term, u, aud)
```

with:

```go
	if u.MFASecret != "" {
		// Enrolled by ANY path (admin-required OR voluntary opt-in) → always
		// verify. Enforcement is secret-first, not mfa_required-gated: a stored
		// secret means the user opted into MFA and must be challenged.
		return s.mfaVerify(ctx, conn, term, u, aud)
	}
	if u.MFARequired {
		// Required but not yet enrolled → force one-time enrollment.
		return s.mfaEnroll(ctx, conn, term, u, aud)
	}
	return true, "", nil // no secret, not required → no MFA
```

- [ ] **Step 5: Run the full server suite with race**

Run: `go test ./internal/server/ -race`
Expected: PASS (the new test passes; existing gate/login/MFA tests stay green — required+enrolled still verifies, required+pending still enrolls).

- [ ] **Step 6: Add the invariant to CLAUDE.md**

In `CLAUDE.md`, in the `internal/server` MFA-gate paragraph, append one sentence:

```
Login enforcement is secret-first: any stored secret is verified at login regardless of mfa_required (so opt-in MFA is enforced; demoting required=false on an enrolled user keeps verifying until the secret is cleared).
```

- [ ] **Step 7: Commit (PR1)**

```bash
git add internal/server/session.go internal/server/session_mfa_test.go CLAUDE.md
git commit -m "feat(server): secret-first MFA login gate (GH #63)

Verify whenever a stored secret exists, regardless of mfa_required, so
voluntarily-enrolled (opt-in) users are challenged at login. Required+pending
still force-enrolls; no-secret/not-required still passes."
```

> PR1 ships here as its own pull request. PR2 builds on it.

---

# PR2 — Self-service User Settings

### Task 2: Extend `menuChoice` and teach `classifyMenuSubmit` about `"0"`

**Files:**
- Modify: `internal/server/presenter.go:37-61`
- Test: `internal/server/presenter_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/presenter_test.go` (mirror the existing `classifyMenuSubmit` table test at line ~60):

```go
func TestClassifyMenuSubmitUserSettings(t *testing.T) {
	mapping := map[string]store.Service{"1": {Name: "SVC"}}
	if c, _ := classifyMenuSubmit("0", mapping, false); c != menuUserSettings {
		t.Errorf(`classify "0" (non-admin) = %v, want menuUserSettings`, c)
	}
	if c, _ := classifyMenuSubmit("0", mapping, true); c != menuUserSettings {
		t.Errorf(`classify "0" (admin) = %v, want menuUserSettings`, c)
	}
	if c, _ := classifyMenuSubmit("0", map[string]store.Service{}, false); c != menuUserSettings {
		t.Errorf(`classify "0" (empty mapping) = %v, want menuUserSettings`, c)
	}
}
```

- [ ] **Step 2: Run to confirm it fails to compile**

Run: `go test ./internal/server/ -run TestClassifyMenuSubmitUserSettings`
Expected: FAIL — `menuUserSettings` undefined.

- [ ] **Step 3: Add the enum values and the classify branch**

In `internal/server/presenter.go`, extend the const block:

```go
const (
	menuReprompt     menuChoice = iota // invalid key, services available — show inline error
	menuRequery                        // no selectable entries — return nil so session re-queries
	menuAdmin                          // admin "A" entry selected
	menuService                        // valid service key selected
	menuUserSettings                   // "0" user-settings entry selected
	menuQuit                           // PF3 at the menu — logoff
)
```

In `classifyMenuSubmit`, add the `"0"` branch after the admin check (before the mapping lookup):

```go
	if admin && key == "A" {
		return menuAdmin, store.Service{}
	}
	if key == "0" {
		return menuUserSettings, store.Service{}
	}
```

- [ ] **Step 4: Run to confirm pass**

Run: `go test ./internal/server/ -run TestClassifyMenuSubmitUserSettings -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/presenter.go internal/server/presenter_test.go
git commit -m "feat(server): classify menu '0' as user-settings choice (GH #63)"
```

### Task 3: Change `Menu` return to `(*store.Service, menuChoice, error)`

**Files:**
- Modify: `internal/server/presenter.go:95-127` (`go3270Presenter.Menu`)
- Modify: `internal/server/session.go:50-64` (`Presenter` interface) and `:349-437` (dispatch)
- Modify: `internal/server/session_test.go:88-107` (`menuResult` + `fakePresenter.Menu`)

This is a pure refactor of an existing return shape plus routing the new choice. Tests gate it.

- [ ] **Step 1: Update the `Presenter` interface signature**

In `internal/server/session.go`, change the `Menu` line in the `Presenter` interface to:

```go
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, status screens.MenuStatus, errMsg string) (selected *store.Service, choice menuChoice, err error)
```

- [ ] **Step 2: Rewrite `go3270Presenter.Menu`**

Replace `internal/server/presenter.go:95-127` with:

```go
func (go3270Presenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, status screens.MenuStatus, errMsg string) (*store.Service, menuChoice, error) {
	geom := term.Geometry()
	status.TermType = term.Type // presenter owns the terminal-derived field
	for {
		status.Now = time.Now() // paint-time clock, refreshed every render
		screen, mapping, cur := screens.MenuScreen(geom, svcs, admin, status, errMsg)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
			)
		})
		if err != nil {
			return nil, menuReprompt, err // choice ignored on error
		}
		if resp.AID == go3270.AIDPF3 {
			return nil, menuQuit, nil
		}
		key := strings.ToUpper(strings.TrimSpace(resp.Values[screens.FieldSelection]))
		switch choice, svc := classifyMenuSubmit(key, mapping, admin); choice {
		case menuAdmin:
			return nil, menuAdmin, nil
		case menuUserSettings:
			return nil, menuUserSettings, nil
		case menuService:
			return &svc, menuService, nil
		case menuRequery:
			return nil, menuRequery, nil
		default: // menuReprompt
			errMsg = "Invalid selection: " + key
		}
	}
}
```

- [ ] **Step 3: Update the `fakePresenter` in tests**

In `internal/server/session_test.go`, change `menuResult` and `fakePresenter.Menu`:

```go
type menuResult struct {
	sel    *store.Service
	choice menuChoice
	quit   bool // convenience: when true, choice is forced to menuQuit
	err    error
}
```

```go
func (f *fakePresenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, status screens.MenuStatus, errMsg string) (*store.Service, menuChoice, error) {
	f.gotTerms = append(f.gotTerms, term)
	f.menuErrors = append(f.menuErrors, errMsg)
	f.gotAdminFlag = append(f.gotAdminFlag, admin)
	f.gotStatus = append(f.gotStatus, status)
	r := f.menuPicks[0]
	f.menuPicks = f.menuPicks[1:]
	ch := r.choice
	if r.quit {
		ch = menuQuit
	}
	return r.sel, ch, r.err
}
```

- [ ] **Step 4: Migrate existing `menuPicks` literals**

Run: `grep -rn "menuResult{" internal/server/*_test.go`
For each existing literal, translate the old `(sel, admin, quit)` fields to the new shape:
- a bridged service pick `{sel: &svc}` → `{sel: &svc, choice: menuService}`
- an admin pick `{admin: true}` → `{choice: menuAdmin}`
- a logoff `{quit: true}` → `{quit: true}` (unchanged; the fake maps it to `menuQuit`)
- a re-query `{}` (returned nil to re-query) → `{choice: menuRequery}`

Edit each occurrence to match. (The `admin bool` field on `menuResult` is removed; the compiler will flag every stale literal.)

- [ ] **Step 5: Rewrite the session dispatch switch**

In `internal/server/session.go`, replace the block from `selected, adminSel, quit, err := s.Presenter.Menu(...)` (line ~362) through the end of the bridge handling. Replace lines 362-412 with:

```go
			selected, choice, err := s.Presenter.Menu(conn, term, services, isAdmin, status, errMsg)
			if err != nil {
				if isTimeoutErr(err) {
					aud.record(ctx, store.AuditEvent{
						Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
					currentUser = ""
					s.Logger = baseLog // revert to pre-user logger
					s.armPreAuth(conn)
					break menu
				}
				endDetail = "menu render error"
				return
			}
			errMsg = ""
			switch choice {
			case menuQuit:
				aud.record(ctx, store.AuditEvent{
					Kind: store.AuditLogout, Username: identity.Username, Detail: "user logoff"})
				currentUser = ""
				s.Logger = baseLog
				s.armPreAuth(conn)
				break menu
			case menuAdmin:
				if !isAdmin {
					continue // guard: a buggy presenter can't open admin for a non-admin
				}
				renderer := func(conn net.Conn) ui3270.Renderer {
					if s.AdminRenderer != nil {
						return s.AdminRenderer(conn, term)
					}
					return ui3270.NewGo3270Renderer(conn, term.dev, term.codepage(), term.Rows)
				}
				flow := &adminFlow{store: s.Store, presenter: s.AdminPresenter,
					renderer: renderer,
					identity: identity, term: term, audit: aud.record,
					logger: s.log()}
				if aerr := flow.Run(ctx, conn); aerr != nil {
					if isTimeoutErr(aerr) {
						aud.record(ctx, store.AuditEvent{
							Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
						currentUser = ""
						s.Logger = baseLog
						s.armPreAuth(conn)
						break menu
					}
					s.log().Error("admin flow error", "error", aerr)
					endDetail = "admin flow error"
					return
				}
				continue
			case menuUserSettings:
				if uerr := s.userSettings(ctx, conn, term, identity, aud); uerr != nil {
					if isTimeoutErr(uerr) {
						aud.record(ctx, store.AuditEvent{
							Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
						currentUser = ""
						s.Logger = baseLog
						s.armPreAuth(conn)
						break menu
					}
					s.log().Error("user settings flow error", "error", uerr)
					endDetail = "user settings flow error"
					return
				}
				continue
			case menuService:
				// falls through to the bridge block below
			default: // menuRequery / menuReprompt
				continue
			}
			if selected == nil {
				continue
			}
```

Note: the `break menu` statements inside a `switch` must break the labeled `menu` loop — they already reference the `menu:` label, so they break the loop, not the switch. Keep the bridge block (lines ~414-437, `addr := net.JoinHostPort(...)` through `s.armPostAuth(conn)`) unchanged immediately after this.

- [ ] **Step 6: Add a stub `userSettings` so it compiles (filled in Task 8)**

Temporarily add to `internal/server/session.go` (replaced in Task 8):

```go
func (s *Session) userSettings(ctx context.Context, conn net.Conn, term Term, identity auth.Identity, aud *auditTrail) error {
	return nil
}
```

- [ ] **Step 7: Add `UserSettings` to the `Presenter` interface + fake stub**

In the `Presenter` interface (`session.go`), add (real impl in Task 6, fake in Task 7 — stub now to compile):

```go
	// UserSettings renders the self-service settings menu with the given
	// adaptive rows and returns the typed option key (e.g. "1"); back=true on
	// PF3 (return to the service menu). It loops internally on invalid input.
	UserSettings(conn net.Conn, term Term, username string, rows []screens.UserSettingsRow, errMsg string) (choice string, back bool, err error)
```

Add a temporary stub to `go3270Presenter` and `fakePresenter` so the package compiles (both replaced later):

```go
// presenter.go
func (go3270Presenter) UserSettings(conn net.Conn, term Term, username string, rows []screens.UserSettingsRow, errMsg string) (string, bool, error) {
	return "", true, nil
}
```
```go
// session_test.go
func (f *fakePresenter) UserSettings(conn net.Conn, term Term, username string, rows []screens.UserSettingsRow, errMsg string) (string, bool, error) {
	return "", true, nil
}
```

> `screens.UserSettingsRow` does not exist yet — Task 4 creates it. Do Task 4 before compiling this task, or temporarily type `rows []struct{ Key, Label string }` and fix the import after Task 4. Recommended order: Task 4 first, then 3. (Listed in dispatch order for readability; the executor should build Task 4's type before compiling Task 3.)

- [ ] **Step 8: Build and run the suite**

Run: `go build ./... && go test ./internal/server/ -race`
Expected: PASS — the refactor preserves behavior; `userSettings` is a no-op stub, `UserSettings` returns back.

- [ ] **Step 9: Commit**

```bash
git add internal/server/presenter.go internal/server/session.go internal/server/session_test.go
git commit -m "refactor(server): Menu returns menuChoice; route user-settings + add seams (GH #63)"
```

### Task 4: `screens.UserSettingsScreen` + row type + field constants

**Files:**
- Create: `internal/screens/usersettings.go`
- Test: `internal/screens/usersettings_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/screens/usersettings_test.go`:

```go
package screens

import "testing"

func TestUserSettingsScreen(t *testing.T) {
	rows := []UserSettingsRow{{Key: "1", Label: "Change Password"}, {Key: "2", Label: "Enroll in MFA"}}
	screen, cur := UserSettingsScreen(Geometry{}, "ALICE", rows, "")
	if _, ok := fieldByName(screen, FieldUSOption); !ok {
		t.Fatalf("missing %q field", FieldUSOption)
	}
	// rows render their key+label somewhere on the screen
	if !screenHasContent(screen, "Change Password") || !screenHasContent(screen, "Enroll in MFA") {
		t.Errorf("rows not rendered: %+v", screen)
	}
	opt, _ := fieldByName(screen, FieldUSOption)
	if cur.Row != opt.Row || cur.Col != opt.Col+1 {
		t.Errorf("cursor = %v, want one past the option field at (%d,%d)", cur, opt.Row, opt.Col)
	}
}
```

> `fieldByName` exists in the screens test helpers (used by `login_test.go`). `screenHasContent` may need adding — check; if absent, add a small helper that scans `screen` for a field whose `Content` contains the substring.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/screens/ -run TestUserSettingsScreen`
Expected: FAIL — undefined `UserSettingsRow`/`UserSettingsScreen`/`FieldUSOption`.

- [ ] **Step 3: Create the builder**

Create `internal/screens/usersettings.go`:

```go
package screens

import "github.com/racingmars/go3270"

// Field names for the self-service User Settings screens.
const (
	FieldUSOption        = "usoption"        // user-settings menu option input
	FieldCurrentPassword = "currentpassword" // self change-password / MFA step-up
)

// UserSettingsRow is one selectable row on the User Settings menu. Key is the
// digit the user types ("1".."3"); Label is the human description. The caller
// builds the ordered slice adaptively from the user's MFA state.
type UserSettingsRow struct {
	Key, Label string
}

// UserSettingsScreen renders the self-service settings menu sized for geom.
// The caller drives it with HandleScreenAlt: AIDEnter submits, PF3 returns to
// the service menu. rows are rendered in order from row 3 down.
func UserSettingsScreen(geom Geometry, username string, rows []UserSettingsRow, errMsg string) (go3270.Screen, Cursor) {
	option := go3270.Field{Row: geom.InputRow(), Col: 7, Name: FieldUSOption, Write: true, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: 0, Col: 25, Intense: true, Content: "TN3270 GATEWAY USER SETTINGS"},
		{Row: 1, Col: 2, Content: "User: " + username},
	}
	row := 3
	for _, r := range rows {
		screen = append(screen,
			go3270.Field{Row: row, Col: 4, Intense: true, Content: r.Key + "."},
			go3270.Field{Row: row, Col: 8, Color: go3270.Green, Content: r.Label},
		)
		row++
	}
	screen = append(screen,
		go3270.Field{Row: geom.InputRow(), Col: 2, Content: "===>"},
		option,
		go3270.Field{Row: geom.InputRow(), Col: 11}, // stop field
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: "PF3=Service Menu"},
	)
	return screen, cursorAt(option)
}
```

- [ ] **Step 4: Run to confirm pass**

Run: `go test ./internal/screens/ -run TestUserSettingsScreen -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/usersettings.go internal/screens/usersettings_test.go
git commit -m "feat(screens): UserSettingsScreen + row type + field constants (GH #63)"
```

### Task 5: Render the `0 User Settings` meta-row on the menu

**Files:**
- Modify: `internal/screens/menu.go:111-127` (meta-entry block)
- Modify: `internal/screens/geometry.go:68-74` (`MenuCapacity`)
- Test: `internal/screens/menu_test.go`, `internal/screens/geometry_test.go`

- [ ] **Step 1: Write the failing menu test**

Add to `internal/screens/menu_test.go`:

```go
func TestMenuShowsUserSettingsEntry(t *testing.T) {
	screen, _, _ := MenuScreen(Geometry{}, nil, false, MenuStatus{}, "")
	if !screenHasContent(screen, "User Settings") {
		t.Errorf("menu missing '0 User Settings' meta entry")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/screens/ -run TestMenuShowsUserSettingsEntry`
Expected: FAIL.

- [ ] **Step 3: Render the `0` row and adjust the admin row**

In `internal/screens/menu.go`, replace the `if admin { ... }` meta block (lines ~111-122) with:

```go
	// Bottom "meta" entries below the service list: User Settings (0) is shown
	// for every user; Administration (A) only for admins. Clamp so they never
	// overrun the input row (MenuCapacity reserved these rows when the list is
	// full).
	metaRow := row + 1
	lastMeta := geom.InputRow() - 2
	if admin {
		lastMeta-- // leave a row below "0" for the "A" entry
	}
	if metaRow > lastMeta {
		metaRow = lastMeta
	}
	screen = append(screen,
		go3270.Field{Row: metaRow, Col: 0, Intense: true, Content: "  0"},
		go3270.Field{Row: metaRow, Col: 13, Color: go3270.Green, Content: "User Settings"},
	)
	if admin {
		adminRow := metaRow + 1
		screen = append(screen,
			go3270.Field{Row: adminRow, Col: 0, Intense: true, Content: "  A"},
			go3270.Field{Row: adminRow, Col: 13, Color: go3270.Green, Content: "Administration"},
		)
	}
```

- [ ] **Step 4: Reserve a capacity row for the always-present `0`**

In `internal/screens/geometry.go`, change `MenuCapacity`:

```go
func (g Geometry) MenuCapacity(admin bool) int {
	n := g.norm().Rows - 11 // chrome + the always-present "0 User Settings" row
	if admin {
		n-- // plus the "A" row
	}
	return n
}
```

- [ ] **Step 5: Update the `MenuCapacity` geometry test**

Run: `grep -n "MenuCapacity" internal/screens/geometry_test.go`
Update the expected values: for a 24-row MOD2 the non-admin capacity is now `24-11 = 13` (was 14) and admin is `12` (was 13). Adjust every asserted value by −1 to match the reserved `0` row. If `menu_test.go` asserts a specific truncation count, decrement it too.

- [ ] **Step 6: Run the screens suite**

Run: `go test ./internal/screens/ -race`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/screens/menu.go internal/screens/geometry.go internal/screens/menu_test.go internal/screens/geometry_test.go
git commit -m "feat(screens): render '0 User Settings' menu meta-row (GH #63)"
```

### Task 6: Real `go3270Presenter.UserSettings`

**Files:**
- Modify: `internal/server/presenter.go` (replace the Task 3 stub)

- [ ] **Step 1: Replace the stub with the real renderer**

In `internal/server/presenter.go`, replace the temporary `UserSettings` stub with:

```go
func (go3270Presenter) UserSettings(conn net.Conn, term Term, username string, rows []screens.UserSettingsRow, errMsg string) (string, bool, error) {
	geom := term.Geometry()
	valid := make(map[string]bool, len(rows))
	for _, r := range rows {
		valid[r.Key] = true
	}
	for {
		screen, cur := screens.UserSettingsScreen(geom, username, rows, errMsg)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
			)
		})
		if err != nil {
			return "", false, err
		}
		if resp.AID == go3270.AIDPF3 {
			return "", true, nil
		}
		key := strings.TrimSpace(resp.Values[screens.FieldUSOption])
		if valid[key] {
			return key, false, nil
		}
		errMsg = "Invalid selection: " + key
	}
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/server/presenter.go
git commit -m "feat(server): go3270 UserSettings menu presenter (GH #63)"
```

### Task 7: Factor `confirmEnroll` out of `mfaEnroll`

**Files:**
- Modify: `internal/server/session.go:510-560` (`mfaEnroll`)
- Test: existing MFA enroll tests must stay green (regression gate)

This extracts the enrollment confirm-loop so both the login gate and self-service reuse it, with identical login-path behavior.

- [ ] **Step 1: Add `confirmEnroll` and slim `mfaEnroll`**

In `internal/server/session.go`, replace `mfaEnroll` (lines 510-560) with:

```go
// confirmEnroll runs the enroll confirm-loop for an already-generated secret:
// show the key, prompt for a code, and on a correct code seal+persist the
// secret (lastStep=0), reset the throttle, and audit mfa_enrolled. It does NOT
// arm any idle regime — the caller maps the outcome to its context. detail is
// set only on a fatal error (for the caller's disconnect audit).
func (s *Session) confirmEnroll(ctx context.Context, conn net.Conn, term Term, u store.User, secret string, aud *auditTrail) (confirmed bool, quit bool, detail string, err error) {
	chunked := mfa.Chunk(secret)
	issuer := s.mfaIssuer(ctx)
	errMsg := ""
	for {
		code, quit, err := s.Presenter.EnrollMFA(conn, term, issuer, u.Username, chunked, errMsg)
		if err != nil {
			return false, false, "mfa render error", err
		}
		if quit {
			return false, true, "", nil
		}
		ok, step, verr := mfa.Validate(secret, code, 0, s.now())
		if verr != nil {
			s.log().Error("mfa: validate error", "error", verr)
			errMsg = "Temporary error; try again"
			continue
		}
		if !ok {
			s.logAuthFailure(s.log(), "bad_mfa")
			delay, count := s.failDelay(ctx, u.Username)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditMFAFailed, Username: u.Username, Detail: throttleDetail("enroll", delay, count)})
			s.sleepFor(delay)
			errMsg = "Code incorrect - check the key and try again"
			continue
		}
		enc, serr := s.MFA.Seal([]byte(secret))
		if serr != nil {
			return false, false, "mfa seal error", serr
		}
		enrolledAt := s.now().UTC().Format(time.RFC3339)
		if err := s.Store.StoreMFAEnrollment(ctx, u.ID, enc, enrolledAt, int64(step)); err != nil {
			return false, false, "mfa store error", err
		}
		s.Throttle.Reset(u.Username)
		aud.record(ctx, store.AuditEvent{Kind: store.AuditMFAEnrolled, Username: u.Username})
		return true, false, "", nil
	}
}

func (s *Session) mfaEnroll(ctx context.Context, conn net.Conn, term Term, u store.User, aud *auditTrail) (bool, string, error) {
	issuer := s.mfaIssuer(ctx)
	secret, gerr := s.generateSecret(issuer, u.Username)
	if gerr != nil {
		s.log().Error("mfa: generate secret failed", "error", gerr)
		return false, "mfa generate error", gerr
	}
	_, quit, detail, err := s.confirmEnroll(ctx, conn, term, u, secret, aud)
	if err != nil {
		if isTimeoutErr(err) {
			aud.record(ctx, store.AuditEvent{Kind: store.AuditLogout, Username: u.Username, Detail: "idle logout"})
			s.armPreAuth(conn)
			return false, "", nil
		}
		return false, detail, err
	}
	if quit {
		s.armPreAuth(conn)
		return false, "", nil
	}
	return true, "", nil
}
```

- [ ] **Step 2: Run the MFA suite (regression gate)**

Run: `go test ./internal/server/ -race -run 'MFA|Enroll|Gate'`
Expected: PASS — login enroll behavior (idle logout audit, armPreAuth on quit, distinct fatal details) is unchanged.

- [ ] **Step 3: Commit**

```bash
git add internal/server/session.go
git commit -m "refactor(server): extract confirmEnroll for reuse by self-service (GH #63)"
```

### Task 8: The `userSettings` flow — adaptive menu + dispatch

**Files:**
- Modify: `internal/server/session.go` (replace the Task 3 `userSettings` stub; add helpers)
- Test: `internal/server/usersettings_test.go` (new)

- [ ] **Step 1: Write the failing flow test (adaptive rows + PF3 exit)**

Create `internal/server/usersettings_test.go`:

```go
package server

import (
	"context"
	"testing"
)

func TestUserSettingsRowsAdaptive(t *testing.T) {
	cases := []struct {
		name      string
		mfaCfg    bool
		required  bool
		enrolled  bool
		wantLabels []string
	}{
		{"mfa_off", false, false, false, []string{"Change Password"}},
		{"can_enroll", true, false, false, []string{"Change Password", "Enroll in MFA"}},
		{"enrolled_required", true, true, true, []string{"Change Password", "Re-enroll MFA"}},
		{"enrolled_optional", true, false, true, []string{"Change Password", "Re-enroll MFA", "Disable MFA"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := userSettingsActions(tc.mfaCfg, tc.required, tc.enrolled)
			if len(got) != len(tc.wantLabels) {
				t.Fatalf("actions=%d want %d (%v)", len(got), len(tc.wantLabels), got)
			}
			for i, a := range got {
				if usActionLabel(a) != tc.wantLabels[i] {
					t.Errorf("row %d label=%q want %q", i, usActionLabel(a), tc.wantLabels[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/server/ -run TestUserSettingsRowsAdaptive`
Expected: FAIL — `userSettingsActions`/`usActionLabel` undefined.

- [ ] **Step 3: Implement the actions model, the flow, and the renderer helper**

Add to `internal/server/session.go` (replace the stub `userSettings`):

```go
type usAction int

const (
	usChangePassword usAction = iota
	usEnroll
	usReenroll
	usDisable
)

func usActionLabel(a usAction) string {
	switch a {
	case usChangePassword:
		return "Change Password"
	case usEnroll:
		return "Enroll in MFA"
	case usReenroll:
		return "Re-enroll MFA"
	case usDisable:
		return "Disable MFA"
	}
	return ""
}

// userSettingsActions returns the ordered self-service actions for a user's
// current MFA state. mfaConfigured is s.MFA != nil.
func userSettingsActions(mfaConfigured, required, enrolled bool) []usAction {
	actions := []usAction{usChangePassword}
	if !mfaConfigured {
		return actions
	}
	if !enrolled {
		return append(actions, usEnroll)
	}
	actions = append(actions, usReenroll)
	if !required {
		actions = append(actions, usDisable) // can self-disable only voluntary MFA
	}
	return actions
}

// renderer builds the ui3270.Renderer for self-service forms, matching the
// admin dispatch (AdminRenderer is the generic factory; nil → go3270).
func (s *Session) renderer(conn net.Conn, term Term) ui3270.Renderer {
	if s.AdminRenderer != nil {
		return s.AdminRenderer(conn, term)
	}
	return ui3270.NewGo3270Renderer(conn, term.dev, term.codepage(), term.Rows)
}

// userSettings drives the self-service settings menu until the user leaves via
// PF3. It reloads the user each loop so the adaptive rows reflect just-applied
// changes (e.g. a fresh enrollment unlocks Re-enroll/Disable).
func (s *Session) userSettings(ctx context.Context, conn net.Conn, term Term, identity auth.Identity, aud *auditTrail) error {
	r := s.renderer(conn, term)
	for {
		u, err := s.Store.GetUserByUsername(ctx, identity.Username)
		if err != nil {
			return err
		}
		actions := userSettingsActions(s.MFA != nil, u.MFARequired, u.MFASecret != "")
		rows := make([]screens.UserSettingsRow, len(actions))
		for i, a := range actions {
			rows[i] = screens.UserSettingsRow{Key: strconv.Itoa(i + 1), Label: usActionLabel(a)}
		}
		choice, back, err := s.Presenter.UserSettings(conn, term, identity.Username, rows, "")
		if err != nil {
			return err
		}
		if back {
			return nil
		}
		idx, cerr := strconv.Atoi(choice)
		if cerr != nil || idx < 1 || idx > len(actions) {
			continue // presenter already re-prompts invalid keys; defensive
		}
		switch actions[idx-1] {
		case usChangePassword:
			if err := s.changePassword(ctx, r, identity, aud); err != nil {
				return err
			}
		case usEnroll, usReenroll:
			if err := s.selfMFAEnroll(ctx, conn, term, r, u, aud); err != nil {
				return err
			}
		case usDisable:
			if err := s.selfMFADisable(ctx, r, u, aud); err != nil {
				return err
			}
		}
	}
}
```

> `changePassword`, `selfMFAEnroll`, `selfMFADisable`, and `stepUpPassword` are added in Tasks 9-10. Add temporary stubs returning `nil` so this compiles, then replace them. (Recommended: implement 9-10 immediately after, in the same session.)

- [ ] **Step 4: Run to confirm pass**

Run: `go test ./internal/server/ -run TestUserSettingsRowsAdaptive -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/usersettings_test.go
git commit -m "feat(server): user-settings flow with adaptive MFA rows (GH #63)"
```

### Task 9: `stepUpPassword` + self change-password

**Files:**
- Modify: `internal/server/session.go` (replace `changePassword`/`stepUpPassword` stubs)
- Modify: `internal/store/audit.go` (add `AuditPasswordSelf`)
- Test: `internal/server/usersettings_test.go`

- [ ] **Step 1: Add the audit kind**

In `internal/store/audit.go`, add to the const block:

```go
	AuditPasswordSelf = "password_self" // user changed their own password (self-service)
```

- [ ] **Step 2: Write the failing test**

Add to `internal/server/usersettings_test.go`. Use the existing in-memory store + fake renderer helpers (look at `admin_test.go` for how a `ui3270.Renderer` fake (`fakeAdminPresenter` implements `List`/`Form`) is built, and how a test `*store.Store` with a seeded user is created; reuse those).

```go
func TestSelfChangePassword(t *testing.T) {
	st := newTestStore(t)               // existing helper
	uid := seedUser(t, st, "ALICE", "oldpass") // existing/seed helper; hashes "oldpass"
	s := newSelfServiceSession(t, st)   // helper: Session with s.Store=st, Throttle, Now, Authenticate=auth.Authenticate
	rec := &recordingAuditor{}          // captures audit events (reuse existing pattern)
	aud := newAuditTrail(rec, "sess", "1.2.3.4")

	form := &fakeFormRenderer{forms: []ui3270.FormAction{
		{Values: map[string]string{
			screens.FieldCurrentPassword: "oldpass",
			screens.FieldPassword:        "newpass",
			screens.FieldRetype:          "newpass",
		}},
	}}
	identity := auth.Identity{UserID: uid, Username: "ALICE"}
	if err := s.changePassword(context.Background(), form, identity, aud); err != nil {
		t.Fatalf("changePassword: %v", err)
	}
	// new password now authenticates; old does not
	if _, err := auth.Authenticate(context.Background(), st, "ALICE", "newpass"); err != nil {
		t.Errorf("new password should authenticate: %v", err)
	}
	if !rec.has(store.AuditPasswordSelf) {
		t.Errorf("expected %q audit; got %v", store.AuditPasswordSelf, rec.kinds())
	}
}
```

> `fakeFormRenderer` is a minimal `ui3270.Renderer` whose `Form` pops scripted `FormAction`s and whose `List` panics. If the repo already has such a fake (check `admin_test.go` — it uses `fakeAdminPresenter` with a real `ui3270`-backed flow via `AdminRenderer`), reuse the established mechanism: inject `s.AdminRenderer` returning a fake `Renderer`. Prefer the existing pattern over a new fake. `recordingAuditor`, `newTestStore`, `seedUser`, `newAuditTrail` are existing helpers — reuse; do not invent new names if equivalents exist.

- [ ] **Step 3: Run to confirm failure**

Run: `go test ./internal/server/ -run TestSelfChangePassword`
Expected: FAIL — `changePassword` is a stub.

- [ ] **Step 4: Implement `changePassword`**

Replace the `changePassword` stub in `internal/server/session.go`:

```go
// changePassword runs the self-service change-password form: re-verify the
// current password (proof of possession), enforce new != current, then persist.
func (s *Session) changePassword(ctx context.Context, r ui3270.Renderer, identity auth.Identity, aud *auditTrail) error {
	fields := []ui3270.FormField{
		{Name: screens.FieldCurrentPassword, Label: "Current pwd", Hidden: true, Length: 32},
		{Name: screens.FieldPassword, Label: "New pwd . .", Hidden: true, Length: 32},
		{Name: screens.FieldRetype, Label: "Retype  . .", Hidden: true, Length: 32},
	}
	return ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title:  "TN3270 GATEWAY: CHANGE PASSWORD",
		Fields: fields,
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			current := vals[screens.FieldCurrentPassword]
			if _, err := s.Authenticate(ctx, s.Store, identity.Username, current); err != nil {
				if errors.Is(err, auth.ErrInvalidCredentials) {
					delay, count := s.failDelay(ctx, identity.Username)
					s.logAuthFailure(s.log(), "invalid_credentials")
					aud.record(ctx, store.AuditEvent{
						Kind: store.AuditAuthFail, Username: identity.Username, Detail: throttleDetail("", delay, count)})
					s.sleepFor(delay)
					return "Current password is incorrect", nil
				}
				s.log().Error("self change-password auth error", "error", err)
				return "Temporary error; try again", nil
			}
			pass, _, msg := passwordFromForm(vals, true)
			if msg != "" {
				return msg, nil
			}
			if pass == current {
				return "New password must differ from current", nil
			}
			hash, err := auth.HashPassword(pass)
			if err != nil {
				s.log().Error("hash password", "error", err)
				return "Could not set password; try again", nil
			}
			if err := s.Store.SetPassword(ctx, identity.UserID, hash); err != nil {
				s.log().Error("set password", "error", err)
				return "Could not set password; try again", nil
			}
			s.Throttle.Reset(identity.Username)
			aud.record(ctx, store.AuditEvent{Kind: store.AuditPasswordSelf, Username: identity.Username})
			return "", nil // success → RunForm returns to the user-settings menu
		},
	})
}
```

- [ ] **Step 5: Implement `stepUpPassword` (used by Task 10)**

Add to `internal/server/session.go`:

```go
// stepUpPassword re-prompts for the current password and verifies it. ok=true
// means verified (proceed); ok=false with err=nil means the user cancelled
// (PF3). Failures fold into the shared throttle. Used as the step-up before
// MFA enroll/re-enroll/disable.
func (s *Session) stepUpPassword(ctx context.Context, r ui3270.Renderer, username string, aud *auditTrail) (bool, error) {
	ok := false
	err := ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title: "TN3270 GATEWAY: CONFIRM PASSWORD",
		Fields: []ui3270.FormField{
			{Name: screens.FieldCurrentPassword, Label: "Password . .", Hidden: true, Length: 32},
		},
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			if _, e := s.Authenticate(ctx, s.Store, username, vals[screens.FieldCurrentPassword]); e != nil {
				if errors.Is(e, auth.ErrInvalidCredentials) {
					delay, count := s.failDelay(ctx, username)
					s.logAuthFailure(s.log(), "invalid_credentials")
					aud.record(ctx, store.AuditEvent{
						Kind: store.AuditAuthFail, Username: username, Detail: throttleDetail("", delay, count)})
					s.sleepFor(delay)
					return "Password is incorrect", nil
				}
				s.log().Error("step-up auth error", "error", e)
				return "Temporary error; try again", nil
			}
			s.Throttle.Reset(username)
			ok = true
			return "", nil // verified → form returns
		},
	})
	return ok, err
}
```

- [ ] **Step 6: Run to confirm pass**

Run: `go test ./internal/server/ -run TestSelfChangePassword -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/server/session.go internal/store/audit.go internal/server/usersettings_test.go
git commit -m "feat(server): self change-password + password step-up (GH #63)"
```

### Task 10: MFA self-service — enroll / re-enroll / disable

**Files:**
- Modify: `internal/server/session.go` (replace `selfMFAEnroll`/`selfMFADisable` stubs)
- Test: `internal/server/usersettings_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/server/usersettings_test.go`:

```go
func TestSelfMFAEnroll(t *testing.T) {
	st := newTestStore(t)
	uid := seedUser(t, st, "ALICE", "pw")
	s := newSelfServiceSession(t, st) // includes a test *mfa.Cipher in s.MFA, deterministic Now + MFAGenerate
	rec := &recordingAuditor{}
	aud := newAuditTrail(rec, "sess", "")
	// step-up renderer accepts the password; EnrollMFA fake returns a valid code
	s.AdminRenderer = stepUpRendererAccepting(t, "pw") // helper: Form pops a FormAction with current pw = "pw"
	fp := s.Presenter.(*fakePresenter)
	fp.enrolls = []mfaResult{{code: validCodeForNewSecret(t, s)}} // helper computes code for the secret MFAGenerate will return

	u, _ := st.GetUserByUsername(context.Background(), "ALICE")
	if err := s.selfMFAEnroll(context.Background(), nil, Term{Rows: 24, Cols: 80}, s.renderer(nil, Term{Rows: 24, Cols: 80}), u, aud); err != nil {
		t.Fatalf("selfMFAEnroll: %v", err)
	}
	got, _ := st.GetUserByUsername(context.Background(), "ALICE")
	if got.MFASecret == "" {
		t.Errorf("expected a stored secret after enroll")
	}
	if !rec.has(store.AuditMFAEnrolled) {
		t.Errorf("expected mfa_enrolled audit; got %v", rec.kinds())
	}
	_ = uid
}

func TestSelfMFADisableClearsAndAudits(t *testing.T) {
	st := newTestStore(t)
	uid := seedEnrolledUser(t, st, "ALICE", "pw") // helper: stores a sealed secret, mfa_required=0
	s := newSelfServiceSession(t, st)
	s.AdminRenderer = stepUpRendererAccepting(t, "pw")
	rec := &recordingAuditor{}
	aud := newAuditTrail(rec, "sess", "")
	u, _ := st.GetUserByUsername(context.Background(), "ALICE")
	if err := s.selfMFADisable(context.Background(), s.renderer(nil, Term{Rows: 24, Cols: 80}), u, aud); err != nil {
		t.Fatalf("selfMFADisable: %v", err)
	}
	got, _ := st.GetUserByUsername(context.Background(), "ALICE")
	if got.MFASecret != "" {
		t.Errorf("expected secret cleared")
	}
	ev, ok := rec.find(store.AuditMFACleared)
	if !ok || ev.Detail != "self-service" {
		t.Errorf("expected mfa_cleared Detail=self-service; got %+v ok=%v", ev, ok)
	}
	_ = uid
}
```

> The helpers `newSelfServiceSession`, `stepUpRendererAccepting`, `validCodeForNewSecret`, `seedEnrolledUser`, `recordingAuditor.find/has/kinds` should reuse existing test infrastructure where it exists (the MFA tests already generate valid codes and seal secrets — reuse those exact helpers). Where a helper genuinely does not exist, add a minimal one alongside these tests. Keep `Now`/`MFAGenerate` deterministic so the code is reproducible.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/server/ -run 'TestSelfMFA'`
Expected: FAIL — stubs.

- [ ] **Step 3: Implement the MFA self-service actions**

Replace the `selfMFAEnroll`/`selfMFADisable` stubs in `internal/server/session.go`:

```go
// selfMFAEnroll handles both opt-in enroll and rotate/re-enroll: a current-
// password step-up, then the shared enrollment confirm-loop (fresh secret,
// lastStep=0, stored on a correct code). Cancelled step-up or enrollment
// returns to the user-settings menu.
func (s *Session) selfMFAEnroll(ctx context.Context, conn net.Conn, term Term, r ui3270.Renderer, u store.User, aud *auditTrail) error {
	ok, err := s.stepUpPassword(ctx, r, u.Username, aud)
	if err != nil {
		return err
	}
	if !ok {
		return nil // cancelled
	}
	secret, gerr := s.generateSecret(s.mfaIssuer(ctx), u.Username)
	if gerr != nil {
		s.log().Error("mfa: generate secret failed", "error", gerr)
		return gerr
	}
	_, _, _, cerr := s.confirmEnroll(ctx, conn, term, u, secret, aud)
	// confirmEnroll audits mfa_enrolled + resets throttle on success; on PF3
	// quit it returns (false,true,"",nil) → back to the menu. A render/idle
	// error propagates so the session classifies the timeout like admin flow.
	return cerr
}

// selfMFADisable removes a voluntarily-enrolled secret after a current-password
// step-up. Callers only surface this action when !MFARequired, but re-check
// defensively so an admin-required user can never self-disable.
func (s *Session) selfMFADisable(ctx context.Context, r ui3270.Renderer, u store.User, aud *auditTrail) error {
	if u.MFARequired {
		return nil // enforcement is admin-only; never self-disable a required user
	}
	ok, err := s.stepUpPassword(ctx, r, u.Username, aud)
	if err != nil {
		return err
	}
	if !ok {
		return nil // cancelled
	}
	if err := s.Store.ClearMFA(ctx, u.ID); err != nil {
		s.log().Error("clear mfa", "error", err)
		return err
	}
	aud.record(ctx, store.AuditEvent{
		Kind: store.AuditMFACleared, Username: u.Username, Detail: "self-service"})
	return nil
}
```

> Note: `selfMFAEnroll` takes `conn` (passed `nil` in tests, real conn in the flow). The Task 8 `userSettings` switch already calls `s.selfMFAEnroll(ctx, conn, term, r, u, aud)` and `s.selfMFADisable(ctx, r, u, aud)` — confirm those call sites match these signatures; adjust the Task 8 stub call if needed.

- [ ] **Step 4: Run the new tests + full suite**

Run: `go test ./internal/server/ -run 'TestSelfMFA' -v && go test ./internal/server/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/usersettings_test.go
git commit -m "feat(server): self-service MFA enroll/re-enroll/disable (GH #63)"
```

### Task 11: End-to-end menu→user-settings dispatch test

**Files:**
- Test: `internal/server/usersettings_test.go`

- [ ] **Step 1: Write the dispatch test**

Drive a full `Session.Run` (or the menu loop entry the other session tests use) with a `menuPicks` script that selects `0`, exercises one action, then PF3s back and logs off. Mirror `TestSessionAdminSelectionRunsFlowAndReturnsToMenu` (session_test.go:340) — copy its scaffolding and swap the admin pick for `{choice: menuUserSettings}`, scripting `fakePresenter.UserSettings` to return `("", true, nil)` (immediate PF3) so the flow returns to the menu, then `{quit: true}`.

```go
func TestSessionUserSettingsReturnsToMenu(t *testing.T) {
	// Arrange a Session like TestSessionAdminSelectionRunsFlowAndReturnsToMenu,
	// with a non-admin identity. fakePresenter.UserSettings returns back=true.
	// menuPicks: [{choice: menuUserSettings}, {quit: true}].
	// Assert: Run completes cleanly, a logout{user logoff} audit is recorded,
	// and UserSettings was invoked once.
}
```

Fill it in by copying the admin-selection test's setup verbatim and adapting. Add a `userSettings []struct{choice string; back bool; err error}` script + call counter to `fakePresenter` if you want to assert the invocation (or assert via an audit/side effect).

- [ ] **Step 2: Run + full suite with race**

Run: `go test ./internal/server/ -race`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/server/usersettings_test.go internal/server/session_test.go
git commit -m "test(server): menu '0' dispatches to user settings and returns (GH #63)"
```

### Task 12: Docs + protocol smoke

**Files:**
- Modify: `CLAUDE.md`
- Verify: `.claude/skills/s3270-smoke-testing/smoke.sh`

- [ ] **Step 1: Update the CLAUDE.md package map**

In the `internal/server` and `internal/screens` paragraphs, note the new self-service flow:
- `internal/server`: "`userSettings` flow (session.go) handles the `0` menu entry: self change-password (current-password re-verify, new≠current) and MFA enroll/re-enroll/disable (current-password step-up; disable only when not admin-required); `confirmEnroll` is shared with the login gate; failures fold into authThrottle; audits `password_self` / `mfa_enrolled` / `mfa_cleared`+`self-service`."
- `internal/screens`: add `UserSettingsScreen` to the builder list and note the `0 User Settings` menu meta-row.

- [ ] **Step 2: Full build + test + vet**

Run: `go build ./... && go test ./... -race && go vet ./...`
Expected: all PASS.

- [ ] **Step 3: Run the s3270 protocol smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Verify: login → menu shows `0 User Settings`; selecting `0` opens the settings menu; cursor lands on the option field; PF3 returns to the service menu. (Exact `0`-row placement / panel geometry is the emulator-pass detail — confirm it renders within columns 0–79 and the cursor rule holds.) If the smoke script has no User Settings step, extend it following the s3270-smoke-testing skill, or note manual verification.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md .claude/skills/s3270-smoke-testing/
git commit -m "docs: note self-service User Settings + secret-first gate (GH #63)"
```

---

## Self-Review

**Spec coverage:**
- Secret-first gate + regression → Task 1. ✅
- Two-PR decomposition → PR1 (Task 1) / PR2 (Tasks 2-12). ✅
- `0` menu entry + `menuChoice` refactor → Tasks 2, 3, 5. ✅
- `userSettings` flow holding identity, adaptive rows → Task 8. ✅
- Self change-password (current re-verify, new≠current, `password_self`) → Task 9. ✅
- MFA enroll/re-enroll/disable, current-password step-up, disable gated on `!required`, reuse `confirmEnroll`, audit kinds → Tasks 7, 10. ✅
- Throttle reuse (`failDelay`/`sleepFor`/`Reset`) on all failure paths → Tasks 9, 10. ✅
- No new throttle; Option B audit kinds (`password_self` new, `mfa_enrolled` reused, `mfa_cleared`+Detail) → Tasks 9, 10. ✅
- Unit tests + s3270 smoke → throughout + Task 12. ✅
- Out-of-scope items (email/full-name, opt-in prohibition #-follow-up, #73) → untouched. ✅

**Type consistency:** `menuChoice` values (`menuUserSettings`/`menuQuit`) used identically in Tasks 2/3/8. `confirmEnroll` signature `(confirmed, quit bool, detail string, err error)` consistent across Tasks 7 and 10. `Presenter.UserSettings(... rows []screens.UserSettingsRow ...) (string, bool, error)` consistent across Tasks 3/6/8. `screens.UserSettingsRow{Key, Label}` consistent across Tasks 4/6/8. `usAction`/`userSettingsActions`/`usActionLabel` consistent across Task 8/tests. `s.renderer(conn, term)` defined once (Task 8), used by Task 8 dispatch.

**Placeholders:** Test helper names (`newTestStore`, `seedUser`, `recordingAuditor`, `validCodeForNewSecret`, etc.) are intentionally deferred to existing repo helpers — the plan instructs reuse and flags where a minimal new helper is acceptable. These are not code placeholders in production paths; every production function body is given in full.

**Known build-order coupling:** Task 3 references `screens.UserSettingsRow` (Task 4) — execute Task 4 before compiling Task 3 (noted in Task 3 Step 7). Tasks 8-10 add interdependent helpers; stub the not-yet-written ones with `return nil` to keep each commit compiling, as noted.
