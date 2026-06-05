# MOTD / NEWS Screen Implementation Plan (GH #45)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a TSO/READY-style red, paged Message-of-the-Day screen once after a successful login (before the menu), driven live from the `MOTD_FILE` system parameter — ENTER pages through, PA3/PF3 inert.

**Architecture:** Three independently testable units plus one session wiring point. `internal/screens/news.go` holds two pure functions (`PaginateNews` splits/truncates text into pages; `NewsScreen` renders one page) — no IO. A new `Presenter.News` seam owns the real go3270 paging loop (smoke-tested, like `Login`/`Menu`). `Session.maybeShowNews` reads the config + file (behind an injectable reader), applies the skip rules, and calls `News` — unit-tested with a fake presenter and injected reader.

**Tech Stack:** Go, `github.com/racingmars/go3270`, `modernc.org/sqlite` (via `internal/store`), standard `testing`.

---

## File Structure

- **Modify** `internal/screens/geometry.go` — add `NewsLinesPerPage()` formula method.
- **Create** `internal/screens/news.go` — `PaginateNews` + `NewsScreen`.
- **Create** `internal/screens/news_test.go` — tests for both.
- **Modify** `internal/sysconfig/catalog.go` — add exported `KeyMOTDFile` constant; use it for the entry key.
- **Modify** `internal/server/session.go` — add `Presenter.News` to the interface; add `MOTDRead` seam field + `readMOTDCapped` default + `motdReadCap` const; add `maybeShowNews`; wire it into `Run`.
- **Modify** `internal/server/presenter.go` — implement `go3270Presenter.News`.
- **Modify** `internal/server/session_test.go` — add `News` to `fakePresenter` (with recording fields).
- **Create** `internal/server/session_motd_test.go` — `maybeShowNews` behaviour tests.
- **Modify** `.claude/skills/s3270-smoke-testing/smoke.sh` — add a MOTD scenario.
- **Modify** `README.md` — operator note on authoring the MOTD file.
- **Modify** `CLAUDE.md` — one line recording the deliberate no-chrome convention departure.

---

## Task 1: `NewsLinesPerPage()` geometry method

**Files:**
- Modify: `internal/screens/geometry.go`
- Test: `internal/screens/geometry_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/screens/geometry_test.go`:

```go
func TestNewsLinesPerPage(t *testing.T) {
	cases := []struct {
		name string
		geom Geometry
		want int
	}{
		{"mod2", Geometry{Rows: 24, Cols: 80}, 22},
		{"mod3", Geometry{Rows: 32, Cols: 80}, 30},
		{"mod4", Geometry{Rows: 43, Cols: 80}, 41},
		{"mod5", Geometry{Rows: 27, Cols: 132}, 25},
		{"zero normalizes to mod2", Geometry{}, 22},
		{"sub-mod2 normalizes to mod2", Geometry{Rows: 10, Cols: 40}, 22},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.geom.NewsLinesPerPage(); got != tc.want {
				t.Errorf("NewsLinesPerPage() = %d, want %d", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestNewsLinesPerPage -v`
Expected: FAIL — `g.NewsLinesPerPage undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/screens/geometry.go`, after the `MenuCapacity` method (end of file, before the closing of the file), add:

```go
// NewsLinesPerPage is how many MOTD/NEWS text lines fit on one page: every row
// except the bottom row reserved for the "***" page gate and the blank row
// above it (22 on MOD 2). The "***" sits on row NewsLinesPerPage()+1 (the last
// row); see NewsScreen.
func (g Geometry) NewsLinesPerPage() int { return g.norm().Rows - 2 }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/screens/ -run TestNewsLinesPerPage -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/geometry.go internal/screens/geometry_test.go
git commit -m "feat(screens): NewsLinesPerPage geometry method (GH #45)"
```

---

## Task 2: `PaginateNews` pure function

**Files:**
- Create: `internal/screens/news.go`
- Test: `internal/screens/news_test.go`

