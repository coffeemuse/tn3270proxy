# System Parameters Form Label/Input Overlap Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `internal/ui3270` form renderer place each form's input column past its longest label so label text can never corrupt an input value, fixing the System Parameters save-blocker (GH #71).

**Architecture:** `buildFormScreen` currently hardcodes the label attribute at col 2 and the input attribute at col 16, leaving only 13 chars for labels. The System Parameters form's throttle labels (up to 23 chars) bleed past col 16 into the input field's buffer, corrupting its value and — via the all-or-nothing `Submit` — blocking every save. The fix computes the input column from the longest label in the form (`max(16, 3+longestLabel+1)`), floored at 16 so every existing (≤12-char-label) form renders byte-identically, with a defensive label-truncation guard so overrun becomes structurally impossible.

**Tech Stack:** Go, `github.com/racingmars/go3270`. Tests are `go test`. Protocol verification via the `s3270-smoke-testing` skill (`.claude/skills/s3270-smoke-testing/smoke.sh`).

---

## Background for the implementer (read once)

- **go3270 field geometry:** a `go3270.Field`'s `Col` is its **attribute byte**; rendered content begins at `Col + 1`. A field "owns" the buffer from its attribute byte until the next field's attribute byte. Fields are emitted in slice order, so a later field's attribute byte overwrites an earlier field's content at the same column.
- **The bug, concretely:** for `"Auth Delay Base (sec):"` (22 chars) with value `"2"`, the label (attribute col 2, content from col 3) writes through col 24, the input attribute at col 16 and value `"2"` at col 17 overwrite the middle, and the input field (col 17 → stop field) reads back as `"2 (sec):"`. `nonNegativeInt` rejects it; the all-or-nothing `Submit` sinks every field's save.
- **Why the floor at 16 matters:** every other admin form pads labels to a ≤12-char dot-leader (`"Group name ."`, `"Userid . . ."`), so `formInputCol(≤12) == 16` keeps them pixel-identical. Only the System Parameters form (max label 23) widens to col 27.
- **Files involved:**
  - Modify: `internal/ui3270/layout.go` — add column-math helpers + constants.
  - Modify: `internal/ui3270/screen.go:71-110` — `buildFormScreen` uses the helpers.
  - Test: `internal/ui3270/layout_test.go` (exists) and `internal/ui3270/screen_test.go` (exists).
- **No other files change.** `FormField`/`FormView`/`FormConfig` (`types.go`), `admin_system.go`, `catalog.go`, and the list renderer (`buildListScreen`) are untouched. Server-package tests use a scripted fake `Renderer` and never reach `buildFormScreen`, so they are unaffected.
- **Run tests for this package with:** `go test ./internal/ui3270/ -run <Name> -v`.

---

## Task 1: Column-math helpers in layout.go

Add three pure helpers and their constants: `formInputCol` (input attribute column from the longest label), `formLabelMax` (max label content length that fits before that column), and `truncRunes` (rune-safe truncation). These are pure functions — easy to unit-test in isolation before wiring them into the renderer.

**Files:**
- Modify: `internal/ui3270/layout.go`
- Test: `internal/ui3270/layout_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui3270/layout_test.go` (keep the existing `package ui3270` and imports; add a `testing` import only if the file lacks one — it already has `import "testing"`):

