# Menu Status Block (#53) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an ISPF-style right-hand status block (User ID / Date / Time / Terminal / System ID / Release) to the TN3270 service menu, and re-tighten the service list grid so it never collides with the block.

**Architecture:** The pure `screens.MenuScreen` builder gains a `MenuStatus` value carrying the block's text inputs plus a paint-time `time.Time` (no hidden clock — the builder stays deterministic). The service list is re-laid-out onto the old fixed grid (number col 0, name col 4, description col 13 hard-cut 40) so a full 40-char description ends near col 53, leaving the right side (block at col 57, 6 rows) collision-free within the existing 80-column rows-only layout. The presenter stamps `time.Now()` per render and derives the terminal type from the negotiated `Term`; the session supplies username + release + a `"PROXY"` System-ID placeholder; the resolved build version is threaded from `cmd` through `NewSessionHandler` → `Session.Release`.

**Tech Stack:** Go, `github.com/racingmars/go3270` (3270 field model), `internal/screens` (pure builders), `internal/server` (session/presenter), `internal/version` (already merged, `version.Resolve`).

---

## Background context the implementer needs

- **go3270 field model:** a `go3270.Field{Row, Col, Content, Color, Intense}` writes a non-printable *attribute byte* at `Col`, and the content renders immediately after it. Adjacent fields on one row are separated by these attribute cells, which is why the grid columns (0 / 4 / 13) leave one-cell gaps. Colors: `go3270.White` (with `Intense:true`), `go3270.Turquoise`, `go3270.Green`, `go3270.Red` are defined in the dependency.
- **Layout convention (CLAUDE.md):** unit tests assert field *names/content*, NOT row/column numbers. Exact column placement (and thus collision-freedom) is verified in a real emulator via the **s3270-smoke-testing** skill. Follow that split here: Go tests check content/truncation; the smoke script checks columns.
- **Rows-only invariant ([internal/screens/geometry.go](../../../internal/screens/geometry.go)):** all content stays within columns 0–79 on every terminal model. The status block lives entirely inside 80 cols; do NOT introduce wide-terminal column adaptation.
- **Decisions locked for this issue:**
  - Block is **6 rows**: User ID, Date, Time, Terminal, System ID, Release.
  - **Date** = Julian `YY.DDD` (e.g. `26.156`), **paint-time**.
  - **Time** = `HH:MM` 24-hour, **paint-time**.
  - **System ID** = hardcoded placeholder `"PROXY"` with a code comment that it will become a DB-backed system-config value (future work; not this issue). Do NOT wire it to `system_config` now.
  - **Release** = `version.Resolve(...)`, hard-cut to 7 runes (a tag like `v0.8.0` fits; the VCS fallback becomes a standard 7-char short SHA).
  - **Values** are hard-cut to 7 runes; **labels** are 10 chars so the colon aligns.
  - **Terminal** value: strip leading `IBM-`, cut to 7, then trim a trailing `-` (`IBM-3278-2-E` → `3278-2`).
  - **Colors** (ISPF-inspired; broader theming is a separate pre-release pass): list number = intense white, name = turquoise, description = green. Block label = turquoise, value = green.
  - The block appears on **every** menu state, **including** the "no services available" screen.
  - Scope is the **menu builder only**; admin list/form screens are untouched.

## File structure

