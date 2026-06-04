# Builder-owned initial cursor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the initial input-cursor coordinates out of the presenters and into the `screens` builders, applying the `(field.Row, field.Col+1)` rule exactly once in the package that owns field geometry.

**Architecture:** Add a `screens.Cursor{Row, Col}` value and a single unexported `cursorAt(field)` helper. Each screen builder gains a `Cursor` return value computed from its own primary input field; the empty admin list returns `Cursor{0,0}` (home). Presenters consume the returned cursor instead of re-deriving literals. A per-screen task structure changes one builder + its presenter + its tests together so the module compiles and stays green at every commit.

**Tech Stack:** Go (package `internal/screens`, `internal/server`), `github.com/racingmars/go3270`, s3270 smoke script (bash).

**Reference spec:** `docs/superpowers/specs/2026-06-04-builder-owned-cursor-design.md`

**Key facts established during planning:**
- All builder call sites: the 5 presenters (non-test) + ~25 existing test calls in `login_test.go`, `menu_test.go`, `admin_test.go`. Adding a return value breaks every destructuring call until updated — the Go compiler lists each one, so fixes are compiler-driven.
- A `fieldByName(screen, name)` helper already exists in `internal/screens/login_test.go` (package `screens`) — reuse it.
- MOD 2 (24×80) row math: `InputRow()=19`, login/form first field row 3 col 16. Cursors: login `{3,17}`, menu/admin-menu `{19,8}`, admin list populated `{4,3}` / empty `{0,0}`, admin form `{3,17}`.
- s3270 status-line format (from existing test 1c): `U F U C(host) I 2 24 80 <crow> <ccol> ...` — so cursor `(R,C)` ⇒ infix pattern `I 2 24 80 R C `.

---

### Task 1: Add the `Cursor` type and `cursorAt` helper

**Files:**
- Create: `internal/screens/cursor.go`
- Test: `internal/screens/cursor_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/screens/cursor_test.go`:

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

package screens

import (
	"testing"

	"github.com/racingmars/go3270"
)