```go
func TestFormInputCol(t *testing.T) {
	// max(16, 2+1+maxLabel+1), clamped to the ceiling (79-16 = 63).
	cases := []struct{ maxLabel, want int }{
		{0, 16},   // empty label → floor
		{11, 16},  // short → floor
		{12, 16},  // dot-leader convention → exactly the historical col 16
		{13, 17},  // first width that pushes right
		{15, 19},  // "Max Auth Tries:"
		{22, 26},  // "Auth Delay Base (sec):"
		{23, 27},  // "Auth Fail Window (min):" — the worst real label
		{80, 63},  // pathological → clamped to ceiling
	}
	for _, c := range cases {
		if got := formInputCol(c.maxLabel); got != c.want {
			t.Errorf("formInputCol(%d) = %d, want %d", c.maxLabel, got, c.want)
		}
	}
}

func TestFormLabelMax(t *testing.T) {
	// inputCol - labelGutter - labelAttrCol - 1.
	cases := []struct{ inputCol, want int }{
		{16, 12}, // historical 12-char dot-leader cap falls out naturally
		{27, 23}, // worst real label fits exactly
		{63, 59}, // at the ceiling
	}
	for _, c := range cases {
		if got := formLabelMax(c.inputCol); got != c.want {
			t.Errorf("formLabelMax(%d) = %d, want %d", c.inputCol, got, c.want)
		}
	}
}

func TestTruncRunes(t *testing.T) {
	if got := truncRunes("abcdef", 3); got != "abc" {
		t.Errorf("truncRunes(\"abcdef\",3) = %q, want \"abc\"", got)
	}
	if got := truncRunes("abc", 10); got != "abc" {
		t.Errorf("truncRunes(\"abc\",10) = %q, want \"abc\"", got)
	}
	if got := truncRunes("abc", -1); got != "" {
		t.Errorf("truncRunes(\"abc\",-1) = %q, want \"\"", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui3270/ -run 'TestFormInputCol|TestFormLabelMax|TestTruncRunes' -v`
Expected: FAIL — compile error `undefined: formInputCol` / `formLabelMax` / `truncRunes`.

- [ ] **Step 3: Implement the helpers**

Append to `internal/ui3270/layout.go` (the file already has `package ui3270` and `import "fmt"`; no new import needed):

```go
// Form label/input column geometry (GH #71). The label attribute byte sits at
// labelAttrCol (content one column right); the input attribute byte is computed
// from the longest label so label text can never overrun it.
const (
	labelAttrCol  = 2  // label field attribute byte; content begins at col 3
	labelGutter   = 1  // ≥1 blank column between label end and input attribute
	minInputCol   = 16 // floor: preserves the historical layout for ≤12-char labels
	minInputWidth = 16 // input data columns kept available at the ceiling
)

// formInputCol returns the input field's attribute-byte column for a form whose
// longest label is maxLabel runes. Floored at minInputCol so short-label forms
// (every form whose labels are ≤12 chars) compute the historical col 16 and
// render byte-identically; clamped to a ceiling that preserves minInputWidth
// input columns even for a pathological label.
func formInputCol(maxLabel int) int {
	col := labelAttrCol + 1 + maxLabel + labelGutter
	if col < minInputCol {
		col = minInputCol
	}
	if ceil := 79 - minInputWidth; col > ceil {
		col = ceil
	}
	return col
}

// formLabelMax returns the longest label content (in runes) that fits before the
// input attribute byte at inputCol. In the normal case this equals the form's
// longest label (a no-op); it only bites when inputCol hit the ceiling.
func formLabelMax(inputCol int) int {
	return inputCol - labelGutter - labelAttrCol - 1
}

// truncRunes returns s limited to at most n runes (n<0 ⇒ empty).
func truncRunes(s string, n int) string {
	if n < 0 {
		n = 0
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui3270/ -run 'TestFormInputCol|TestFormLabelMax|TestTruncRunes' -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/layout.go internal/ui3270/layout_test.go
git commit -m "feat(ui3270): form column-math helpers for dynamic input column — GH #71"
```

---

## Task 2: Rewire buildFormScreen to the dynamic input column

Replace the hardcoded `16`/`17`/`17+Length` in `buildFormScreen` with the computed column, apply the truncation guard to every label, and rebase the input content, stop field, cursor, and read-only value on it.

