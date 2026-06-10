# ISPF Screen Polish — Plan 1: `internal/screens` package

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-lay the five `internal/screens` panels (login, service menu, admin menu, User Settings, MFA enroll/verify) onto the three-band ISPF layout and CUA palette defined in `docs/ispf-style-guide.md`.

**Architecture:** Add additive top-band geometry helpers to `screens.Geometry`, then migrate each builder: centered white title (row 0), `Option ===>` command line at the top (row 1, menus only), red message line (row 2), body from row 3, PF-key help unchanged at the bottom. Apply the tri-color option grid to all three menus. The `internal/ui3270` package is Plan 2 (independent geometry).

**Tech Stack:** Go, `github.com/racingmars/go3270`. Tests assert field *names/content/color*, not row numbers; the s3270 smoke script is the protocol-surface gate.

**Scope guardrails:** Presentation only — no auth/bridge/store behavior changes. Title *text* is unchanged on every screen (only centered + recolored). Only the admin menu *option labels* change (approved ISPF keywords). `news.go` (MOTD) is exempt and untouched.

---

## File structure

| File | Responsibility | Change |
|---|---|---|
| `internal/screens/geometry.go` | Geometry math | Add top-band helpers + `CenterCol`; recompute `MenuCapacity`; remove `InputRow`/`ErrorRow` (Task 7) |
| `internal/screens/geometry_test.go` | Geometry tests | Add band-helper tests; drop `InputRow`/`ErrorRow` tests (Task 7) |
| `internal/screens/commandline.go` | Shared `Option/Command ===>` builder (new) | Create (Task 3) |
| `internal/screens/login.go` | Login panel | Re-lay onto bands + palette |
| `internal/screens/menu.go` | Service menu | Command line→top, message→top, instruction turquoise; tri-color already present |
| `internal/screens/admin.go` | Admin menu | Bands + tri-color grid + ISPF keywords |
| `internal/screens/usersettings.go` | User Settings menu | Bands + tri-color grid; `UserSettingsRow` gains `Name`/`Description` |
| `internal/screens/mfa.go` | MFA enroll/verify | Bands + palette + yellow caution |
| `internal/server/session.go` | User Settings row build | Split `usActionLabel` → name + description |
| `.claude/skills/s3270-smoke-testing/smoke.sh` | Protocol gate | Update menu/admin cursor asserts to `(1,15)` |

---

## Task 1: Additive top-band geometry helpers

**Files:**
- Modify: `internal/screens/geometry.go`
- Test: `internal/screens/geometry_test.go`

Purely additive — keeps `InputRow`/`ErrorRow` so the build stays green until each screen migrates.

- [ ] **Step 1: Write the failing test**

Add to `internal/screens/geometry_test.go`:

```go
func TestTopBandHelpers(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	if g.TitleRow() != 0 {
		t.Errorf("TitleRow = %d, want 0", g.TitleRow())
	}
	if g.CommandRow() != 1 {
		t.Errorf("CommandRow = %d, want 1", g.CommandRow())
	}
	if g.MessageRow() != 2 {
		t.Errorf("MessageRow = %d, want 2", g.MessageRow())
	}
	if g.BodyTopRow() != 3 {
		t.Errorf("BodyTopRow = %d, want 3", g.BodyTopRow())
	}
	if g.BodyBottomRow() != 22 {
		t.Errorf("BodyBottomRow = %d, want 22", g.BodyBottomRow())
	}
}

func TestCenterCol(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	// "TN3270 GATEWAY MENU" is 19 runes → (80-19)/2 = 30.
	if got := g.CenterCol(19); got != 30 {
		t.Errorf("CenterCol(19) = %d, want 30", got)
	}
	// Over-wide text clamps to 0, never negative.
	if got := g.CenterCol(200); got != 0 {
		t.Errorf("CenterCol(200) = %d, want 0", got)
	}
	// Zero geometry normalizes to 24x80 before centering.
	if got := (Geometry{}).CenterCol(19); got != 30 {
		t.Errorf("zero-geom CenterCol(19) = %d, want 30", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run 'TestTopBandHelpers|TestCenterCol' -v`
