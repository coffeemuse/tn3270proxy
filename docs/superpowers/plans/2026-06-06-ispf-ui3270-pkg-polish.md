# ISPF Screen Polish — Plan 2: `internal/ui3270` package

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the four generic `internal/ui3270` screen builders (list, form, snapshot, detail) onto the three-band ISPF layout and CUA palette from `docs/ispf-style-guide.md`, matching the `internal/screens` work in Plan 1.

**Architecture:** `ui3270` has its own geometry (free funcs in `layout.go`, taking `rows int`; content is fixed within cols 0–79). Add `messageRow`/`bodyTopRow`/`centerCol` helpers, then migrate each builder: centered white title (row 0), red message line at row 2 (was bottom `errorRow`), blue column headings, turquoise labels, green inputs/values, PF help turquoise. **All four builders are live** (list/form drive admin CRUD + System Parameters; snapshot/detail drive the live Audit Log viewer via `internal/server/admin_audit.go`).

**Tech Stack:** Go, `github.com/racingmars/go3270`. Tests assert field positions/colors; the s3270 smoke script is the protocol gate.

**Scope guardrails:** Presentation only. **Data positions are preserved** — list cmd field stays at row 4 (cursor `4,3`), form first input stays at row 3 (cursor `3,28`/`3,17`/`5,17`) — so `screen_test.go` cursor tests and the s3270 smoke (checks 9c/11c/14d) keep passing untouched. `listPageSize`/`legendRow`/`formMaxFields` are deliberately **unchanged** (the existing layout already gaps above the legend; reclaiming that space is a non-goal here).

**Deferred (documented deviation):** Lists do **not** gain the `Command ===>` line in this pass. The style guide calls for it, but `RunList` has no primary-command processing — wiring one is *functional* work, out of scope for this polish pass. Row 1 stays blank on lists; line commands (S/D) + PF keys remain the interaction model. Recorded in the style guide deviation log (Task 6).

---

## File structure

| File | Responsibility | Change |
|---|---|---|
| `internal/ui3270/layout.go` | Geometry free funcs | Add `messageRow`/`bodyTopRow`/`centerCol`; remove dead `errorRow` (Task 5) |
| `internal/ui3270/layout_test.go` | Geometry tests | Add band-helper test |
| `internal/ui3270/screen.go` | `buildListScreen`, `buildFormScreen` | Re-lay onto bands + palette |
| `internal/ui3270/screen_test.go` | List/form builder tests | Add palette assertions (cursor tests untouched) |
| `internal/ui3270/snapshotscreen.go` | `buildSnapshotScreen`, `buildDetailScreen`, `snapSegments` | Re-lay onto bands + palette |
| `internal/ui3270/snapshotscreen_test.go` | Snapshot/detail tests | Fix 3 position asserts + add palette asserts |
| `CLAUDE.md` | Project orientation | Point "Screen layout convention" at the style guide |
| `docs/ispf-style-guide.md` | Style guide | Add the list-command-line deferral to the deviation log |

---

## Task 1: Band helpers in layout.go (additive)

**Files:**
- Modify: `internal/ui3270/layout.go`
- Test: `internal/ui3270/layout_test.go`