**Files:**
- Modify: `internal/ui3270/screen.go:71-110`
- Test: `internal/ui3270/screen_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui3270/screen_test.go`. It currently has only `import "testing"` — change that import block to add `strings` and the go3270 package (the new `fieldByName` helper references `go3270.Screen`/`go3270.Field`):

```go
import (
	"strings"
	"testing"

	"github.com/racingmars/go3270"
)
```

Then add a small lookup helper and the new tests:

```go
// fieldByName returns the first field with the given Name (writable inputs).
func fieldByName(s go3270.Screen, name string) (go3270.Field, bool) {
	for _, f := range s {
		if f.Name == name {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestBuildFormScreenLongLabelInputColumn(t *testing.T) {
	// "Auth Fail Window (min):" is 23 chars; the input attribute must sit at
	// col 27 and the cursor one right, so the label can't bleed into the input
	// buffer (GH #71).
	label := "Auth Fail Window (min):"
	screen, cur := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "X", Label: label, Length: 8},
	}})
	in, ok := fieldByName(screen, "X")
	if !ok {
		t.Fatal("input field X not found")
	}
	if in.Col != 27 {
		t.Errorf("input attribute col = %d, want 27", in.Col)
	}
	if cur != (Cursor{Row: 3, Col: 28}) {
		t.Errorf("cursor = %+v, want {3,28}", cur)
	}
	// Geometry guard: the label's last content column is strictly left of the
	// input attribute byte.
	if last := 2 + len(label); last >= in.Col {
		t.Errorf("label ends at col %d, overlaps input attr at %d", last, in.Col)
	}
}

func TestBuildFormScreenShortLabelUnchanged(t *testing.T) {
	// A ≤12-char dot-leader label keeps the historical col 16 / cursor {3,17}
	// (byte-identical to pre-#71 — regression guard for every existing form).
	screen, cur := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "A", Label: "Group name .", Length: 32}, // 12 chars
	}})
	in, _ := fieldByName(screen, "A")
	if in.Col != 16 {
		t.Errorf("input attribute col = %d, want 16", in.Col)
	}
	if cur != (Cursor{Row: 3, Col: 17}) {
		t.Errorf("cursor = %+v, want {3,17}", cur)
	}
}

func TestBuildFormScreenInputColumnUsesLongestLabel(t *testing.T) {
	// Mixed lengths: both rows' inputs align to the longest label
	// ("Auth Delay Base (sec):", 22 → col 26).
	screen, _ := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "SHORT", Label: "MOTD File:", Length: 8},            // 10
		{Name: "LONG", Label: "Auth Delay Base (sec):", Length: 8}, // 22
	}})
	for _, name := range []string{"SHORT", "LONG"} {
		in, ok := fieldByName(screen, name)
		if !ok {
			t.Fatalf("input field %s not found", name)
		}
		if in.Col != 26 {
			t.Errorf("%s input col = %d, want 26", name, in.Col)
		}
	}
}

func TestBuildFormScreenStopFieldRebases(t *testing.T) {
	// Stop field follows the dynamic column: inputCol+1+Length = 26+1+8 = 35.
	screen, _ := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "LONG", Label: "Auth Delay Base (sec):", Length: 8}, // inputCol 26
	}})
	var sawStop bool
	for _, f := range screen {
		if f.Row == 3 && f.Col == 35 && f.Name == "" && f.Content == "" && !f.Write {
			sawStop = true
		}
	}
	if !sawStop {
		t.Errorf("stop field at (3,35) not found")
	}
}

func TestBuildFormScreenReadOnlyUsesDynamicColumn(t *testing.T) {
	// A read-only value's attribute byte aligns with the dynamic input column
	// (27 here), not a hardcoded 16, so it lines up with editable rows.
	screen, _ := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "RO", Label: "Auth Fail Window (min):", Value: "V", ReadOnly: true}, // 23 → col 27
		{Name: "ED", Label: "Auth Fail Window (min):", Length: 8},
	}})
	valCol := -1
	for _, f := range screen {
		if f.Content == "V" && !f.Write {
			valCol = f.Col
		}
	}
	if valCol != 27 {
		t.Errorf("read-only value attribute col = %d, want 27", valCol)
	}
}

func TestBuildFormScreenTruncatesPathologicalLabel(t *testing.T) {
	// A label longer than the ceiling allows is truncated so it can never reach
	// the input attribute byte (defense in depth, GH #71).
	screen, _ := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "P", Label: strings.Repeat("X", 80), Length: 8},
	}})
	in, _ := fieldByName(screen, "P")
	if in.Col != 63 { // clamped to the ceiling
		t.Errorf("input col = %d, want ceiling 63", in.Col)
	}
	labelLen := -1
	for _, f := range screen {
		if f.Row == 3 && f.Col == 2 { // the label attribute byte
			labelLen = len([]rune(f.Content))
		}
	}
	if labelLen != 59 { // formLabelMax(63)
		t.Errorf("label truncated to %d runes, want 59", labelLen)
	}
	if last := 2 + labelLen; last >= in.Col {
		t.Errorf("truncated label reaches col %d, overlaps input attr at %d", last, in.Col)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui3270/ -run 'TestBuildFormScreenLongLabelInputColumn|TestBuildFormScreenShortLabelUnchanged|TestBuildFormScreenInputColumnUsesLongestLabel|TestBuildFormScreenStopFieldRebases|TestBuildFormScreenReadOnlyUsesDynamicColumn|TestBuildFormScreenTruncatesPathologicalLabel' -v`