func TestCursorAtAppliesAttributeOffset(t *testing.T) {
	// A field's Col is its attribute byte; input begins one column right.
	f := go3270.Field{Row: 5, Col: 16}
	if got, want := cursorAt(f), (Cursor{Row: 5, Col: 17}); got != want {
		t.Errorf("cursorAt(%+v) = %+v, want %+v", f, got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestCursorAtAppliesAttributeOffset`
Expected: FAIL — compile error `undefined: cursorAt` / `undefined: Cursor`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/screens/cursor.go`:

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

package screens

import "github.com/racingmars/go3270"

// Cursor is a screen builder's initial input-cursor position, already adjusted
// for the 3270 attribute-byte offset. Cursor{0,0} means "home" — no input
// field to land on (e.g. an empty admin list).
type Cursor struct{ Row, Col int }

// cursorAt applies the go3270 attribute-byte rule: a field's Col is its
// attribute byte, so input begins one column to the right. This is the ONE
// place the (field.Row, field.Col+1) rule lives; every builder routes its
// primary input field through here.
func cursorAt(f go3270.Field) Cursor { return Cursor{Row: f.Row, Col: f.Col + 1} }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/screens/ -run TestCursorAtAppliesAttributeOffset`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/screens/cursor.go internal/screens/cursor_test.go
git commit -m "feat(screens): add Cursor type and cursorAt helper (#33)"
```

---

### Task 2: Login screen returns its cursor

**Files:**
- Modify: `internal/screens/login.go`
- Modify: `internal/server/presenter.go` (Login, ~line 75-81)
- Test: `internal/screens/login_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/screens/login_test.go`:

```go
func TestLoginScreenCursor(t *testing.T) {
	screen, _, cur := LoginScreen(DefaultGeometry, "")
	uf, ok := fieldByName(screen, FieldUsername)
	if !ok {
		t.Fatalf("missing %q field", FieldUsername)
	}
	if want := cursorAt(uf); cur != want {
		t.Errorf("login cursor = %+v, want %+v (username field row %d col %d)", cur, want, uf.Row, uf.Col)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestLoginScreenCursor`
Expected: FAIL — compile error `assignment mismatch: 3 variables but LoginScreen returns 2 values`.

- [ ] **Step 3: Change the builder to return its cursor**

In `internal/screens/login.go`, extract the username field into a variable so the screen literal and `cursorAt` share one source, and add the `Cursor` return:

```go
func LoginScreen(geom Geometry, errMsg string) (go3270.Screen, go3270.Rules, Cursor) {
	username := go3270.Field{Row: 3, Col: 16, Name: FieldUsername, Write: true, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY LOGIN"},
		{Row: 3, Col: 2, Content: "Userid . . ."},
		username,
		{Row: 3, Col: 33}, // stop field: closes the username input
		{Row: 5, Col: 2, Content: "Password . ."},
		{Row: 5, Col: 16, Name: FieldPassword, Write: true, Hidden: true, Highlighting: go3270.Underscore},
		{Row: 5, Col: 33}, // stop field
		{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Content: "Enter = sign on    PF3 = disconnect"},
	}
	rules := go3270.Rules{
		FieldUsername: {Validator: go3270.NonBlank, ErrorText: "Userid is required"},
	}
	return screen, rules, cursorAt(username)
}
```

- [ ] **Step 4: Fix the broken call sites (compiler-driven)**

Run: `go build ./... 2>&1`
Expected: errors at `internal/server/presenter.go:75` and `internal/screens/login_test.go:38,67,85`.

In `internal/server/presenter.go` Login method, capture and use the cursor:

```go
	screen, rules, cur := screens.LoginScreen(term.Geometry(), errMsg)
	resp, err := handleScreen(func() (go3270.Response, error) {
		return go3270.HandleScreenAlt(
			screen, rules, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			withSilentExits([]go3270.AID{go3270.AIDPF3}),
			screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
		)
	})
```

In `internal/screens/login_test.go`, add `, _` for the new return value at the three existing calls:
- line 38: `screen, rules := LoginScreen(DefaultGeometry, "")` → `screen, rules, _ := LoginScreen(DefaultGeometry, "")`
- line 67: `screen, _ := LoginScreen(DefaultGeometry, "Invalid credentials")` → `screen, _, _ := LoginScreen(DefaultGeometry, "Invalid credentials")`
- line 85: `screen, _ := LoginScreen(g, "err")` → `screen, _, _ := LoginScreen(g, "err")`

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/screens/ ./internal/server/ -race`
Expected: PASS (all, including TestLoginScreenCursor).

- [ ] **Step 6: Commit**

```bash
git add internal/screens/login.go internal/screens/login_test.go internal/server/presenter.go
git commit -m "feat(screens): LoginScreen returns its initial cursor (#33)"
```

---

### Task 3: Menu screen returns its cursor

**Files:**
- Modify: `internal/screens/menu.go`
- Modify: `internal/server/presenter.go` (Menu, ~line 97-103)
- Test: `internal/screens/menu_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/screens/menu_test.go`:

```go
func TestMenuScreenCursor(t *testing.T) {
	screen, _, cur := MenuScreen(DefaultGeometry, nil, false, "")
	sf, ok := fieldByName(screen, FieldSelection)
	if !ok {
		t.Fatalf("missing %q field", FieldSelection)
	}
	if want := cursorAt(sf); cur != want {
		t.Errorf("menu cursor = %+v, want %+v (selection field row %d col %d)", cur, want, sf.Row, sf.Col)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestMenuScreenCursor`
Expected: FAIL — `assignment mismatch: 3 variables but MenuScreen returns 2 values`.

- [ ] **Step 3: Change the builder to return its cursor**

In `internal/screens/menu.go`, extract the selection field and add the `Cursor` return. Replace the signature and the trailing append/return:

```go
func MenuScreen(geom Geometry, services []store.Service, admin bool, errMsg string) (go3270.Screen, map[string]store.Service, Cursor) {
```

Then replace the final `screen = append(...)` / `return` block at the end of the function with:

```go
	selection := go3270.Field{Row: geom.InputRow(), Col: 7, Name: FieldSelection, Write: true, NumericOnly: !admin, Highlighting: go3270.Underscore}
	screen = append(screen,
		go3270.Field{Row: geom.InputRow(), Col: 2, Content: "===>"},
		selection,
		go3270.Field{Row: geom.InputRow(), Col: 15}, // stop field
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: "Enter = connect    PF3 = logoff    (PA3 returns here from a session)"},
	)
	return screen, mapping, cursorAt(selection)
}
```

- [ ] **Step 4: Fix the broken call sites (compiler-driven)**

Run: `go build ./... 2>&1` and `go vet ./internal/screens/ 2>&1`
Expected: errors at `internal/server/presenter.go:97` and many lines in `internal/screens/menu_test.go`.

In `internal/server/presenter.go` Menu method:

```go
		screen, mapping, cur := screens.MenuScreen(geom, svcs, admin, errMsg)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
			)
		})
```

In `internal/screens/menu_test.go`, add the third return slot to every `MenuScreen(...)` call. The compiler names each line; the mechanical fix is:
- `screen, mapping := MenuScreen(...)` → `screen, mapping, _ := MenuScreen(...)` (lines 35, 53, 74, 138, 160, 184)
- `screen, _ := MenuScreen(...)` → `screen, _, _ := MenuScreen(...)` (lines 63, 86, 101, 110, 118)
- `_, mapping = MenuScreen(...)` → `_, mapping, _ = MenuScreen(...)` (line 150)

After editing, re-run `go build ./...` until clean.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/screens/ ./internal/server/ -race`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/screens/menu.go internal/screens/menu_test.go internal/server/presenter.go
git commit -m "feat(screens): MenuScreen returns its initial cursor (#33)"
```

---

### Task 4: Admin menu screen returns its cursor

**Files:**
- Modify: `internal/screens/admin.go` (AdminMenuScreen, ~line 136-148)
- Modify: `internal/server/presenter_admin.go` (AdminMenu, ~line 62-68)
- Test: `internal/screens/admin_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/screens/admin_test.go`:

```go
func TestAdminMenuScreenCursor(t *testing.T) {
	screen, cur := AdminMenuScreen(DefaultGeometry, "")
	of, ok := fieldByName(screen, FieldOption)
	if !ok {
		t.Fatalf("missing %q field", FieldOption)
	}
	if want := cursorAt(of); cur != want {
		t.Errorf("admin menu cursor = %+v, want %+v (option field row %d col %d)", cur, want, of.Row, of.Col)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestAdminMenuScreenCursor`
Expected: FAIL — `assignment mismatch: 2 variables but AdminMenuScreen returns 1 value`.

- [ ] **Step 3: Change the builder to return its cursor**

In `internal/screens/admin.go`, rewrite `AdminMenuScreen`:

```go
func AdminMenuScreen(geom Geometry, errMsg string) (go3270.Screen, Cursor) {
	option := go3270.Field{Row: geom.InputRow(), Col: 7, Name: FieldOption, Write: true, Highlighting: go3270.Underscore}
	return go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY ADMIN"},
		{Row: 3, Col: 4, Content: "1.  Users"},
		{Row: 4, Col: 4, Content: "2.  Groups"},
		{Row: 5, Col: 4, Content: "3.  Services"},
		{Row: geom.InputRow(), Col: 2, Content: "===>"},
		option,
		{Row: geom.InputRow(), Col: 11}, // stop field
		{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Content: "Enter = select    PF3 = main menu"},
	}, cursorAt(option)
}
```

- [ ] **Step 4: Fix the broken call sites (compiler-driven)**

Run: `go build ./... 2>&1`
Expected: errors at `internal/server/presenter_admin.go:62` and `internal/screens/admin_test.go:40,163`.

In `internal/server/presenter_admin.go` AdminMenu method:

```go
		screen, cur := screens.AdminMenuScreen(geom, errMsg)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
			)
		})