Additive — keeps `errorRow` so the build stays green until each builder migrates.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui3270/layout_test.go`:

```go
func TestBandHelpers(t *testing.T) {
	if messageRow() != 2 {
		t.Errorf("messageRow() = %d, want 2", messageRow())
	}
	if bodyTopRow() != 3 {
		t.Errorf("bodyTopRow() = %d, want 3", bodyTopRow())
	}
	// "RECENT ACTIVITY" is 15 runes → (80-15)/2 = 32.
	if got := centerCol(15); got != 32 {
		t.Errorf("centerCol(15) = %d, want 32", got)
	}
	if got := centerCol(200); got != 0 {
		t.Errorf("centerCol(200) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui3270/ -run TestBandHelpers -v`
Expected: FAIL — `messageRow undefined`.

- [ ] **Step 3: Add the helpers**

In `internal/ui3270/layout.go`, after the existing `helpRow`/`errorRow`/`legendRow` block, add:

```go
// Top-band layout rows (ISPF style guide §2): title at row 0, an optional command
// line at row 1, the message line at row 2, body from row 3. Fixed on every model
// (content stays within cols 0–79; ui3270 adapts rows only).
func messageRow() int { return 2 }
func bodyTopRow() int { return 3 }

// centerCol returns the start column to center an n-rune title within the 0–79
// content band, clamped to ≥0.
func centerCol(n int) int {
	c := (80 - n) / 2
	if c < 0 {
		return 0
	}
	return c
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui3270/ -run TestBandHelpers -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/layout.go internal/ui3270/layout_test.go
git commit -m "feat(ui3270): add top-band geometry helpers (GH #65)"
```

---

## Task 2: buildFormScreen onto bands + palette

**Files:**
- Modify: `internal/ui3270/screen.go`
- Test: `internal/ui3270/screen_test.go`

Center+whiten the title, message→row 2, turquoise labels, green inputs, standardized PF help. Field rows (3,5,…) and dynamic input columns are **unchanged**, so all existing form cursor/column tests keep passing.

- [ ] **Step 1: Write the failing palette test**

Add to `internal/ui3270/screen_test.go`:

```go
func TestBuildFormScreenPalette(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{
		Title:  "EDIT USER",
		Fields: []FormField{{Name: "x", Label: "Name", Length: 8}},
		ErrMsg: "boom",
	})
	title, ok := fieldAt(screen, 0, centerCol(len("EDIT USER")))
	if !ok || title.Content != "EDIT USER" || title.Color != go3270.White || !title.Intense {
		t.Errorf("title = %+v ok=%v, want centered white intense", title, ok)
	}
	label, ok := fieldAt(screen, 3, labelAttrCol)
	if !ok || label.Color != go3270.Turquoise {
		t.Errorf("label = %+v ok=%v, want turquoise", label, ok)
	}
	in, _ := fieldByName(screen, "x")
	if in.Color != go3270.Green {
		t.Errorf("input color = %v, want green", in.Color)
	}
	msg, ok := fieldByName(screen, fieldError)
	if !ok || msg.Row != 2 || msg.Color != go3270.Red || !msg.Intense {
		t.Errorf("message = %+v ok=%v, want row 2 red intense", msg, ok)
	}
}
```

(`fieldAt` already exists in `snapshotscreen_test.go`; `fieldByName` and `fieldError` already exist in the package.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui3270/ -run TestBuildFormScreenPalette -v`
Expected: FAIL — title at col 2 default color, label no color, message on bottom errorRow.

- [ ] **Step 3: Apply the edits to `buildFormScreen`**

In `internal/ui3270/screen.go`, inside `buildFormScreen`:

1. Title — change `{Row: 0, Col: 2, Intense: true, Content: v.Title},` to:
```go
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
```
2. ReadOnly branch — add color to both fields:
```go
			screen = append(screen,
				go3270.Field{Row: row, Col: labelAttrCol, Color: go3270.Turquoise, Content: label},
				go3270.Field{Row: row, Col: inputCol, Color: go3270.Green, Content: f.Value},
			)
```
3. Editable input — add `Color: go3270.Green` to the `input :=` field:
```go
		input := go3270.Field{Row: row, Col: inputCol, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Color: go3270.Green, Highlighting: go3270.Underscore}
```
4. Normal-branch label — add color to the label field in the final `append`:
```go
		screen = append(screen,
			go3270.Field{Row: row, Col: labelAttrCol, Color: go3270.Turquoise, Content: label},
			input,
			go3270.Field{Row: row, Col: stopCol}, // stop field
		)
```
5. Footer — change the error + help fields to:
```go
	screen = append(screen,
		go3270.Field{Row: messageRow(), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 2, Color: go3270.Turquoise, Content: "Enter=Save    PF3=Cancel"},
	)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui3270/ -run 'TestBuildForm' -v`
Expected: PASS — the new palette test plus all existing form tests (they assert cursor/columns/label-content, none of which moved).

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/screen.go internal/ui3270/screen_test.go
git commit -m "feat(ui3270): form view onto ISPF bands + palette (GH #65)"
```

---

## Task 3: buildListScreen onto bands + palette

**Files:**
- Modify: `internal/ui3270/screen.go`
- Test: `internal/ui3270/screen_test.go`

Center+whiten the title, blue column-heading row at row 3, message→row 2, green data + cmd fields, turquoise legend/PF help. Data rows stay at row 4 (cursor `4,3` preserved).

- [ ] **Step 1: Write the failing palette test**

Add to `internal/ui3270/screen_test.go`:

```go
func TestBuildListScreenPalette(t *testing.T) {
	screen, cur := buildListScreen(24, ListView{
		Title: "USER ADMINISTRATION", RowInfo: "ROW 1 TO 2 OF 2",
		Header: "Userid    Name", Rows: []string{"ALICE  Alice"},
		Legend: "S=Select", ErrMsg: "boom", PFHelp: "PF3=Back",
	})
	title, ok := fieldAt(screen, 0, centerCol(len("USER ADMINISTRATION")))
	if !ok || title.Content != "USER ADMINISTRATION" || title.Color != go3270.White || !title.Intense {
		t.Errorf("title = %+v ok=%v, want centered white intense", title, ok)
	}
	hdr, ok := fieldAt(screen, bodyTopRow(), 2)
	if !ok || hdr.Color != go3270.Blue || hdr.Content != "Userid    Name" {
		t.Errorf("header = %+v ok=%v, want row 3 blue", hdr, ok)
	}
	msg, ok := fieldByName(screen, fieldError)
	if !ok || msg.Row != 2 {
		t.Errorf("message row = %d ok=%v, want 2", msg.Row, ok)
	}
	if cur != (Cursor{Row: 4, Col: 3}) {
		t.Errorf("cursor = %+v, want {4,3} (unchanged)", cur)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui3270/ -run TestBuildListScreenPalette -v`
Expected: FAIL — title at col 2 default, header at row 2 default, message on bottom.

- [ ] **Step 3: Apply the edits to `buildListScreen`**

In `internal/ui3270/screen.go`, inside `buildListScreen`:

1. Replace the opening `screen := go3270.Screen{...}` (title, RowInfo, Header) with:
```go
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: bodyTopRow(), Col: 2, Color: go3270.Blue, Content: v.Header},
	}
```
2. The per-row `cmd` field — add `Color: go3270.Green`:
```go
		cmd := go3270.Field{Row: row, Col: 2, Name: fmt.Sprintf("%s%d", fieldCmdPrefix, i), Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
```
3. The data content field — add `Color: go3270.Green`:
```go
		screen = append(screen,
			cmd,
			go3270.Field{Row: row, Col: 4}, // stop field: 1-char command input
			go3270.Field{Row: row, Col: 7, Color: go3270.Green, Content: r},
		)
```
4. Footer — change the legend/error/help block to:
```go
	screen = append(screen,
		go3270.Field{Row: legendRow(rows), Col: 2, Color: go3270.Turquoise, Content: v.Legend},
		go3270.Field{Row: messageRow(), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 2, Color: go3270.Turquoise, Content: v.PFHelp},
	)
```

(Leave the data loop's `row := 4 + i`, the empty-`(none)` field, and the cursor logic unchanged.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui3270/ -run 'TestBuildList' -v`
Expected: PASS — new palette test + existing cursor tests (4,3 and empty-home unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/screen.go internal/ui3270/screen_test.go
git commit -m "feat(ui3270): list view onto ISPF bands + blue headings (GH #65)"
```

---

## Task 4: buildSnapshotScreen + buildDetailScreen onto bands + palette

**Files:**
- Modify: `internal/ui3270/snapshotscreen.go`
- Test: `internal/ui3270/snapshotscreen_test.go`

Snapshot (live Audit Log list) and detail (live record view). Center+whiten titles; move the snapshot AsOf stamp to row 1 (row 2 is now the message line); blue heading row; green data segments; turquoise labels; detail body bound moves off `errorRow`.

- [ ] **Step 1: Fix the broken position asserts + add palette asserts**

In `internal/ui3270/snapshotscreen_test.go`:

1. In `TestBuildSnapshotScreen_RowLayoutAndColor`, change the title assert (was `fieldAt(screen, 0, 2)`):
```go
	if f, ok := fieldAt(screen, 0, centerCol(len("RECENT ACTIVITY"))); !ok || f.Content != "RECENT ACTIVITY" || f.Color != go3270.White || !f.Intense {
		t.Errorf("title missing/not centered-white: %+v ok=%v", f, ok)
	}
```
2. In the same test, change the AsOf assert (was `fieldAt(screen, 2, snapLeftAttr)`) to row 1, and assert the heading is blue:
```go
	if f, ok := fieldAt(screen, 1, snapLeftAttr); !ok || f.Content != v.AsOf {
		t.Errorf("as-of stamp missing on row 1: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 3, snapLeftAttr); !ok || f.Color != go3270.Blue {
		t.Errorf("heading-left not blue on row 3: %+v ok=%v", f, ok)
	}
```
3. In `TestBuildDetailScreen`, change the title assert (was `fieldAt(screen, 0, 2)`):
```go
	if f, ok := fieldAt(screen, 0, centerCol(len("AUDIT DETAIL"))); !ok || f.Content != "AUDIT DETAIL" || f.Color != go3270.White || !f.Intense {
		t.Errorf("title not centered-white: %+v ok=%v", f, ok)
	}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui3270/ -run 'TestBuildSnapshot|TestBuildDetail' -v`
Expected: FAIL — current builders put titles at col 2, AsOf at row 2, headings uncolored.

- [ ] **Step 3: Update `snapSegments` to green data values**

In `internal/ui3270/snapshotscreen.go`, change `snapSegments` so data Left/Right are green (Mid keeps its per-row color):
```go
func snapSegments(row int, r SnapshotRow) go3270.Screen {
	return go3270.Screen{
		{Row: row, Col: snapLeftAttr, Color: go3270.Green, Content: r.Left},
		{Row: row, Col: snapMidAttr, Content: r.Mid, Color: r.MidColor},
		{Row: row, Col: snapRightAttr, Color: go3270.Green, Content: r.Right},
	}
}
```

- [ ] **Step 4: Re-lay `buildSnapshotScreen`**

In `buildSnapshotScreen`, change the opening block (title, AsOf, Head) — note the Head row is now rendered as explicit blue fields instead of via `snapSegments`:
```go
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: 1, Col: snapLeftAttr, Color: go3270.Turquoise, Content: v.AsOf},
		{Row: bodyTopRow(), Col: snapLeftAttr, Color: go3270.Blue, Content: v.Head.Left},
		{Row: bodyTopRow(), Col: snapMidAttr, Color: go3270.Blue, Content: v.Head.Mid},
		{Row: bodyTopRow(), Col: snapRightAttr, Color: go3270.Blue, Content: v.Head.Right},
	}
```
(Delete the old `screen = append(screen, snapSegments(3, v.Head)...)` line — the Head is now in the literal above.)

And change the footer legend/error/help block to:
```go
	screen = append(screen,
		go3270.Field{Row: legendRow(rows), Col: 2, Color: go3270.Turquoise, Content: v.Legend},
		go3270.Field{Row: messageRow(), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 2, Color: go3270.Turquoise, Content: v.PFHelp},
	)
```
(Leave the data loop at `row := 4 + i`, the cmd field — add `Color: go3270.Green` to it like the list — and the empty-marker / cursor logic otherwise unchanged.)

The snapshot cmd field becomes:
```go
		cmd := go3270.Field{Row: row, Col: snapCmdAttr, Name: fmt.Sprintf("%s%d", fieldCmdPrefix, i), Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
```

- [ ] **Step 5: Re-lay `buildDetailScreen`**

In `buildDetailScreen`:
1. Title:
```go
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
	}
```
2. Field labels — add turquoise (the label is the first field in the loop append):
```go
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Color: go3270.Turquoise, Content: f.Label},
			go3270.Field{Row: row, Col: detailValueCol, Content: f.Value, Color: f.Color},
		)
```
3. Body label — turquoise:
```go
	if v.BodyLabel != "" {
		screen = append(screen, go3270.Field{Row: row, Col: 2, Color: go3270.Turquoise, Content: v.BodyLabel})
		row++
	}
```
4. Body wrap bound — change `if row >= errorRow(rows)` to `if row >= helpRow(rows)` (the body may now use the rows the bottom error line used to occupy):
```go
	for _, line := range wrapText(v.Body, detailBodyWidth) {
		if row >= helpRow(rows) { // never overrun the PF-key row
			break
		}
		screen = append(screen, go3270.Field{Row: row, Col: 2, Content: line})
		row++
	}
```
5. PF help — turquoise:
```go
	screen = append(screen, go3270.Field{Row: helpRow(rows), Col: 2, Color: go3270.Turquoise, Content: v.PFHelp})
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui3270/ -run 'TestBuildSnapshot|TestBuildDetail|TestWrapText' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui3270/snapshotscreen.go internal/ui3270/snapshotscreen_test.go
git commit -m "feat(ui3270): snapshot + detail onto ISPF bands + palette (GH #65)"
```

---

## Task 5: Remove the dead `errorRow` helper

**Files:**
- Modify: `internal/ui3270/layout.go`

`errorRow` is now unused (all four builders use `messageRow()`; detail uses `helpRow`).

- [ ] **Step 1: Confirm unused**

Run: `grep -rn "errorRow" internal/ui3270/`
Expected: only the definition in `layout.go`. If any builder still references it, that builder's task is incomplete.

- [ ] **Step 2: Delete the definition**

In `internal/ui3270/layout.go`, delete the `errorRow` func and its `// 21` comment line.

- [ ] **Step 3: Build + test**

Run: `go build ./... && go test ./internal/ui3270/ -v`
Expected: clean, all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/ui3270/layout.go
git commit -m "refactor(ui3270): drop dead errorRow helper (GH #65)"
```

---

## Task 6: Docs — CLAUDE.md + style-guide deviation

**Files:**
- Modify: `CLAUDE.md`, `docs/ispf-style-guide.md`

- [ ] **Step 1: Point CLAUDE.md at the style guide**

In `CLAUDE.md`, in the "Screen layout convention" bullet under **Gotchas**, append a sentence (keep the existing text; add at the end of that bullet):
```
  The full palette + three-band layout (top title/command/message band, bottom
  PF-key row) is specified in `docs/ispf-style-guide.md` — new screens follow it.
```

- [ ] **Step 2: Record the list-command-line deferral**

In `docs/ispf-style-guide.md`, in the **Deviation Log** (§6), add:
```
7. **List panels have no command line yet.** The guide calls for `Command ===>` on
   list panels, but the `RunList` driver has no primary-command processing — adding
   a functional command line is deferred to a follow-up. Lists use line commands
   (S/D) + PF keys; row 1 stays blank. (The `Option`/`Command` split still governs
   the label once it lands.)
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md docs/ispf-style-guide.md
git commit -m "docs: reference ISPF style guide from CLAUDE.md; log list cmd-line deferral (GH #65)"
```

---

## Task 7: Full verification (unit + s3270 smoke)

**Files:** none (verification only).

- [ ] **Step 1: Whole-suite race tests + build**

Run: `go build ./... && go test ./... -race`
Expected: all PASS.

- [ ] **Step 2: Run the s3270 smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: **all checks PASS, unchanged from Plan 1's 52/52.** The list/form data positions and cursors (`4,3` / `3,28` / `5,17`) are preserved, so checks 9c/11c/14d still pass; titles only moved column (centered) and messages moved to the top band — neither is asserted by position in the smoke checks (they assert content presence). If a check fails, read the evidence and reconcile: a *content* failure means a title/label text changed (it should not have); a *cursor* failure means a data row moved (it should not have) — investigate the builder rather than blindly editing the assert.

> The Audit Log viewer (snapshot/detail) is not currently walked by the smoke script; its unit tests are the gate, plus the human c3270 QA pass.

- [ ] **Step 3: No commit** (verification only). If any smoke assert needed updating, commit that separately with a `test(smoke):` message explaining what moved.

---

## Done criteria for Plan 2

- All four `ui3270` builders render on the three-band layout with the CUA palette (white titles, blue headings, turquoise labels, green inputs/values, red messages at row 2, turquoise PF help).
- `go build ./... && go test ./... -race` clean; s3270 smoke green.
- CLAUDE.md references the style guide; the list-command-line deferral is logged.
- Issue #65's existing-screen audit is then complete across both packages; the planned-screen conformance (#63 User Settings done in Plan 1; #40 Audit Log now on the bands) is satisfied. Remaining issue housekeeping: tick the #65 checklist + note the human color QA.
