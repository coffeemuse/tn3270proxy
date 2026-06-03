# Larger Terminal Models (MOD 3/4/5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the proxy's own screens (login, menu, admin) correctly on terminals larger than 24×80 by bottom-anchoring layout rows to the negotiated screen size and growing list capacity with the row count.

**Architecture:** A new `screens.Geometry` value (self-normalizing to 24×80) parameterizes every screen builder; a new `server.Term` value (terminal type + dimensions + unexported `go3270.DevInfo`) is returned by `Presenter.Negotiate` and threaded through the `Presenter`/`AdminPresenter` seams. The real presenter switches from `HandleScreen` to `HandleScreenAlt` and starts passing the detected codepage. Columns stay fixed at 0–79 (rows-only adaptation).

**Tech Stack:** Go, `github.com/racingmars/go3270` v0.9.13 (`DevInfo.AltDimensions()`, `HandleScreenAlt`, `Codepage()`).

**Spec:** `docs/superpowers/specs/2026-06-03-larger-terminals-design.md` — read it first, especially the geometry-formula table.

**Key library facts (verified against go3270 v0.9.13 source):**
- `HandleScreenAlt(screen, rules, values, pfkeys, exitkeys, errorField, crow, ccol, conn, dev, codepage...)` — with `dev == nil` it behaves exactly like `HandleScreen` (24×80). This is our fallback path.
- `go3270.DevInfo` has a private method — it **cannot be faked or constructed** outside go3270. Test fakes leave it nil; only the real `Negotiate` ever sets it.
- A nil `go3270.Codepage` argument means the library default (code page 1047) — identical to today's behavior, so passing `nil` is always safe.

**Build-order strategy:** Tasks 1–4 change `internal/screens` builders and immediately update their `internal/server` call sites with the fixed `screens.DefaultGeometry` so the tree builds and all tests pass at every commit. Tasks 5–6 introduce `Term` and replace `DefaultGeometry` at the call sites with the real negotiated geometry. Task 7 is housekeeping + docs + the manual smoke checklist.

---

## File structure

| File | Change | Responsibility |
|---|---|---|
| `internal/screens/geometry.go` | **Create** | `Geometry` type, normalization, all layout-row formulas (single source of truth) |
| `internal/screens/geometry_test.go` | **Create** | Table tests for the formulas across MOD 2/3/4/5 + zero/sub-MOD 2 |
| `internal/screens/login.go` | Modify | `LoginScreen(geom, errMsg)` — bottom-anchor error/help |
| `internal/screens/menu.go` | Modify | `MenuScreen(geom, ...)` — bottom-anchor + capacity/truncation |
| `internal/screens/admin.go` | Modify | `Admin*Screen(geom, ...)`; delete `AdminListPageSize`/`AdminFormMaxFields` consts |
| `internal/screens/*_test.go` | Modify | Pass geometry; new bottom-anchor + capacity tests |
| `internal/server/term.go` | **Create** | `Term` type, `normalizeTerm`, `Geometry()`, `codepage()` |
| `internal/server/term_test.go` | **Create** | Normalization/fallback unit tests |
| `internal/server/session.go` | Modify | `Presenter` interface takes/returns `Term`; thread through `Run`/`doLogin` |
| `internal/server/presenter.go` | Modify | `Negotiate` returns `Term`; `HandleScreenAlt` + codepage; dynamic cursor |
| `internal/server/presenter_admin.go` | Modify | `AdminPresenter` methods take `Term`; same switch |
| `internal/server/admin.go` | Modify | `adminFlow.term` field; `pageBounds` becomes a method with dynamic size |
| `internal/server/admin_users.go` / `admin_groups.go` / `admin_services.go` | Modify | `f.pageBounds(...)` + pass `f.term` to presenter calls |
| `internal/server/session_test.go`, `admin_test.go` | Modify | Fakes adopt `Term`; new threading + MOD 3 paging tests |
| `go.mod` | Modify | `go mod tidy` (go3270 is wrongly `// indirect`) |
| `docs/superpowers/ROADMAP.md`, `CLAUDE.md` | Modify | Mark #7 done; document the Geometry convention |

---

### Task 1: `screens.Geometry` — the layout formulas

**Files:**
- Create: `internal/screens/geometry.go`
- Test: `internal/screens/geometry_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/screens/geometry_test.go`:

```go
package screens

import "testing"

func TestGeometryFormulas(t *testing.T) {
	cases := []struct {
		name                                  string
		g                                     Geometry
		help, errRow, legend, input, page, form int
	}{
		{"zero value", Geometry{}, 23, 21, 20, 19, 14, 9},
		{"MOD 2", Geometry{Rows: 24, Cols: 80}, 23, 21, 20, 19, 14, 9},
		{"MOD 3", Geometry{Rows: 32, Cols: 80}, 31, 29, 28, 27, 22, 13},
		{"MOD 4", Geometry{Rows: 43, Cols: 80}, 42, 40, 39, 38, 33, 18},
		{"MOD 5", Geometry{Rows: 27, Cols: 132}, 26, 24, 23, 22, 17, 10},
		{"sub-MOD 2 falls back", Geometry{Rows: 12, Cols: 40}, 23, 21, 20, 19, 14, 9},
		{"tall but narrow falls back", Geometry{Rows: 43, Cols: 40}, 23, 21, 20, 19, 14, 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := []int{c.g.HelpRow(), c.g.ErrorRow(), c.g.LegendRow(), c.g.InputRow(), c.g.ListPageSize(), c.g.FormMaxFields()}
			want := []int{c.help, c.errRow, c.legend, c.input, c.page, c.form}
			for i, name := range []string{"HelpRow", "ErrorRow", "LegendRow", "InputRow", "ListPageSize", "FormMaxFields"} {
				if got[i] != want[i] {
					t.Errorf("%s() = %d, want %d", name, got[i], want[i])
				}
			}
		})
	}
}

func TestGeometryMenuCapacity(t *testing.T) {
	cases := []struct {
		g     Geometry
		admin bool
		want  int
	}{
		{Geometry{Rows: 24, Cols: 80}, false, 14}, // rows 4..17
		{Geometry{Rows: 24, Cols: 80}, true, 13},  // one row reserved for the A entry
		{Geometry{Rows: 32, Cols: 80}, false, 22},
		{Geometry{Rows: 43, Cols: 80}, true, 32},
		{Geometry{}, false, 14}, // zero value normalizes
	}
	for _, c := range cases {
		if got := c.g.MenuCapacity(c.admin); got != c.want {
			t.Errorf("%+v.MenuCapacity(%v) = %d, want %d", c.g, c.admin, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/screens/ -run TestGeometry -v`