Expected: FAIL — `g.TitleRow undefined` (compile error).

- [ ] **Step 3: Add the helpers**

In `internal/screens/geometry.go`, after the existing bottom-anchored helpers (after `InputRow`), add:

```go
// Top-band layout rows (ISPF style guide §2). The title/command/message band is
// fixed at rows 0–2; the body fills row 3 down to BodyBottomRow; PF-key help
// stays on the last row. See docs/ispf-style-guide.md.

// TitleRow is the centered panel-title row.
func (g Geometry) TitleRow() int { return 0 }

// CommandRow is the "Option ===>" / "Command ===>" command line (menus & lists).
func (g Geometry) CommandRow() int { return 1 }

// MessageRow is the red message line, directly under the command line.
func (g Geometry) MessageRow() int { return 2 }

// BodyTopRow is the first body row (column headings on a list).
func (g Geometry) BodyTopRow() int { return 3 }

// BodyBottomRow is the last usable body row — one above the PF-key help row.
func (g Geometry) BodyBottomRow() int { return g.norm().Rows - 2 }

// CenterCol returns the starting column to center an n-rune string within
// columns 0..Cols-1, clamped to 0 (never negative).
func (g Geometry) CenterCol(n int) int {
	c := (g.norm().Cols - n) / 2
	if c < 0 {
		return 0
	}
	return c
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/screens/ -run 'TestTopBandHelpers|TestCenterCol' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/geometry.go internal/screens/geometry_test.go
git commit -m "feat(screens): add top-band geometry helpers (GH #65)"
```

---

## Task 2: Login panel onto bands + palette

**Files:**
- Modify: `internal/screens/login.go`
- Test: `internal/screens/login_test.go`

Title centered+white; message→row 2; turquoise labels; green inputs; keep `Userid . . .` dot-leader and the row-3 userid input (cursor stays `(3,17)`).

- [ ] **Step 1: Write the failing test**

Add to `internal/screens/login_test.go`:

```go
func TestLoginScreenPalette(t *testing.T) {
	screen, _, cur := LoginScreen(Geometry{Rows: 24, Cols: 80}, "bad creds")

	title := fieldByContent(t, screen, "TN3270 GATEWAY LOGIN")
	if title.Row != 0 || !title.Intense || title.Color != go3270.White {
		t.Errorf("title = %+v, want row 0 white intense", title)
	}
	if title.Col != (Geometry{Rows: 24, Cols: 80}).CenterCol(len("TN3270 GATEWAY LOGIN")) {
		t.Errorf("title not centered: col %d", title.Col)
	}
	label := fieldByContent(t, screen, "Userid . . .")
	if label.Color != go3270.Turquoise {
		t.Errorf("userid label color = %v, want Turquoise", label.Color)
	}
	user := fieldByName(t, screen, FieldUsername)
	if user.Color != go3270.Green || !user.Write {
		t.Errorf("userid input = %+v, want green writable", user)
	}
	msg := fieldByName(t, screen, FieldError)
	if msg.Row != 2 || msg.Color != go3270.Red || !msg.Intense {
		t.Errorf("message field = %+v, want row 2 red intense", msg)
	}
	if cur.Row != 3 || cur.Col != 17 {
		t.Errorf("cursor = %+v, want (3,17)", cur)
	}
}
```

If `fieldByName`/`fieldByContent` helpers don't already exist in the package's tests, add them to `login_test.go`:

```go
func fieldByName(t *testing.T, s go3270.Screen, name string) go3270.Field {
	t.Helper()
	for _, f := range s {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no field named %q", name)
	return go3270.Field{}
}

func fieldByContent(t *testing.T, s go3270.Screen, content string) go3270.Field {
	t.Helper()
	for _, f := range s {
		if f.Content == content {
			return f
		}
	}
	t.Fatalf("no field with content %q", content)
	return go3270.Field{}
}
```