Expected: FAIL — the long-label/mixed/stop/read-only/truncation tests fail (input still at col 16 / 17). `TestBuildFormScreenShortLabelUnchanged` may already pass; that's fine.

- [ ] **Step 3: Rewrite buildFormScreen**

Replace the body of `buildFormScreen` in `internal/ui3270/screen.go` (lines 71-110, from `func buildFormScreen` through its closing brace) with:

```go
func buildFormScreen(rows int, v FormView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
	}
	fields := v.Fields
	if max := formMaxFields(rows); len(fields) > max {
		fields = fields[:max]
	}
	// Place the input column past the longest label so label text can never
	// overrun the input field's buffer (GH #71). Forms whose labels are ≤12
	// chars compute the historical col 16 and render byte-identically.
	maxLabel := 0
	for _, f := range fields {
		if n := len([]rune(f.Label)); n > maxLabel {
			maxLabel = n
		}
	}
	inputCol := formInputCol(maxLabel)
	labelMax := formLabelMax(inputCol)
	cur := Cursor{Row: 0, Col: 0}
	for i, f := range fields {
		row := 3 + 2*i
		label := truncRunes(f.Label, labelMax)
		if f.ReadOnly {
			// Display-only: label + static value, no writable input, never the
			// cursor target.
			screen = append(screen,
				go3270.Field{Row: row, Col: labelAttrCol, Content: label},
				go3270.Field{Row: row, Col: inputCol, Content: f.Value},
			)
			continue
		}
		stopCol := inputCol + 1 + f.Length
		if stopCol > 79 {
			stopCol = 79
		}
		input := go3270.Field{Row: row, Col: inputCol, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Highlighting: go3270.Underscore}
		if cur == (Cursor{Row: 0, Col: 0}) {
			cur = Cursor{Row: input.Row, Col: input.Col + 1}
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: labelAttrCol, Content: label},
			input,
			go3270.Field{Row: row, Col: stopCol}, // stop field
		)
	}
	screen = append(screen,
		go3270.Field{Row: errorRow(rows), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 2, Content: "Enter = save    PF3 = cancel"},
	)
	return screen, cur
}
```

- [ ] **Step 4: Run the new tests to verify they pass**

