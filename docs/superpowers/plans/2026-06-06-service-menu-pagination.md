# Service Menu Pagination (PF7/PF8) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the group-filtered service menu page through the user's full service list with PF8 (page down) / PF7 (page up) instead of silently truncating the overflow.

**Architecture:** Keep the bespoke `screens.MenuScreen` builder and `go3270Presenter.Menu` loop (the menu's selection-by-number + status-block model can't reuse `ui3270.RunList`). A single pure helper, `screens.MenuPageBounds`, owns the paging math (clamp + window + indicator) and is the only place that math lives — `MenuScreen` uses it to render the window, the presenter uses it to clamp its stored page so PF7/PF8 no-op at the ends. Numbering is global/stable: `MenuScreen` returns a mapping covering *all* services, renders only the current page's slice with global row numbers, and anchors the `0`/`A` meta band to a fixed bottom position on every page. A new blank separator row sits above the PF legend (menu only).

**Tech Stack:** Go 1.25, `github.com/racingmars/go3270`, `modernc.org/sqlite`, s3270 for the protocol smoke test.

**Spec:** `docs/superpowers/specs/2026-06-06-service-menu-pagination-design.md`

---

## File Structure

- **`internal/screens/geometry.go`** (modify) — add unexported `menuBottomRow()` (effective menu bottom = `BodyBottomRow()-1`, reserving the blank separator); `MenuCapacity` uses it.
- **`internal/screens/geometry_test.go`** (modify) — update the `MenuCapacity` expectation table to the one-row-shorter values.
- **`internal/screens/menu.go`** (modify) — add exported `MenuPageBounds`; rewrite `MenuScreen` to take a `page` parameter, return a full mapping, render the page window with global numbers, anchor the meta band to `menuBottomRow()`, render the `ITEMS x TO y OF z` indicator, and add PF7/PF8 to the help line.
- **`internal/screens/menu_paging_test.go`** (create) — unit tests for `MenuPageBounds`.
- **`internal/screens/menu_test.go`** (modify) — add the `page` arg to all existing `MenuScreen` calls; fix the two tests that asserted `len(mapping) == MenuCapacity` (mapping is now the full count); add multi-page render tests.
- **`internal/server/presenter.go`** (modify) — `go3270Presenter.Menu` tracks `page`, clamps it via `MenuPageBounds`, adds `AIDPF7`/`AIDPF8` as exit keys, and pages on them.
- **`.claude/skills/s3270-smoke-testing/smoke.sh`** (modify) — seed a 22-service `pager` user and add a multi-page menu scenario (content + indicator + cursor + PF7/PF8).
- **`CLAUDE.md`** (modify) — update the `screens` package-map entry for the new `MenuScreen` signature and drop the "truncated (no pagination)" note.

---

## Task 1: Reserve the blank separator row in menu geometry

**Files:**
- Modify: `internal/screens/geometry.go:64-70` (`MenuCapacity`)
- Test: `internal/screens/geometry_test.go:51-71` (`TestGeometryMenuCapacity`)

- [ ] **Step 1: Update the failing test**

Replace the `cases` table and comments in `TestGeometryMenuCapacity` (`internal/screens/geometry_test.go`) with the one-row-shorter expectations (the blank separator above the PF legend costs one service row):

```go
	cases := []struct {
		g     Geometry
		admin bool
		want  int
	}{
		{Geometry{Rows: 24, Cols: 80}, false, 17}, // body rows 4..21 minus "0 User Settings"; row 22 is the blank separator
		{Geometry{Rows: 24, Cols: 80}, true, 16},  // one more row reserved for the A entry
		{Geometry{Rows: 32, Cols: 80}, false, 25},
		{Geometry{Rows: 43, Cols: 80}, true, 35},
		{Geometry{Rows: 27, Cols: 132}, false, 20},
		{Geometry{Rows: 27, Cols: 132}, true, 19},
		{Geometry{}, false, 17}, // zero value normalizes
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/screens/ -run TestGeometryMenuCapacity -v`
Expected: FAIL — e.g. `MenuCapacity(false) = 18, want 17`.

- [ ] **Step 3: Implement the geometry change**

In `internal/screens/geometry.go`, add `menuBottomRow` just above `MenuCapacity` and change `MenuCapacity` to use it:

```go
// menuBottomRow is the menu's effective bottom content row: one above
// BodyBottomRow, leaving a blank separator line above the PF-key legend
// (menu-only; other screens use BodyBottomRow directly).
func (g Geometry) menuBottomRow() int { return g.BodyBottomRow() - 1 }

// MenuCapacity is how many service lines fit on one menu page: body rows
// BodyTopRow+1 .. menuBottomRow (row BodyTopRow is the instruction line), minus
// one for the always-present "0 User Settings" row and one more for the admin
// "A" row. The blank separator above the PF legend is excluded via menuBottomRow.
func (g Geometry) MenuCapacity(admin bool) int {
	n := g.menuBottomRow() - (g.BodyTopRow() + 1) + 1 - 1 // body rows minus the "0" meta row
	if admin {
		n--
	}
	return n
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/screens/ -run TestGeometryMenuCapacity -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/geometry.go internal/screens/geometry_test.go
git commit -m "feat(screens): reserve a blank separator row in menu capacity (GH #77)"
```

---

## Task 2: `MenuPageBounds` paging-math helper

**Files:**
- Modify: `internal/screens/menu.go` (add `MenuPageBounds`)
- Test: `internal/screens/menu_paging_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/screens/menu_paging_test.go`:

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

import "testing"

func TestMenuPageBounds(t *testing.T) {
	g := DefaultGeometry // MOD 2: capacity 17 non-admin, 16 admin
	cases := []struct {
		name                   string
		total, page            int
		admin                  bool
		wantPage, wantStart    int
		wantEnd                int
		wantIndicator          string
	}{
		{"empty", 0, 0, false, 0, 0, 0, "ITEMS 0 OF 0"},
		{"empty ignores page", 0, 3, false, 0, 0, 0, "ITEMS 0 OF 0"},
		{"single page", 5, 0, false, 0, 0, 5, "ITEMS 1 TO 5 OF 5"},
		{"single page over-clamps", 5, 9, false, 0, 0, 5, "ITEMS 1 TO 5 OF 5"},
		{"two pages first", 22, 0, false, 0, 0, 17, "ITEMS 1 TO 17 OF 22"},
		{"two pages second", 22, 1, false, 1, 17, 22, "ITEMS 18 TO 22 OF 22"},
		{"over-clamp to last", 22, 9, false, 1, 17, 22, "ITEMS 18 TO 22 OF 22"},
		{"under-clamp to first", 22, -3, false, 0, 0, 17, "ITEMS 1 TO 17 OF 22"},
		{"admin smaller page", 22, 1, true, 1, 16, 22, "ITEMS 17 TO 22 OF 22"},
	}
	for _, c := range cases {
		gotPage, gotStart, gotEnd, gotInd := MenuPageBounds(g, c.total, c.admin, c.page)
		if gotPage != c.wantPage || gotStart != c.wantStart || gotEnd != c.wantEnd || gotInd != c.wantIndicator {
			t.Errorf("%s: MenuPageBounds(total=%d, admin=%v, page=%d) = (%d,%d,%d,%q), want (%d,%d,%d,%q)",
				c.name, c.total, c.admin, c.page,
				gotPage, gotStart, gotEnd, gotInd,
				c.wantPage, c.wantStart, c.wantEnd, c.wantIndicator)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/screens/ -run TestMenuPageBounds -v`
Expected: FAIL to compile — `undefined: MenuPageBounds`.

- [ ] **Step 3: Implement `MenuPageBounds`**

Add to `internal/screens/menu.go` (after the `FieldSelection` const, before `MenuScreen`):

```go
// MenuPageBounds clamps page against the service total and the per-page menu
// capacity, returning the clamped page, the [start,end) slice bounds for that
// page, and the "ITEMS x TO y OF z" indicator string. It is the single source
// of menu paging math: MenuScreen uses it to render the page window, and the
// presenter uses it to clamp its stored page so PF7/PF8 are no-ops at the ends.
func MenuPageBounds(geom Geometry, total int, admin bool, page int) (clamped, start, end int, indicator string) {
	if total == 0 {
		return 0, 0, 0, "ITEMS 0 OF 0"
	}
	size := geom.MenuCapacity(admin)
	maxPage := (total - 1) / size
	if page > maxPage {
		page = maxPage
	}
	if page < 0 {
		page = 0
	}
	start = page * size
	end = min(start+size, total)
	return page, start, end, fmt.Sprintf("ITEMS %d TO %d OF %d", start+1, end, total)
}
```

(`fmt` is already imported in `menu.go`; `min` is a Go builtin.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/screens/ -run TestMenuPageBounds -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/menu.go internal/screens/menu_paging_test.go
git commit -m "feat(screens): add MenuPageBounds paging-math helper (GH #77)"
```

---

## Task 3: Paginate `MenuScreen` (page param, full mapping, windowed render, static meta, indicator)

**Files:**
- Modify: `internal/screens/menu.go:75-152` (`MenuScreen`)
- Modify: `internal/server/presenter.go:106` (caller — pass literal `0`, no loop yet)
- Test: `internal/screens/menu_test.go` (add `page` arg everywhere; fix two mapping-count tests; add paging tests)

> **Note:** `MenuScreen`'s signature changes, so every caller must be updated in this task to keep the build green. The presenter gets a literal `0` here; its paging loop is Task 4.

- [ ] **Step 1: Update existing tests to the new signature and semantics**

In `internal/screens/menu_test.go`, append `, 0` as the final argument to **every** `MenuScreen(...)` call. There are calls at (approx.) lines 37, 55, 65, 76, 89, 105, 114, 122, 149, 169, 181, 191, 214, 230, 263, 288, 299, 311, 321. Example:

```go
	screen, mapping, _ := MenuScreen(DefaultGeometry, svcs, false, MenuStatus{}, "", 0)
```

Then fix the two tests that asserted the mapping is capped at capacity — the mapping is now the **full** service count (global numbering). Replace `TestMenuScreenCapacityGrowsAndTruncates` with:

```go
func TestMenuScreenMappingCoversAllServices(t *testing.T) {
	svcs := make([]store.Service, 30)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}

	// Mapping is global: every service is selectable by number regardless of
	// page. The rendered page window must never touch the help row or below.
	g2 := Geometry{Rows: 24, Cols: 80}
	screen, mapping, _ := MenuScreen(g2, svcs, false, MenuStatus{}, "", 0)
	if len(mapping) != len(svcs) {
		t.Errorf("MOD 2 mapping = %d entries, want %d (full list)", len(mapping), len(svcs))
	}
	for _, f := range screen {
		if f.Row > g2.HelpRow() {
			t.Errorf("MOD 2: field beyond help row %d: %+v", g2.HelpRow(), f)
		}
	}

	// MOD 3 has the same full mapping (paging, not truncation).
	g3 := Geometry{Rows: 32, Cols: 80}
	_, mapping, _ = MenuScreen(g3, svcs, false, MenuStatus{}, "", 0)
	if len(mapping) != len(svcs) {
		t.Errorf("MOD 3 mapping = %d entries, want %d (full list)", len(mapping), len(svcs))
	}
}
```

In `TestMenuScreenAdminEntryNeverCollidesWhenFull`, change the two mapping assertions from capacity to the full count:

```go
		screen, mapping, _ := MenuScreen(g, svcs, true, MenuStatus{}, "", 0)
		if len(mapping) != len(svcs) { // global mapping covers every service
			t.Errorf("%+v: admin mapping = %d entries, want %d", g, len(mapping), len(svcs))
		}
```

- [ ] **Step 2: Add the new paging-render tests**

Append to `internal/screens/menu_test.go`:

```go
func TestMenuScreenPagesWithGlobalNumbers(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry // capacity 17 non-admin

	// Page 0: shows global numbers 1..17, the page-1 indicator, and NOT item 18.
	p0, mapping, _ := MenuScreen(g, svcs, false, MenuStatus{}, "", 0)
	if len(mapping) != 22 {
		t.Fatalf("mapping = %d, want 22 (full list)", len(mapping))
	}
	if mapping["18"].Name != "SVC18" {
		t.Errorf(`mapping["18"] = %q, want SVC18 (off-page but selectable)`, mapping["18"].Name)
	}
	if !hasContent(p0, fmt.Sprintf("%3d", 1)) {
		t.Errorf("page 0 missing global number 1")
	}
	if hasContent(p0, fmt.Sprintf("%3d", 18)) {
		t.Errorf("page 0 should not render global number 18")
	}
	if !hasContent(p0, "ITEMS 1 TO 17 OF 22") {
		t.Errorf("page 0 missing indicator 'ITEMS 1 TO 17 OF 22'")
	}

	// Page 1: shows global numbers 18..22, the page-2 indicator, and NOT item 1.
	p1, _, _ := MenuScreen(g, svcs, false, MenuStatus{}, "", 1)
	if !hasContent(p1, fmt.Sprintf("%3d", 18)) {
		t.Errorf("page 1 missing global number 18")
	}
	if hasContent(p1, fmt.Sprintf("%3d", 1)) {
		t.Errorf("page 1 should not render global number 1")
	}
	if !hasContent(p1, "ITEMS 18 TO 22 OF 22") {
		t.Errorf("page 1 missing indicator 'ITEMS 18 TO 22 OF 22'")
	}
}

func TestMenuScreenMetaBandOnEveryPage(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry
	for _, page := range []int{0, 1} {
		screen, _, _ := MenuScreen(g, svcs, true, MenuStatus{}, "", page)
		if !screenContains(screen, "User Settings") {
			t.Errorf("page %d missing '0 User Settings' meta entry", page)
		}
		if !screenContains(screen, "Administration") {
			t.Errorf("page %d missing 'A Administration' meta entry", page)
		}
		// Meta band is bottom-anchored: "0" one above menuBottomRow, "A" on it.
		us, _ := fieldByContent(screen, "User Settings")
		if us.Row != g.menuBottomRow()-1 {
			t.Errorf("page %d: User Settings row = %d, want %d", page, us.Row, g.menuBottomRow()-1)
		}
		admin, _ := fieldByContent(screen, "Administration")
		if admin.Row != g.menuBottomRow() {
			t.Errorf("page %d: Administration row = %d, want %d", page, admin.Row, g.menuBottomRow())
		}
	}
}

func TestMenuScreenBlankSeparatorAboveHelp(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry
	screen, _, _ := MenuScreen(g, svcs, true, MenuStatus{}, "", 0)
	sep := g.BodyBottomRow() // the row left blank between content and PF legend
	for _, f := range screen {
		if f.Row == sep && f.Content != "" {
			t.Errorf("separator row %d should be blank, found %+v", sep, f)
		}
	}
}

func TestMenuScreenHelpListsPagingKeys(t *testing.T) {
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "PF7") || !screenContains(screen, "PF8") {
		t.Errorf("menu help should list PF7/PF8 paging keys")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/screens/ 2>&1 | head -20`
Expected: FAIL to compile — `not enough arguments in call to MenuScreen` (the implementation still has the 5-arg signature).

- [ ] **Step 4: Rewrite `MenuScreen`**

Replace `MenuScreen` in `internal/screens/menu.go` (the doc comment and the whole function body, lines ~75-152) with:

```go
// MenuScreen renders one page of the service menu sized for geom and returns a
// mapping from the user's typed selection (e.g. "1") to the chosen service,
// plus the initial cursor. Numbering is global and stable: the mapping covers
// ALL services keyed by global index, while only page's window (sized by
// MenuCapacity) is rendered, each row showing its global number. Services render
// on a fixed grid (number col 0, name col 6, description col 17, hard-cut 40) so
// they never collide with the right-hand status block (StatusBlockCol). status
// supplies the block's values; an empty MenuStatus renders blank values. The
// "0 User Settings" meta row (and, when admin, "A Administration") is bottom-
// anchored on every page. An "ITEMS x TO y OF z" indicator sits on the title
// row. errMsg, if non-empty, shows on the message line. PF7/PF8 page; out-of-
// range pages clamp (see MenuPageBounds).
func MenuScreen(geom Geometry, services []store.Service, admin bool, status MenuStatus, errMsg string, page int) (go3270.Screen, map[string]store.Service, Cursor) {
	_, start, end, indicator := MenuPageBounds(geom, len(services), admin, page)

	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len("TN3270 GATEWAY MENU")), Color: go3270.White, Intense: true, Content: "TN3270 GATEWAY MENU"},
		{Row: geom.TitleRow(), Col: geom.StatusBlockCol(), Color: go3270.Turquoise, Content: indicator},
		{Row: geom.BodyTopRow(), Col: 2, Color: go3270.Turquoise, Content: "Select a service and press ENTER:"},
	}

	// Global mapping: every service is selectable by its global number, even one
	// that lives on another page.
	mapping := make(map[string]store.Service, len(services))
	for i, svc := range services {
		mapping[fmt.Sprintf("%d", i+1)] = svc
	}

	// Render only the current page's window, with global (stable) numbers.
	row := geom.BodyTopRow() + 1
	for i := start; i < end; i++ {
		svc := services[i]
		screen = append(screen,
			go3270.Field{Row: row, Col: 0, Intense: true, Content: fmt.Sprintf("%3d", i+1)},
			go3270.Field{Row: row, Col: 6, Color: go3270.Turquoise, Content: truncateRunes(svc.Name, 8)},
			go3270.Field{Row: row, Col: 17, Color: go3270.Green, Content: truncateRunes(svc.Description, 40)},
		)
		row++
	}
	if len(services) == 0 {
		screen = append(screen, go3270.Field{Row: geom.BodyTopRow() + 1, Col: 6, Content: "(no services available for your account)"})
	}

	// Meta band: bottom-anchored on every page, one blank separator row above the
	// PF legend (menuBottomRow). Non-admin: "0" on menuBottomRow. Admin: "0" one
	// row above, "A" on menuBottomRow. The page window is capped at MenuCapacity,
	// so service rows never reach the meta band.
	metaRow := geom.menuBottomRow()
	if admin {
		metaRow-- // leave the bottom row for the "A" entry
	}
	screen = append(screen,
		go3270.Field{Row: metaRow, Col: 0, Intense: true, Content: "  0"},
		go3270.Field{Row: metaRow, Col: 17, Color: go3270.Green, Content: "User Settings"},
	)
	if admin {
		adminRow := metaRow + 1
		screen = append(screen,
			go3270.Field{Row: adminRow, Col: 0, Intense: true, Content: "  A"},
			go3270.Field{Row: adminRow, Col: 17, Color: go3270.Green, Content: "Administration"},
		)
	}

	// Right-hand status block: 6 rows starting at the first service row.
	screen = append(screen, statusBlockFields(geom, status)...)

	promptF, selection, stopF := commandLine(geom, "Option ===>", FieldSelection)
	selection.NumericOnly = !admin
	screen = append(screen,
		promptF,
		selection,
		stopF,
		go3270.Field{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Logoff   PF7=PgUp  PF8=PgDn   (PA3 returns here from a session)"},
	)
	return screen, mapping, cursorAt(selection)
}
```

- [ ] **Step 5: Fix the presenter caller (compile only; loop is Task 4)**

In `internal/server/presenter.go:106`, add the page argument as a literal `0`:

```go
		screen, mapping, cur := screens.MenuScreen(geom, svcs, admin, status, errMsg, 0)
```

- [ ] **Step 6: Run the screens tests + full build**

Run: `go build ./... && go test ./internal/screens/ -v 2>&1 | tail -30`
Expected: build succeeds; all screens tests PASS (including the new paging tests).

- [ ] **Step 7: Commit**

```bash
git add internal/screens/menu.go internal/screens/menu_test.go internal/server/presenter.go
git commit -m "feat(screens): paginate MenuScreen with global numbering + meta band (GH #77)"
```

---

## Task 4: Presenter paging loop (PF7/PF8)

**Files:**
- Modify: `internal/server/presenter.go:101-135` (`go3270Presenter.Menu`)

> **Verification note:** `go3270Presenter.Menu` drives a live `net.Conn` via `go3270.HandleScreenAlt`, so it has no pure unit test (the repo's existing menu reprompt loop is likewise unit-test-free and relies on the s3270 smoke test). The interesting math — clamping and no-op-at-ends — lives in `MenuPageBounds`, already covered in Task 2. This task is the wiring; it is verified by `go build`/`go vet` here and end-to-end by the smoke scenario in Task 5.

- [ ] **Step 1: Rewrite `go3270Presenter.Menu` with page state**

Replace the body of `go3270Presenter.Menu` in `internal/server/presenter.go` with:

```go
func (go3270Presenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, status screens.MenuStatus, errMsg string) (*store.Service, menuChoice, error) {
	geom := term.Geometry()
	status.TermType = term.Type // presenter owns the terminal-derived field
	page := 0
	for {
		// Clamp the stored page each render so PF7 at the top / PF8 at the bottom
		// re-present the same page (no drift, no error) — see MenuPageBounds.
		page, _, _, _ = screens.MenuPageBounds(geom, len(svcs), admin, page)
		status.Now = time.Now() // paint-time clock, refreshed every render
		screen, mapping, cur := screens.MenuScreen(geom, svcs, admin, status, errMsg, page)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3, go3270.AIDPF7, go3270.AIDPF8}),
				screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
			)
		})
		if err != nil {
			return nil, menuReprompt, err // choice ignored on error
		}
		switch resp.AID {
		case go3270.AIDPF3:
			return nil, menuQuit, nil
		case go3270.AIDPF7:
			page-- // clamped to 0 on the next render
			continue
		case go3270.AIDPF8:
			page++ // clamped to the last page on the next render
			continue
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

- [ ] **Step 2: Build, vet, and run the full suite with the race detector**

Run: `go build ./... && go vet ./... && go test ./... -race 2>&1 | tail -30`
Expected: build/vet clean; all packages PASS (the bridge is concurrent — `-race` per CLAUDE.md).

- [ ] **Step 3: Commit**

```bash
git add internal/server/presenter.go
git commit -m "feat(server): page the service menu on PF7/PF8 (GH #77)"
```

---

## Task 5: Multi-page menu smoke scenario

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`

> Requires `s3270` installed. The scenario seeds a `pager` user with 22 services (forcing 2 pages at MOD 2's non-admin capacity of 17) into the existing front proxy DB (`seed` is additive/idempotent) and drives PF7/PF8.

- [ ] **Step 1: Add the pager seed after the existing front/back seeding**

In `.claude/skills/s3270-smoke-testing/smoke.sh`, immediately after the line
`"$WORK/tn3270proxy" seed -db "$WORK/back.db" -file "$WORK/back-seed.json" >/dev/null || exit 1`
insert:

```bash
# Pager seed: user 'pager' (group 'many') sees 22 services, forcing a 2-page
# menu at MOD 2's non-admin capacity (17). Services point at a dead port (never
# bridged — we only page through the list). NAMEs are <=8 A-Z/0-9.
pager_svcs=""
for i in $(seq 1 22); do
  n=$(printf "PAGE%02d" "$i")
  sep=","; [ -z "$pager_svcs" ] && sep=""
  pager_svcs="${pager_svcs}${sep}{\"name\":\"$n\",\"description\":\"Pager Service $i\",\"host\":\"127.0.0.1\",\"port\":1,\"groups\":[\"many\"]}"
done
cat > "$WORK/pager-seed.json" <<EOF
{"groups":["many"],
 "users":[{"username":"pager","password":"changeme","groups":["many"]}],
 "services":[$pager_svcs]}
EOF
"$WORK/tn3270proxy" seed -db "$WORK/front.db" -file "$WORK/pager-seed.json" >/dev/null || exit 1
```

- [ ] **Step 2: Add the paging scenario before the final `echo` summary**

Find the end-of-file summary (the block that prints `PASS`/`FAIL` totals, e.g. `echo "$PASS passed, $FAIL failed"`). Immediately **above** it, insert:

```bash
# --- 9. multi-page service menu: pager (22 services) pages with PF7/PF8 ---
s3 t9p1 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Ascii()
ReadBuffer(Ascii)
Quit()
EOF
check  "9a page 1 indicator" "ITEMS 1 TO 17 OF 22" "$WORK/t9p1.out"
check  "9b page 1 first service"  "PAGE01" "$WORK/t9p1.out"
check  "9c page 1 last on-page service" "PAGE17" "$WORK/t9p1.out"
ncheck "9d page 1 hides overflow service" "PAGE18" "$WORK/t9p1.out"
check  "9e meta entry present on page 1" "User Settings" "$WORK/t9p1.out"
check  "9f menu cursor on selection input (1,15)" "I 2 24 80 1 15 " "$WORK/t9p1.out"

s3 t9p2 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
PF(8)
Wait(5,InputField)
Ascii()
Quit()
EOF
check  "9g PF8 -> page 2 indicator" "ITEMS 18 TO 22 OF 22" "$WORK/t9p2.out"
check  "9h page 2 shows overflow service" "PAGE22" "$WORK/t9p2.out"
ncheck "9i page 2 hides page-1 service" "PAGE01" "$WORK/t9p2.out"
check  "9j meta entry present on page 2" "User Settings" "$WORK/t9p2.out"

# PF8 at the last page is a no-op (still page 2); PF7 then returns to page 1.
s3 t9p3 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
PF(8)
Wait(5,InputField)
PF(8)
Wait(5,InputField)
Ascii()
ReadBuffer(Ascii)
Quit()
EOF
check  "9k PF8 at last page is a no-op" "ITEMS 18 TO 22 OF 22" "$WORK/t9p3.out"

s3 t9p4 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
PF(8)
Wait(5,InputField)
PF(7)
Wait(5,InputField)
Ascii()
Quit()
EOF
check  "9l PF7 returns to page 1" "ITEMS 1 TO 17 OF 22" "$WORK/t9p4.out"
```

- [ ] **Step 3: Run the smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh 2>&1 | grep -E "9[a-l]|passed"`
Expected: scenarios `9a`–`9l` all `PASS`, and the final summary shows `0 failed`.

- [ ] **Step 4: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "test(smoke): multi-page service menu (PF7/PF8) scenario (GH #77)"
```

---

## Task 6: Update CLAUDE.md package map

**Files:**
- Modify: `CLAUDE.md` (the `internal/screens` package-map entry)

- [ ] **Step 1: Update the screens description**

In `CLAUDE.md`, find the `internal/screens` block describing `MenuScreen`. Update the signature reference and remove any "truncated (no pagination)" wording, replacing it with a note that the menu paginates. Change the `MenuScreen(geom, svcs, admin, status, errMsg)` mention to `MenuScreen(geom, svcs, admin, status, errMsg, page)` and add: "The menu paginates (PF7/PF8) via `MenuPageBounds`; numbering is global/stable and the `0`/`A` meta band is bottom-anchored on every page with an `ITEMS x TO y OF z` indicator on the title row."

- [ ] **Step 2: Verify the build is unaffected and commit**

Run: `go build ./...`
Expected: success (docs-only change).

```bash
git add CLAUDE.md
git commit -m "docs: note service menu pagination in package map (GH #77)"
```

---

## Self-Review

**1. Spec coverage:**
- Pages through full list / no service unreachable → Task 3 (full mapping + windowed render) + Task 4 (PF7/PF8). ✓
- PF7/PF8 no-op at ends → `MenuPageBounds` clamp (Task 2) + presenter re-clamp (Task 4); smoke 9k (Task 5). ✓
- `ITEMS x TO y OF z` indicator, always shown, `ITEMS 0 OF 0` empty → Task 2 + Task 3; smoke 9a/9g. ✓
- PF7/PF8 in help line → Task 3 (`TestMenuScreenHelpListsPagingKeys` + help string). ✓
- `0`/`A` meta on every page, fixed position → Task 3 (`TestMenuScreenMetaBandOnEveryPage`); smoke 9e/9j. ✓
- Selection numbering global/stable across pages → Task 3 (`TestMenuScreenPagesWithGlobalNumbers`, off-page `mapping["18"]`). ✓
- MOD 2–5 scaling → `MenuCapacity`/`menuBottomRow` (Task 1); `MenuPageBounds` admin case (Task 2). ✓
- Blank separator row (menu shortening) → Task 1 (capacity) + Task 3 (`TestMenuScreenBlankSeparatorAboveHelp`). ✓
- Smoke covers multi-page (content + cursor + PF7/PF8) → Task 5. ✓

**2. Placeholder scan:** No TBD/TODO; every code step shows full code; every run step shows the command and expected result. ✓

**3. Type consistency:** `MenuPageBounds(geom Geometry, total int, admin bool, page int) (clamped, start, end int, indicator string)` is defined in Task 2 and consumed identically in Task 3 (`_, start, end, indicator`) and Task 4 (`page, _, _, _`). `menuBottomRow()` defined in Task 1, used in Task 3 tests and `MenuScreen`. `MenuScreen`'s six-arg signature is defined in Task 3 and matched by the presenter caller (Task 3 Step 5, then Task 4). Help string contains `PF7`/`PF8` (Task 3) matching the `TestMenuScreenHelpListsPagingKeys` assertion. ✓