Expected: FAIL — `undefined: Geometry`

- [ ] **Step 3: Implement `Geometry`**

Create `internal/screens/geometry.go`:

```go
package screens

// Geometry is the client terminal's screen size in rows × columns. The zero
// value — and anything smaller than the 24×80 MOD 2 default in either
// dimension — normalizes to 24×80, so callers may pass an unknown geometry
// safely. Content stays within columns 0–79 regardless of width (rows-only
// adaptation; see the larger-terminals design spec).
type Geometry struct{ Rows, Cols int }

// DefaultGeometry is the 24×80 MOD 2 screen every 3270 client supports.
var DefaultGeometry = Geometry{Rows: 24, Cols: 80}

// norm clamps zero/unknown/sub-MOD 2 dimensions to the 24×80 default.
func (g Geometry) norm() Geometry {
	if g.Rows < 24 || g.Cols < 80 {
		return DefaultGeometry
	}
	return g
}

// Bottom-anchored layout rows (CLAUDE.md layout convention: title on row 0,
// PF help on the last row, error line just above the action line). Every
// formula reproduces the historical fixed layout at 24 rows.

// HelpRow is the PF-key help line (last row; 23 on MOD 2).
func (g Geometry) HelpRow() int { return g.norm().Rows - 1 }

// ErrorRow is the red error/message line (21 on MOD 2).
func (g Geometry) ErrorRow() int { return g.norm().Rows - 3 }

// LegendRow is the admin-list line-command legend (20 on MOD 2).
func (g Geometry) LegendRow() int { return g.norm().Rows - 4 }

// InputRow is the "===>" command/selection input line (19 on MOD 2).
func (g Geometry) InputRow() int { return g.norm().Rows - 5 }

// ListPageSize is how many data rows fit on an admin list screen
// (rows 4 .. Rows-7; 14 on MOD 2).
func (g Geometry) ListPageSize() int { return g.norm().Rows - 10 }

// FormMaxFields is how many labeled inputs fit on an admin form
// (rows 3, 5, ..., Rows-5; 9 on MOD 2).
func (g Geometry) FormMaxFields() int { return (g.norm().Rows-8)/2 + 1 }

// MenuCapacity is how many service lines fit on the menu (rows 4 .. Rows-7,
// minus one when the admin A entry needs a reserved row).
func (g Geometry) MenuCapacity(admin bool) int {
	n := g.norm().Rows - 10
	if admin {
		n--
	}
	return n
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/screens/ -run TestGeometry -v`
Expected: PASS (both tests, all subtests)

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./... -race`
Expected: all PASS (nothing consumes `Geometry` yet)

```bash
git add internal/screens/geometry.go internal/screens/geometry_test.go
git commit -m "feat(screens): Geometry type with bottom-anchored layout formulas"
```

---

### Task 2: `LoginScreen` takes `Geometry`

**Files:**
- Modify: `internal/screens/login.go`
- Modify: `internal/screens/login_test.go`
- Modify: `internal/server/presenter.go:26` (call site — pass `DefaultGeometry` for now)

- [ ] **Step 1: Update existing tests and add the bottom-anchor test**

In `internal/screens/login_test.go`, change the two existing calls:

```go
	screen, rules := LoginScreen(DefaultGeometry, "")
```
```go
	screen, _ := LoginScreen(DefaultGeometry, "Invalid credentials")