Semantics: split on `\n`, strip a trailing `\r`, truncate each line to 79 runes, drop trailing blank (whitespace-only) lines so an EOF newline never makes a spurious blank page, return `nil` if nothing remains, otherwise chunk into pages of `geom.NewsLinesPerPage()` lines. Leading/interior blank lines are preserved (the author owns spacing).

- [ ] **Step 1: Write the failing test**

Create `internal/screens/news_test.go`:

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
	"strings"
	"testing"
)

func TestPaginateNewsEmptyAndWhitespace(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	for _, raw := range []string{"", "   ", "\n\n", "  \n \t \n"} {
		if pages := PaginateNews(mod2, raw); pages != nil {
			t.Errorf("PaginateNews(%q) = %v, want nil", raw, pages)
		}
	}
}

func TestPaginateNewsSinglePage(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	pages := PaginateNews(mod2, "line one\nline two\nline three")
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages))
	}
	want := []string{"line one", "line two", "line three"}
	if len(pages[0]) != len(want) {
		t.Fatalf("page has %d lines, want %d", len(pages[0]), len(want))
	}
	for i, w := range want {
		if pages[0][i] != w {
			t.Errorf("line %d = %q, want %q", i, pages[0][i], w)
		}
	}
}

func TestPaginateNewsStripsTrailingBlankLinesAndCR(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	// CRLF line endings and a trailing newline must not create a blank page.
	pages := PaginateNews(mod2, "alpha\r\nbeta\r\n\r\n")
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages))
	}
	if len(pages[0]) != 2 || pages[0][0] != "alpha" || pages[0][1] != "beta" {
		t.Errorf("page = %#v, want [alpha beta]", pages[0])
	}
}

func TestPaginateNewsPreservesInteriorBlankLines(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	pages := PaginateNews(mod2, "top\n\nbottom")
	if len(pages) != 1 || len(pages[0]) != 3 || pages[0][1] != "" {
		t.Errorf("page = %#v, want [top, \"\", bottom]", pages[0])
	}
}

func TestPaginateNewsTruncatesAt79(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	long := strings.Repeat("X", 100)
	pages := PaginateNews(mod2, long)
	if len(pages) != 1 || len(pages[0]) != 1 {
		t.Fatalf("got pages %#v, want one line", pages)
	}
	if got := pages[0][0]; len([]rune(got)) != 79 {
		t.Errorf("truncated line length = %d, want 79", len([]rune(got)))
	}
}

func TestPaginateNewsSplitsAcrossPages(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80} // 22 lines/page
	var b strings.Builder
	for i := 0; i < 45; i++ { // 45 lines → 22 + 22 + 1
		b.WriteString("L")
		b.WriteByte('\n')
	}
	pages := PaginateNews(mod2, b.String())
	if len(pages) != 3 {
		t.Fatalf("got %d pages, want 3", len(pages))
	}
	if len(pages[0]) != 22 || len(pages[1]) != 22 || len(pages[2]) != 1 {
		t.Errorf("page sizes = %d/%d/%d, want 22/22/1",
			len(pages[0]), len(pages[1]), len(pages[2]))
	}
}