```

In `internal/screens/admin_test.go`:
- line 40: `screen := AdminMenuScreen(DefaultGeometry, "boom")` → `screen, _ := AdminMenuScreen(DefaultGeometry, "boom")`
- line 163: `menu := AdminMenuScreen(g, "boom")` → `menu, _ := AdminMenuScreen(g, "boom")`

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/screens/ ./internal/server/ -race`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/screens/admin.go internal/screens/admin_test.go internal/server/presenter_admin.go
git commit -m "feat(screens): AdminMenuScreen returns its initial cursor (#33)"
```

---

### Task 5: Admin list screen returns its cursor (populated + empty-homes)

**Files:**
- Modify: `internal/screens/admin.go` (AdminListScreen, ~line 58-85)
- Modify: `internal/server/presenter_admin.go` (AdminList, ~line 89-104 — delete the literal/empty branch)
- Test: `internal/screens/admin_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/screens/admin_test.go`:

```go
func TestAdminListScreenCursorPopulated(t *testing.T) {
	screen, cur := AdminListScreen(DefaultGeometry, AdminListView{Title: "T", Rows: []string{"r0", "r1"}})
	cf, ok := fieldByName(screen, FieldCmdPrefix+"0")
	if !ok {
		t.Fatalf("missing %q field", FieldCmdPrefix+"0")
	}
	if want := cursorAt(cf); cur != want {
		t.Errorf("list cursor = %+v, want %+v (cmd0 field row %d col %d)", cur, want, cf.Row, cf.Col)
	}
}