Run: `go test ./internal/ui3270/ -run 'TestBuildFormScreen' -v`
Expected: PASS — including the pre-existing `TestBuildFormScreenCursor` ({3,17}) and `TestBuildFormScreenReadOnlyField` ({5,17}), which still hold because their labels are ≤4 chars (col 16).

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/screen.go internal/ui3270/screen_test.go
git commit -m "fix(ui3270): dynamic form input column so labels can't corrupt values — GH #71"
```

---

## Task 3: Full unit-test + race sweep

Confirm nothing else in the repo regressed (especially the server package, which builds these forms through the real flow).

**Files:** none (verification only).

- [ ] **Step 1: Run the full suite with the race detector**

Run: `go build ./... && go test ./... -race`
Expected: PASS for every package (`ok` lines; no `FAIL`). If any server-package form test fails, inspect whether it asserted a hardcoded column — none are expected to, since server tests use the scripted fake `Renderer`.

- [ ] **Step 2: Commit (only if Step 1 surfaced a fix)**

If Step 1 was clean, skip. If a follow-up fix was needed, commit it:

```bash
git add -A
git commit -m "test(ui3270): keep full suite green after dynamic input column — GH #71"
```

---

## Task 4: s3270 protocol smoke suite → 47/47

The unit tests prove column math; the real protocol surface (the actual save round-trip) is only verified against a live emulator + backend. This is the acceptance gate from the spec.

**Files:** none (verification only).

- [ ] **Step 1: Run the smoke suite**

REQUIRED SUB-SKILL: invoke the `s3270-smoke-testing` skill for setup/usage details (it documents prerequisites: `s3270` installed, a throwaway backend, the `-config` with TLS disabled).

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: **47/47 passing**, specifically tests **12a, 12b, 12c** (MOTD save + persist) and **13a, 13b** (MOTD display) now PASS where they previously FAILED.

- [ ] **Step 2: If any smoke test still fails**

REQUIRED SUB-SKILL: use `superpowers:systematic-debugging`. Capture the s3270 `Ascii()` dump of the System Parameters form and confirm the label no longer reaches the input column; check the input field's submitted value is the clean integer. Do not patch the smoke script to pass — fix the renderer.

- [ ] **Step 3: No commit**

Smoke verification produces no source changes. Proceed to Task 5.

---

## Task 5: Finish the branch

**Files:** none (integration).

- [ ] **Step 1: Confirm the working tree is clean and the branch is ready**

Run: `git status` (expect clean) and `git log --oneline origin/main..HEAD` (expect the Task 1 + Task 2 commits plus the earlier spec/plan docs).

- [ ] **Step 2: Hand off via the finishing-a-development-branch skill**

REQUIRED SUB-SKILL: use `superpowers:finishing-a-development-branch` to choose merge/PR. The PR body must reference **GH #71** and note that smoke tests 12a–12c and 13a–13b are restored (47/47).

---

## Self-review notes (for the executor)

- **Spec coverage:** dynamic input column (Tasks 1–2), floor-at-16 no-regression property (Task 2 `TestBuildFormScreenShortLabelUnchanged` + existing cursor tests), truncation guard (Tasks 1–2), cursor correctness `field.Col+1` (Task 2 cursor assertions), stop-field rebasing (Task 2), 0–79 column bound preserved by the ceiling (Task 1 `formInputCol(80)==63` + Task 2 truncation test), `go test -race` (Task 3), smoke 47/47 (Task 4).
- **Numeric cross-check:** `formInputCol`: `2+1+maxLabel+1` floored 16, ceiled 63 → 23⇒27, 22⇒26, 12⇒16, 0⇒16, 80⇒63. `formLabelMax`: `inputCol-1-2-1` → 16⇒12, 27⇒23, 63⇒59. Cursor = `inputCol+1` → 27⇒28, 16⇒17. Stop = `inputCol+1+Length` → 26+1+8=35. All consistent across Task 1 and Task 2.
- **No placeholders:** every code/test block is complete and compilable; every command has an expected result.