- **Modify** [internal/screens/geometry.go](../../../internal/screens/geometry.go) — add `StatusBlockCol()`.
- **Modify** [internal/screens/menu.go](../../../internal/screens/menu.go) — add `MenuStatus` struct + value-format helpers; rewrite the list grid and selection mapping; render the status block; change `MenuScreen` signature.
- **Modify** [internal/screens/menu_test.go](../../../internal/screens/menu_test.go) — update all `MenuScreen(...)` callers to the new signature; add block-content/truncation tests.
- **Create** [internal/screens/status_test.go](../../../internal/screens/status_test.go) — unit tests for the value-format helpers.
- **Modify** [internal/server/session.go](../../../internal/server/session.go) — add `Session.Release` field; add `systemIDPlaceholder` const; build `screens.MenuStatus` at the menu call; update the `Presenter.Menu` interface signature.
- **Modify** [internal/server/presenter.go](../../../internal/server/presenter.go) — update `go3270Presenter.Menu`: per-render `Now`, derive `TermType` from `term`.
- **Modify** [internal/server/session_test.go](../../../internal/server/session_test.go) — update `fakePresenter.Menu` to the new signature.
- **Modify** [internal/server/server.go](../../../internal/server/server.go) — thread `release` through `sessionHandler` / `NewSessionHandler` / `sessionFor`.
- **Modify** [internal/server/server_test.go](../../../internal/server/server_test.go) — update the `NewSessionHandler(...)` call.
- **Modify** [cmd/tn3270proxy/main.go](../../../cmd/tn3270proxy/main.go) — pass `resolvedVersion()` into `NewSessionHandler`.
- **Modify** [.claude/skills/s3270-smoke-testing/smoke.sh](../../../.claude/skills/s3270-smoke-testing/smoke.sh) — assert the status block renders and is collision-free on the menu.
- **Modify** [CLAUDE.md](../../../CLAUDE.md) — update the `internal/screens` package-map line for the new `MenuScreen` signature.

---

## Task 1: `Geometry.StatusBlockCol()` helper

**Files:**
- Modify: `internal/screens/geometry.go`
- Test: `internal/screens/geometry_test.go` (add a test func; create the file if it does not exist)

- [ ] **Step 1: Write the failing test**

Add to `internal/screens/geometry_test.go` (if the file does not exist, prepend the standard GPL header copied from the top of `internal/screens/menu.go`, then `package screens` and `import "testing"`):

```go
func TestStatusBlockCol(t *testing.T) {
	// Fixed at 57 on the 80-col layout: the service grid (desc col 13, cut 40)
	// ends near col 53, leaving a gutter before the block. The value is the
	// same on taller models because content is always within cols 0-79.
	for _, g := range []Geometry{DefaultGeometry, {Rows: 43, Cols: 80}, {}} {
		if got := g.StatusBlockCol(); got != 57 {
			t.Errorf("StatusBlockCol(%+v) = %d, want 57", g, got)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/screens/ -run TestStatusBlockCol -v`
Expected: FAIL — `g.StatusBlockCol undefined`.

- [ ] **Step 3: Implement the helper**

Append to `internal/screens/geometry.go` (after `NewsLinesPerPage`):

```go
// StatusBlockCol is the left column of the menu's right-hand status block
// (the per-session User ID / Date / Time / Terminal / System ID / Release
// panel). It is fixed at 57: the service grid (number col 0, name col 4,
// description col 13 hard-cut to 40) ends near col 53, and the block's 10-char
// labels + 7-rune values fit within cols 57-79. Content is always within cols
// 0-79 (rows-only adaptation), so this does not widen on taller models.
func (g Geometry) StatusBlockCol() int { return 57 }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/screens/ -run TestStatusBlockCol -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/geometry.go internal/screens/geometry_test.go
git commit -m "feat: add Geometry.StatusBlockCol() for the menu status block"
```

---

## Task 2: status-block value-format helpers + `MenuStatus` struct

**Files:**
- Modify: `internal/screens/menu.go` (add struct + helpers + `time` import)
- Test: `internal/screens/status_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `internal/screens/status_test.go` (prepend the GPL header from `internal/screens/menu.go`, then):

```go
package screens

import (
	"testing"
	"time"
)

func TestJulianDate(t *testing.T) {
	got := julianDate(time.Date(2026, 6, 5, 21, 14, 0, 0, time.UTC))
	if got != "26.156" {
		t.Errorf("julianDate = %q, want %q", got, "26.156")
	}
}

