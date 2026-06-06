# Audit Log Viewer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only, newest-first 3270 viewer of the last 72h of the audit trail to the admin menu, with derived-severity colour-coding and a PTR-enriched detail drill-down.

**Architecture:** A new snapshot-paging driver (`ui3270.RunSnapshotList`) fetches once and pages a held slice; plain Enter re-queries. Two bespoke builders (`buildSnapshotScreen`, `buildDetailScreen`) live in `ui3270` behind a widened `Renderer`. The `server` layer (`admin_audit.go`) reads two new system parameters, formats rows (computing the event colour from `store` kinds), runs the driver, and on `S` resolves a reverse-DNS PTR (behind a testable `Resolver` seam) before painting the detail screen. No store changes — the existing `ListAudit` filters cover newest-first + time-bounded.

**Tech Stack:** Go, `github.com/racingmars/go3270`, `modernc.org/sqlite`, stdlib `net` (reverse DNS).

**Spec:** `docs/superpowers/specs/2026-06-06-audit-log-viewer-design.md`

---

## File Structure

**Modify:**
- `internal/sysconfig/catalog.go` — two new param consts + entries; `intInRange`/`onOff` validators.
- `internal/sysconfig/catalog_test.go` — validator + catalog tests.
- `internal/ui3270/types.go` — new view types; widen `Renderer` with `Snapshot`/`Detail`.
- `internal/ui3270/renderer.go` — real `Snapshot`/`Detail` impls on `go3270Renderer`.
- `internal/ui3270/form_test.go` — add `Snapshot`/`Detail` to the existing `fakeRenderer`.
- `internal/server/admin.go` — `ListAudit` on `AdminStore`; `resolver`/`now` fields on `adminFlow`; `case 6` dispatch.
- `internal/server/presenter_admin.go` — accept `"6"` in `AdminMenu`; update doc.
- `internal/screens/admin.go` — add `6.  Audit Log` to `AdminMenuScreen`.
- `internal/server/session.go` — thread `resolver`/`now` into the `adminFlow` literal.
- `internal/server/admin_test.go` — add `Snapshot`/`Detail` to `fakeAdminPresenter`.

**Create:**
- `internal/ui3270/snapshotscreen.go` — `buildSnapshotScreen`, `buildDetailScreen`, `wrapText`, column constants.
- `internal/ui3270/snapshotscreen_test.go` — builder tests.
- `internal/ui3270/snapshotlist.go` — `RunSnapshotList` driver + config/types.
- `internal/ui3270/snapshotlist_test.go` — driver tests.
- `internal/server/resolver.go` — `Resolver` seam + `ptr` helper.
- `internal/server/resolver_test.go` — fake-resolver tests.
- `internal/server/admin_audit.go` — the audit flow + colour map + param readers + julian stamp.
- `internal/server/admin_audit_test.go` — flow tests.

**Conventions:** every new `.go` file starts with the GPL header block copied verbatim from an existing file in the same package (e.g. `internal/ui3270/list.go`). Run `go test ./... -race` (the bridge/driver are concurrent). Commit after each task.

---

## Task 1: System-parameter validators (`intInRange`, `onOff`)

**Files:**
- Modify: `internal/sysconfig/catalog.go`
- Test: `internal/sysconfig/catalog_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/sysconfig/catalog_test.go`:

```go
func TestIntInRange(t *testing.T) {
	v := intInRange(1, 10000)
	for _, ok := range []string{"1", "10000", " 500 "} {
		if msg := v(ok); msg != "" {
			t.Errorf("intInRange(%q) = %q, want accept", ok, msg)
		}
	}
	for _, bad := range []string{"0", "10001", "-1", "", "x", "1.5"} {
		if v(bad) == "" {
			t.Errorf("intInRange(%q) accepted, want reject", bad)
		}
	}
}

func TestOnOff(t *testing.T) {
	for _, ok := range []string{"ON", "OFF", " on ", "off"} {
		if msg := onOff(ok); msg != "" {
			t.Errorf("onOff(%q) = %q, want accept", ok, msg)
		}
	}
	for _, bad := range []string{"", "YES", "1", "TRUE"} {
		if onOff(bad) == "" {
			t.Errorf("onOff(%q) accepted, want reject", bad)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sysconfig/ -run 'TestIntInRange|TestOnOff' -v`
Expected: FAIL — `undefined: intInRange`, `undefined: onOff`.

- [ ] **Step 3: Implement the validators**

Add to `internal/sysconfig/catalog.go` (next to `nonNegativeInt`/`positiveInt`):

```go
// intInRange returns a validator accepting an integer in [min, max] inclusive.
func intInRange(min, max int) func(string) string {
	return func(v string) string {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < min || n > max {
			return fmt.Sprintf("MUST BE AN INTEGER FROM %d TO %d", min, max)
		}
		return ""
	}
}

// onOff accepts the literals ON or OFF (case-insensitive, surrounding space
// trimmed). Used by boolean-toggle parameters.
func onOff(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "ON", "OFF":
		return ""
	}
	return "MUST BE ON OR OFF"
}
```