(If they already exist elsewhere in the package's `_test.go` files, skip re-declaring — Go will error on a redeclaration; reuse the existing ones.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestLoginScreenPalette -v`
Expected: FAIL — title color is `DefaultColor`, label not turquoise, message not on row 2.

- [ ] **Step 3: Rewrite `LoginScreen`**

Replace the `screen := go3270.Screen{...}` block in `internal/screens/login.go` with:

```go
	title := "TN3270 GATEWAY LOGIN"
	username := go3270.Field{Row: 3, Col: 16, Name: FieldUsername, Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: 3, Col: 2, Color: go3270.Turquoise, Content: "Userid . . ."},
		username,
		{Row: 3, Col: 33}, // stop field: closes the username input
		{Row: 5, Col: 2, Color: go3270.Turquoise, Content: "Password . ."},
		{Row: 5, Col: 16, Name: FieldPassword, Write: true, Hidden: true, Color: go3270.Green, Highlighting: go3270.Underscore},
		{Row: 5, Col: 33}, // stop field
		{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Disconnect"},
	}
```

(Leave the `rules` and `return ... cursorAt(username)` lines unchanged.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/screens/ -run 'TestLogin' -v`
Expected: PASS (the new test plus the existing login tests — they assert names/content, which are unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/screens/login.go internal/screens/login_test.go
git commit -m "feat(screens): re-lay login onto ISPF bands + palette (GH #65)"
```

---

## Task 3: Service menu — command line to top + shared `commandLine` helper

**Files:**
- Create: `internal/screens/commandline.go`
- Modify: `internal/screens/menu.go`, `internal/screens/geometry.go`
- Test: `internal/screens/commandline_test.go`, `internal/screens/menu_test.go`

The tri-color service grid already exists; this moves the selection field to the top command line, the error to row 2, makes the instruction turquoise, centers the title, and introduces the shared `commandLine` builder reused by Tasks 4–5.

- [ ] **Step 1: Write the failing test for `commandLine`**

Create `internal/screens/commandline_test.go`:

```go
package screens

import (
	"testing"

	"github.com/racingmars/go3270"
)

func TestCommandLine(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	prompt, input, _ := commandLine(g, "Option ===>", FieldSelection)

	if prompt.Row != 1 || prompt.Col != 2 || prompt.Color != go3270.Turquoise {
		t.Errorf("prompt = %+v, want row 1 col 2 turquoise", prompt)
	}
	if prompt.Content != "Option ===>" {
		t.Errorf("prompt content = %q", prompt.Content)
	}
	// "Option ===>" is 11 runes: content cols 3..13, next attr at col 14.
	if input.Col != 14 || input.Color != go3270.Green || !input.Write {
		t.Errorf("input = %+v, want col 14 green writable", input)
	}
	if input.Name != FieldSelection {
		t.Errorf("input name = %q, want %q", input.Name, FieldSelection)
	}
	if c := cursorAt(input); c.Row != 1 || c.Col != 15 {
		t.Errorf("cursor = %+v, want (1,15)", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestCommandLine -v`
Expected: FAIL — `commandLine undefined`.

- [ ] **Step 3: Implement `commandLine`**

Create `internal/screens/commandline.go` (include the standard GPL header from any sibling file), with:

```go
package screens

import "github.com/racingmars/go3270"

// commandLine builds the ISPF command line at CommandRow: a turquoise prompt
// ("Option ===>" on menus, "Command ===>" on lists) and the named green input
// field placed just past the prompt. It returns the prompt, input, and stop
// fields separately so callers can tune the input (e.g. NumericOnly) and compute
// the cursor with cursorAt(input). See docs/ispf-style-guide.md §2.
func commandLine(geom Geometry, prompt, fieldName string) (promptF, input, stop go3270.Field) {
	const promptAttrCol = 2
	// Prompt content runs cols 3..(2+len); the next field's attribute byte sits
	// one past it, giving a one-column gap.
	inputCol := promptAttrCol + 1 + len([]rune(prompt))
	promptF = go3270.Field{Row: geom.CommandRow(), Col: promptAttrCol, Color: go3270.Turquoise, Content: prompt}
	input = go3270.Field{Row: geom.CommandRow(), Col: inputCol, Name: fieldName, Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	stop = go3270.Field{Row: geom.CommandRow(), Col: min(inputCol+9, 79)} // ~8-col input window
	return promptF, input, stop
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/screens/ -run TestCommandLine -v`
Expected: PASS.

- [ ] **Step 5: Recompute `MenuCapacity` for the new bands**

In `internal/screens/geometry.go`, replace the body of `MenuCapacity`:

```go
// MenuCapacity is how many service lines fit on the menu: body rows
// BodyTopRow+1 .. BodyBottomRow (row BodyTopRow is the instruction line), minus
// one for the always-present "0 User Settings" row and one more for the admin
// "A" row.
func (g Geometry) MenuCapacity(admin bool) int {
	n := g.BodyBottomRow() - (g.BodyTopRow() + 1) + 1 - 1 // body rows minus the "0" meta row
	if admin {
		n--
	}
	return n
}
```

- [ ] **Step 6: Update the menu test for the new positions**

In `internal/screens/menu_test.go`, find the assertions that pin the selection field / error / instruction to bottom rows and update them. Add this focused test:

```go
func TestMenuScreenTopBand(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, cur := MenuScreen(g, nil, false, MenuStatus{}, "oops")

	sel := fieldByName(t, screen, FieldSelection)
	if sel.Row != 1 || sel.Col != 14 {
		t.Errorf("selection = row %d col %d, want row 1 col 14", sel.Row, sel.Col)
	}
	if cur.Row != 1 || cur.Col != 15 {
		t.Errorf("cursor = %+v, want (1,15)", cur)
	}
	msg := fieldByName(t, screen, FieldError)
	if msg.Row != 2 {
		t.Errorf("message row = %d, want 2", msg.Row)
	}
	instr := fieldByContent(t, screen, "Select a service and press ENTER:")
	if instr.Row != 3 || instr.Color != go3270.Turquoise {
		t.Errorf("instruction = %+v, want row 3 turquoise", instr)
	}
}
```

- [ ] **Step 7: Run the test to verify it fails**

Run: `go test ./internal/screens/ -run TestMenuScreenTopBand -v`
Expected: FAIL — selection still on the bottom InputRow, instruction on row 2 with default color.

- [ ] **Step 8: Rewrite `menu.go` placements**

In `internal/screens/menu.go`:

1. Center the title — change the first field:
```go
		{Row: geom.TitleRow(), Col: geom.CenterCol(len("TN3270 GATEWAY MENU")), Color: go3270.White, Intense: true, Content: "TN3270 GATEWAY MENU"},
```
2. Move the instruction to the body top and color it turquoise:
```go
		{Row: geom.BodyTopRow(), Col: 2, Color: go3270.Turquoise, Content: "Select a service and press ENTER:"},
```
3. Replace the service-row start `row := 4` with `row := geom.BodyTopRow() + 1` and the empty-state `Row: 4` / `row = 5` with `Row: geom.BodyTopRow() + 1` / `row = geom.BodyTopRow() + 2`.
4. Replace the meta-row clamp `lastMeta := geom.InputRow() - 2` with `lastMeta := geom.BodyBottomRow()` (and keep the `if admin { lastMeta-- }`).
5. Replace the bottom command/selection block. Remove the old `selection := ...InputRow...` line and the `===>`/selection/stop/error fields, and replace with:
```go
	promptF, selection, stopF := commandLine(geom, "Option ===>", FieldSelection)
	selection.NumericOnly = !admin
	screen = append(screen,
		promptF,
		selection,
		stopF,
		go3270.Field{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Logoff    (PA3 returns here from a session)"},
	)
	return screen, mapping, cursorAt(selection)
```
6. The status block (`statusBlockFields`, rows `4+i`) is unchanged — it stays on the right at rows 4–9.

- [ ] **Step 9: Run the menu tests**

Run: `go test ./internal/screens/ -run 'TestMenu' -v`
Expected: PASS. If a pre-existing test pinned the old bottom selection row, update its expected row to 1 (selection) / 2 (error) — assert names/content, not the old bottom rows.

- [ ] **Step 10: Commit**

```bash
git add internal/screens/commandline.go internal/screens/commandline_test.go internal/screens/menu.go internal/screens/menu_test.go internal/screens/geometry.go
git commit -m "feat(screens): service menu command line to top band (GH #65)"
```

---

## Task 4: Admin menu — tri-color grid + ISPF keywords

**Files:**
- Modify: `internal/screens/admin.go`
- Test: `internal/screens/admin_test.go`

Center title; command line→top via `commandLine`; message→row 2; render the six options on the tri-color grid (white numeral col 0 / turquoise keyword col 4 / green description col 13) per the style-guide mapping.

- [ ] **Step 1: Write the failing test**

Add to `internal/screens/admin_test.go`:

```go
func TestAdminMenuTriColor(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, cur := AdminMenuScreen(g, "")

	// Row 3: option 1 = "  1" white / "Users" turquoise / description green.
	key := fieldByContent(t, screen, "  1")
	if key.Color != go3270.White || !key.Intense {
		t.Errorf("option key = %+v, want white intense", key)
	}
	name := fieldByContent(t, screen, "Users")
	if name.Col != 4 || name.Color != go3270.Turquoise {
		t.Errorf("option name = %+v, want col 4 turquoise", name)
	}
	desc := fieldByContent(t, screen, "User accounts and group membership")
	if desc.Col != 13 || desc.Color != go3270.Green {
		t.Errorf("option desc = %+v, want col 13 green", desc)
	}
	// ISPF keyword rename present.
	_ = fieldByContent(t, screen, "Sysparms")
	_ = fieldByContent(t, screen, "Networks")
	_ = fieldByContent(t, screen, "Audit")
	opt := fieldByName(t, screen, FieldOption)
	if opt.Row != 1 {
		t.Errorf("option input row = %d, want 1", opt.Row)
	}
	if cur.Row != 1 || cur.Col != 15 {
		t.Errorf("cursor = %+v, want (1,15)", cur)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestAdminMenuTriColor -v`
Expected: FAIL — options are flat `1.  Users`, input on the bottom InputRow.

- [ ] **Step 3: Rewrite `AdminMenuScreen`**

Replace the body of `AdminMenuScreen` in `internal/screens/admin.go` with:

```go
func AdminMenuScreen(geom Geometry, errMsg string) (go3270.Screen, Cursor) {
	title := "TN3270 GATEWAY ADMIN"
	opts := []struct{ key, name, desc string }{
		{"1", "Users", "User accounts and group membership"},
		{"2", "Groups", "Group definitions"},
		{"3", "Services", "Backend TN3270 services"},
		{"4", "Sysparms", "Runtime system parameters"},
		{"5", "Networks", "Trusted networks (DoS allow-list)"},
		{"6", "Audit", "Browse the audit trail"},
	}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
	}
	for i, o := range opts {
		row := geom.BodyTopRow() + i
		screen = append(screen,
			go3270.Field{Row: row, Col: 0, Color: go3270.White, Intense: true, Content: "  " + o.key},
			go3270.Field{Row: row, Col: 4, Color: go3270.Turquoise, Content: o.name},
			go3270.Field{Row: row, Col: 13, Color: go3270.Green, Content: o.desc},
		)
	}
	promptF, option, stopF := commandLine(geom, "Option ===>", FieldOption)
	screen = append(screen,
		promptF,
		option,
		stopF,
		go3270.Field{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Main Menu"},
	)
	return screen, cursorAt(option)
}
```

- [ ] **Step 4: Run the admin tests**

Run: `go test ./internal/screens/ -run 'TestAdminMenu' -v`
Expected: PASS. Update any pre-existing test that asserted the old `1.  Users` flat label or the bottom input row.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/admin.go internal/screens/admin_test.go
git commit -m "feat(screens): admin menu tri-color grid + ISPF keywords (GH #65)"
```

---

## Task 5: User Settings menu — tri-color grid (struct + session split)

**Files:**
- Modify: `internal/screens/usersettings.go`, `internal/server/session.go`
- Test: `internal/screens/usersettings_test.go`, `internal/server/session_test.go` (only if an existing test references `UserSettingsRow{Key,Label}`)

`UserSettingsRow` gains `Name` + `Description`; the session builds them from a new `usActionRow` split; the screen renders them on the tri-color grid with the command line at the top.

- [ ] **Step 1: Write the failing test**

Replace/extend the row-rendering test in `internal/screens/usersettings_test.go`:

```go
func TestUserSettingsTriColor(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	rows := []UserSettingsRow{
		{Key: "1", Name: "Password", Description: "Change your sign-on password"},
		{Key: "2", Name: "MFA", Description: "Enroll in multi-factor authentication"},
	}
	screen, cur := UserSettingsScreen(g, "ROBERT", rows, "")

	key := fieldByContent(t, screen, "  1")
	if key.Color != go3270.White || !key.Intense {
		t.Errorf("key = %+v, want white intense", key)
	}
	name := fieldByContent(t, screen, "Password")
	if name.Col != 4 || name.Color != go3270.Turquoise {
		t.Errorf("name = %+v, want col 4 turquoise", name)
	}
	desc := fieldByContent(t, screen, "Change your sign-on password")
	if desc.Col != 13 || desc.Color != go3270.Green {
		t.Errorf("desc = %+v, want col 13 green", desc)
	}
	opt := fieldByName(t, screen, FieldUSOption)
	if opt.Row != 1 || cur.Row != 1 || cur.Col != 15 {
		t.Errorf("option row/cursor wrong: opt=%d cur=%+v", opt.Row, cur)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestUserSettingsTriColor -v`
Expected: FAIL — `UserSettingsRow` has no `Name`/`Description`; rows render at col 4/8 with the old two-color style.

- [ ] **Step 3: Update the `UserSettingsRow` struct and screen**

In `internal/screens/usersettings.go`, change the struct:

```go
// UserSettingsRow is one selectable row on the User Settings menu, rendered on
// the shared tri-color option grid: Key (white numeral), Name (turquoise short
// keyword), Description (green). The caller builds the ordered slice adaptively
// from the user's MFA state.
type UserSettingsRow struct {
	Key, Name, Description string
}
```

Replace the row-render loop with the tri-color grid and route the input through `commandLine`:

```go
	title := "TN3270 GATEWAY USER SETTINGS"
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: geom.BodyTopRow(), Col: 2, Color: go3270.Turquoise, Content: "User: " + username},
	}
	row := geom.BodyTopRow() + 2
	for _, r := range rows {
		screen = append(screen,
			go3270.Field{Row: row, Col: 0, Color: go3270.White, Intense: true, Content: "  " + r.Key},
			go3270.Field{Row: row, Col: 4, Color: go3270.Turquoise, Content: r.Name},
			go3270.Field{Row: row, Col: 13, Color: go3270.Green, Content: r.Description},
		)
		row++
	}
	promptF, option, stopF := commandLine(geom, "Option ===>", FieldUSOption)
	screen = append(screen,
		promptF,
		option,
		stopF,
		go3270.Field{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Service Menu"},
	)
	return screen, cursorAt(option)
```

- [ ] **Step 4: Split `usActionLabel` in the session**

In `internal/server/session.go`, replace `usActionLabel` with a name/description split:

```go
// usActionRow returns the tri-color grid name (short keyword) and description for
// a self-service action.
func usActionRow(a usAction) (name, desc string) {
	switch a {
	case usChangePassword:
		return "Password", "Change your sign-on password"
	case usEnroll:
		return "MFA", "Enroll in multi-factor authentication"
	case usReenroll:
		return "MFA", "Re-enroll your authenticator (replaces the key)"
	case usDisable:
		return "MFA", "Disable multi-factor authentication"
	}
	return "", ""
}
```

And update the row build in `userSettings` (around line 762-765):

```go
		rows := make([]screens.UserSettingsRow, len(actions))
		for i, a := range actions {
			name, desc := usActionRow(a)
			rows[i] = screens.UserSettingsRow{Key: strconv.Itoa(i + 1), Name: name, Description: desc}
		}
```

- [ ] **Step 5: Run the affected packages**

Run: `go test ./internal/screens/ ./internal/server/ -run 'UserSettings|userSettings' -v`
Then a full build: `go build ./...`
Expected: PASS / clean build. If `session_test.go` referenced `usActionLabel` or `UserSettingsRow{...Label}`, update those references to the new fields.

- [ ] **Step 6: Commit**

```bash
git add internal/screens/usersettings.go internal/screens/usersettings_test.go internal/server/session.go internal/server/session_test.go
git commit -m "feat(screens): user settings tri-color grid (GH #65)"
```

---

## Task 6: MFA enroll + verify onto bands + palette

**Files:**
- Modify: `internal/screens/mfa.go`
- Test: `internal/screens/mfa_test.go`

Center both titles; message→row 2; the "MFA is now required" line becomes a yellow caution; labels turquoise, the code input green; `Key:` stays intense.

- [ ] **Step 1: Write the failing test**

Add to `internal/screens/mfa_test.go`:

```go
func TestMFAScreensPalette(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}

	enroll, _, _ := EnrollMFAScreen(g, "TN3270", "ROBERT", "ABCD EFGH", "")
	et := fieldByContent(t, enroll, "MFA ENROLLMENT - SECURITY KEY SETUP")
	if et.Row != 0 || et.Color != go3270.White || !et.Intense {
		t.Errorf("enroll title = %+v, want row 0 white intense", et)
	}
	caution := fieldByContent(t, enroll, "Multi-factor authentication is now required for your account.")
	if caution.Color != go3270.Yellow || !caution.Intense {
		t.Errorf("caution = %+v, want yellow intense", caution)
	}
	ec := fieldByName(t, enroll, FieldMFACode)
	if ec.Color != go3270.Green {
		t.Errorf("enroll code color = %v, want Green", ec.Color)
	}
	em := fieldByName(t, enroll, FieldError)
	if em.Row != 2 {
		t.Errorf("enroll message row = %d, want 2", em.Row)
	}

	verify, _, _ := VerifyMFAScreen(g, "")
	vt := fieldByContent(t, verify, "MFA VERIFICATION")
	if vt.Row != 0 || vt.Color != go3270.White || !vt.Intense {
		t.Errorf("verify title = %+v, want row 0 white intense", vt)
	}
	vm := fieldByName(t, verify, FieldError)
	if vm.Row != 2 {
		t.Errorf("verify message row = %d, want 2", vm.Row)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestMFAScreensPalette -v`
Expected: FAIL — titles left at col 2 default color, caution not yellow, message on the bottom ErrorRow.

- [ ] **Step 3: Rewrite the two builders**

In `internal/screens/mfa.go`, replace the `EnrollMFAScreen` screen literal:

```go
	title := "MFA ENROLLMENT - SECURITY KEY SETUP"
	code := go3270.Field{Row: 11, Col: 25, Name: FieldMFACode, Write: true, NumericOnly: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: 4, Col: 2, Color: go3270.Yellow, Intense: true, Content: "Multi-factor authentication is now required for your account."},
		{Row: 5, Col: 2, Color: go3270.Turquoise, Content: "Enter the key below into your authenticator app (any TOTP app),"},
		{Row: 6, Col: 2, Color: go3270.Turquoise, Content: "then type the current 6-digit code to confirm enrollment."},
		{Row: 8, Col: 5, Color: go3270.Turquoise, Content: "Issuer:   " + issuer},
		{Row: 9, Col: 5, Color: go3270.Turquoise, Content: "Account:  " + account},
		{Row: 10, Col: 5, Intense: true, Color: go3270.White, Content: "Key:      " + chunkedSecret},
		{Row: 11, Col: 5, Color: go3270.Turquoise, Content: "Confirmation code:"},
		code,
		{Row: 11, Col: 32}, // stop field
		{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "Enter=Confirm   PF3=Cancel"},
	}
```

(The body rows shifted down by ~2 to clear the top band; `code` stays at row 11. Leave `rules`/`return ... cursorAt(code)` unchanged.)

Replace the `VerifyMFAScreen` screen literal:

```go
	title := "MFA VERIFICATION"
	code := go3270.Field{Row: 5, Col: 12, Name: FieldMFACode, Write: true, NumericOnly: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: 4, Col: 2, Color: go3270.Turquoise, Content: "Enter the current 6-digit code from your authenticator app."},
		{Row: 5, Col: 2, Color: go3270.Turquoise, Content: "Code:"},
		code,
		{Row: 5, Col: 19}, // stop field
		{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "Enter=Verify   PF3=Cancel"},
	}
```

- [ ] **Step 4: Run the MFA tests**

Run: `go test ./internal/screens/ -run 'TestMFA|TestEnroll|TestVerify' -v`
Expected: PASS. Update any existing test asserting the old `Code:`/title rows.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/mfa.go internal/screens/mfa_test.go
git commit -m "feat(screens): MFA screens onto ISPF bands + yellow caution (GH #65)"
```

---

## Task 7: Remove the dead bottom-anchored helpers

**Files:**
- Modify: `internal/screens/geometry.go`, `internal/screens/geometry_test.go`

`InputRow` and `ErrorRow` are now unused in the package (every screen uses `CommandRow`/`MessageRow`). Remove them and their tests.

- [ ] **Step 1: Confirm they're unused**

Run: `grep -rn "InputRow\|ErrorRow" internal/screens/ internal/server/ internal/ui3270/`
Expected: only matches in `geometry.go` (definitions) and `geometry_test.go`. If any screen still references them, that screen's task is incomplete — fix it first.

- [ ] **Step 2: Delete the definitions**

In `internal/screens/geometry.go`, delete the `ErrorRow` and `InputRow` methods (the two funcs and their doc comments).

- [ ] **Step 3: Delete their tests**

In `internal/screens/geometry_test.go`, delete any assertions referencing `ErrorRow()` or `InputRow()`.

- [ ] **Step 4: Build + test the package**

Run: `go build ./... && go test ./internal/screens/ -v`
Expected: clean build, all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/geometry.go internal/screens/geometry_test.go
git commit -m "refactor(screens): drop dead InputRow/ErrorRow helpers (GH #65)"
```

---

## Task 8: Update the s3270 smoke asserts + full verification

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`

Only the menu/admin command-line cursor moved (to `(1,15)`); login stays `(3,17)`. List/form are Plan 2.

- [ ] **Step 1: Update the menu cursor assert**

In `.claude/skills/s3270-smoke-testing/smoke.sh`, change check `3j` (line ~144) from:
```
check "3j menu cursor on selection input" "I 2 24 80 19 8 " "$WORK/t3.out"
```
to:
```
check "3j menu cursor on selection input (1,15)" "I 2 24 80 1 15 " "$WORK/t3.out"
```

- [ ] **Step 2: Update the admin menu cursor assert**

Change check `9b` (line ~289) from:
```
check "9b admin/menu cursor on option field (19,8)" "I 2 24 80 19 8 " "$WORK/t9.out"
```
to:
```
check "9b admin/menu cursor on option field (1,15)" "I 2 24 80 1 15 " "$WORK/t9.out"
```

- [ ] **Step 3: Whole-suite unit tests + build**

Run: `go build ./... && go test ./... -race`
Expected: all PASS.

- [ ] **Step 4: Run the s3270 smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all checks PASS. If a cursor check fails, read the reported `I 2 24 80 <row> <col>` line — that is the builder's deterministic output. Confirm the row/col matches the style guide (command line at row 1) and reconcile the assert. If a *title/content* check fails, a title text was altered (it should not have been) — fix the builder.

> If s3270 is unavailable in this environment, the skill's instructions explain setup; the unit tests + build are the minimum gate, and the emulator run is required before declaring the screens done.

- [ ] **Step 5: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "test(smoke): menu/admin cursor now on top command line (GH #65)"
```

---

## Done criteria for Plan 1

- All five `internal/screens` panels render on the three-band layout with the CUA palette.
- All three option menus share the tri-color grid; admin uses the ISPF keywords.
- `go build ./... && go test ./... -race` clean; s3270 smoke green.
- CLAUDE.md "Screen layout convention" update and the `internal/ui3270` screens are **Plan 2** (`docs/superpowers/plans/2026-06-06-ispf-ui3270-pkg-polish.md`, to be written).