func TestAdminListScreenCursorEmptyHomes(t *testing.T) {
	// No CMD input fields exist on an empty list, so the builder homes the
	// cursor to (0,0) — the one place that knows len(Rows)==0.
	_, cur := AdminListScreen(DefaultGeometry, AdminListView{Title: "T"})
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("empty-list cursor = %+v, want {0 0} (home)", cur)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/screens/ -run TestAdminListScreenCursor`
Expected: FAIL — `assignment mismatch: 2 variables but AdminListScreen returns 1 value`.

- [ ] **Step 3: Change the builder to return its cursor**

In `internal/screens/admin.go`, rewrite `AdminListScreen` to capture the first CMD field and home on empty:

```go
func AdminListScreen(geom Geometry, v AdminListView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: 2, Col: 2, Content: v.Header},
	}
	rows := v.Rows
	if size := geom.ListPageSize(); len(rows) > size {
		rows = rows[:size]
	}
	cur := Cursor{Row: 0, Col: 0} // empty list: no input field, home the cursor
	for i, r := range rows {
		row := 4 + i
		cmd := go3270.Field{Row: row, Col: 2, Name: fmt.Sprintf("%s%d", FieldCmdPrefix, i), Write: true, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = cursorAt(cmd)
		}
		screen = append(screen,
			cmd,
			go3270.Field{Row: row, Col: 4}, // stop field: 1-char command input
			go3270.Field{Row: row, Col: 7, Content: r},
		)
	}
	if len(rows) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: 7, Content: "(none)"})
	}
	screen = append(screen,
		go3270.Field{Row: geom.LegendRow(), Col: 2, Content: v.Legend},
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: v.PFHelp},
	)
	return screen, cur
}
```

- [ ] **Step 4: Fix the broken call sites and delete the presenter's literal**

Run: `go build ./... 2>&1`
Expected: errors at `internal/server/presenter_admin.go:90` and `internal/screens/admin_test.go:69,95,109,169,209`.

In `internal/server/presenter_admin.go` AdminList method, delete the hardcoded `crow, ccol` block (current lines 91-96) and consume the returned cursor:

```go
func (go3270Presenter) AdminList(conn net.Conn, term Term, v screens.AdminListView) (AdminListAction, error) {
	screen, cur := screens.AdminListScreen(term.Geometry(), v)
	resp, err := handleScreen(func() (go3270.Response, error) {
		return go3270.HandleScreenAlt(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			withSilentExits(adminListExitKeys),
			screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
		)
	})
	if err != nil {
		return AdminListAction{}, err
	}
	return listActionFromResponse(resp, len(v.Rows)), nil
}
```

In `internal/screens/admin_test.go`, add `, _` to each `AdminListScreen(...)` call:
- lines 69, 95, 109, 169, 209: `screen := AdminListScreen(...)` / `list := AdminListScreen(...)` → add `, _` after the assigned variable.

After editing, re-run `go build ./...` until clean.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/screens/ ./internal/server/ -race`
Expected: PASS (including both new list cursor tests).

- [ ] **Step 6: Commit**

```bash
git add internal/screens/admin.go internal/screens/admin_test.go internal/server/presenter_admin.go
git commit -m "feat(screens): AdminListScreen returns its initial cursor, homes empty list (#33)"
```

---

### Task 6: Admin form screen returns its cursor

**Files:**
- Modify: `internal/screens/admin.go` (AdminFormScreen, ~line 103-131 — including the stale `(3, 17)` prose comment)
- Modify: `internal/server/presenter_admin.go` (AdminForm, ~line 111-118)
- Test: `internal/screens/admin_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/screens/admin_test.go`:

```go
func TestAdminFormScreenCursor(t *testing.T) {
	fields := []AdminFormField{{Name: FieldName, Label: "Name"}, {Name: FieldHost, Label: "Host"}}
	screen, cur := AdminFormScreen(DefaultGeometry, AdminFormView{Title: "T", Fields: fields})
	ff, ok := fieldByName(screen, FieldName)
	if !ok {
		t.Fatalf("missing %q field", FieldName)
	}
	if want := cursorAt(ff); cur != want {
		t.Errorf("form cursor = %+v, want %+v (first field row %d col %d)", cur, want, ff.Row, ff.Col)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestAdminFormScreenCursor`