Add `"fmt"` to the import block of `catalog.go` (it currently imports only `strconv` and `strings`).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sysconfig/ -run 'TestIntInRange|TestOnOff' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sysconfig/catalog.go internal/sysconfig/catalog_test.go
git commit -m "feat(sysconfig): add intInRange and onOff param validators (GH #40)"
```

---

## Task 2: Audit system-parameter catalog entries

**Files:**
- Modify: `internal/sysconfig/catalog.go`
- Test: `internal/sysconfig/catalog_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/sysconfig/catalog_test.go`:

```go
func TestAuditCatalogEntries(t *testing.T) {
	want := map[string]string{
		KeyAuditMaxRows:    "1000",
		KeyAuditReverseDNS: "ON",
	}
	got := map[string]string{}
	for _, e := range Catalog {
		if _, ok := want[e.Key]; ok {
			got[e.Key] = e.Default
			if e.Validate == nil {
				t.Errorf("%s has no Validate", e.Key)
			}
		}
	}
	for k, def := range want {
		if got[k] != def {
			t.Errorf("Catalog[%s].Default = %q, want %q", k, got[k], def)
		}
	}
	// AUDIT_MAX_ROWS bounds: 0 and 10001 rejected, 1000 accepted.
	for _, e := range Catalog {
		if e.Key == KeyAuditMaxRows {
			if e.Validate("0") == "" || e.Validate("10001") == "" || e.Validate("1000") != "" {
				t.Errorf("AUDIT_MAX_ROWS validator bounds wrong")
			}
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sysconfig/ -run TestAuditCatalogEntries -v`
Expected: FAIL — `undefined: KeyAuditMaxRows`.

- [ ] **Step 3: Add the consts and Catalog entries**

Add consts near the other key consts in `internal/sysconfig/catalog.go`:

```go
// KeyAuditMaxRows caps how many audit rows the admin Audit Log viewer (GH #40)
// holds in a single snapshot. Clamped to [1, 10000]; read with a fallback to
// DefaultAuditMaxRows for a hand-edited DB.
const (
	KeyAuditMaxRows     = "AUDIT_MAX_ROWS"
	DefaultAuditMaxRows = 1000
)

// KeyAuditReverseDNS toggles the reverse-DNS (PTR) lookup on the Audit Log
// detail screen. "ON" (default) or "OFF"; OFF suppresses all outbound DNS the
// viewer would otherwise emit (air-gapped / egress-locked deployments).
const (
	KeyAuditReverseDNS     = "AUDIT_REVERSE_DNS"
	DefaultAuditReverseDNS = "ON"
)
```

Append two entries to the `Catalog` slice (after the throttle entries, before the closing `}`):

```go
	{
		Key:      KeyAuditMaxRows,
		Label:    "Audit View Max Rows:",
		Default:  strconv.Itoa(DefaultAuditMaxRows),
		Validate: intInRange(1, 10000),
	},
	{
		Key:       KeyAuditReverseDNS,
		Label:     "Audit Reverse DNS:",
		Default:   DefaultAuditReverseDNS,
		Length:    3,
		Normalize: func(v string) string { return strings.ToUpper(strings.TrimSpace(v)) },
		Validate:  onOff,
	},
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sysconfig/ -v`
Expected: PASS (all sysconfig tests, including existing catalog/seed coverage).

- [ ] **Step 5: Verify the params seed into a fresh DB**

Run: `go test ./internal/store/ -run Config -v`
Expected: PASS (migrate() seeds every Catalog key via INSERT OR IGNORE; existing config tests iterate the catalog and stay green).

- [ ] **Step 6: Commit**

```bash
git add internal/sysconfig/catalog.go internal/sysconfig/catalog_test.go
git commit -m "feat(sysconfig): AUDIT_MAX_ROWS and AUDIT_REVERSE_DNS params (GH #40)"
```

---

## Task 3: ui3270 view types (no interface change yet)

**Files:**
- Modify: `internal/ui3270/types.go`

> **Ordering note:** the `Renderer` interface is widened in Task 6, together with
> the real methods that satisfy it — not here. Adding the methods to the
> interface before `go3270Renderer` implements them (and before the builders they
> call exist) would break the package build. This task adds only the data types,
> which the builders (Tasks 4–5) consume; the package keeps compiling throughout.

- [ ] **Step 1: Add the new view types**

In `internal/ui3270/types.go`, add `"github.com/racingmars/go3270"` to the import block (currently only `"context"`), then add:

```go
// SnapshotRow is one row of a snapshot list, laid as three fields so the middle
// segment (Mid) can carry its own colour. Left/Mid/Right are pre-formatted and
// pre-padded by the caller; the builder places them at fixed columns.
type SnapshotRow struct {
	Left, Mid, Right string
	MidColor         go3270.Color // go3270.DefaultColor ⇒ no explicit colour
}

// SnapshotView is what to paint for a snapshot (read-only, paged) list. AsOf is
// a caller-formatted stamp shown on row 2; Head is the column-heading row;
// Empty is shown in place of rows when there are none.
type SnapshotView struct {
	Title, RowInfo, AsOf, Legend, ErrMsg, PFHelp, Empty string
	Head                                                SnapshotRow
	Rows                                                []SnapshotRow
}

// DetailField is one label/value line on the detail screen. Color tints the
// value (used for the event field); go3270.DefaultColor leaves it plain.
type DetailField struct {
	Label, Value string
	Color        go3270.Color
}

// DetailView is what to paint for a read-only detail screen: a column of
// label/value fields, then a full-width wrapped free-text block under BodyLabel.
type DetailView struct {
	Title, BodyLabel, Body, PFHelp string
	Fields                         []DetailField
}
```

(Do **not** touch the `Renderer` interface in this task — that happens in Task 6.)

- [ ] **Step 2: Verify the package still builds and passes**

Run: `go test ./internal/ui3270/ -race`
Expected: PASS — the new types are additive and unused so far; nothing else changes.

- [ ] **Step 3: Commit**

```bash
git add internal/ui3270/types.go
git commit -m "feat(ui3270): snapshot/detail view types (GH #40)"
```

---

## Task 4: `buildSnapshotScreen` builder

**Files:**
- Create: `internal/ui3270/snapshotscreen.go`
- Test: `internal/ui3270/snapshotscreen_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui3270/snapshotscreen_test.go` (with the GPL header):

```go
package ui3270

import (
	"testing"

	"github.com/racingmars/go3270"
)

// fieldAt returns the field whose attribute byte is at (row, col), or false.
func fieldAt(s go3270.Screen, row, col int) (go3270.Field, bool) {
	for _, f := range s {
		if f.Row == row && f.Col == col {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestBuildSnapshotScreen_RowLayoutAndColor(t *testing.T) {
	v := SnapshotView{
		Title: "RECENT ACTIVITY", RowInfo: "ROW 1 TO 1 OF 1",
		AsOf: "AS OF 2026-06-06 (2026.157)  14:32 UTC",
		Head: SnapshotRow{Left: "MM/DD HH:MM USERNAME", Mid: "EVENT", Right: "DETAIL"},
		Rows: []SnapshotRow{
			{Left: "06/06 14:28 BADGUY", Mid: "AUTH_FAIL", Right: "delay=4s count=2", MidColor: go3270.Red},
		},
		Legend: "S=Detail", PFHelp: "PF3=Back   PF7=Bkwd  PF8=Fwd   Enter=Refresh",
		Empty:  "(none)",
	}
	screen, cur := buildSnapshotScreen(24, v)

	// Title row 0; as-of row 2; headings row 3; first data row 4.
	if f, ok := fieldAt(screen, 0, 2); !ok || f.Content != "RECENT ACTIVITY" {
		t.Errorf("title missing/wrong: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 2, snapLeftAttr); !ok || f.Content != v.AsOf {
		t.Errorf("as-of stamp missing on row 2: %+v ok=%v", f, ok)
	}
	// Command field on the data row (col snapCmdAttr, writable).
	if f, ok := fieldAt(screen, 4, snapCmdAttr); !ok || !f.Write {
		t.Errorf("cmd field missing/not writable on data row: %+v ok=%v", f, ok)
	}
	// Mid (event) is its own field at snapMidAttr with the row's colour.
	if f, ok := fieldAt(screen, 4, snapMidAttr); !ok || f.Content != "AUTH_FAIL" || f.Color != go3270.Red {
		t.Errorf("event field wrong: %+v ok=%v", f, ok)
	}
	// Right (detail) at snapRightAttr.
	if f, ok := fieldAt(screen, 4, snapRightAttr); !ok || f.Content != "delay=4s count=2" {
		t.Errorf("detail field wrong: %+v ok=%v", f, ok)
	}
	// Cursor homes to the first command field (attr col + 1).
	if cur.Row != 4 || cur.Col != snapCmdAttr+1 {
		t.Errorf("cursor = %+v, want {4,%d}", cur, snapCmdAttr+1)
	}
}

func TestBuildSnapshotScreen_Empty(t *testing.T) {
	screen, cur := buildSnapshotScreen(24, SnapshotView{Empty: "(none)"})
	if f, ok := fieldAt(screen, 4, snapLeftAttr); !ok || f.Content != "(none)" {
		t.Errorf("empty marker missing: %+v ok=%v", f, ok)
	}
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("empty cursor = %+v, want home {0,0}", cur)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui3270/ -run TestBuildSnapshotScreen 2>&1 | head`
Expected: FAIL — `undefined: buildSnapshotScreen` / `undefined: snapLeftAttr`.

- [ ] **Step 3: Implement the builder**

Create `internal/ui3270/snapshotscreen.go` (with the GPL header):

```go
package ui3270

import "github.com/racingmars/go3270"

// Snapshot row column geometry (0-based; attribute byte at the named column,
// content one column right). The Mid/Right attribute bytes double as the
// column separators, so the visible budget is Left 20 (8-27), Mid 12 (29-40),
// Right 38 (42-79).
const (
	snapCmdAttr   = 2  // line-command field attr; input at col 3
	snapCmdStop   = 4  // stop field: 1-char command input
	snapLeftAttr  = 7  // Left segment attr; content cols 8-27
	snapMidAttr   = 28 // Mid (event) attr; content cols 29-40
	snapRightAttr = 41 // Right (detail) attr; content cols 42-79
)

// segments appends the three display fields for one snapshot row at the given
// terminal row. The Mid field carries midColor (DefaultColor ⇒ plain).
func snapSegments(row int, r SnapshotRow) go3270.Screen {
	return go3270.Screen{
		{Row: row, Col: snapLeftAttr, Content: r.Left},
		{Row: row, Col: snapMidAttr, Content: r.Mid, Color: r.MidColor},
		{Row: row, Col: snapRightAttr, Content: r.Right},
	}
}

// buildSnapshotScreen renders a read-only paged list: title + row indicator on
// row 0, the as-of stamp on row 2, column headings on row 3, data from row 4,
// and the bottom-anchored legend/error/help rows. Cursor homes to the first
// command field, or {0,0} when the page is empty.
func buildSnapshotScreen(rows int, v SnapshotView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: 2, Col: snapLeftAttr, Content: v.AsOf},
	}
	screen = append(screen, snapSegments(3, v.Head)...)

	data := v.Rows
	if size := listPageSize(rows); len(data) > size {
		data = data[:size]
	}
	cur := Cursor{Row: 0, Col: 0}
	for i, r := range data {
		row := 4 + i
		cmd := go3270.Field{Row: row, Col: snapCmdAttr, Name: fmt.Sprintf("%s%d", fieldCmdPrefix, i), Write: true, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = Cursor{Row: cmd.Row, Col: cmd.Col + 1}
		}
		screen = append(screen, cmd, go3270.Field{Row: row, Col: snapCmdStop})
		screen = append(screen, snapSegments(row, r)...)
	}
	if len(data) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: snapLeftAttr, Content: v.Empty})
	}
	screen = append(screen,
		go3270.Field{Row: legendRow(rows), Col: 2, Content: v.Legend},
		go3270.Field{Row: errorRow(rows), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 2, Content: v.PFHelp},
	)
	return screen, cur
}
```

Add `"fmt"` to the import block (the `cmd` field name uses `fmt.Sprintf`).

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui3270/ -run TestBuildSnapshotScreen -v`
Expected: PASS. (Other ui3270 tests still fail to compile until Task 6 — that is fine; `-run` targets these.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/snapshotscreen.go internal/ui3270/snapshotscreen_test.go
git commit -m "feat(ui3270): buildSnapshotScreen with colourable event field (GH #40)"
```

---

## Task 5: `buildDetailScreen` + `wrapText`

**Files:**
- Modify: `internal/ui3270/snapshotscreen.go`
- Test: `internal/ui3270/snapshotscreen_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/ui3270/snapshotscreen_test.go`:

```go
func TestWrapText(t *testing.T) {
	got := wrapText("alpha beta gamma delta", 11)
	want := []string{"alpha beta", "gamma delta"}
	if len(got) != len(want) {
		t.Fatalf("wrapText lines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	// A single token longer than width is hard-split, never dropped.
	if g := wrapText("abcdefgh", 3); len(g) != 3 || g[0] != "abc" {
		t.Errorf("hard-split = %v", g)
	}
	if g := wrapText("", 10); len(g) != 0 {
		t.Errorf("empty = %v, want no lines", g)
	}
}

func TestBuildDetailScreen(t *testing.T) {
	v := DetailView{
		Title: "AUDIT DETAIL",
		Fields: []DetailField{
			{Label: "Date/Time", Value: "2026-06-06 (2026.157) 14:28:07 UTC"},
			{Label: "Event", Value: "AUTH_FAIL", Color: go3270.Red},
		},
		BodyLabel: "Detail", Body: "delay=4s count=2",
		PFHelp: "PF3=Back",
	}
	screen, cur := buildDetailScreen(24, v)

	if f, ok := fieldAt(screen, 0, 2); !ok || f.Content != "AUDIT DETAIL" {
		t.Errorf("title wrong: %+v ok=%v", f, ok)
	}
	// Fields start at row 2: label at col 2, value at detailValueCol.
	if f, ok := fieldAt(screen, 2, 2); !ok || f.Content != "Date/Time" {
		t.Errorf("first label wrong: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 3, detailValueCol); !ok || f.Content != "AUTH_FAIL" || f.Color != go3270.Red {
		t.Errorf("event value wrong/uncoloured: %+v ok=%v", f, ok)
	}
	// Read-only screen: cursor homes.
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("cursor = %+v, want home", cur)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui3270/ -run 'TestWrapText|TestBuildDetailScreen' 2>&1 | head`
Expected: FAIL — `undefined: wrapText` / `undefined: buildDetailScreen` / `undefined: detailValueCol`.

- [ ] **Step 3: Implement `wrapText` and `buildDetailScreen`**

Append to `internal/ui3270/snapshotscreen.go`:

```go
// detailValueCol is the attribute-byte column of the value fields on the detail
// screen (content one column right). Matches the form's historical input column.
const detailValueCol = 16

// detailBodyWidth is the wrap width for the free-text body (content cols 3-79).
const detailBodyWidth = 76

// wrapText greedily wraps s to lines of at most width runes, breaking on spaces.
// A token longer than width is hard-split. Returns nil for empty input.
func wrapText(s string, width int) []string {
	if width < 1 || s == "" {
		if s == "" {
			return nil
		}
		width = 1
	}
	var lines []string
	cur := ""
	flush := func() {
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
	}
	for _, word := range strings.Fields(s) {
		for len([]rune(word)) > width { // hard-split an over-long token
			flush()
			r := []rune(word)
			lines = append(lines, string(r[:width]))
			word = string(r[width:])
		}
		switch {
		case cur == "":
			cur = word
		case len([]rune(cur))+1+len([]rune(word)) <= width:
			cur += " " + word
		default:
			flush()
			cur = word
		}
	}
	flush()
	return lines
}

// buildDetailScreen renders a read-only record: title on row 0, label/value
// fields from row 2, then BodyLabel and the wrapped Body. Cursor homes ({0,0}).
func buildDetailScreen(rows int, v DetailView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
	}
	row := 2
	for _, f := range v.Fields {
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Content: f.Label},
			go3270.Field{Row: row, Col: detailValueCol, Content: f.Value, Color: f.Color},
		)
		row++
	}
	row++ // blank separator
	if v.BodyLabel != "" {
		screen = append(screen, go3270.Field{Row: row, Col: 2, Content: v.BodyLabel})
		row++
	}
	for _, line := range wrapText(v.Body, detailBodyWidth) {
		if row >= errorRow(rows) { // never overrun the bottom chrome
			break
		}
		screen = append(screen, go3270.Field{Row: row, Col: 2, Content: line})
		row++
	}
	screen = append(screen, go3270.Field{Row: helpRow(rows), Col: 2, Content: v.PFHelp})
	return screen, Cursor{Row: 0, Col: 0}
}
```

`strings` is already needed — add `"strings"` to the import block of `snapshotscreen.go` (it currently imports `"fmt"` and `go3270`).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/ui3270/ -run 'TestWrapText|TestBuildDetailScreen' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/snapshotscreen.go internal/ui3270/snapshotscreen_test.go
git commit -m "feat(ui3270): buildDetailScreen with wrapped body (GH #40)"
```

---

## Task 6: Widen the Renderer interface + real `Snapshot`/`Detail` methods

Now that the builders exist (Tasks 4–5), widen the interface and implement it in
one commit so the package compiles throughout.

**Files:**
- Modify: `internal/ui3270/types.go` (widen `Renderer`)
- Modify: `internal/ui3270/renderer.go` (real methods)
- Modify: `internal/ui3270/form_test.go` (extend existing `fakeRenderer`)

- [ ] **Step 1: Widen the interface**

In `internal/ui3270/types.go`, extend the `Renderer` interface:

```go
type Renderer interface {
	List(ListView) (ListAction, error)
	Form(FormView) (FormAction, error)
	Snapshot(SnapshotView) (ListAction, error) // paged read-only list
	Detail(DetailView) error                   // read-only screen; returns on PF3
}
```

- [ ] **Step 2: Implement the real methods**

Add to `internal/ui3270/renderer.go` (after the `Form` method). `snapshotExitKeys` mirrors `listExitKeys` but without PF4 (no Add on a read-only list):

```go
var snapshotExitKeys = []go3270.AID{go3270.AIDPF3, go3270.AIDPF7, go3270.AIDPF8}

func (g *go3270Renderer) Snapshot(v SnapshotView) (ListAction, error) {
	screen, cur := buildSnapshotScreen(g.rows, v)
	resp, err := g.call(screen, snapshotExitKeys, cur)
	if err != nil {
		return ListAction{}, err
	}
	return listAction(resp, len(v.Rows)), nil
}

func (g *go3270Renderer) Detail(v DetailView) error {
	screen, cur := buildDetailScreen(g.rows, v)
	_, err := g.call(screen, []go3270.AID{go3270.AIDPF3}, cur)
	return err
}
```

`listAction` already maps PF3/7/8 and a typed `S` line command to a `ListAction`; plain Enter yields `ListAction{}` (Cmd 0, PF 0), which the driver treats as refresh.

- [ ] **Step 3: Extend the existing test fake to satisfy the widened interface**

In `internal/ui3270/form_test.go`, add to `fakeRenderer` (after its `Form` method). These are minimal stubs; the dedicated snapshot tests use their own fake (Task 7).

```go
func (f *fakeRenderer) Snapshot(SnapshotView) (ListAction, error) {
	return ListAction{PF: 3}, nil
}

func (f *fakeRenderer) Detail(DetailView) error { return nil }
```

- [ ] **Step 4: Verify the whole package compiles and all tests pass**

Run: `go test ./internal/ui3270/ -race`
Expected: PASS — `*go3270Renderer` and `fakeRenderer` now satisfy the widened `Renderer`, so `renderer.go` compiles and the Task 4/5 builder tests plus all pre-existing ui3270 tests are green.

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/types.go internal/ui3270/renderer.go internal/ui3270/form_test.go
git commit -m "feat(ui3270): widen Renderer with Snapshot/Detail + go3270 impls (GH #40)"
```

---

## Task 7: `RunSnapshotList` driver

**Files:**
- Create: `internal/ui3270/snapshotlist.go`
- Test: `internal/ui3270/snapshotlist_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui3270/snapshotlist_test.go` (with the GPL header):

```go
package ui3270

import (
	"context"
	"testing"
)

// scriptRenderer returns scripted Snapshot actions and records each view; Detail
// calls are counted.
type scriptRenderer struct {
	acts      []ListAction
	views     []SnapshotView
	detailHit int
}

func (s *scriptRenderer) List(ListView) (ListAction, error) { panic("unused") }
func (s *scriptRenderer) Form(FormView) (FormAction, error) { panic("unused") }
func (s *scriptRenderer) Detail(DetailView) error           { s.detailHit++; return nil }
func (s *scriptRenderer) Snapshot(v SnapshotView) (ListAction, error) {
	s.views = append(s.views, v)
	if len(s.acts) == 0 {
		panic("unexpected Snapshot call")
	}
	a := s.acts[0]
	s.acts = s.acts[1:]
	return a, nil
}

func entries(n int) []SnapshotEntry[int] {
	out := make([]SnapshotEntry[int], n)
	for i := range out {
		out[i] = SnapshotEntry[int]{Row: SnapshotRow{Left: "row"}, Item: i}
	}
	return out
}

func TestRunSnapshotList_SnapshotIsStableAcrossPaging(t *testing.T) {
	fetches := 0
	cfg := SnapshotConfig[int]{
		Rows: 24,
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) {
			fetches++
			return entries(30), "AS OF X", "" // 30 rows, 14/page → 3 pages
		},
	}
	// PF8 (page down), PF8, PF3 (quit). One fetch only — paging must not re-query.
	r := &scriptRenderer{acts: []ListAction{{PF: 8}, {PF: 8}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if fetches != 1 {
		t.Errorf("fetches = %d, want 1 (paging must page the held snapshot)", fetches)
	}
}

func TestRunSnapshotList_EnterRefetches(t *testing.T) {
	fetches := 0
	cfg := SnapshotConfig[int]{
		Rows:  24,
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) { fetches++; return entries(2), "X", "" },
	}
	// Plain Enter (refresh), then PF3.
	r := &scriptRenderer{acts: []ListAction{{}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if fetches != 2 {
		t.Errorf("fetches = %d, want 2 (initial + Enter refresh)", fetches)
	}
}

func TestRunSnapshotList_SelectInvokesOnSelect(t *testing.T) {
	var picked int = -1
	cfg := SnapshotConfig[int]{
		Rows:  24,
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) { return entries(3), "X", "" },
		OnSelect: func(_ context.Context, r Renderer, item int) error {
			picked = item
			return r.Detail(DetailView{})
		},
	}
	// S on row index 1, then PF3.
	r := &scriptRenderer{acts: []ListAction{{Cmd: 'S', Row: 1}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if picked != 1 {
		t.Errorf("OnSelect item = %d, want 1", picked)
	}
	if r.detailHit != 1 {
		t.Errorf("Detail calls = %d, want 1", r.detailHit)
	}
}

func TestRunSnapshotList_EmptyShowsMarker(t *testing.T) {
	cfg := SnapshotConfig[int]{
		Rows: 24, Empty: "(none)",
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) { return nil, "X", "" },
	}
	r := &scriptRenderer{acts: []ListAction{{PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if r.views[0].Empty != "(none)" {
		t.Errorf("empty marker = %q, want (none)", r.views[0].Empty)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui3270/ -run TestRunSnapshotList 2>&1 | head`
Expected: FAIL — `undefined: RunSnapshotList` / `undefined: SnapshotConfig` / `undefined: SnapshotEntry`.

- [ ] **Step 3: Implement the driver**

Create `internal/ui3270/snapshotlist.go` (with the GPL header):

```go
package ui3270

import "context"

// SnapshotEntry pairs a row's display segments with its domain payload. The
// driver never inspects Item; it hands it to OnSelect.
type SnapshotEntry[T any] struct {
	Row  SnapshotRow
	Item T
}

// SnapshotConfig parameterizes RunSnapshotList. Fetch runs once on entry and
// again only on an explicit refresh (plain Enter), returning the rows, a
// caller-formatted as-of stamp (overflow marker already baked in), and an error
// message (non-"" replaces the rows with the error line). OnSelect handles the
// 'S' line command (nil ⇒ 'S' is inert).
type SnapshotConfig[T any] struct {
	Title, Legend, PFHelp, Empty string
	Head                         SnapshotRow
	Rows                         int // terminal row count → page-size math
	Fetch                        func(ctx context.Context) (rows []SnapshotEntry[T], asOf, errMsg string)
	OnSelect                     func(ctx context.Context, r Renderer, item T) error
}

// RunSnapshotList drives a read-only paged viewer until PF3. It fetches a
// snapshot once and pages the held slice; PF7/PF8 page, plain Enter re-fetches
// (and returns to the newest page), 'S' opens a detail via OnSelect and resumes
// the same snapshot. A non-nil error is a dead connection.
func RunSnapshotList[T any](ctx context.Context, r Renderer, cfg SnapshotConfig[T]) error {
	var (
		rows   []SnapshotEntry[T]
		asOf   string
		errMsg string
		page   int
		loaded bool
	)
	for {
		if !loaded {
			rows, asOf, errMsg = cfg.Fetch(ctx)
			if errMsg != "" {
				rows = nil
			}
			loaded = true
		}
		page, start, end, rowInfo := pageBounds(page, len(rows), cfg.Rows)
		pageRows := rows[start:end]
		display := make([]SnapshotRow, len(pageRows))
		for i, e := range pageRows {
			display[i] = e.Row
		}
		act, err := r.Snapshot(SnapshotView{
			Title: cfg.Title, RowInfo: rowInfo, AsOf: asOf, Head: cfg.Head,
			Rows: display, Legend: cfg.Legend, ErrMsg: errMsg, PFHelp: cfg.PFHelp, Empty: cfg.Empty,
		})
		if err != nil {
			return err
		}
		errMsg = ""

		switch {
		case act.PF == 3:
			return nil
		case act.PF == 7:
			page--
		case act.PF == 8:
			if end < len(rows) {
				page++
			}
		case act.Cmd == 'S':
			if cfg.OnSelect != nil && act.Row < len(pageRows) {
				if ferr := cfg.OnSelect(ctx, r, pageRows[act.Row].Item); ferr != nil {
					return ferr
				}
			}
		case act.Cmd == 0 && act.PF == 0: // plain Enter = refresh
			loaded = false
			page = 0
		}
	}
}
```

Note: `pageBounds` returns the clamped page as its first value; the `page, start, end, rowInfo :=` reassignment keeps `page` clamped across iterations (same idiom as `RunList`).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/ui3270/ -run TestRunSnapshotList -race -v`
Expected: PASS (all four cases).

- [ ] **Step 5: Full package check**

Run: `go test ./internal/ui3270/ -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui3270/snapshotlist.go internal/ui3270/snapshotlist_test.go
git commit -m "feat(ui3270): RunSnapshotList snapshot-paging driver (GH #40)"
```

---

## Task 8: Reverse-DNS resolver seam

**Files:**
- Create: `internal/server/resolver.go`
- Test: `internal/server/resolver_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/server/resolver_test.go` (with the GPL header):

```go
package server

import (
	"context"
	"errors"
	"testing"
)

type fakeResolver struct {
	names []string
	err   error
}

func (f fakeResolver) LookupAddr(context.Context, string) ([]string, error) {
	return f.names, f.err
}

func TestPTR(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		addr     string
		res      Resolver
		want     string
	}{
		{"present strips trailing dot", "203.0.113.9:54221", fakeResolver{names: []string{"host.example.de."}}, "host.example.de"},
		{"no record", "203.0.113.9:54221", fakeResolver{names: nil}, "(none)"},
		{"lookup error", "203.0.113.9:54221", fakeResolver{err: errors.New("nx")}, "(unavailable)"},
		{"bare ip no port", "203.0.113.9", fakeResolver{names: []string{"a.example."}}, "a.example"},
		{"empty addr", "", fakeResolver{names: []string{"x."}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ptr(ctx, c.res, c.addr); got != c.want {
				t.Errorf("ptr(%q) = %q, want %q", c.addr, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run TestPTR 2>&1 | head`
Expected: FAIL — `undefined: Resolver` / `undefined: ptr`.

- [ ] **Step 3: Implement the seam and helper**

Create `internal/server/resolver.go` (with the GPL header):

```go
package server

import (
	"context"
	"net"
	"strings"
	"time"
)

// Resolver is the slice of *net.Resolver the audit detail screen needs for the
// reverse-DNS (PTR) lookup. *net.Resolver satisfies it directly; tests inject a
// fake. The lookup is advisory only — PTR records are controlled by the owner
// of the IP's reverse zone and are not authenticated (no FCrDNS).
type Resolver interface {
	LookupAddr(ctx context.Context, addr string) (names []string, err error)
}

// ptrTimeout bounds the reverse-DNS lookup so a slow or hostile resolver cannot
// hang the admin's session.
const ptrTimeout = 2 * time.Second

// ptr resolves the first PTR name for the host portion of remoteAddr
// ("host:port" or bare host). Returns "" for an empty address, "(none)" when no
// record exists, and "(unavailable)" on timeout/error. The trailing dot of an
// FQDN is stripped.
func ptr(ctx context.Context, res Resolver, remoteAddr string) string {
	if strings.TrimSpace(remoteAddr) == "" {
		return ""
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ctx, cancel := context.WithTimeout(ctx, ptrTimeout)
	defer cancel()
	names, err := res.LookupAddr(ctx, host)
	if err != nil {
		return "(unavailable)"
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.TrimSuffix(names[0], ".")
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/server/ -run TestPTR -v`
Expected: PASS (all five sub-cases).

- [ ] **Step 5: Commit**

```bash
git add internal/server/resolver.go internal/server/resolver_test.go
git commit -m "feat(server): reverse-DNS resolver seam + ptr helper (GH #40)"
```

---

## Task 9: Audit event colour map + julian stamp + param readers

**Files:**
- Create: `internal/server/admin_audit.go` (helpers only in this task; the flow is added in Task 10)
- Test: `internal/server/admin_audit_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/server/admin_audit_test.go` (with the GPL header):

```go
package server

import (
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)

func TestAuditEventColor(t *testing.T) {
	red := []string{store.AuditAuthFail, store.AuditAuthError, store.AuditMFAFailed}
	yellow := []string{store.AuditAdmin, store.AuditMFACleared, store.AuditMFAEnforced, store.AuditMFAEnrolled}
	plain := []string{store.AuditConnect, store.AuditAuthOK, store.AuditMFASuccess, store.AuditBridgeStart, store.AuditBridgeEnd, store.AuditLogout, store.AuditDisconnect}

	for _, k := range red {
		if c := auditEventColor(k); c != go3270.Red {
			t.Errorf("color(%s) = %v, want Red", k, c)
		}
	}
	for _, k := range yellow {
		if c := auditEventColor(k); c != go3270.Yellow {
			t.Errorf("color(%s) = %v, want Yellow", k, c)
		}
	}
	for _, k := range plain {
		if c := auditEventColor(k); c != go3270.DefaultColor {
			t.Errorf("color(%s) = %v, want Default", k, c)
		}
	}
}

func TestJulianStamp(t *testing.T) {
	// 2026-06-06 is day-of-year 157 (2026 is not a leap year).
	ts := time.Date(2026, 6, 6, 14, 32, 0, 0, time.UTC)
	if got := julianStamp(ts); got != "2026-06-06 (2026.157)" {
		t.Errorf("julianStamp = %q, want 2026-06-06 (2026.157)", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run 'TestAuditEventColor|TestJulianStamp' 2>&1 | head`
Expected: FAIL — `undefined: auditEventColor` / `undefined: julianStamp`.

- [ ] **Step 3: Implement the helpers**

Create `internal/server/admin_audit.go` (with the GPL header). This task adds only the leaf helpers; Task 10 adds the flow to the same file.

```go
package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
	"github.com/racingmars/go3270"
)

// auditEventColor maps an audit kind to its display colour (derived severity;
// there is no stored severity). Only failures (red) and security-state changes
// (yellow) are coloured; routine events stay plain so the exceptions pop.
func auditEventColor(kind string) go3270.Color {
	switch kind {
	case store.AuditAuthFail, store.AuditAuthError, store.AuditMFAFailed:
		return go3270.Red
	case store.AuditAdmin, store.AuditMFACleared, store.AuditMFAEnforced, store.AuditMFAEnrolled:
		return go3270.Yellow
	default:
		return go3270.DefaultColor
	}
}

// julianStamp formats t as "YYYY-MM-DD (YYYY.DDD)" with the day-of-year, used on
// both the list as-of header and the detail Date/Time line.
func julianStamp(t time.Time) string {
	return fmt.Sprintf("%s (%d.%03d)", t.Format("2006-01-02"), t.Year(), t.YearDay())
}

// auditCap reads AUDIT_MAX_ROWS, clamped to [1,10000] with a fallback to the
// default for a hand-edited DB (mirrors Session.throttleInt).
func (f *adminFlow) auditCap(ctx context.Context) int {
	v, err := f.store.GetConfig(ctx, sysconfig.KeyAuditMaxRows)
	if err != nil {
		return sysconfig.DefaultAuditMaxRows
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > 10000 {
		return sysconfig.DefaultAuditMaxRows
	}
	return n
}

// auditReverseDNS reports whether the reverse-DNS lookup is enabled
// (AUDIT_REVERSE_DNS == "ON"; default on when unreadable).
func (f *adminFlow) auditReverseDNS(ctx context.Context) bool {
	v, err := f.store.GetConfig(ctx, sysconfig.KeyAuditReverseDNS)
	if err != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(v), "ON")
}
```

- [ ] **Step 4: Run to verify the helper tests pass**

Run: `go test ./internal/server/ -run 'TestAuditEventColor|TestJulianStamp' -v`
Expected: PASS. (The package will still build — `auditCap`/`auditReverseDNS` are referenced in Task 10 but compile fine now as unused methods.)

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin_audit.go internal/server/admin_audit_test.go
git commit -m "feat(server): audit colour map, julian stamp, param readers (GH #40)"
```

---

## Task 10: Audit flow + AdminStore.ListAudit + adminFlow fields + fake updates

**Files:**
- Modify: `internal/server/admin.go` (AdminStore interface; adminFlow fields)
- Modify: `internal/server/admin_audit.go` (the flow)
- Modify: `internal/server/admin_test.go` (fakeAdminPresenter Snapshot/Detail; fixture wiring)
- Test: `internal/server/admin_audit_test.go`

- [ ] **Step 1: Add `ListAudit` to the AdminStore interface**

In `internal/server/admin.go`, add to the `AdminStore` interface (e.g. after the `SetConfig` line):

```go
	ListAudit(ctx context.Context, f store.AuditFilter) ([]store.AuditEvent, error)
```

`*store.Store` already implements this, so the `var _ AdminStore = (*store.Store)(nil)` assertion stays valid.

- [ ] **Step 2: Add `resolver` and `now` fields to adminFlow**

In `internal/server/admin.go`, add to the `adminFlow` struct (after `logger`):

```go
	// resolver does reverse-DNS for the audit detail screen; nil → net.DefaultResolver.
	resolver Resolver
	// now returns the current time (72h window + as-of stamp); nil → time.Now.
	now func() time.Time
```

Add `"time"` to the `admin.go` import block if not already present.

- [ ] **Step 3: Add `Snapshot`/`Detail` to the test fake and script fields**

In `internal/server/admin_test.go`, add fields to `fakeAdminPresenter`:

```go
	snaps    []ui3270.ListAction
	gotSnaps []ui3270.SnapshotView
	gotDets  []ui3270.DetailView
```

And the methods (after `Form`):

```go
func (f *fakeAdminPresenter) Snapshot(v ui3270.SnapshotView) (ui3270.ListAction, error) {
	f.gotSnaps = append(f.gotSnaps, v)
	if len(f.snaps) == 0 {
		panic("unexpected Snapshot call")
	}
	a := f.snaps[0]
	f.snaps = f.snaps[1:]
	return a, nil
}

func (f *fakeAdminPresenter) Detail(v ui3270.DetailView) error {
	f.gotDets = append(f.gotDets, v)
	return nil
}
```

- [ ] **Step 4: Write the failing flow test**

Add to `internal/server/admin_audit_test.go`. The file (from Task 9) already imports `testing`, `time`, `store`, and `go3270`; add `"context"`, `"strings"`, and `"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"`. `newAdminFixture` opens a real store; record two audit rows, then drive the flow (conn is `nil` — the fixture's renderer ignores it).

```go
func TestAuditLogFlow(t *testing.T) {
	p := &fakeAdminPresenter{}
	f, _ := newAdminFixture(t, p)
	f.resolver = fakeResolver{names: []string{"host.example.de."}}
	f.now = func() time.Time { return time.Date(2026, 6, 6, 14, 32, 0, 0, time.UTC) }

	ctx := context.Background()
	// Two events inside the 72h window (stamped relative to f.now()).
	base := f.now().Add(-time.Hour)
	for _, ev := range []store.AuditEvent{
		{At: base, Kind: store.AuditAuthOK, Username: "ALICE", RemoteAddr: "203.0.113.9:5"},
		{At: base.Add(time.Minute), Kind: store.AuditAuthFail, Username: "BADGUY", RemoteAddr: "203.0.113.9:6", Detail: "delay=4s count=2"},
	} {
		if err := f.store.RecordAudit(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	// Drive: S on row 0 (newest = AUTH_FAIL) → detail, then PF3 to exit.
	p.snaps = []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}}
	if err := f.auditLog(ctx, nil); err != nil {
		t.Fatal(err)
	}

	// List view: as-of stamp present, newest-first (AUTH_FAIL on top, coloured red).
	lv := p.gotSnaps[0]
	if !strings.Contains(lv.AsOf, "2026-06-06 (2026.157)") {
		t.Errorf("as-of stamp = %q", lv.AsOf)
	}
	// Mid is padded to the 12-col EVENT budget, so compare trimmed.
	if strings.TrimSpace(lv.Rows[0].Mid) != "AUTH_FAIL" || lv.Rows[0].MidColor != go3270.Red {
		t.Errorf("top row = %+v, want AUTH_FAIL red", lv.Rows[0])
	}
	// Detail view: PTR resolved and shown.
	dv := p.gotDets[0]
	var sawPTR bool
	for _, fld := range dv.Fields {
		if fld.Label == "PTR" && fld.Value == "host.example.de" {
			sawPTR = true
		}
	}
	if !sawPTR {
		t.Errorf("detail PTR missing; fields=%+v", dv.Fields)
	}
}
```

Add `"strings"` and `"github.com/racingmars/go3270"` to this test file's imports (the helper test already imports `store`, `time`, `go3270`).

- [ ] **Step 5: Run to verify it fails**

Run: `go test ./internal/server/ -run TestAuditLogFlow 2>&1 | head`
Expected: FAIL — `f.auditLog undefined`.

- [ ] **Step 6: Implement the flow**

Append to `internal/server/admin_audit.go`. Add `"net"` and
`"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"` to its import block (the
Task 9 imports — `context`, `fmt`, `strconv`, `strings`, `time`, `store`,
`sysconfig`, `go3270` — all remain in use).

```go
// auditWindow is the bounded look-back of the RECENT view.
const auditWindow = 72 * time.Hour

// auditLog drives the read-only RECENT activity viewer (GH #40): a 72h snapshot,
// newest-first, paged, with an 'S' drill-down to a PTR-enriched detail screen.
func (f *adminFlow) auditLog(ctx context.Context, conn net.Conn) error {
	r := f.renderer(conn)
	maxRows := f.auditCap(ctx)
	cfg := ui3270.SnapshotConfig[store.AuditEvent]{
		Title:  "RECENT ACTIVITY",
		Head:   ui3270.SnapshotRow{Left: "MM/DD HH:MM USERNAME", Mid: "EVENT", Right: "DETAIL"},
		Legend: "S = detail",
		PFHelp: "PF3=Admin Menu   PF7=PgUp  PF8=PgDn   Enter=Refresh",
		Empty:  "(no activity in the last 72 hours)",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.SnapshotEntry[store.AuditEvent], string, string) {
			now := f.clock()
			evs, err := f.store.ListAudit(ctx, store.AuditFilter{
				Since: now.Add(-auditWindow),
				Limit: maxRows + 1, // +1 detects window overflow
			})
			if err != nil {
				return nil, "", f.storeErr("list audit", err)
			}
			asOf := "AS OF " + julianStamp(now) + "  " + now.Format("15:04") + " UTC"
			if len(evs) > maxRows {
				evs = evs[:maxRows]
				asOf += fmt.Sprintf("  (NEWEST %d)", maxRows)
			}
			rows := make([]ui3270.SnapshotEntry[store.AuditEvent], len(evs))
			for i, ev := range evs {
				rows[i] = ui3270.SnapshotEntry[store.AuditEvent]{Row: auditRow(ev), Item: ev}
			}
			return rows, asOf, ""
		},
		OnSelect: func(ctx context.Context, r ui3270.Renderer, ev store.AuditEvent) error {
			return r.Detail(f.auditDetail(ctx, ev))
		},
	}
	return ui3270.RunSnapshotList(ctx, r, cfg)
}

// clock returns the flow's current time (now seam; nil → time.Now), in UTC.
func (f *adminFlow) clock() time.Time {
	if f.now != nil {
		return f.now().UTC()
	}
	return time.Now().UTC()
}

// auditRow formats one event into the list's three coloured segments. The Mid
// (event) segment is the uppercased kind; Left/Right are truncated to budget.
func auditRow(ev store.AuditEvent) ui3270.SnapshotRow {
	at := ev.At.UTC()
	left := fmt.Sprintf("%s %s %-8.8s", at.Format("01/02"), at.Format("15:04"), ev.Username)
	return ui3270.SnapshotRow{
		Left:     left,
		Mid:      fmt.Sprintf("%-12.12s", strings.ToUpper(ev.Kind)),
		Right:    truncate(ev.Detail, 38),
		MidColor: auditEventColor(ev.Kind),
	}
}

// auditDetail builds the read-only detail view for one event, resolving the PTR
// when reverse DNS is enabled and the event carries a remote address.
func (f *adminFlow) auditDetail(ctx context.Context, ev store.AuditEvent) ui3270.DetailView {
	at := ev.At.UTC()
	fields := []ui3270.DetailField{
		{Label: "Date/Time", Value: julianStamp(at) + " " + at.Format("15:04:05") + " UTC"},
		{Label: "Session", Value: ev.SessionID},
		{Label: "Username", Value: ev.Username},
		{Label: "Event", Value: strings.ToUpper(ev.Kind), Color: auditEventColor(ev.Kind)},
	}
	if ev.RemoteAddr != "" {
		fields = append(fields, ui3270.DetailField{Label: "Remote", Value: ev.RemoteAddr})
		if f.auditReverseDNS(ctx) {
			res := f.resolver
			if res == nil {
				res = net.DefaultResolver
			}
			if name := ptr(ctx, res, ev.RemoteAddr); name != "" {
				fields = append(fields, ui3270.DetailField{Label: "PTR", Value: name})
			}
		}
	}
	fields = append(fields, ui3270.DetailField{Label: "Service", Value: ev.Service})
	return ui3270.DetailView{
		Title:     "AUDIT DETAIL",
		Fields:    fields,
		BodyLabel: "Detail",
		Body:      ev.Detail,
		PFHelp:    "PF3=Back",
	}
}

// truncate limits s to n runes (the list summary; full text is on the detail).
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
```

- [ ] **Step 7: Run to verify it passes**

Run: `go test ./internal/server/ -run 'TestAuditLogFlow|TestAuditEventColor|TestJulianStamp' -race -v`
Expected: PASS.

- [ ] **Step 8: Run the whole server package (fakes now satisfy the widened Renderer)**

Run: `go test ./internal/server/ -race`
Expected: PASS — `fakeAdminPresenter` implements all four `Renderer` methods, so every admin test still compiles and passes.

- [ ] **Step 9: Commit**

```bash
git add internal/server/admin.go internal/server/admin_audit.go internal/server/admin_test.go internal/server/admin_audit_test.go
git commit -m "feat(server): audit log flow with PTR-enriched detail (GH #40)"
```

---

## Task 11: Wire the admin menu entry

**Files:**
- Modify: `internal/screens/admin.go`
- Modify: `internal/server/presenter_admin.go`
- Modify: `internal/server/admin.go` (dispatch `case 6`)
- Modify: `internal/server/session.go` (thread `resolver`/`now`)
- Test: `internal/screens/admin_test.go`, `internal/server/admin_test.go`

- [ ] **Step 1: Write the failing screen test**

Add to `internal/screens/admin_test.go` (follow the existing test style in that file for locating field content):

```go
func TestAdminMenuScreen_HasAuditEntry(t *testing.T) {
	screen, _ := AdminMenuScreen(DefaultGeometry, "")
	var found bool
	for _, f := range screen {
		if f.Content == "6.  Audit Log" {
			found = true
		}
	}
	if !found {
		t.Errorf("AdminMenuScreen missing '6.  Audit Log' entry")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/screens/ -run TestAdminMenuScreen_HasAuditEntry -v`
Expected: FAIL — entry not present.

- [ ] **Step 3: Add the menu line**

In `internal/screens/admin.go`, add the entry after the Trusted Networks line:

```go
		{Row: 8, Col: 4, Content: "6.  Audit Log"},
```

(The numbered entries currently occupy rows 3–7; Audit Log goes on row 8. The input/error/help rows are geometry-relative and unaffected.)

- [ ] **Step 4: Run to verify the screen test passes**

Run: `go test ./internal/screens/ -run TestAdminMenuScreen_HasAuditEntry -v`
Expected: PASS.

- [ ] **Step 5: Accept "6" in the presenter**

In `internal/server/presenter_admin.go`, add a case to the `switch` in `AdminMenu` (after `case "5"`):

```go
		case "6":
			return 6, false, nil
```

Update the `AdminMenu` interface doc comment and the function's `// AdminMenu returns choice 1-5 …` comment to say `1-6 (… / trusted networks / audit log)`.

- [ ] **Step 6: Dispatch `case 6` in the flow**

In `internal/server/admin.go`, add to the `switch choice` in `adminFlow.Run` (after `case 5`):

```go
		case 6:
			err = f.auditLog(ctx, conn)
```

- [ ] **Step 7: Thread `resolver`/`now` into the adminFlow literal**

In `internal/server/session.go`, in the `flow := &adminFlow{…}` literal (around line 392), add:

```go
				flow := &adminFlow{store: s.Store, presenter: s.AdminPresenter,
					renderer: renderer,
					identity: identity, term: term, audit: aud.record,
					logger: s.log(), now: s.now}
```

(`s.now` is the existing `Session.now` method value, satisfying `func() time.Time`. Leave `resolver` unset — nil selects `net.DefaultResolver` at lookup time, per Task 10.)

- [ ] **Step 8: Add a presenter parse test**

Add to `internal/server/admin_test.go` a test that menu choice 6 dispatches to the audit flow (drive via the fixture):

```go
func TestAdminRun_Choice6DispatchesAudit(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 6}, {back: true}},
		snaps: []ui3270.ListAction{{PF: 3}}, // audit list opens then PF3 back
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(p.gotSnaps) != 1 {
		t.Errorf("expected one Snapshot render from the audit flow, got %d", len(p.gotSnaps))
	}
}
```

(`newAdminFixture` already wires `presenter: p` and `renderer: func(net.Conn) ui3270.Renderer { return p }`, and existing admin tests pass `nil` as the conn. The fixture does not set `now`, so the flow falls back to `time.Now` — fine here since no rows are asserted.)

- [ ] **Step 9: Run the affected packages**

Run: `go test ./internal/screens/ ./internal/server/ -race`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/screens/admin.go internal/server/presenter_admin.go internal/server/admin.go internal/server/session.go internal/screens/admin_test.go internal/server/admin_test.go
git commit -m "feat: wire Audit Log onto the admin menu (option 6) (GH #40)"
```

---

## Task 12: Full build, race suite, and protocol smoke test

**Files:** none (verification only)

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 2: Full race test suite**

Run: `go test ./... -race`
Expected: PASS across all packages.

- [ ] **Step 3: Vet**

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 4: Protocol smoke test (s3270)**

Use the **s3270-smoke-testing skill** to verify the protocol surface that unit tests cannot:
- Admin menu shows `6.  Audit Log`; selecting it opens `RECENT ACTIVITY`.
- Column alignment of `MM/DD HH:MM USERNAME EVENT DETAIL` against the heading row (the 8/12/38 budget) on an 80-column screen.
- The event column renders in colour (an `auth_fail` row is red, an `admin` row yellow, a routine row plain).
- Cursor lands on the first command field; an empty 72h window homes the cursor to `{0,0}`.
- `S` opens the detail screen (with `Date/Time` showing the Julian day, and a `PTR` line when reverse DNS is on and resolvable); `PF3` returns to the list; `PF7`/`PF8` page; plain `Enter` re-stamps the `AS OF` header.

Seed some audit rows first (run the proxy, connect/login/fail a login) so the window is non-empty.

- [ ] **Step 5: Final commit (if the smoke test prompted any fixes)**

```bash
git add -A
git commit -m "test: s3270 smoke pass for audit log viewer (GH #40)"
```

---

## Self-Review Notes

- **Spec coverage:** entry/menu (T11), snapshot list + 72h window + newest-first + overflow marker (T7, T10), staleness stamp with Julian day (T9, T10), event colour map (T9), detail screen with Remote/PTR/Service + wrapped untruncated detail (T5, T10), PTR seam with timeout + states + toggle (T8, T10), two system parameters with validators (T1, T2), UTC everywhere (T10 formats `.UTC()`), empty-window home cursor (T4, T7), zero store changes (T10 uses existing `ListAudit`). All covered.
- **Layering:** `ui3270` never imports `store`; the event colour is computed in `server` (`auditEventColor`) and passed as a `go3270.Color`. PTR + fetch live in `server`. No SQL outside `store`.
- **Type consistency:** `SnapshotRow{Left,Mid,Right,MidColor}`, `SnapshotEntry[T]{Row,Item}`, `SnapshotConfig[T]{...,Fetch,OnSelect}`, `DetailView{Title,Fields,BodyLabel,Body,PFHelp}`, `DetailField{Label,Value,Color}` are used identically in every task that references them. `Renderer` gains `Snapshot`/`Detail` (T3) and every implementer (real T6, `fakeRenderer` T3, `fakeAdminPresenter` T10) provides them.