func TestClockHM(t *testing.T) {
	got := clockHM(time.Date(2026, 6, 5, 21, 14, 0, 0, time.UTC))
	if got != "21:14" {
		t.Errorf("clockHM = %q, want %q", got, "21:14")
	}
}

func TestTermDisplay(t *testing.T) {
	cases := map[string]string{
		"IBM-3278-2-E": "3278-2",
		"IBM-3279-2":   "3279-2",
		"IBM-DYNAMIC":  "DYNAMIC", // 7 runes exactly, no trailing dash
		"":             "",
	}
	for in, want := range cases {
		if got := termDisplay(in); got != want {
			t.Errorf("termDisplay(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("ABCDEFGHIJ", 7); got != "ABCDEFG" {
		t.Errorf("truncateRunes cut = %q, want ABCDEFG", got)
	}
	if got := truncateRunes("ABC", 7); got != "ABC" {
		t.Errorf("truncateRunes short = %q, want ABC", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/screens/ -run 'TestJulianDate|TestClockHM|TestTermDisplay|TestTruncateRunes' -v`
Expected: FAIL — `julianDate`, `clockHM`, `termDisplay`, `truncateRunes` undefined.

- [ ] **Step 3: Add the `time` import, the `MenuStatus` struct, and the helpers**

In `internal/screens/menu.go`, change the import block from:

```go
import (
	"fmt"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)
```

to:

```go
import (
	"fmt"
	"strings"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)
```

Then add, just above `func MenuScreen(`:

```go
// MenuStatus carries the values shown in the menu's right-hand status block.
// Now is the paint-time clock; the presenter stamps it on every render so the
// builder stays deterministic (no hidden time source). Username/SystemID/
// Release are supplied by the session; TermType is filled by the presenter
// from the negotiated terminal.
type MenuStatus struct {
	Username string
	TermType string
	SystemID string
	Release  string
	Now      time.Time
}

// julianDate formats t as YY.DDD (two-digit year, three-digit day-of-year),
// e.g. 2026-06-05 -> "26.156". Six runes, so it fits the 7-rune value column.
func julianDate(t time.Time) string {
	return fmt.Sprintf("%02d.%03d", t.Year()%100, t.YearDay())
}

// clockHM formats t as 24-hour HH:MM.
func clockHM(t time.Time) string { return t.Format("15:04") }

// termDisplay prepares a terminal type for the status block: strip a leading
// "IBM-", hard-cut to 7 runes, then trim a trailing "-" so "IBM-3278-2-E"
// becomes "3278-2".
func termDisplay(t string) string {
	t = strings.TrimPrefix(t, "IBM-")
	t = truncateRunes(t, 7)
	return strings.TrimRight(t, "-")
}

// truncateRunes hard-cuts s to at most n runes. Status values and descriptions
// are ASCII in this EBCDIC display context, but rune-safe to match news.go.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/screens/ -run 'TestJulianDate|TestClockHM|TestTermDisplay|TestTruncateRunes' -v`
Expected: PASS. (`MenuStatus` is defined but as-yet unused — that compiles fine in Go.)

- [ ] **Step 5: Commit**

```bash
git add internal/screens/menu.go internal/screens/status_test.go
git commit -m "feat: add menu status-block value formatters and MenuStatus"
```

---

## Task 3: rewrite `MenuScreen` (grid + status block + new signature) and update all callers

This is the atomic signature change: `MenuScreen` gains a `status MenuStatus` parameter, so its body, the presenter, the session call site, the interface, the session fake, and every `menu_test.go` caller all change in one commit to keep the tree compiling.

**Files:**
- Modify: `internal/screens/menu.go`
- Modify: `internal/screens/menu_test.go`
- Modify: `internal/server/session.go` (interface signature + call site + `Session.Release` field + `systemIDPlaceholder`)
- Modify: `internal/server/presenter.go` (impl signature + paint-time)
- Modify: `internal/server/session_test.go` (fake signature)

- [ ] **Step 1: Write the new/updated failing tests**

In `internal/screens/menu_test.go`, update the import block to include `"time"`:

```go
import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)
```

Update **every** existing `MenuScreen(` call in that file to pass a `MenuStatus` as the new fourth argument (before `errMsg`). The existing tests assert mapping/field-name behavior, so an empty `MenuStatus{}` is fine for them. The exact replacements:

- line ~35: `MenuScreen(DefaultGeometry, svcs, false, "")` → `MenuScreen(DefaultGeometry, svcs, false, MenuStatus{}, "")`
- line ~53: `MenuScreen(DefaultGeometry, nil, false, "")` → `MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "")`
- line ~63: `MenuScreen(DefaultGeometry, nil, false, "Backend unreachable")` → `MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "Backend unreachable")`
- line ~74: `MenuScreen(DefaultGeometry, nil, true, "")` → `MenuScreen(DefaultGeometry, nil, true, MenuStatus{}, "")`
- line ~86: `MenuScreen(DefaultGeometry, nil, false, "")` → `MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "")`
- line ~101: `MenuScreen(DefaultGeometry, svcs, true, "")` → `MenuScreen(DefaultGeometry, svcs, true, MenuStatus{}, "")`
- line ~110: `MenuScreen(DefaultGeometry, nil, false, "")` → `MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "")`
- line ~118: `MenuScreen(g, nil, false, "err")` → `MenuScreen(g, nil, false, MenuStatus{}, "err")`
- line ~138: `MenuScreen(g2, svcs, false, "")` → `MenuScreen(g2, svcs, false, MenuStatus{}, "")`
- line ~150: `MenuScreen(g3, svcs, false, "")` → `MenuScreen(g3, svcs, false, MenuStatus{}, "")`
- line ~160: `MenuScreen(DefaultGeometry, svcs, false, "")` → `MenuScreen(DefaultGeometry, svcs, false, MenuStatus{}, "")`
- line ~179: `MenuScreen(DefaultGeometry, nil, false, "")` → `MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "")`
- line ~195: `MenuScreen(g, svcs, true, "")` → `MenuScreen(g, svcs, true, MenuStatus{}, "")`

(Use `grep -n 'MenuScreen(' internal/screens/menu_test.go` to confirm you caught them all; the rule is identical for any not listed.)

Then append these new tests asserting the status block and the grid content:

```go
func TestMenuScreenStatusBlock(t *testing.T) {
	status := MenuStatus{
		Username: "merlin",
		TermType: "IBM-3279-2",
		SystemID: "PROXY",
		Release:  "v0.8.0",
		Now:      time.Date(2026, 6, 5, 21, 14, 0, 0, time.UTC),
	}
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, status, "")

	// The block shows even on the no-services screen. Assert content (not
	// columns — columns are verified by the s3270 smoke script).
	wantContents := []string{
		"User ID. :", "MERLIN", // upper-cased
		"Date . . :", "26.156",
		"Time . . :", "21:14",
		"Terminal :", "3279-2", // IBM- stripped
		"System ID:", "PROXY",
		"Release. :", "v0.8.0",
	}
	for _, want := range wantContents {
		if !hasContent(screen, want) {
			t.Errorf("status block missing content %q", want)
		}
	}
}

func TestMenuScreenStatusBlockTruncates(t *testing.T) {
	status := MenuStatus{
		Username: "averylongusername",
		Release:  "0123456789ab", // 12-char VCS-style rev
		Now:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, status, "")
	if !hasContent(screen, "AVERYLO") { // username upper, cut to 7
		t.Errorf("username not cut to 7 runes")
	}
	if !hasContent(screen, "0123456") { // release cut to 7
		t.Errorf("release not cut to 7 runes")
	}
}

func TestMenuScreenGridDescription(t *testing.T) {
	svcs := []store.Service{{ID: 1, Name: "CICSPROD", Description: "CICS TS Prod"}}
	screen, _, _ := MenuScreen(DefaultGeometry, svcs, false, MenuStatus{}, "")
	// Name and description are now separate fields (separately colored grid),
	// not one combined string.
	if !hasContent(screen, "CICSPROD") {
		t.Errorf("service name field missing")
	}
	if !hasContent(screen, "CICS TS Prod") {
		t.Errorf("service description field missing")
	}
}

// hasContent reports whether any field's Content equals want.
func hasContent(screen go3270.Screen, want string) bool {
	for _, f := range screen {
		if f.Content == want {
			return true
		}
	}
	return false
}
```

Add `"github.com/racingmars/go3270"` to the `menu_test.go` imports (needed by `hasContent`'s `go3270.Screen` param):

```go
import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)
```

- [ ] **Step 2: Run the tests to verify they fail (compile error first)**

Run: `go test ./internal/screens/ -run TestMenuScreen -v`
Expected: FAIL — the new `status` parameter is not yet in `MenuScreen`'s signature (compile error: too many arguments), and the new tests reference behavior not yet implemented.

- [ ] **Step 3: Rewrite `MenuScreen` in `internal/screens/menu.go`**

Replace the entire `MenuScreen` function (the doc comment + body, currently lines ~32–80) with:

```go
// MenuScreen renders the service menu sized for geom and returns a mapping
// from the user's typed selection (e.g. "1") to the chosen service, plus the
// initial cursor. Services render on a fixed grid (number col 0, name col 4,
// description col 13, hard-cut to 40) so they never collide with the right-hand
// status block (StatusBlockCol). status supplies the block's values; an empty
// MenuStatus simply renders blank values. When admin is true an
// "A  Administration" entry is shown (handled by the presenter, not the
// mapping) and the selection field accepts letters. errMsg, if non-empty, is
// shown on the error line. Services beyond the screen's capacity are truncated
// (no pagination) so the list can never collide with the input/error/help rows.
func MenuScreen(geom Geometry, services []store.Service, admin bool, status MenuStatus, errMsg string) (go3270.Screen, map[string]store.Service, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY MENU"},
		{Row: 2, Col: 2, Content: "Select a service and press ENTER:"},
	}

	shown := services
	if capacity := geom.MenuCapacity(admin); len(shown) > capacity {
		shown = shown[:capacity]
	}
	mapping := make(map[string]store.Service, len(shown))

	// Fixed grid: number col 0 (intense white), name col 4 (turquoise),
	// description col 13 (green, hard-cut 40). Three separate fields keep the
	// columns aligned and individually colored (ISPF style).
	row := 4
	for i, svc := range shown {
		key := fmt.Sprintf("%d", i+1)
		mapping[key] = svc
		screen = append(screen,
			go3270.Field{Row: row, Col: 0, Intense: true, Content: fmt.Sprintf("%3d", i+1)},
			go3270.Field{Row: row, Col: 4, Color: go3270.Turquoise, Content: truncateRunes(svc.Name, 8)},
			go3270.Field{Row: row, Col: 13, Color: go3270.Green, Content: truncateRunes(svc.Description, 40)},
		)
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
		screen = append(screen,
			go3270.Field{Row: adminRow, Col: 0, Intense: true, Content: "  A"},
			go3270.Field{Row: adminRow, Col: 13, Color: go3270.Green, Content: "Administration"},
		)
	}

	// Right-hand status block: 6 rows starting at the first service row.
	screen = append(screen, statusBlockFields(geom, status)...)

	selection := go3270.Field{Row: geom.InputRow(), Col: 7, Name: FieldSelection, Write: true, NumericOnly: !admin, Highlighting: go3270.Underscore}
	screen = append(screen,
		go3270.Field{Row: geom.InputRow(), Col: 2, Content: "===>"},
		selection,
		go3270.Field{Row: geom.InputRow(), Col: 15}, // stop field
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: "PF3=Logoff    (PA3 returns here from a session)"},
	)
	return screen, mapping, cursorAt(selection)
}

// statusBlockFields builds the right-hand ISPF-style status block: six rows
// (User ID / Date / Time / Terminal / System ID / Release) starting at the
// first service row (row 4). Labels are 10 chars (colon-aligned, turquoise);
// values are hard-cut to 7 runes (green). Placement is geom.StatusBlockCol().
func statusBlockFields(geom Geometry, status MenuStatus) go3270.Screen {
	const labelWidth = 10 // "System ID:" etc.; value field sits one space past
	labelCol := geom.StatusBlockCol()
	valueCol := labelCol + labelWidth + 1
	rows := []struct{ label, value string }{
		{"User ID. :", truncateRunes(strings.ToUpper(status.Username), 7)},
		{"Date . . :", julianDate(status.Now)},
		{"Time . . :", clockHM(status.Now)},
		{"Terminal :", termDisplay(status.TermType)},
		{"System ID:", truncateRunes(status.SystemID, 7)},
		{"Release. :", truncateRunes(status.Release, 7)},
	}
	var fields go3270.Screen
	for i, r := range rows {
		fields = append(fields,
			go3270.Field{Row: 4 + i, Col: labelCol, Color: go3270.Turquoise, Content: r.label},
			go3270.Field{Row: 4 + i, Col: valueCol, Color: go3270.Green, Content: r.value},
		)
	}
	return fields
}
```

- [ ] **Step 4: Update the `Presenter.Menu` interface in `internal/server/session.go`**

Change the interface method (currently line ~52) from:

```go
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, errMsg string) (selected *store.Service, adminSel bool, quit bool, err error)
```

to:

```go
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, status screens.MenuStatus, errMsg string) (selected *store.Service, adminSel bool, quit bool, err error)
```

- [ ] **Step 5: Add `Session.Release` field and `systemIDPlaceholder`, and build the status at the call site (session.go)**

In the `Session` struct in `internal/server/session.go`, add a field after `Logger *slog.Logger`:

```go
	// Release is the resolved build version shown in the menu status block
	// (version.Resolve, threaded from cmd via NewSessionHandler). Empty renders
	// a blank Release row.
	Release string
```

Add this constant near the top of `internal/server/session.go` (just after the `import (...)` block):

```go
// systemIDPlaceholder is shown in the menu status block's "System ID" row.
// TODO(#53): replace with a DB-backed system-config value entered via the
// future System Configuration admin screen; hardcoded for now.
const systemIDPlaceholder = "PROXY"
```

Change the menu call site (currently line ~265) from:

```go
			selected, adminSel, quit, err := s.Presenter.Menu(conn, term, services, isAdmin, errMsg)
```

to:

```go
			status := screens.MenuStatus{
				Username: identity.Username,
				SystemID: systemIDPlaceholder,
				Release:  s.Release,
			}
			selected, adminSel, quit, err := s.Presenter.Menu(conn, term, services, isAdmin, status, errMsg)
```

- [ ] **Step 6: Update `go3270Presenter.Menu` in `internal/server/presenter.go`**

Add `"time"` to the `internal/server/presenter.go` import block (alphabetical, after `"strings"`):

```go
import (
	"crypto/tls"
	"net"
	"strings"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)
```

Change the `Menu` method signature and add the per-render paint-time + terminal type. Replace the function header and the start of the render loop (currently lines ~94–97):

```go
func (go3270Presenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, errMsg string) (*store.Service, bool, bool, error) {
	geom := term.Geometry()
	for {
		screen, mapping, cur := screens.MenuScreen(geom, svcs, admin, errMsg)
```

becomes:

```go
func (go3270Presenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, status screens.MenuStatus, errMsg string) (*store.Service, bool, bool, error) {
	geom := term.Geometry()
	status.TermType = term.Type // presenter owns the terminal-derived field
	for {
		status.Now = time.Now() // paint-time clock, refreshed every render
		screen, mapping, cur := screens.MenuScreen(geom, svcs, admin, status, errMsg)
```

(The rest of the loop body is unchanged.)

- [ ] **Step 7: Update the `fakePresenter.Menu` in `internal/server/session_test.go`**

Change the fake's method (currently line ~84) from:

```go
func (f *fakePresenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, errMsg string) (*store.Service, bool, bool, error) {
```

to:

```go
func (f *fakePresenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, status screens.MenuStatus, errMsg string) (*store.Service, bool, bool, error) {
```

If `internal/server/session_test.go` does not already import `"github.com/CoffeeMuse/tn3270proxy/internal/screens"`, add it. (Check with `grep -n 'internal/screens' internal/server/session_test.go`; the body of the fake can ignore `status`.)

- [ ] **Step 8: Run the screens + server tests to verify they pass**

Run: `go build ./... && go test ./internal/screens/ ./internal/server/ -v`
Expected: PASS, including the new `TestMenuScreenStatusBlock*` and `TestMenuScreenGridDescription`. Fix any remaining compile errors from a missed `MenuScreen(`/`Menu(` caller until green.

- [ ] **Step 9: Commit**

```bash
git add internal/screens/menu.go internal/screens/menu_test.go internal/server/session.go internal/server/presenter.go internal/server/session_test.go
git commit -m "feat: render ISPF status block on the service menu (#53)"
```

---

## Task 4: thread the resolved version from cmd into `Session.Release`

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`
- Modify: `cmd/tn3270proxy/main.go`

- [ ] **Step 1: Write the failing test**

In `internal/server/server_test.go`, the existing test builds a handler at line ~87:

```go
	h := NewSessionHandler(st, 0x6B, limits, slog.Default()).(sessionHandler)
```

Update it to pass a release and assert it propagates into the built `Session`. Replace that line and add an assertion after the existing handler assertions in the same test (find where it inspects `h`; if it builds a session via `h.sessionFor(...)`, assert `.Release`). Concretely, change the construction to:

```go
	h := NewSessionHandler(st, 0x6B, limits, slog.Default(), "v9.9.9").(sessionHandler)
```

and add:

```go
	if got := h.sessionFor(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, slog.Default()).Release; got != "v9.9.9" {
		t.Errorf("Session.Release = %q, want v9.9.9", got)
	}
```

(If `net` is not yet imported in `server_test.go`, add it. If the existing test already calls `sessionFor`, reuse that call and just assert `.Release`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run TestNewSessionHandler -v` (use the actual test name containing line 87 — find it with `grep -n 'func Test' internal/server/server_test.go` near line 87).
Expected: FAIL — `NewSessionHandler` takes 4 args, not 5 (compile error).

- [ ] **Step 3: Thread `release` through `server.go`**

In `internal/server/server.go`, add a field to `sessionHandler` (after `logger *slog.Logger`):

```go
	release   string
```

In `sessionFor`, add `Release: h.release,` to the returned `&Session{...}` literal (place it near `Logger: connLog,`):

```go
		Logger:           connLog,
		Release:          h.release,
```

Change `NewSessionHandler`'s signature and body:

```go
func NewSessionHandler(st *store.Store, escapeAID byte, limits Limits, logger *slog.Logger, release string) connHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return sessionHandler{store: st, escapeAID: escapeAID, limits: limits, logger: logger, release: release}
}
```

Update the doc comment above `NewSessionHandler` to mention `release` is the resolved build version shown in the menu status block.

- [ ] **Step 4: Update the `cmd` call site**

In `cmd/tn3270proxy/main.go`, change line ~109 from:

```go
	handler := server.NewSessionHandler(st, bridge.EscapeAIDPA3, limits, logger)
```

to:

```go
	handler := server.NewSessionHandler(st, bridge.EscapeAIDPA3, limits, logger, resolvedVersion())
```

- [ ] **Step 5: Run build + tests to verify they pass**

Run: `go build ./... && go test ./internal/server/ ./cmd/... -v`
Expected: PASS, including the updated handler test asserting `Release == "v9.9.9"`.

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go cmd/tn3270proxy/main.go
git commit -m "feat: thread resolved version into the menu status block (#53)"
```

---

## Task 5: full test sweep, smoke-test assertions, and docs

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Run the whole suite with the race detector**

Run: `go test ./... -race`
Expected: PASS across all packages.

- [ ] **Step 2: Add status-block assertions to the smoke script**

In `.claude/skills/s3270-smoke-testing/smoke.sh`, find the scenario that logs `alice` in and captures the menu (the block around `check "3a menu shown after login" "TN3270 GATEWAY MENU"`). After its `Ascii()` capture (`$WORK/t3.out`), add assertions that the status block rendered and that a long description did not overprint it. Add these `check` lines:

```bash
check "3e status block User ID row" "User ID. :" "$WORK/t3.out"
check "3f status block Release row"  "Release. :" "$WORK/t3.out"
check "3g status block Terminal row" "Terminal :" "$WORK/t3.out"
```

(`alice`'s terminal is `3279-2` per the `s3270 -model 3279-2` invocation, and her username renders as `ALICE`. The columns themselves are visually confirmed by the cursor/screen-shape; the existing menu checks already prove no render crash.) If you want a hard column guard, additionally seed a 40-char-description service for `alice` in the front seed and assert both the description text and `User ID. :` appear on the SAME captured screen (they will, since they no longer collide).

- [ ] **Step 3: Run the smoke script**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all PASS, including the new `3e/3f/3g` status-block checks. (Requires `s3270`; it is installed at `/opt/homebrew/bin/s3270`.)

- [ ] **Step 4: Update the package-map doc in `CLAUDE.md`**

In `CLAUDE.md`, update the `internal/screens` description so the `MenuScreen` signature line reflects the new parameter. Change:

```
internal/screens   Pure go3270 screen builders: LoginScreen(), MenuScreen(geom, svcs,
                   admin, errMsg). The user menu renders ISPF-style `NN NAME Description`
                   rows and deliberately hides backend host/port (admin-only).
```

to:

```
internal/screens   Pure go3270 screen builders: LoginScreen(), MenuScreen(geom, svcs,
                   admin, status, errMsg). The user menu renders an ISPF-style fixed grid
                   (number col 0 / name col 4 / description col 13, hard-cut 40) plus a
                   right-hand status block (MenuStatus: User ID / Date / Time / Terminal /
                   System ID / Release; paint-time clock passed in, not read) at
                   Geometry.StatusBlockCol(). Backend host/port stay hidden (admin-only).
```

- [ ] **Step 5: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh CLAUDE.md
git commit -m "test: assert menu status block in smoke test; docs: update screens map"
```

---

## Self-review notes (already applied)

- **Spec coverage:** status block 6 rows (Task 3) ✓; Julian `YY.DDD` + `HH:MM` paint-time (Task 2/3) ✓; Terminal `IBM-` strip/cut/trim (Task 2) ✓; System ID `"PROXY"` placeholder + comment (Task 3) ✓; Release from `version.Resolve` cut-7 (Task 2/3/4) ✓; geometry-driven placement (Task 1) ✓; no host/port leak (block carries none) ✓; appears on no-services screen (Task 3 renders the block unconditionally) ✓; builder takes paint-time, no hidden clock (Task 2/3) ✓; emulator verification (Task 5) ✓.
- **Type consistency:** `MenuStatus{Username,TermType,SystemID,Release,Now}` used identically in builder, presenter, session, and tests; helper names `julianDate`/`clockHM`/`termDisplay`/`truncateRunes`/`statusBlockFields`/`StatusBlockCol` are consistent across tasks.
- **Compile-safety between tasks:** Task 2 adds only additive (unused-OK) declarations; Task 3 changes the `MenuScreen`/`Menu` signature and all callers in one commit; Task 4's `Session.Release` field exists from Task 3 and is merely populated in Task 4.