Expected: FAIL — `assignment mismatch: 2 variables but AdminFormScreen returns 1 value`.

- [ ] **Step 3: Change the builder to return its cursor**

In `internal/screens/admin.go`, replace the stale prose comment (the `(3, 17)` sentence at lines 103-105) and rewrite `AdminFormScreen` to capture the first input field:

```go
// AdminFormScreen renders v sized for geom. The first input is at row 3 col 16;
// inputs are two rows apart. The returned Cursor lands on that first input
// (or homes to {0,0} if v has no fields). Fields beyond geom.FormMaxFields()
// are truncated.
func AdminFormScreen(geom Geometry, v AdminFormView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
	}
	fields := v.Fields
	if max := geom.FormMaxFields(); len(fields) > max {
		fields = fields[:max]
	}
	cur := Cursor{Row: 0, Col: 0} // no fields: home the cursor
	for i, f := range fields {
		row := 3 + 2*i
		stopCol := 17 + f.Length
		if stopCol > 79 {
			stopCol = 79
		}
		input := go3270.Field{Row: row, Col: 16, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = cursorAt(input)
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Content: f.Label},
			input,
			go3270.Field{Row: row, Col: stopCol}, // stop field
		)
	}
	screen = append(screen,
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: "Enter = save    PF3 = cancel"},
	)
	return screen, cur
}
```

- [ ] **Step 4: Fix the broken call sites (compiler-driven)**

Run: `go build ./... 2>&1`
Expected: errors at `internal/server/presenter_admin.go:112` and `internal/screens/admin_test.go:124,142,187`.

In `internal/server/presenter_admin.go` AdminForm method:

```go
func (go3270Presenter) AdminForm(conn net.Conn, term Term, v screens.AdminFormView) (AdminFormAction, error) {
	screen, cur := screens.AdminFormScreen(term.Geometry(), v)
	resp, err := handleScreen(func() (go3270.Response, error) {
		return go3270.HandleScreenAlt(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			withSilentExits([]go3270.AID{go3270.AIDPF3}),
			screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
		)
	})
	if err != nil {
		return AdminFormAction{}, err
	}
	return formActionFromResponse(resp, v.Fields), nil
}
```

In `internal/screens/admin_test.go`, add `, _` to each `AdminFormScreen(...)` call:
- lines 124, 142, 187: `screen := AdminFormScreen(...)` / `form := AdminFormScreen(...)` → add `, _` after the assigned variable.

After editing, re-run `go build ./...` until clean.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./... -race`
Expected: PASS (whole module).

- [ ] **Step 6: Commit**

```bash
git add internal/screens/admin.go internal/screens/admin_test.go internal/server/presenter_admin.go
git commit -m "feat(screens): AdminFormScreen returns its initial cursor (#33)"
```

---

### Task 7: Smoke-test cursor assertions (close the rot gap)

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`

This task requires `s3270` installed. It adds a `ZZADMIN` user to the front seed, asserts the service-menu cursor in the existing scenario 3, and adds a new admin-navigation scenario asserting the admin-menu, users-list, and add-user-form cursors. The empty admin-list `(0,0)` case is NOT reachable here (Users/Groups/Services are all non-empty in the seed) and is covered by `TestAdminListScreenCursorEmptyHomes` (Task 5) — note this in the script comment so the gap is explicit, not silent.

- [ ] **Step 1: Add an admin user to the front seed**

In `.claude/skills/s3270-smoke-testing/smoke.sh`, change the front-seed `users` array (currently `alice` + `charlie`) to also include an admin. Replace:

```
 "users":[
  {"username":"alice","password":"changeme","groups":["ops"]},
  {"username":"charlie","password":"changeme","groups":["empty"]}],
```

with:

```
 "users":[
  {"username":"alice","password":"changeme","groups":["ops"]},
  {"username":"charlie","password":"changeme","groups":["empty"]},
  {"username":"admin","password":"changeme","groups":["ZZADMIN"]}],
```