```

Append a new test:

```go
func TestLoginScreenBottomAnchored(t *testing.T) {
	for _, g := range []Geometry{{Rows: 24, Cols: 80}, {Rows: 32, Cols: 80}, {Rows: 43, Cols: 80}, {Rows: 27, Cols: 132}} {
		screen, _ := LoginScreen(g, "err")
		f, ok := fieldByName(screen, FieldError)
		if !ok || f.Row != g.ErrorRow() {
			t.Errorf("%+v: error row = %d, want %d", g, f.Row, g.ErrorRow())
		}
		foundHelp := false
		for _, fl := range screen {
			if fl.Row == g.HelpRow() && fl.Content != "" {
				foundHelp = true
			}
			if fl.Row > g.HelpRow() {
				t.Errorf("%+v: field %+v beyond last row %d", g, fl, g.HelpRow())
			}
		}
		if !foundHelp {
			t.Errorf("%+v: no help line on last row %d", g, g.HelpRow())
		}
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/screens/ -run TestLogin -v`
Expected: FAIL — compile error (`too many arguments in call to LoginScreen`)

- [ ] **Step 3: Implement**

In `internal/screens/login.go`, replace the `LoginScreen` function (keep the doc comment, adding the geometry sentence):

```go
// LoginScreen returns the login screen and its validation rules, sized for
// geom (bottom rows anchored to the last screen rows). errMsg, if non-empty,
// is shown on the error line (e.g. a generic "invalid credentials" message
// after a failed sign-on). The caller drives it with go3270.HandleScreenAlt
// using AIDEnter to submit and AIDPF3 to quit, with errorField = FieldError.
func LoginScreen(geom Geometry, errMsg string) (go3270.Screen, go3270.Rules) {
	screen := go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY LOGIN"},
		{Row: 3, Col: 2, Content: "Userid . . ."},
		{Row: 3, Col: 16, Name: FieldUsername, Write: true, Highlighting: go3270.Underscore},
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
	return screen, rules
}
```

In `internal/server/presenter.go`, update the call site inside `Login` (real geometry arrives in Task 5):

```go
	screen, rules := screens.LoginScreen(screens.DefaultGeometry, errMsg)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/screens/ -run TestLogin -v` then `go test ./... -race`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/screens/login.go internal/screens/login_test.go internal/server/presenter.go
git commit -m "feat(screens): LoginScreen takes Geometry, bottom-anchors error/help rows"
```

---

### Task 3: `MenuScreen` takes `Geometry` (+ explicit truncation)

**Files:**
- Modify: `internal/screens/menu.go`
- Modify: `internal/screens/menu_test.go`
- Modify: `internal/server/presenter.go:45` (call site)

- [ ] **Step 1: Update existing tests and add geometry tests**

In `internal/screens/menu_test.go`, add `DefaultGeometry` as the first argument to every existing `MenuScreen(` call (6 call sites: `TestMenuScreenMapping`, `TestMenuScreenEmpty`, `TestMenuScreenShowsError`, `TestMenuScreenAdminEntry` ×2, `TestMenuScreenAdminEntryClampedWithManyServices`, `TestMenuScreenHelpSaysLogoff`), e.g.:

```go
	screen, mapping := MenuScreen(DefaultGeometry, svcs, false, "")
```

Append new tests:

```go
func TestMenuScreenBottomAnchored(t *testing.T) {
	for _, g := range []Geometry{{Rows: 24, Cols: 80}, {Rows: 32, Cols: 80}, {Rows: 43, Cols: 80}} {
		screen, _ := MenuScreen(g, nil, false, "err")
		sel, ok := fieldByName(screen, FieldSelection)
		if !ok || sel.Row != g.InputRow() {
			t.Errorf("%+v: selection row = %d, want %d", g, sel.Row, g.InputRow())
		}
		e, _ := fieldByName(screen, FieldError)
		if e.Row != g.ErrorRow() {
			t.Errorf("%+v: error row = %d, want %d", g, e.Row, g.ErrorRow())
		}
	}
}

func TestMenuScreenCapacityGrowsAndTruncates(t *testing.T) {
	svcs := make([]store.Service, 30)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}

	// MOD 2: truncated to capacity; nothing may touch the input row or below.
	g2 := Geometry{Rows: 24, Cols: 80}
	screen, mapping := MenuScreen(g2, svcs, false, "")
	if len(mapping) != g2.MenuCapacity(false) {
		t.Errorf("MOD 2 mapping = %d entries, want %d", len(mapping), g2.MenuCapacity(false))
	}
	for _, f := range screen {
		if f.Row >= g2.InputRow() && f.Name != FieldSelection && f.Name != FieldError &&
			f.Row != g2.InputRow() && f.Row != g2.ErrorRow() && f.Row != g2.HelpRow() {
			t.Errorf("MOD 2: unexpected field on row %d: %+v", f.Row, f)
		}
	}

	// MOD 3: more services fit.
	g3 := Geometry{Rows: 32, Cols: 80}
	_, mapping = MenuScreen(g3, svcs, false, "")
	if len(mapping) != g3.MenuCapacity(false) {
		t.Errorf("MOD 3 mapping = %d entries, want %d", len(mapping), g3.MenuCapacity(false))
	}
}

func TestMenuScreenAdminEntryNeverCollidesWhenFull(t *testing.T) {
	svcs := make([]store.Service, 30)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}
	g := Geometry{Rows: 24, Cols: 80}
	screen, mapping := MenuScreen(g, svcs, true, "")
	if len(mapping) != g.MenuCapacity(true) { // one less: row reserved for A entry
		t.Errorf("admin mapping = %d entries, want %d", len(mapping), g.MenuCapacity(true))
	}
	adminRow := -1
	occupied := map[int]int{}
	for _, f := range screen {
		if strings.Contains(f.Content, "Administration") {
			adminRow = f.Row
		}
		if f.Content != "" {
			occupied[f.Row]++
		}
	}
	if adminRow < 0 || adminRow > g.InputRow()-2 {
		t.Errorf("admin entry row = %d, want ≤ %d", adminRow, g.InputRow()-2)
	}
	if occupied[adminRow] != 1 {
		t.Errorf("admin entry shares row %d with another field", adminRow)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/screens/ -run TestMenu -v`
Expected: FAIL — compile error on the new signature

- [ ] **Step 3: Implement**

Replace `MenuScreen` in `internal/screens/menu.go`:

```go
// MenuScreen renders the service menu sized for geom and returns a mapping
// from the user's typed selection (e.g. "1") to the chosen service. When
// admin is true an "A.  Administration" entry is shown (handled by the
// presenter, not the mapping) and the selection field accepts letters.
// errMsg, if non-empty, is shown on the error line. Services beyond the
// screen's capacity are truncated (no pagination) so the list can never
// collide with the input/error/help rows.
func MenuScreen(geom Geometry, services []store.Service, admin bool, errMsg string) (go3270.Screen, map[string]store.Service) {
	screen := go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY MENU"},
		{Row: 2, Col: 2, Content: "Select a service and press ENTER:"},
	}

	shown := services
	if capacity := geom.MenuCapacity(admin); len(shown) > capacity {
		shown = shown[:capacity]
	}
	mapping := make(map[string]store.Service, len(shown))

	row := 4
	for i, svc := range shown {
		key := fmt.Sprintf("%d", i+1)
		mapping[key] = svc
		label := fmt.Sprintf("%2s.  %-20s (%s:%d)", key, svc.Name, svc.Host, svc.Port)
		screen = append(screen, go3270.Field{Row: row, Col: 4, Content: label})
		row++
	}
	if len(shown) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: 4, Content: "(no services available for your account)"})
		row = 5
	}
	if admin {
		adminRow := row + 1
		if last := geom.InputRow() - 2; adminRow > last {
			adminRow = last // MenuCapacity reserved this row when the list is full
		}
		screen = append(screen, go3270.Field{Row: adminRow, Col: 4, Content: " A.  Administration"})
	}

	screen = append(screen,
		go3270.Field{Row: geom.InputRow(), Col: 2, Content: "===>"},
		go3270.Field{Row: geom.InputRow(), Col: 7, Name: FieldSelection, Write: true, NumericOnly: !admin, Highlighting: go3270.Underscore},
		go3270.Field{Row: geom.InputRow(), Col: 15}, // stop field
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: "Enter = connect    PF3 = logoff    (PA3 returns here from a session)"},
	)
	return screen, mapping
}
```

In `internal/server/presenter.go`, update the call site inside `Menu`:

```go
		screen, mapping := screens.MenuScreen(screens.DefaultGeometry, svcs, admin, errMsg)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/screens/ -run TestMenu -v` then `go test ./... -race`
Expected: all PASS (including the pre-existing clamp test, now backed by real truncation)

- [ ] **Step 5: Commit**

```bash
git add internal/screens/menu.go internal/screens/menu_test.go internal/server/presenter.go
git commit -m "feat(screens): MenuScreen takes Geometry; capacity grows with rows, truncates explicitly"
```

---

### Task 4: Admin screen builders take `Geometry`

**Files:**
- Modify: `internal/screens/admin.go`
- Modify: `internal/screens/admin_test.go`
- Modify: `internal/server/presenter_admin.go` (3 call sites)
- Modify: `internal/server/admin.go:48` (`adminPageSize`)

- [ ] **Step 1: Update existing tests and add geometry tests**

In `internal/screens/admin_test.go`:
- Add `DefaultGeometry` as the first argument to every `AdminMenuScreen(`, `AdminListScreen(`, `AdminFormScreen(` call (6 call sites).
- Replace the deleted constants in the two overflow tests: `AdminListPageSize` → `DefaultGeometry.ListPageSize()` (3 uses in `TestAdminListScreenTruncatesOverflow`), `AdminFormMaxFields` → `DefaultGeometry.FormMaxFields()` (3 uses in `TestAdminFormScreenTruncatesOverflow`).

Append:

```go
func TestAdminScreensBottomAnchored(t *testing.T) {
	g := Geometry{Rows: 32, Cols: 80}

	menu := AdminMenuScreen(g, "boom")
	opt, _ := fieldByName(menu, FieldOption)
	if opt.Row != g.InputRow() {
		t.Errorf("admin menu option row = %d, want %d", opt.Row, g.InputRow())
	}

	list := AdminListScreen(g, AdminListView{Title: "T", Legend: "L", ErrMsg: "E", PFHelp: "H", Rows: []string{"r"}})
	e, _ := fieldByName(list, FieldError)
	if e.Row != g.ErrorRow() {
		t.Errorf("list error row = %d, want %d", e.Row, g.ErrorRow())
	}
	legendOK, helpOK := false, false
	for _, f := range list {
		if f.Row == g.LegendRow() && f.Content == "L" {
			legendOK = true
		}
		if f.Row == g.HelpRow() && f.Content == "H" {
			helpOK = true
		}
	}
	if !legendOK || !helpOK {
		t.Errorf("legend on %d / help on %d not found (legendOK=%v helpOK=%v)", g.LegendRow(), g.HelpRow(), legendOK, helpOK)
	}

	form := AdminFormScreen(g, AdminFormView{Title: "T", ErrMsg: "E"})
	e, _ = fieldByName(form, FieldError)
	if e.Row != g.ErrorRow() {
		t.Errorf("form error row = %d, want %d", e.Row, g.ErrorRow())
	}
}

func TestAdminListScreenPageSizeGrowsWithRows(t *testing.T) {
	g := Geometry{Rows: 32, Cols: 80} // page size 22
	rows := make([]string, 25)
	for i := range rows {
		rows[i] = fmt.Sprintf("row%d", i)
	}
	screen := AdminListScreen(g, AdminListView{Title: "T", Rows: rows})
	last := fmt.Sprintf("%s%d", FieldCmdPrefix, g.ListPageSize()-1)
	if _, ok := fieldByName(screen, last); !ok {
		t.Errorf("missing last in-page cmd field %q", last)
	}
	if _, ok := fieldByName(screen, fmt.Sprintf("%s%d", FieldCmdPrefix, g.ListPageSize())); ok {
		t.Errorf("row beyond MOD 3 page size should be truncated")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/screens/ -run TestAdmin -v`
Expected: FAIL — compile errors (signatures, removed constants)

- [ ] **Step 3: Implement the builders**

In `internal/screens/admin.go`:

Delete the `AdminListPageSize` and `AdminFormMaxFields` constant declarations (their docs move onto the `Geometry` methods, already written in Task 1).

Replace the three builders:

```go
// AdminListScreen renders v sized for geom. Data rows start at row 4; the CMD
// input for row i is named FieldCmdPrefix+i ("cmd0", "cmd1", ...). At most
// geom.ListPageSize() rows fit; rows beyond that are truncated — callers
// paginate via the same method.
func AdminListScreen(geom Geometry, v AdminListView) go3270.Screen {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: 2, Col: 2, Content: v.Header},
	}
	rows := v.Rows
	if size := geom.ListPageSize(); len(rows) > size {
		rows = rows[:size]
	}
	for i, r := range rows {
		row := 4 + i
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Name: fmt.Sprintf("%s%d", FieldCmdPrefix, i), Write: true, Highlighting: go3270.Underscore},
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
	return screen
}
```

```go
// AdminFormScreen renders v sized for geom. The first input is at row 3
// col 16 (so the caller's initial cursor is (3, 17)); inputs are two rows
// apart. Fields beyond geom.FormMaxFields() are truncated.
func AdminFormScreen(geom Geometry, v AdminFormView) go3270.Screen {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
	}
	fields := v.Fields
	if max := geom.FormMaxFields(); len(fields) > max {
		fields = fields[:max]
	}
	for i, f := range fields {
		row := 3 + 2*i
		stopCol := 17 + f.Length
		if stopCol > 79 {
			stopCol = 79
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Content: f.Label},
			go3270.Field{Row: row, Col: 16, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Highlighting: go3270.Underscore},
			go3270.Field{Row: row, Col: stopCol}, // stop field
		)
	}
	screen = append(screen,
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: "Enter = save    PF3 = cancel"},
	)
	return screen
}
```

```go
// AdminMenuScreen renders the top-level admin menu sized for geom. The caller
// drives it with HandleScreenAlt: AIDEnter submits, PF3 returns to the
// service menu.
func AdminMenuScreen(geom Geometry, errMsg string) go3270.Screen {
	return go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY ADMIN"},
		{Row: 3, Col: 4, Content: "1.  Users"},
		{Row: 4, Col: 4, Content: "2.  Groups"},
		{Row: 5, Col: 4, Content: "3.  Services"},
		{Row: geom.InputRow(), Col: 2, Content: "===>"},
		{Row: geom.InputRow(), Col: 7, Name: FieldOption, Write: true, Highlighting: go3270.Underscore},
		{Row: geom.InputRow(), Col: 11}, // stop field
		{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Content: "Enter = select    PF3 = main menu"},
	}
}
```

- [ ] **Step 4: Update the server call sites (placeholder geometry)**

In `internal/server/presenter_admin.go`:

```go
		screen := screens.AdminMenuScreen(screens.DefaultGeometry, errMsg)
```
```go
	screen := screens.AdminListScreen(screens.DefaultGeometry, v)
```
```go
	screen := screens.AdminFormScreen(screens.DefaultGeometry, v)
```

In `internal/server/admin.go`, replace the constant (Task 6 deletes this entirely):

```go
var adminPageSize = screens.DefaultGeometry.ListPageSize()
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/screens/ -v` then `go test ./... -race`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/screens/admin.go internal/screens/admin_test.go internal/server/presenter_admin.go internal/server/admin.go
git commit -m "feat(screens): admin builders take Geometry; page/form capacity from formulas"
```

---

### Task 5: `server.Term` + thread through `Presenter` seam

**Files:**
- Create: `internal/server/term.go`
- Create: `internal/server/term_test.go`
- Modify: `internal/server/session.go` (`Presenter` interface, `Run`, `doLogin`)
- Modify: `internal/server/presenter.go` (`Negotiate`, `Login`, `Menu` → `HandleScreenAlt` + codepage + dynamic cursor)
- Modify: `internal/server/session_test.go` (`fakePresenter` + new threading test)

- [ ] **Step 1: Write the failing `Term` tests**

Create `internal/server/term_test.go`:

```go
package server

import "testing"

func TestNormalizeTermFallsBackTo24x80(t *testing.T) {
	cases := []struct {
		name               string
		in                 Term
		wantRows, wantCols int
	}{
		{"zero dims", Term{Type: "IBM-3278-2"}, 24, 80},
		{"sub-MOD 2", Term{Type: "IBM-DYNAMIC", Rows: 12, Cols: 40}, 24, 80},
		{"narrow", Term{Type: "IBM-DYNAMIC", Rows: 43, Cols: 40}, 24, 80},
		{"MOD 2 exact", Term{Type: "IBM-3278-2", Rows: 24, Cols: 80}, 24, 80},
		{"MOD 4 kept", Term{Type: "IBM-3278-4", Rows: 43, Cols: 80}, 43, 80},
		{"MOD 5 kept", Term{Type: "IBM-3278-5", Rows: 27, Cols: 132}, 27, 132},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeTerm(c.in)
			if got.Rows != c.wantRows || got.Cols != c.wantCols {
				t.Errorf("normalizeTerm(%+v) = %dx%d, want %dx%d", c.in, got.Rows, got.Cols, c.wantRows, c.wantCols)
			}
			if got.Type != c.in.Type {
				t.Errorf("Type changed: %q → %q", c.in.Type, got.Type)
			}
			if (c.wantRows == 24 && c.in.Rows != 24) && got.dev != nil {
				t.Errorf("fallback must drop dev")
			}
		})
	}
}