func TestPaginateNewsExactFitOnePage(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80} // 22 lines/page
	var b strings.Builder
	for i := 0; i < 22; i++ {
		b.WriteString("L\n")
	}
	pages := PaginateNews(mod2, b.String())
	if len(pages) != 1 || len(pages[0]) != 22 {
		t.Errorf("got %d pages (first %d lines), want 1 page of 22",
			len(pages), len(pages[0]))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestPaginateNews -v`
Expected: FAIL — `undefined: PaginateNews`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/screens/news.go`:

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

import "strings"

// newsMaxCols is the rightmost usable column for MOTD text. The file is a
// deliberately composed fixed-width banner; the author owns line breaks and
// indentation, and anything past column 79 is silently dropped (all MOD types,
// including MOD 5, truncate at 79).
const newsMaxCols = 79

// PaginateNews turns raw MOTD file text into pages for NewsScreen. Lines split
// on "\n" (a trailing "\r" is stripped) and truncate at column 79. Trailing
// blank lines are dropped so a conventional EOF newline never yields a spurious
// blank page; leading and interior blank lines are preserved (the author owns
// spacing). Returns nil when the text is empty or whitespace-only — the caller
// treats nil as "nothing to show". Each page holds up to geom.NewsLinesPerPage()
// lines.
func PaginateNews(geom Geometry, raw string) [][]string {
	lines := strings.Split(raw, "\n")
	for i, ln := range lines {
		ln = strings.TrimSuffix(ln, "\r")
		if r := []rune(ln); len(r) > newsMaxCols {
			ln = string(r[:newsMaxCols])
		}
		lines[i] = ln
	}
	// Drop trailing whitespace-only lines (EOF newline / blank padding).
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	per := geom.NewsLinesPerPage()
	var pages [][]string
	for i := 0; i < len(lines); i += per {
		end := i + per
		if end > len(lines) {
			end = len(lines)
		}
		pages = append(pages, lines[i:end])
	}
	return pages
}
```

> `NewsScreen` is added to this file in Task 3 (it brings the `go3270` import). For now the file imports only `strings`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/screens/ -run TestPaginateNews -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/screens/news.go internal/screens/news_test.go
git commit -m "feat(screens): PaginateNews splits/truncates MOTD text into pages (GH #45)"
```

---

## Task 3: `NewsScreen` builder

**Files:**
- Modify: `internal/screens/news.go`
- Test: `internal/screens/news_test.go`

Layout: one red, protected field per text line at `(row i, Col 0)`; the `***` page gate as a red protected field on `(NewsLinesPerPage()+1, Col 0)` — the last row. No title, no PF-key help (deliberate TSO fidelity). Cursor homes to `{0,0}` (no input field).

- [ ] **Step 1: Write the failing test**

Append to `internal/screens/news_test.go`:

```go
func TestNewsScreenFieldsRedProtectedWithGate(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	page := []string{"NEWS LINE A", "NEWS LINE B"}
	screen, rules, cur := NewsScreen(mod2, page)

	if rules != nil {
		t.Errorf("rules = %v, want nil (no input fields)", rules)
	}
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("cursor = %+v, want home {0,0}", cur)
	}
	// One field per line + one "***" gate field.
	if len(screen) != len(page)+1 {
		t.Fatalf("screen has %d fields, want %d", len(screen), len(page)+1)
	}
	for i := 0; i < len(page); i++ {
		f := screen[i]
		if f.Row != i || f.Col != 0 {
			t.Errorf("line %d at (%d,%d), want (%d,0)", i, f.Row, f.Col, i)
		}
		if f.Content != page[i] {
			t.Errorf("line %d content = %q, want %q", i, f.Content, page[i])
		}
		if f.Color != go3270.Red {
			t.Errorf("line %d color = %v, want Red", i, f.Color)
		}
		if f.Write {
			t.Errorf("line %d is writable; MOTD text must be protected", i)
		}
	}
	gate := screen[len(screen)-1]
	if gate.Content != "***" {
		t.Errorf("gate content = %q, want ***", gate.Content)
	}
	if gate.Row != mod2.NewsLinesPerPage()+1 { // 23 on MOD 2 (last row)
		t.Errorf("gate row = %d, want %d", gate.Row, mod2.NewsLinesPerPage()+1)
	}
	if gate.Color != go3270.Red || gate.Write {
		t.Errorf("gate field = %+v, want red protected", gate)
	}
}

func TestNewsScreenGateRowFixedOnShortPage(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := NewsScreen(mod2, []string{"only one line"})
	gate := screen[len(screen)-1]
	if gate.Row != 23 {
		t.Errorf("gate row on short page = %d, want 23 (always last row)", gate.Row)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestNewsScreen -v`
Expected: FAIL — `undefined: NewsScreen`.

- [ ] **Step 3: Write minimal implementation**

In `internal/screens/news.go`, change the import line `import "strings"` to a grouped block adding go3270:

```go
import (
	"strings"

	"github.com/racingmars/go3270"
)
```

Then append `NewsScreen`:

```go
// NewsScreen renders one MOTD/NEWS page: each text line as a red, protected
// field at column 0, then the "***" page gate on the last row. There is no
// title row and no PF-key help row — a deliberate departure from the app's
// usual screen chrome, matching classic TSO/READY logon messages. The cursor
// homes to {0,0}; there is no input field. The caller (Presenter.News) drives
// it with go3270.HandleScreenAlt: AIDEnter advances, PA3/PF3 are silent no-ops.
func NewsScreen(geom Geometry, page []string) (go3270.Screen, go3270.Rules, Cursor) {
	screen := make(go3270.Screen, 0, len(page)+1)
	for i, line := range page {
		screen = append(screen, go3270.Field{
			Row: i, Col: 0, Color: go3270.Red, Content: line,
		})
	}
	screen = append(screen, go3270.Field{
		Row: geom.NewsLinesPerPage() + 1, Col: 0, Color: go3270.Red, Content: "***",
	})
	return screen, nil, Cursor{Row: 0, Col: 0}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/screens/ -v`
Expected: PASS (PaginateNews + NewsScreen + NewsLinesPerPage).

- [ ] **Step 5: Commit**

```bash
git add internal/screens/news.go internal/screens/news_test.go
git commit -m "feat(screens): NewsScreen renders a red, chrome-less MOTD page (GH #45)"
```

---

## Task 4: `KeyMOTDFile` constant in sysconfig

**Files:**
- Modify: `internal/sysconfig/catalog.go`
- Test: `internal/sysconfig/catalog_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/sysconfig/catalog_test.go`:

```go
func TestKeyMOTDFileConstant(t *testing.T) {
	if KeyMOTDFile != "MOTD_FILE" {
		t.Errorf("KeyMOTDFile = %q, want MOTD_FILE", KeyMOTDFile)
	}
	found := false
	for _, e := range Catalog {
		if e.Key == KeyMOTDFile {
			found = true
		}
	}
	if !found {
		t.Errorf("Catalog has no entry keyed by KeyMOTDFile")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sysconfig/ -run TestKeyMOTDFileConstant -v`
Expected: FAIL — `undefined: KeyMOTDFile`.

- [ ] **Step 3: Write minimal implementation**

In `internal/sysconfig/catalog.go`, add the constant above `var Catalog` and use it for the entry key:

```go
// KeyMOTDFile is the system_config key whose value is the absolute path to the
// MOTD/NEWS file shown after login. Empty disables the feature.
const KeyMOTDFile = "MOTD_FILE"
```

Then change the entry's `Key: "MOTD_FILE",` line to:

```go
		Key:     KeyMOTDFile,
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sysconfig/ -v`
Expected: PASS (existing catalog tests + the new one).

- [ ] **Step 5: Commit**

```bash
git add internal/sysconfig/catalog.go internal/sysconfig/catalog_test.go
git commit -m "refactor(sysconfig): export KeyMOTDFile constant (GH #45)"
```

---

## Task 5: `Presenter.News` seam — interface, real impl, fake stub

This task adds the seam and makes everything compile. The real `go3270Presenter.News` has no unit test (like `Login`/`Menu`, it is verified by the smoke test in Task 7); correctness here is "still builds, all existing tests pass."

**Files:**
- Modify: `internal/server/session.go` (interface)
- Modify: `internal/server/presenter.go` (real impl)
- Modify: `internal/server/session_test.go` (fake impl + recording fields)

- [ ] **Step 1: Add `News` to the `Presenter` interface**

In `internal/server/session.go`, add the method to the `Presenter` interface (after `Menu`):

```go
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, errMsg string) (selected *store.Service, adminSel bool, quit bool, err error)
	// News shows the MOTD pages (already paginated) one at a time: ENTER
	// advances, the last ENTER returns nil. PA3/PF3 are silent no-ops. A
	// non-nil error is a disconnect or an idle timeout (classified by the
	// caller). News is only called with at least one page.
	News(conn net.Conn, term Term, pages [][]string) error
```

- [ ] **Step 2: Verify it fails to build (no impl of News yet)**

Run: `go build ./internal/server/`
Expected: FAIL — `cannot use go3270Presenter{} ... missing method News` at `server.go:120` (the real presenter is assigned to a `Presenter` field). The fake also lacks `News`, so the test build fails too. Steps 3–4 add both impls.

- [ ] **Step 3: Implement `go3270Presenter.News`**

In `internal/server/presenter.go`, add after the `Menu` method:

```go
func (go3270Presenter) News(conn net.Conn, term Term, pages [][]string) error {
	geom := term.Geometry()
	for i := 0; i < len(pages); {
		screen, rules, cur := screens.NewsScreen(geom, pages[i])
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, rules, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				"", cur.Row, cur.Col, conn, term.dev, term.codepage(),
			)
		})
		if err != nil {
			return err
		}
		// ENTER advances (the last page's ENTER ends the loop → nil). PF3
		// returns here but is a deliberate no-op: re-present the same page.
		// PA1/PA2/PA3/Clear never reach here (handleScreen swallows them).
		if resp.AID == go3270.AIDEnter {
			i++
		}
	}
	return nil
}
```

- [ ] **Step 4: Implement the fake `News` with recording fields**

In `internal/server/session_test.go`, add recording fields to the `fakePresenter` struct (after `gotTerms`):

```go
	gotTerms     []Term // every term passed to Login/Menu, in call order
	newsCalls    [][][]string // pages passed to each News call, in order
	newsResults  []error      // queued News return values; default nil
```

Then add the method (after the `Menu` method):

```go
func (f *fakePresenter) News(conn net.Conn, term Term, pages [][]string) error {
	f.gotTerms = append(f.gotTerms, term)
	f.newsCalls = append(f.newsCalls, pages)
	if len(f.newsResults) > 0 {
		r := f.newsResults[0]
		f.newsResults = f.newsResults[1:]
		return r
	}
	return nil
}
```

- [ ] **Step 5: Verify the build and the full suite pass**

Run: `go build ./... && go test ./internal/server/ -race`
Expected: PASS. Existing tests are unaffected because the test store seeds `MOTD_FILE=""`, so `maybeShowNews` (added next task) will skip — but it doesn't exist yet, so for now nothing calls `News`. All green.

- [ ] **Step 6: Commit**

```bash
git add internal/server/session.go internal/server/presenter.go internal/server/session_test.go
git commit -m "feat(server): add Presenter.News seam + go3270 paging impl (GH #45)"
```

---

## Task 6: Session wiring — `maybeShowNews` + reader seam

**Files:**
- Modify: `internal/server/session.go` (struct field, default reader, `maybeShowNews`, `Run` wiring, imports)
- Test: `internal/server/session_motd_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `internal/server/session_motd_test.go`:

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

package server

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// motdSession builds a test session whose MOTD_FILE is set to path and whose
// MOTDRead returns (data, readErr). readCalled (if non-nil) is set true when
// the reader is invoked, so tests can assert the reader was skipped.
func motdSession(t *testing.T, p *fakePresenter, path string, data []byte, readErr error, readCalled *bool) *Session {
	t.Helper()
	s := newTestSession(t, p, &fakeBridger{})
	// MOTD_FILE is seeded empty by migrate(); set it for the test.
	if err := s.Store.SetConfig(context.Background(), "MOTD_FILE", path); err != nil {
		t.Fatalf("SetConfig MOTD_FILE: %v", err)
	}
	s.MOTDRead = func(string) ([]byte, error) {
		if readCalled != nil {
			*readCalled = true
		}
		return data, readErr
	}
	return s
}

func TestMOTDShownBeforeMenu(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := motdSession(t, p, "/etc/motd.txt", []byte("hello\nworld"), nil, nil)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.newsCalls) != 1 {
		t.Fatalf("News called %d times, want 1", len(p.newsCalls))
	}
	pages := p.newsCalls[0]
	if len(pages) != 1 || len(pages[0]) != 2 ||
		pages[0][0] != "hello" || pages[0][1] != "world" {
		t.Errorf("News pages = %#v, want one page [hello world]", pages)
	}
}

func TestMOTDUnsetSkips(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{{quit: true}},
	}
	// MOTD_FILE stays at its seeded default "".
	s := newTestSession(t, p, &fakeBridger{})

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.newsCalls) != 0 {
		t.Errorf("News called %d times, want 0 when MOTD_FILE unset", len(p.newsCalls))
	}
}

func TestMOTDRelativePathSkipsWithoutReading(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{{quit: true}},
	}
	read := false
	s := motdSession(t, p, "relative/motd.txt", []byte("x"), nil, &read)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if read {
		t.Error("reader was called for a relative path; want skipped before read")
	}
	if len(p.newsCalls) != 0 {
		t.Errorf("News called %d times, want 0 for relative path", len(p.newsCalls))
	}
}

func TestMOTDReadErrorSkips(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := motdSession(t, p, "/etc/motd.txt", nil, errors.New("boom"), nil)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.newsCalls) != 0 {
		t.Errorf("News called %d times, want 0 on read error", len(p.newsCalls))
	}
}

func TestMOTDEmptyFileSkips(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := motdSession(t, p, "/etc/motd.txt", []byte("   \n\n"), nil, nil)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.newsCalls) != 0 {
		t.Errorf("News called %d times, want 0 for whitespace-only file", len(p.newsCalls))
	}
}

func TestMOTDIdleTimeoutLogsOut(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // re-presented login after idle-logout
		},
	}
	s := motdSession(t, p, "/etc/motd.txt", []byte("news"), nil, nil)
	p.newsResults = []error{os.ErrDeadlineExceeded}
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.logins) != 0 {
		t.Fatalf("expected both login renders consumed, %d left (login not re-presented)", len(p.logins))
	}
	var logout *store.AuditEvent
	for i := range rec.events {
		if rec.events[i].Kind == store.AuditLogout {
			logout = &rec.events[i]
		}
	}
	if logout == nil || logout.Detail != "idle logout" || logout.Username != "alice" {
		t.Errorf("want AuditLogout{idle logout, alice}, got %+v", logout)
	}
}

func TestMOTDRenderErrorDisconnects(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}},
	}
	s := motdSession(t, p, "/etc/motd.txt", []byte("news"), nil, nil)
	p.newsResults = []error{errors.New("stream broke")}
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	var disc *store.AuditEvent
	for i := range rec.events {
		if rec.events[i].Kind == store.AuditDisconnect {
			disc = &rec.events[i]
		}
	}
	if disc == nil || disc.Detail != "news render error" {
		t.Errorf("disconnect = %+v, want Detail %q", disc, "news render error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run TestMOTD -v`
Expected: FAIL — `s.MOTDRead undefined`.

- [ ] **Step 3: Implement the session changes**

In `internal/server/session.go`:

(a) Add imports. The import block currently has `context, errors, log, net, slices, strconv, time` plus the internal packages. Add `io`, `os`, `path/filepath`, `strings`, and the `screens` and `sysconfig` packages:

```go
import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
)
```

(b) Add the size cap constant and the `MOTDRead` field. After the `Session` struct's `BridgeIdleExempt bool` field, add inside the struct:

```go
	// MOTDRead reads the MOTD file for maybeShowNews; nil selects the capped
	// os.ReadFile default (readMOTDCapped). Tests inject a fake.
	MOTDRead func(path string) ([]byte, error)
```

After the `Session` struct definition, add:

```go
// motdReadCap bounds how much of the MOTD file is read. A legitimate notice is
// a few screens of text; the cap is a defensive ceiling against a misconfigured
// path (a device/FIFO or a huge file). An over-cap file is truncated, not
// rejected — the banner is simply clipped.
const motdReadCap = 8 << 10 // 8 KiB

// readMOTDCapped is the default MOTDRead: it reads at most motdReadCap bytes.
func readMOTDCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, motdReadCap))
}
```

(c) Add the `maybeShowNews` method. Place it just after `Run` (before `doLogin`):

```go
// maybeShowNews renders the MOTD/NEWS gate once after login, before the menu.
// It returns (true, nil) to proceed into the menu — including every skip case
// (MOTD disabled, unreadable, relative path, or empty). It returns (false, nil)
// when the user idled out during the gate (audited + pre-auth re-armed here; the
// caller returns to the login screen). A non-nil error is a fatal render error
// (the caller disconnects).
func (s *Session) maybeShowNews(ctx context.Context, conn net.Conn, term Term, identity auth.Identity, aud *auditTrail) (bool, error) {
	path, err := s.Store.GetConfig(ctx, sysconfig.KeyMOTDFile)
	if err != nil || strings.TrimSpace(path) == "" {
		return true, nil // unset/empty (or store hiccup) → straight to the menu
	}
	if !filepath.IsAbs(path) {
		log.Printf("MOTD file %q is not absolute; skipping", path)
		return true, nil
	}
	read := s.MOTDRead
	if read == nil {
		read = readMOTDCapped
	}
	data, err := read(path)
	if err != nil {
		log.Printf("MOTD file %q unreadable; skipping: %v", path, err)
		return true, nil
	}
	pages := screens.PaginateNews(term.Geometry(), string(data))
	if len(pages) == 0 {
		return true, nil // empty/whitespace-only → straight to the menu
	}
	if err := s.Presenter.News(conn, term, pages); err != nil {
		if isTimeoutErr(err) {
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
			s.armPreAuth(conn)
			return false, nil
		}
		return false, err
	}
	return true, nil
}
```

(d) Wire it into `Run`. In `internal/server/session.go`, the block after a successful login currently reads:

```go
		currentUser = identity.Username
		s.armPostAuth(conn) // authenticated: post-auth idle window

		isAdmin := s.AdminPresenter != nil && slices.Contains(identity.Groups, store.AdminGroup)
```

Insert the gate between `armPostAuth` and the `isAdmin` line:

```go
		currentUser = identity.Username
		s.armPostAuth(conn) // authenticated: post-auth idle window

		// MOTD/NEWS gate: shown once per login, before the menu.
		enterMenu, nerr := s.maybeShowNews(ctx, conn, term, identity, aud)
		if nerr != nil {
			endDetail = "news render error"
			return
		}
		if !enterMenu { // idled out during the gate: back to the login screen
			currentUser = ""
			continue
		}

		isAdmin := s.AdminPresenter != nil && slices.Contains(identity.Groups, store.AdminGroup)
```

- [ ] **Step 4: Run the MOTD tests, then the full suite**

Run: `go test ./internal/server/ -run TestMOTD -v`
Expected: PASS (all seven MOTD tests).

Run: `go test ./... -race`
Expected: PASS across all packages (existing server tests still green — the seeded `MOTD_FILE=""` skips the gate everywhere).

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/session_motd_test.go
git commit -m "feat(server): MOTD/NEWS gate between login and menu (GH #45)"
```

---

## Task 7: Smoke test scenario (live protocol verification)

The real `go3270Presenter.News` is verified here, against s3270 — the red attribute, page-advance on ENTER, PA3/PF3 inert, and the last ENTER landing on the menu. This task is run and iterated via the **s3270-smoke-testing** skill; exact s3270 cursor/macro coordinates are finalized during live runs (this is the one layer where bytes can only be confirmed against a live emulator).

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`

- [ ] **Step 1: Add MOTD setup to the front-proxy fixture**

Before the front proxy is started, create a two-page MOTD file under `$WORK` and configure it. Because system parameters are runtime config (set via the admin 3270 UI, not seed JSON), drive the admin path that scenario 9 already uses (root → admin menu → System Parameters), type the absolute path `$WORK/motd.txt` into the `MOTD File` field, press ENTER to save, then PF3 back and PF3 to log off. Compose `motd.txt` with ~25 lines so it spans two MOD-2 pages:

```sh
# MOTD: two MOD-2 pages (≥23 lines) to exercise the *** page gate.
{
  echo "*** SYSTEM NEWS ***"
  for i in $(seq 1 24); do echo "NEWS LINE $i"; done
} > "$WORK/motd.txt"
```

- [ ] **Step 2: Add the assertions**

After a fresh `alice` login (reuse the scenario-3 login macro), the MOTD must appear before the menu. Add checks (the `check`/`ncheck` helpers already exist in smoke.sh):

```sh
# --- N. MOTD/NEWS gate: red, paged, ENTER advances, PA3/PF3 inert ---
# Fresh alice login → MOTD page 1 (NOT the menu yet).
check  "Na MOTD page 1 shown before menu"   "SYSTEM NEWS"            "$WORK/tN.out"
ncheck "Nb menu not shown on MOTD page 1"    "TN3270 GATEWAY MENU"   "$WORK/tN.out"
# PA3 and PF3 on the MOTD are no-ops: still on the MOTD, still not the menu.
check  "Nc PA3/PF3 inert on MOTD"            "SYSTEM NEWS"           "$WORK/tNb.out"
ncheck "Nd PF3 did not skip to menu"         "TN3270 GATEWAY MENU"   "$WORK/tNb.out"
# ENTER through both pages → lands on the menu.
check  "Ne ENTER through MOTD reaches menu"  "TN3270 GATEWAY MENU"   "$WORK/tNc.out"
```

- [ ] **Step 3: Run the smoke suite and iterate**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all existing checks plus the new MOTD checks PASS. Adjust the s3270 macro ENTER counts / capture points until green. If the red attribute needs asserting at the protocol level, capture the s3270 `ReadBuffer(Ascii)` field attributes the way the existing non-display-password check (scenario 1) does.

- [ ] **Step 4: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "test(smoke): MOTD/NEWS gate — red, paged, PA3/PF3 inert (GH #45)"
```

---

## Task 8: Operator + convention documentation

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add the operator note to README.md**

Insert a new section immediately before the `## Status` line:

```markdown
## Message of the Day (MOTD / NEWS)

Set the **MOTD File** system parameter (admin menu → System Parameters) to the
**absolute path** of a plain-text file. After a successful login, its contents
are shown in red, one page at a time, before the menu — press **ENTER** to page
through (PA3 and PF3 do nothing here). Leave the parameter empty to disable it.

The file is read **fresh on every login**, so edits take effect on the next
sign-on — no restart. Authoring rules:

- **Absolute path only.** A relative path is ignored (logged, then skipped).
- **Fixed width: lines truncate at column 79** ("80-column record length"). The
  file is a composed banner — you own the line breaks and indentation; anything
  past column 79 is dropped. Compose to fit.
- **Keep it short** — realistically no more than ~3 screens; longer notices tend
  to get paged past unread. Files larger than 8 KiB are clipped.
- Write maintenance windows in **UTC** by convention.
```

- [ ] **Step 2: Add the convention note to CLAUDE.md**

In `CLAUDE.md`, under the **Gotchas** or **Screen layout convention** area, add one bullet so a future reviewer doesn't "fix" the missing chrome:

```markdown
- **MOTD/NEWS screen is deliberately chrome-less** (`internal/screens/news.go`):
  no title row, no PF-key help row — just red protected text and a `***` page
  gate on the last row, matching classic TSO/READY logon messages. This is an
  intentional exception to the "title on row 0, PF help on the last row" rule.
  Lines truncate at column 79; the gate sits on `NewsLinesPerPage()+1`.
```

- [ ] **Step 3: Verify the build/tests are unaffected and commit**

Run: `go build ./... && go test ./...`
Expected: PASS (docs-only change).

```bash
git add README.md CLAUDE.md
git commit -m "docs: document MOTD/NEWS authoring + chrome-less convention (GH #45)"
```

---

## Final verification

- [ ] **Run the full suite with the race detector**

Run: `go test ./... -race`
Expected: PASS across all packages.

- [ ] **Run the smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all checks PASS, including the new MOTD scenario.

- [ ] **Confirm spec coverage** — every spec section maps to a task: pagination/truncation (T2), rendering/no-chrome/`***` (T1, T3), config key (T4), presenter seam + ENTER/PA3/PF3 (T5), session gate + skip rules + idle/error handling + read cap + reader seam (T6), smoke verification (T7), operator + convention docs (T8). No audit event is added (by design).