(Seed's `CreateGroup` is idempotent and upper-folds; `ZZADMIN` is pre-created by `migrate()`, so this just attaches membership.)

- [ ] **Step 2: Assert the service-menu cursor in scenario 3**

In scenario 3 (the group-filtered menu), after the existing `3d` check, add:

```bash
# Cursor on the selection input (===> field at row 19 col 8 on a MOD 2);
# login is the only other screen and it reports 3 17, so 19 8 is the menu.
check "3e menu cursor on selection input" "I 2 24 80 19 8 " "$WORK/t3.out"
```

- [ ] **Step 3: Add the admin-navigation scenario**

Insert this new scenario after scenario 8 (before the final `echo`/summary):

```bash
# --- 9. admin cursor positions. admin is a ZZADMIN member with no service
# groups, so the menu shows the "A" entry over an empty service list and the
# selection field accepts letters. Walk: login -> service menu -> admin menu
# -> users list -> add-user form (PF4), capturing the cursor at each stop.
# The empty admin-list (0,0) cursor is NOT exercised here (every entity list is
# non-empty in this seed); TestAdminListScreenCursorEmptyHomes covers it. ---
s3 t9 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(admin)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Ascii()
String(A)
Enter()
Wait(5,InputField)
Ascii()
String(1)
Enter()
Wait(5,InputField)
Ascii()
PF(4)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "9a admin menu renders" "TN3270 GATEWAY ADMIN" "$WORK/t9.out"
# Admin menu option field at row 19 col 8 (same row as the service menu — both
# are correct; the service menu also reports 19 8 in this run).
check "9b admin/menu cursor on option field (19,8)" "I 2 24 80 19 8 " "$WORK/t9.out"
# Users list: first CMD field at row 4 col 3 — produced only by the list.
check "9c users list cursor on first CMD field (4,3)" "I 2 24 80 4 3 " "$WORK/t9.out"
# Add-user form: first input at row 3 col 17. Login also reports 3 17, so assert
# a 3 17 cursor line that appears AFTER the list's 4 3 line — that one is the
# form, not the earlier login screen.
if awk '/I 2 24 80 4 3 /{seen=1} seen && /I 2 24 80 3 17 /{ok=1} END{exit !ok}' "$WORK/t9.out"; then
  PASS=$((PASS+1)); echo "PASS: 9d add-user form cursor (3,17) after users list"
else
  FAIL=$((FAIL+1)); echo "FAIL: 9d add-user form cursor not at (3,17) after users list"
fi
```

- [ ] **Step 4: Run the smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: final line `=== N passed, 0 failed ...` with the new 3e and 9a–9d included in the passes. If `s3270` is not installed, install it (e.g. `brew install x3270`) before running; do not skip this task.

- [ ] **Step 5: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "test(smoke): assert per-screen cursor row/col (#33)"
```

---

### Task 8: Final verification and doc note

**Files:**
- Modify: `CLAUDE.md` (the go3270 cursor gotcha note)

- [ ] **Step 1: Full module test with race detector**

Run: `go build ./... && go test ./... -race`
Expected: PASS, no races.

- [ ] **Step 2: Update the CLAUDE.md gotcha note**

In `CLAUDE.md`, under "Gotchas", augment the **go3270 cursor** bullet so it records that the rule is now enforced in one place. Append this sentence to that bullet:

```
Builders now own this: each screen builder returns a `screens.Cursor` computed
via the single `cursorAt(field)` helper (`internal/screens/cursor.go`), and
presenters consume it instead of hardcoding coordinates — so moving a field
moves its cursor automatically. The smoke script asserts cursor row/col per
screen as the regression guard.
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: record builder-owned cursor convention (#33)"
```

- [ ] **Step 4: Confirm the diff against the spec**

Run: `git log --oneline main..HEAD`
Expected: the Task 1–8 commits, all conventional-prefixed, each focused.

---

## Notes for the executor

- **Why per-screen tasks (not all-builders-then-all-presenters):** adding a return value breaks every destructuring call site at compile time. Keeping each builder + its presenter + its tests in one task means the module compiles and `go test ./... -race` passes at every commit.
- **The cursor tests derive the expected cursor from the field** (`cursorAt(fieldByName(...))`), never a hardcoded literal — so if a field moves, the test follows it. That is the whole point of the issue; do not "simplify" them to `Cursor{3,17}`.
- **No behavior change is expected** — every cursor value is identical to today's hardcoded value. The unit tests and smoke run should pass without any coordinate surprises; if a smoke cursor assertion fails, a presenter is still passing a stale literal or a builder lost its field.