func TestTermGeometryAndCodepage(t *testing.T) {
	tm := Term{Type: "IBM-3278-4", Rows: 43, Cols: 80}
	if g := tm.Geometry(); g.Rows != 43 || g.Cols != 80 {
		t.Errorf("Geometry() = %+v", g)
	}
	if cp := tm.codepage(); cp != nil {
		t.Errorf("nil dev → codepage must be nil, got %v", cp)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestNormalizeTerm|TestTermGeometry' -v`
Expected: FAIL — `undefined: Term`

- [ ] **Step 3: Implement `Term`**

Create `internal/server/term.go`:

```go
package server

import (
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/racingmars/go3270"
)

// Term describes the negotiated client terminal. The real presenter's
// Negotiate fills dev so screens render at the terminal's alternate size with
// its detected codepage; test fakes construct Term directly and leave dev nil
// (go3270 cannot be faked: DevInfo has a private method), which renders plain
// 24×80 — exactly the fallback behavior.
type Term struct {
	Type       string // negotiated terminal type, e.g. "IBM-3278-4-E"
	Rows, Cols int    // alternate screen dimensions

	dev go3270.DevInfo // nil ⇒ HandleScreenAlt behaves like HandleScreen (24×80)
}

// normalizeTerm applies the fallback rule: an unknown or sub-MOD 2 alternate
// size renders as plain 24×80 (dev dropped so go3270 never writes to an
// alternate buffer smaller than the layout).
func normalizeTerm(t Term) Term {
	if t.Rows < 24 || t.Cols < 80 {
		t.Rows, t.Cols, t.dev = 24, 80, nil
	}
	return t
}

// Geometry converts to the screens-layer value. screens.Geometry normalizes
// again on its own — belt and braces for fakes that skip normalizeTerm.
func (t Term) Geometry() screens.Geometry {
	return screens.Geometry{Rows: t.Rows, Cols: t.Cols}
}

// codepage is the detected client codepage; nil selects go3270's default
// (code page 1047, the pre-existing behavior).
func (t Term) codepage() go3270.Codepage {
	if t.dev == nil {
		return nil
	}
	return t.dev.Codepage()
}
```

- [ ] **Step 4: Run the Term tests**

Run: `go test ./internal/server/ -run 'TestNormalizeTerm|TestTermGeometry' -v`
Expected: PASS

- [ ] **Step 5: Update the `Presenter` seam — tests first**

In `internal/server/session_test.go`:

Add capture fields to `fakePresenter` and update the three methods (the struct keeps its existing fields):

```go
type fakePresenter struct {
	termType     string
	rows, cols   int // 0,0 → Negotiate reports 24×80
	logins       []loginResult
	menuPicks    []menuResult
	menuErrors   []string
	loginErrors  []string
	gotAdminFlag []bool
	gotTerms     []Term // every term passed to Login/Menu, in call order
}
```

```go
func (f *fakePresenter) Negotiate(conn net.Conn) (Term, error) {
	rows, cols := f.rows, f.cols
	if rows == 0 {
		rows, cols = 24, 80
	}
	return Term{Type: f.termType, Rows: rows, Cols: cols}, nil
}

func (f *fakePresenter) Login(conn net.Conn, term Term, errMsg string) (string, string, bool, error) {
	f.gotTerms = append(f.gotTerms, term)
	f.loginErrors = append(f.loginErrors, errMsg)
	r := f.logins[0]
	f.logins = f.logins[1:]
	return r.user, r.pass, r.quit, r.err
}

func (f *fakePresenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, errMsg string) (*store.Service, bool, bool, error) {
	f.gotTerms = append(f.gotTerms, term)
	f.menuErrors = append(f.menuErrors, errMsg)
	f.gotAdminFlag = append(f.gotAdminFlag, admin)
	r := f.menuPicks[0]
	f.menuPicks = f.menuPicks[1:]
	return r.sel, r.admin, r.quit, r.err
}
```

Append a threading test (mirror the conn setup of the existing tests — they pass a `client` conn from a pipe/helper; reuse the same pattern):

```go
func TestSessionThreadsTermToScreens(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-4",
		rows:      43,
		cols:      80,
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	client, srv := net.Pipe()
	defer client.Close()
	go func() { s.Run(srv); srv.Close() }()
	// drain until the session ends (fakes never touch the conn)
	buf := make([]byte, 1)
	for {
		if _, err := client.Read(buf); err != nil {
			break
		}
	}
	if len(p.gotTerms) == 0 {
		t.Fatal("no terms captured")
	}
	for i, term := range p.gotTerms {
		if term.Type != "IBM-3278-4" || term.Rows != 43 || term.Cols != 80 {
			t.Errorf("call %d: term = %+v, want IBM-3278-4 43x80", i, term)
		}
	}
}
```

**Note:** if the existing tests call `s.Run` differently (e.g. synchronously with a dummy conn), copy *their* pattern instead — the fakes never read or write the conn, so whatever conn the suite already uses is correct.

- [ ] **Step 6: Run to verify failure**

Run: `go test ./internal/server/ -v 2>&1 | head -30`
Expected: FAIL — compile errors (interface mismatch)

- [ ] **Step 7: Implement the seam change**

In `internal/server/session.go`, replace the `Presenter` interface:

```go
// Presenter renders the proxy's own 3270 screens to the client. The real
// implementation wraps go3270; tests use a fake. The Term returned by
// Negotiate must be passed back into every subsequent call so screens render
// at the client's negotiated size and codepage.
type Presenter interface {
	Negotiate(conn net.Conn) (Term, error)
	Login(conn net.Conn, term Term, errMsg string) (username, password string, quit bool, err error)
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, errMsg string) (selected *store.Service, adminSel bool, quit bool, err error)
}
```

In `Session.Run`, rename the negotiation result and thread it:

```go
	term, err := s.Presenter.Negotiate(conn)
	if err != nil {
		log.Printf("telnet negotiation failed: %v", err)
		return
	}
```
```go
		identity, ok := s.doLogin(ctx, conn, term)
```
```go
			selected, adminSel, quit, err := s.Presenter.Menu(conn, term, services, isAdmin, errMsg)
```
```go
			cause, berr := s.Bridger.Bridge(conn, addr, term.Type, s.EscapeAID, btls)
```

(The `adminFlow` literal gains its `term` field in Task 6.)

And `doLogin`:

```go
func (s *Session) doLogin(ctx context.Context, conn net.Conn, term Term) (auth.Identity, bool) {
	errMsg := ""
	for {
		user, pass, quit, err := s.Presenter.Login(conn, term, errMsg)
```

In `internal/server/presenter.go`, replace `Negotiate`, `Login`, and `Menu`:

```go
func (go3270Presenter) Negotiate(conn net.Conn) (Term, error) {
	dev, err := go3270.NegotiateTelnet(conn)
	if err != nil {
		return Term{}, err
	}
	rows, cols := dev.AltDimensions()
	return normalizeTerm(Term{Type: dev.TerminalType(), Rows: rows, Cols: cols, dev: dev}), nil
}

func (go3270Presenter) Login(conn net.Conn, term Term, errMsg string) (string, string, bool, error) {
	screen, rules := screens.LoginScreen(term.Geometry(), errMsg)
	resp, err := go3270.HandleScreenAlt(
		screen, rules, map[string]string{},
		[]go3270.AID{go3270.AIDEnter},
		[]go3270.AID{go3270.AIDPF3},
		screens.FieldError, 3, 17, conn, term.dev, term.codepage(),
	)
	if err != nil {
		return "", "", false, err
	}
	if resp.AID == go3270.AIDPF3 {
		return "", "", true, nil
	}
	return strings.TrimSpace(resp.Values[screens.FieldUsername]),
		resp.Values[screens.FieldPassword], false, nil
}

func (go3270Presenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, errMsg string) (*store.Service, bool, bool, error) {
	geom := term.Geometry()
	for {
		screen, mapping := screens.MenuScreen(geom, svcs, admin, errMsg)
		resp, err := go3270.HandleScreenAlt(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			[]go3270.AID{go3270.AIDPF3},
			screens.FieldError, geom.InputRow(), 8, conn, term.dev, term.codepage(),
		)
		if err != nil {
			return nil, false, false, err
		}
		if resp.AID == go3270.AIDPF3 {
			return nil, false, true, nil
		}
		key := strings.ToUpper(strings.TrimSpace(resp.Values[screens.FieldSelection]))
		if admin && key == "A" {
			return nil, true, false, nil
		}
		if svc, ok := mapping[key]; ok {
			return &svc, false, false, nil
		}
		errMsg = "Invalid selection: " + key
	}
}
```

(Cursor rule preserved: the menu input field's attribute byte is at `(InputRow, 7)`, so the cursor goes to column 8. Login stays `(3, 17)` — top-anchored.)

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/server/ -v 2>&1 | tail -20` then `go test ./... -race`
Expected: all PASS

- [ ] **Step 9: Commit**

```bash
git add internal/server/term.go internal/server/term_test.go internal/server/session.go internal/server/presenter.go internal/server/session_test.go
git commit -m "feat(server): Term threads negotiated dimensions+codepage through the Presenter seam"
```

---

### Task 6: Thread `Term` through `AdminPresenter` + dynamic paging

**Files:**
- Modify: `internal/server/presenter_admin.go` (interface + 3 impls)
- Modify: `internal/server/admin.go` (`adminFlow.term`, `pageBounds` method, delete `adminPageSize`)
- Modify: `internal/server/admin_users.go`, `admin_groups.go`, `admin_services.go` (call sites)
- Modify: `internal/server/session.go` (pass `term` to `adminFlow`)
- Modify: `internal/server/admin_test.go` (fake + fixture + paging tests)

- [ ] **Step 1: Update tests first**

In `internal/server/admin_test.go`:

Update the three `fakeAdminPresenter` methods to accept (and ignore) a `Term`:

```go
func (f *fakeAdminPresenter) AdminMenu(_ net.Conn, _ Term, errMsg string) (int, bool, error) {
```
```go
func (f *fakeAdminPresenter) AdminList(_ net.Conn, _ Term, v screens.AdminListView) (AdminListAction, error) {
```
```go
func (f *fakeAdminPresenter) AdminForm(_ net.Conn, _ Term, v screens.AdminFormView) (AdminFormAction, error) {
```

In `newAdminFixture`, add the term to the flow literal:

```go
	f := &adminFlow{
		store:     st,
		presenter: p,
		identity:  auth.Identity{UserID: ids["root"], Username: "root", Groups: []string{store.AdminGroup}},
		term:      Term{Type: "IBM-3278-2", Rows: 24, Cols: 80},
	}
```

In `TestPageBounds` (the table-driven test), switch to the method and add MOD 3 coverage — replace the call line with:

```go
		f := &adminFlow{term: Term{Rows: 24, Cols: 80}}
		page, s, e, info := f.pageBounds(c.page, c.total)
```

and append a new test after it:

```go
func TestPageBoundsGrowsWithTerminalRows(t *testing.T) {
	f := &adminFlow{term: Term{Rows: 32, Cols: 80}} // page size 22
	page, s, e, info := f.pageBounds(0, 30)
	if page != 0 || s != 0 || e != 22 || info != "ROW 1 TO 22 OF 30" {
		t.Errorf("MOD 3 pageBounds(0,30) = %d,%d,%d,%q", page, s, e, info)
	}
	page, s, e, info = f.pageBounds(1, 30)
	if page != 1 || s != 22 || e != 30 || info != "ROW 23 TO 30 OF 30" {
		t.Errorf("MOD 3 pageBounds(1,30) = %d,%d,%d,%q", page, s, e, info)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/ -v 2>&1 | head -20`
Expected: FAIL — compile errors

- [ ] **Step 3: Implement**

In `internal/server/presenter_admin.go`, update the interface:

```go
// AdminPresenter renders the admin screens. The real implementation wraps
// go3270; adminFlow tests use a fake. term carries the client's negotiated
// screen size (and codepage) from Presenter.Negotiate.
type AdminPresenter interface {
	// AdminMenu returns choice 1/2/3 (users/groups/services) or back (PF3,
	// to the service menu). It loops internally on invalid input.
	AdminMenu(conn net.Conn, term Term, errMsg string) (choice int, back bool, err error)
	AdminList(conn net.Conn, term Term, v screens.AdminListView) (AdminListAction, error)
	AdminForm(conn net.Conn, term Term, v screens.AdminFormView) (AdminFormAction, error)
}
```

and the three implementations:

```go
func (go3270Presenter) AdminMenu(conn net.Conn, term Term, errMsg string) (int, bool, error) {
	geom := term.Geometry()
	for {
		screen := screens.AdminMenuScreen(geom, errMsg)
		resp, err := go3270.HandleScreenAlt(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			[]go3270.AID{go3270.AIDPF3},
			screens.FieldError, geom.InputRow(), 8, conn, term.dev, term.codepage(),
		)
		if err != nil {
			return 0, false, err
		}
		if resp.AID == go3270.AIDPF3 {
			return 0, true, nil
		}
		switch strings.TrimSpace(resp.Values[screens.FieldOption]) {
		case "1":
			return 1, false, nil
		case "2":
			return 2, false, nil
		case "3":
			return 3, false, nil
		}
		errMsg = "Invalid option"
	}
}

func (go3270Presenter) AdminList(conn net.Conn, term Term, v screens.AdminListView) (AdminListAction, error) {
	screen := screens.AdminListScreen(term.Geometry(), v)
	// cursor on the first CMD field (attribute col 2 → input col 3); no input
	// fields exist on an empty list, so home the cursor there.
	crow, ccol := 4, 3
	if len(v.Rows) == 0 {
		crow, ccol = 0, 0
	}
	resp, err := go3270.HandleScreenAlt(
		screen, nil, map[string]string{},
		[]go3270.AID{go3270.AIDEnter},
		adminListExitKeys,
		screens.FieldError, crow, ccol, conn, term.dev, term.codepage(),
	)
	if err != nil {
		return AdminListAction{}, err
	}
	return listActionFromResponse(resp, len(v.Rows)), nil
}

func (go3270Presenter) AdminForm(conn net.Conn, term Term, v screens.AdminFormView) (AdminFormAction, error) {
	screen := screens.AdminFormScreen(term.Geometry(), v)
	resp, err := go3270.HandleScreenAlt(
		screen, nil, map[string]string{},
		[]go3270.AID{go3270.AIDEnter},
		[]go3270.AID{go3270.AIDPF3},
		screens.FieldError, 3, 17, conn, term.dev, term.codepage(),
	)
	if err != nil {
		return AdminFormAction{}, err
	}
	return formActionFromResponse(resp, v.Fields), nil
}
```

In `internal/server/admin.go`:
- Delete the `var adminPageSize = ...` line (and the `screens` import if now unused — it is still used by other files in the package, but check this file's own imports).
- Add the field to `adminFlow`:

```go
type adminFlow struct {
	store     AdminStore
	presenter AdminPresenter
	identity  auth.Identity
	term      Term // negotiated client terminal; drives page size + screen rendering
}
```

- Convert `pageBounds` to a method with dynamic size:

```go
// pageBounds clamps page to the data and returns the slice bounds plus the
// row indicator. The page size follows the client terminal's row count.
// Clamping matters after deletions shrink the list.
func (f *adminFlow) pageBounds(page, total int) (clamped, start, end int, info string) {
	size := f.term.Geometry().ListPageSize()
	if total == 0 {
		return 0, 0, 0, "ROW 0 OF 0"
	}
	maxPage := (total - 1) / size
	if page > maxPage {
		page = maxPage
	}
	if page < 0 {
		page = 0
	}
	start = page * size
	end = min(start+size, total)
	return page, start, end, fmt.Sprintf("ROW %d TO %d OF %d", start+1, end, total)
}
```

- In `adminFlow.Run`, pass the term to the menu:

```go
		choice, back, err := f.presenter.AdminMenu(conn, f.term, errMsg)
```

Update **every** call site in `admin_users.go`, `admin_groups.go`, `admin_services.go` (mechanical; exact locations as of this writing):

| File | Line (≈) | Old | New |
|---|---|---|---|
| `admin_users.go` | 29 | `pageBounds(page, len(users))` | `f.pageBounds(page, len(users))` |
| `admin_users.go` | 39 | `f.presenter.AdminList(conn, screens.AdminListView{` | `f.presenter.AdminList(conn, f.term, screens.AdminListView{` |
| `admin_users.go` | 163 | `f.presenter.AdminForm(conn, screens.AdminFormView{` | `f.presenter.AdminForm(conn, f.term, screens.AdminFormView{` |
| `admin_users.go` | 212 | `f.presenter.AdminForm(conn, screens.AdminFormView{` | `f.presenter.AdminForm(conn, f.term, screens.AdminFormView{` |
| `admin_users.go` | 264 | `pageBounds(page, len(groups))` | `f.pageBounds(page, len(groups))` |
| `admin_users.go` | 274 | `f.presenter.AdminList(conn, screens.AdminListView{` | `f.presenter.AdminList(conn, f.term, screens.AdminListView{` |
| `admin_groups.go` | 32 | `pageBounds(page, len(groups))` | `f.pageBounds(page, len(groups))` |
| `admin_groups.go` | 43 | `f.presenter.AdminList(conn, screens.AdminListView{` | `f.presenter.AdminList(conn, f.term, screens.AdminListView{` |
| `admin_groups.go` | 130 | `pageBounds(page, len(users))` | `f.pageBounds(page, len(users))` |
| `admin_groups.go` | 140 | `f.presenter.AdminList(conn, screens.AdminListView{` | `f.presenter.AdminList(conn, f.term, screens.AdminListView{` |
| `admin_groups.go` | 192 | `f.presenter.AdminForm(conn, screens.AdminFormView{` | `f.presenter.AdminForm(conn, f.term, screens.AdminFormView{` |
| `admin_services.go` | 45 | `pageBounds(page, len(svcs))` | `f.pageBounds(page, len(svcs))` |
| `admin_services.go` | 52 | `f.presenter.AdminList(conn, screens.AdminListView{` | `f.presenter.AdminList(conn, f.term, screens.AdminListView{` |
| `admin_services.go` | 129 | `f.presenter.AdminForm(conn, screens.AdminFormView{` | `f.presenter.AdminForm(conn, f.term, screens.AdminFormView{` |
| `admin_services.go` | 220 | `pageBounds(page, len(groups))` | `f.pageBounds(page, len(groups))` |
| `admin_services.go` | 230 | `f.presenter.AdminList(conn, screens.AdminListView{` | `f.presenter.AdminList(conn, f.term, screens.AdminListView{` |

(If line numbers have drifted, grep: `grep -n 'pageBounds(\|presenter.Admin' internal/server/admin_*.go` — every `pageBounds(` gains `f.` and every `AdminList(conn,`/`AdminForm(conn,` gains `f.term`.)

In `internal/server/session.go`, complete the `adminFlow` literal:

```go
				flow := &adminFlow{store: s.Store, presenter: s.AdminPresenter, identity: identity, term: term}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -v 2>&1 | tail -20` then `go test ./... -race`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/presenter_admin.go internal/server/admin.go internal/server/admin_users.go internal/server/admin_groups.go internal/server/admin_services.go internal/server/session.go internal/server/admin_test.go
git commit -m "feat(admin): thread Term through AdminPresenter; list paging follows terminal rows"
```

---

### Task 7: Housekeeping, docs, and verification

**Files:**
- Modify: `go.mod` / `go.sum` (tidy)
- Modify: `docs/superpowers/ROADMAP.md` (#7 completion block + progress line)
- Modify: `CLAUDE.md` (screens convention now geometry-driven)

- [ ] **Step 1: Tidy the module**

Run: `go mod tidy && go build ./...`
Expected: `github.com/racingmars/go3270` loses its `// indirect` comment in `go.mod` (it has always been a direct dependency); build clean.

- [ ] **Step 2: Full suite**

Run: `go test ./... -race`
Expected: all PASS

- [ ] **Step 3: Update `docs/superpowers/ROADMAP.md`**

- Add to the `## 7. Support larger terminal models` heading: ` ✅ **DONE** *(merged; live smoke test pending)*` and insert a completion block (mirroring the style of #1–#3.5) summarizing: `screens.Geometry` (self-normalizing, formula methods), `server.Term` (dimensions + codepage through the Presenter/AdminPresenter seams), `HandleScreenAlt` switch, rows-only adaptation (columns fixed at 0–79), explicit menu truncation, dynamic admin page size.
- Update the **Progress** paragraph: milestones 1/2/3/3.5/**7** complete; remaining order 5 → 4 → 6; next up #5 (audit logging).
- Update the "Quick reference" table row for larger terminals to ✅ done.

- [ ] **Step 4: Update `CLAUDE.md`**

- In the architecture map, `internal/screens` line: mention builders take a `Geometry` (self-normalizing to 24×80; formulas for bottom-anchored rows).
- In Gotchas, amend the screen-layout bullet: the help line is on the **last row (`geom.HelpRow()`, 23 on MOD 2)** and the error line on `geom.ErrorRow()`; new screens must take `Geometry` instead of hard-coding rows.
- In the architecture map, `internal/server` line: mention `Term` (negotiated type + dimensions + codepage) threaded from `Negotiate` through both presenter seams.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum docs/superpowers/ROADMAP.md CLAUDE.md
git commit -m "docs: roadmap #7 implemented; document Geometry/Term conventions; go mod tidy"
```

- [ ] **Step 6: Manual smoke test (live emulator — required before declaring done)**

Unit tests cannot verify the 3270 protocol surface (CLAUDE.md). With a seeded DB (`./bin/tn3270proxy seed -db proxy.db -file seed.example.json && ./bin/tn3270proxy serve -db proxy.db -listen :2323`):

1. **MOD 2 regression** — `c3270 -model 3279-2 127.0.0.1:2323`: login renders exactly as before (error row 21, help row 23, cursor on Userid); menu, admin menu, admin lists (legend 20), admin forms all unchanged; full walk login → menu → admin → lists → forms → PF3 chain back out.
2. **MOD 4 rows** — `c3270 -model 3279-4 127.0.0.1:2323` (43×80): help line on the *bottom* row (42), error on 40, menu input on 38; no floating help line mid-screen; admin USERS list shows up to 33 rows per page and the `ROW x TO y OF z` indicator matches; PF7/PF8 paging consistent; cursor lands on the first input field on every screen.
3. **MOD 3** — `c3270 -model 3279-3 127.0.0.1:2323` (32×80): spot-check login + one admin list (22 rows/page).
4. **MOD 5 width** — `c3270 -model 3278-5 127.0.0.1:2323` (27×132): content left-aligned in the first 80 columns, no wrapped/garbled lines; bottom rows on 26/24/22.
5. **Bridge unaffected** — from a larger model, connect to a backend service, verify PA3 still escapes back to the menu, and the menu re-renders at the large size afterwards.

Record results; update the ROADMAP "smoke test pending" qualifier when passed.

---

## Self-review notes

- **Spec coverage:** Geometry formulas/table → Task 1; login/menu/admin builders → Tasks 2–4; Term + seam threading + normalization fallback + codepage → Task 5; AdminPresenter + dynamic paging → Task 6; truncation rule incl. admin-entry reservation → Tasks 1 & 3; testing strategy → per-task tests + Task 7 smoke checklist; docs/tidy → Task 7.
- **Type consistency:** `screens.Geometry` methods (`HelpRow/ErrorRow/LegendRow/InputRow/ListPageSize/FormMaxFields/MenuCapacity`) defined in Task 1 and used identically in Tasks 2–6; `server.Term` (`Type/Rows/Cols/dev`, `Geometry()`, `codepage()`, `normalizeTerm`) defined in Task 5 and used in Tasks 5–6.
- **Tree builds at every commit:** Tasks 2–4 update server call sites with `DefaultGeometry` in the same commit as each builder signature change; Tasks 5–6 swap in the real geometry.
