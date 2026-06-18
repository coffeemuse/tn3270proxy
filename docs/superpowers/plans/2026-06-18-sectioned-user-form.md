# Sectioned Form Layout + Edit/Add User Redesign — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the admin Edit/Add User screens a sectioned, single-spaced layout with full dot-leader labels, inline hints, and grouped MFA/policy/action controls, by extending the shared `internal/ui3270` form engine with opt-in capabilities.

**Architecture:** Add four opt-in knobs to the shared form engine — `FormField.Section`, `FormField.Suffix`, `FormField.SameRow`, and `FormConfig/FormView.Compact`. A new `buildCompactFormScreen` renders the sectioned single-spaced layout; the legacy `buildFormScreen` path is unchanged (and is refactored to share a `placeFormField` helper, with the existing column tests proving byte-identical output). The user form then opts into `Compact + DotLeader` and supplies section/suffix/same-row annotations plus a new typed-CLEAR confirmation field.

**Tech Stack:** Go, `github.com/racingmars/go3270`, modernc SQLite. TDD; `go test ./... -race`; gofmt gate; s3270 smoke test.

**Spec:** `docs/superpowers/specs/2026-06-18-sectioned-user-form-design.md`

---

## File Structure

- `internal/ui3270/types.go` — add `Section`/`Suffix`/`SameRow` to `FormField`; add `Compact` to `FormView` and `FormConfig`.
- `internal/ui3270/form.go` — thread `Compact` from `FormConfig` into `FormView`.
- `internal/ui3270/layout.go` — new layout constants + helpers (`compactBodyTopRow`, `sameRow*` columns, `compactMessageRow`, `sectionBanner`).
- `internal/ui3270/screen.go` — extract `placeFormField`; add `buildCompactFormScreen`; dispatch from `buildFormScreen`.
- `internal/ui3270/screen_test.go` — compact-path unit tests.
- `internal/ui3270/layout_test.go` — `sectionBanner` unit test (create if absent).
- `internal/screens/admin.go` — add `FieldMFAClearConfirm` constant.
- `internal/server/admin_users.go` — rewrite `userEdit` field slice (sections/suffixes/same-row/full labels, new confirm field, `Compact+DotLeader`); add CLEAR-confirm guard to `applyMFAEdit`.
- `internal/server/admin_users_test.go` — CLEAR-confirm guard test (use existing test file if present).
- `.claude/skills/s3270-smoke-testing/` — update assertions for the new layout.

---

## Task 1: Engine API additions

**Files:**
- Modify: `internal/ui3270/types.go:38-44` (FormField), `:46-51` (FormView), `:134-145` (FormConfig)
- Modify: `internal/ui3270/form.go:31`

- [ ] **Step 1: Add the new fields to `FormField`**

In `internal/ui3270/types.go`, replace the `FormField` struct (lines 38-44) with:

```go
// FormField is one labeled input on a form.
type FormField struct {
	Name, Label, Value string
	Hidden             bool   // non-display (passwords)
	ReadOnly           bool   // display-only: rendered as static content, never an input
	Length             int    // input columns; effective max 62 (stop field clamps at col 79)
	Section            string // compact layout only: emit a "--- Section ---" banner above this field
	Suffix             string // compact layout only: static hint text after the input (e.g. "Y/N")
	SameRow            bool   // compact layout only: render on the previous field's row, second column
}
```

- [ ] **Step 2: Add `Compact` to `FormView`**

Replace the `FormView` struct (lines 46-51) with:

```go
// FormView is what to paint for a labeled-input form.
type FormView struct {
	Title, ErrMsg string
	Fields        []FormField
	DotLeader     bool // render labels with right-aligned-colon dot leaders
	Compact       bool // sectioned, single-spaced layout (Section/Suffix/SameRow honored; bottom message line)
}
```

- [ ] **Step 3: Add `Compact` to `FormConfig`**

In the `FormConfig` struct (lines 134-145), add after the `DotLeader bool` field:

```go
	// Compact renders the form in the sectioned, single-spaced layout: Section
	// banners group fields, Suffix adds trailing hint text, SameRow pairs a field
	// onto the previous row, and the message line moves to the bottom. Default
	// off (the flat double-spaced layout).
	Compact bool
```

- [ ] **Step 4: Thread `Compact` through `RunForm`**

In `internal/ui3270/form.go`, line 31, replace the `r.Form(...)` call with:

```go
		act, err := r.Form(FormView{Title: cfg.Title, Fields: cfg.Fields, ErrMsg: errMsg, DotLeader: cfg.DotLeader, Compact: cfg.Compact})
```

- [ ] **Step 5: Verify it compiles and existing tests pass**

Run: `go build ./... && go test ./internal/ui3270/`
Expected: build OK; all existing ui3270 tests PASS (additive change, nothing reads the new fields yet).

- [ ] **Step 6: Commit**

```bash
git add internal/ui3270/types.go internal/ui3270/form.go
git commit -m "feat(ui3270): add opt-in Section/Suffix/SameRow/Compact form knobs"
```

---

## Task 2: Layout constants and `sectionBanner` helper

**Files:**
- Modify: `internal/ui3270/layout.go` (append constants + helpers)
- Test: `internal/ui3270/layout_test.go`

- [ ] **Step 1: Write the failing test for `sectionBanner`**

Create or append to `internal/ui3270/layout_test.go`:

```go
package ui3270

import (
	"strings"
	"testing"
)

func TestSectionBanner(t *testing.T) {
	b := sectionBanner("Identity")
	if !strings.HasPrefix(b, "--- Identity ") {
		t.Errorf("banner = %q, want prefix %q", b, "--- Identity ")
	}
	// Fills to the right margin within the 0-79 band (content begins at col 3).
	if got := len([]rune(b)); got != sectionBannerWidth {
		t.Errorf("banner width = %d, want %d", got, sectionBannerWidth)
	}
	if !strings.HasSuffix(b, "-") {
		t.Errorf("banner should end in dashes: %q", b)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui3270/ -run TestSectionBanner`
Expected: FAIL — `undefined: sectionBanner` / `sectionBannerWidth`.

- [ ] **Step 3: Add the constants and helper**

Append to `internal/ui3270/layout.go` (before the final closing of the file, after `pageBounds`):

```go
// Compact-layout geometry (FormView.Compact). The body starts at row 2 (the
// message line moves to the bottom); SameRow fields and section banners use
// fixed columns. Content stays within cols 0-79.
const (
	compactBodyTopRow = 2  // first content row in compact mode (row 2 is freed up)
	sameRowLabelCol   = 40 // second-column label attribute byte for a SameRow field
	sameRowInputCol   = 54 // second-column value attribute byte for a SameRow field
	sameRowLabelMax   = 12 // dot-leader width for a SameRow label (fits "MFA status :")
	sectionBannerWidth = 69 // banner content width: cols 3..71 within the 0-79 band
)

// compactMessageRow is the red message line row in compact mode (just above the
// PF-key help line, since row 2 now carries content). 22 on MOD 2.
func compactMessageRow(rows int) int { return helpRow(rows) - 1 }

// sectionBanner renders an ISPF-style group header, e.g.
// "--- Identity ----------------------------------------------------------",
// padded with trailing dashes to sectionBannerWidth runes.
func sectionBanner(name string) string {
	prefix := "--- " + name + " "
	if len([]rune(prefix)) >= sectionBannerWidth {
		return string([]rune(prefix)[:sectionBannerWidth])
	}
	return prefix + strings.Repeat("-", sectionBannerWidth-len([]rune(prefix)))
}
```

- [ ] **Step 4: Add the `strings` import to layout.go**

`internal/ui3270/layout.go` currently imports only `"fmt"`. Replace the import block (line 22) with:

```go
import (
	"fmt"
	"strings"
)
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/ui3270/ -run TestSectionBanner`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui3270/layout.go internal/ui3270/layout_test.go
git commit -m "feat(ui3270): compact-layout constants + sectionBanner helper"
```

---

## Task 3: Compact render path — single-spacing, bottom message line, cursor

**Files:**
- Modify: `internal/ui3270/screen.go` (extract `placeFormField`, add `buildCompactFormScreen`, dispatch)
- Test: `internal/ui3270/screen_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui3270/screen_test.go`:

```go
func TestBuildFormScreenCompactSingleSpaced(t *testing.T) {
	screen, cur := buildFormScreen(24, FormView{Compact: true, Fields: []FormField{
		{Name: "a", Label: "A", Length: 8},
		{Name: "b", Label: "B", Length: 8},
	}})
	a, _ := fieldByName(screen, "a")
	b, _ := fieldByName(screen, "b")
	if a.Row != 2 || b.Row != 3 {
		t.Errorf("rows = %d,%d, want 2,3 (single-spaced from row 2)", a.Row, b.Row)
	}
	if cur != (Cursor{Row: 2, Col: a.Col + 1}) {
		t.Errorf("cursor = %+v, want first writable input", cur)
	}
}

func TestBuildFormScreenCompactMessageRowAtBottom(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{Compact: true, Fields: []FormField{
		{Name: "a", Label: "A", Length: 8},
	}})
	msg, ok := fieldByName(screen, fieldError)
	if !ok {
		t.Fatal("error field not found")
	}
	if msg.Row != 22 {
		t.Errorf("message row = %d, want 22", msg.Row)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/ui3270/ -run TestBuildFormScreenCompact`
Expected: FAIL — compact path not implemented (fields land at legacy double-spaced rows / message at row 2).

- [ ] **Step 3: Extract `placeFormField` and refactor the legacy loop to use it**

In `internal/ui3270/screen.go`, inside `buildFormScreen`, replace the cursor declaration + per-field loop (lines 93-119 — the `cur := Cursor{Row: 0, Col: 0}` line through the closing `}` of the `for i, f := range fields { ... }` block) with a call to a shared helper:

```go
	cur := Cursor{Row: 0, Col: 0}
	for i, f := range fields {
		row := 3 + 2*i
		placeFormField(&screen, &cur, f, row, labelAttrCol, inputCol, labelMax, v.DotLeader)
	}
```

Then add this helper function at the end of `screen.go`:

```go
// placeFormField appends the go3270 fields for one labeled input at the given
// row: a label at labelCol, then either a static value (ReadOnly) or a writable
// input at inputCol with its stop field, plus an optional Suffix. It updates cur
// to the first writable input encountered (the first call that sets it wins).
func placeFormField(screen *go3270.Screen, cur *Cursor, f FormField, row, labelCol, inputCol, labelMax int, dotLeader bool) {
	label := truncRunes(f.Label, labelMax)
	if dotLeader {
		label = dotLeaderLabel(f.Label, labelMax)
	}
	if f.ReadOnly {
		*screen = append(*screen,
			go3270.Field{Row: row, Col: labelCol, Color: go3270.Turquoise, Content: label},
			go3270.Field{Row: row, Col: inputCol, Color: go3270.Green, Content: f.Value},
		)
		return
	}
	stopCol := min(inputCol+1+f.Length, 79)
	input := go3270.Field{Row: row, Col: inputCol, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Color: go3270.Green, Highlighting: go3270.Underscore}
	if *cur == (Cursor{Row: 0, Col: 0}) {
		*cur = Cursor{Row: input.Row, Col: input.Col + 1}
	}
	*screen = append(*screen,
		go3270.Field{Row: row, Col: labelCol, Color: go3270.Turquoise, Content: label},
		input,
		go3270.Field{Row: row, Col: stopCol}, // stop field
	)
	if f.Suffix != "" {
		*screen = append(*screen, go3270.Field{Row: row, Col: stopCol + 1, Color: go3270.Turquoise, Content: f.Suffix})
	}
}
```

- [ ] **Step 4: Verify the refactor is byte-identical (legacy tests still pass)**

Run: `go test ./internal/ui3270/ -run TestBuildFormScreen`
Expected: all the pre-existing `TestBuildFormScreen*` tests PASS (proves the legacy path is unchanged). The two new compact tests still FAIL.

- [ ] **Step 5: Add the compact dispatch and `buildCompactFormScreen`**

In `internal/ui3270/screen.go`, at the top of `buildFormScreen` (right after the `func buildFormScreen(...) {` line, before building `screen`), add:

```go
	if v.Compact {
		return buildCompactFormScreen(rows, v)
	}
```

Then add the compact builder at the end of `screen.go`:

```go
// buildCompactFormScreen renders v in the sectioned, single-spaced layout. The
// body starts at row 2; Section banners (with a leading gutter row except before
// the first section) group fields; SameRow pairs a field onto the previous row's
// second column; Suffix adds trailing hint text; the message line sits at the
// bottom. Cursor lands on the first writable input.
func buildCompactFormScreen(rows int, v FormView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
	}
	// Input column derives from the longest PRIMARY (non-SameRow) label.
	maxLabel := 0
	for _, f := range v.Fields {
		if f.SameRow {
			continue
		}
		if n := len([]rune(f.Label)); n > maxLabel {
			maxLabel = n
		}
	}
	inputCol := formInputCol(maxLabel)
	labelMax := formLabelMax(inputCol)

	cur := Cursor{Row: 0, Col: 0}
	row := compactBodyTopRow
	lastRow := row
	limit := compactMessageRow(rows) - 1
	first := true
	for _, f := range v.Fields {
		if f.SameRow {
			placeFormField(&screen, &cur, f, lastRow, sameRowLabelCol, sameRowInputCol, sameRowLabelMax, v.DotLeader)
			continue
		}
		if f.Section != "" {
			if !first {
				row++ // blank gutter row before the section banner
			}
			screen = append(screen, go3270.Field{Row: row, Col: labelAttrCol, Color: go3270.Blue, Content: sectionBanner(f.Section)})
			row++
		}
		if row > limit {
			break // out of vertical room; never overlap the message line
		}
		placeFormField(&screen, &cur, f, row, labelAttrCol, inputCol, labelMax, v.DotLeader)
		lastRow = row
		row++
		first = false
	}
	screen = append(screen,
		go3270.Field{Row: compactMessageRow(rows), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 0, Color: go3270.Blue, Content: "Enter=Save    PF3=Cancel"},
	)
	return screen, cur
}
```

- [ ] **Step 6: Run the compact tests to verify they pass**

Run: `go test ./internal/ui3270/ -run TestBuildFormScreenCompact`
Expected: PASS.

- [ ] **Step 7: Run the whole package (regression check)**

Run: `go test ./internal/ui3270/`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/ui3270/screen.go internal/ui3270/screen_test.go
git commit -m "feat(ui3270): compact form render path (single-spaced, bottom message)"
```

---

## Task 4: Section banners and gutter rows

**Files:**
- Test: `internal/ui3270/screen_test.go`

(The banner logic was implemented in Task 3; this task adds the regression tests that lock its row math — first-section-no-gutter and mid-form gutter.)

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui3270/screen_test.go` (ensure `"strings"` is imported in this file; add it if missing):

```go
func TestBuildFormScreenCompactFirstSectionNoGutter(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{Compact: true, Fields: []FormField{
		{Name: "a", Label: "A", Length: 8, Section: "Identity"},
	}})
	bannerRow := -1
	for _, f := range screen {
		if strings.HasPrefix(f.Content, "--- Identity") {
			bannerRow = f.Row
		}
	}
	if bannerRow != 2 {
		t.Errorf("banner row = %d, want 2 (no gutter before the first section)", bannerRow)
	}
	a, _ := fieldByName(screen, "a")
	if a.Row != 3 {
		t.Errorf("field row = %d, want 3", a.Row)
	}
}

func TestBuildFormScreenCompactSectionGutter(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{Compact: true, Fields: []FormField{
		{Name: "a", Label: "A", Length: 8},                  // row 2
		{Name: "b", Label: "B", Length: 8, Section: "Sect"}, // gutter 3, banner 4, field 5
	}})
	a, _ := fieldByName(screen, "a")
	b, _ := fieldByName(screen, "b")
	if a.Row != 2 || b.Row != 5 {
		t.Errorf("rows = %d,%d, want 2,5", a.Row, b.Row)
	}
	bannerRow := -1
	for _, f := range screen {
		if strings.HasPrefix(f.Content, "--- Sect") {
			bannerRow = f.Row
		}
	}
	if bannerRow != 4 {
		t.Errorf("banner row = %d, want 4 (gutter at 3)", bannerRow)
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/ui3270/ -run TestBuildFormScreenCompact`
Expected: PASS (the Task 3 implementation already produces this layout).

- [ ] **Step 3: Commit**

```bash
git add internal/ui3270/screen_test.go
git commit -m "test(ui3270): lock compact section banner + gutter row math"
```

---

## Task 5: Suffix placement test

**Files:**
- Test: `internal/ui3270/screen_test.go`

(Suffix rendering was implemented in `placeFormField` in Task 3; lock it.)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui3270/screen_test.go`:

```go
func TestBuildFormScreenCompactSuffix(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{Compact: true, Fields: []FormField{
		{Name: "a", Label: "A", Length: 1, Suffix: "Y/N"},
	}})
	a, _ := fieldByName(screen, "a")
	stop := a.Col + 1 + 1 // inputCol + 1 + Length
	sawSuffix := false
	for _, f := range screen {
		if f.Content == "Y/N" && f.Row == a.Row && f.Col == stop+1 {
			sawSuffix = true
		}
	}
	if !sawSuffix {
		t.Errorf("suffix Y/N not found at row %d col %d", a.Row, stop+1)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/ui3270/ -run TestBuildFormScreenCompactSuffix`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui3270/screen_test.go
git commit -m "test(ui3270): lock compact form suffix placement"
```

---

## Task 6: Same-row field pairing test

**Files:**
- Test: `internal/ui3270/screen_test.go`

(SameRow placement was implemented in Task 3; lock it.)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui3270/screen_test.go`:

```go
func TestBuildFormScreenCompactSameRow(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{Compact: true, DotLeader: true, Fields: []FormField{
		{Name: "req", Label: "MFA required", Length: 1, Suffix: "Y/N"},
		{Name: "stat", Label: "MFA status", Value: "ENROLLED", ReadOnly: true, SameRow: true},
	}})
	req, _ := fieldByName(screen, "req")
	statRow, statCol := -1, -1
	for _, f := range screen {
		if f.Content == "ENROLLED" && !f.Write {
			statRow, statCol = f.Row, f.Col
		}
	}
	if statRow != req.Row {
		t.Errorf("status row = %d, want same as req row %d", statRow, req.Row)
	}
	if statCol != sameRowInputCol {
		t.Errorf("status value col = %d, want %d", statCol, sameRowInputCol)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/ui3270/ -run TestBuildFormScreenCompactSameRow`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui3270/screen_test.go
git commit -m "test(ui3270): lock compact form same-row field pairing"
```

---

## Task 7: New field constant + rewrite the user form

**Files:**
- Modify: `internal/screens/admin.go:43` (add `FieldMFAClearConfirm`)
- Modify: `internal/server/admin_users.go:152-227` (`userEdit`)

- [ ] **Step 1: Add the `FieldMFAClearConfirm` constant**

In `internal/screens/admin.go`, in the field-name const block, after the `FieldMFAClear` line (line 42) add:

```go
	FieldMFAClearConfirm    = "mfaclearconf"   // edit-user form: typed "CLEAR" confirmation for the wipe
```

- [ ] **Step 2: Rewrite `userEdit` field construction**

In `internal/server/admin_users.go`, replace the body of `userEdit` from the `fields := []ui3270.FormField{...}` declaration through the end of the conditional field appends (lines 162-203) with:

```go
	// Identity. User ID stands alone above the Identity banner (it is the key:
	// read-only in edit, editable in create).
	fields := []ui3270.FormField{
		{Name: screens.FieldUsername, Label: "User ID", Length: 32, Value: username, ReadOnly: !create},
		{Name: screens.FieldFullName, Label: "Full name", Length: 40, Value: fullName, Section: "Identity"},
		{Name: screens.FieldEmail, Label: "Email", Length: 40, Value: email},
	}
	// Authentication. The "blank = no change" hint applies only in edit mode.
	pwSuffix := ""
	if !create {
		pwSuffix = "(blank = no change)"
	}
	fields = append(fields,
		ui3270.FormField{Name: screens.FieldPassword, Label: "New password", Hidden: true, Length: 32, Section: "Authentication", Suffix: pwSuffix},
		ui3270.FormField{Name: screens.FieldRetype, Label: "Confirm password", Hidden: true, Length: 32},
	)
	if !create {
		mfaReq := "N"
		if u.MFARequired {
			mfaReq = "Y"
		}
		status := "NONE"
		switch {
		case !u.MFARequired:
			status = "NONE"
		case u.MFASecret == "":
			status = "PENDING"
		default:
			status = "ENROLLED"
		}
		fields = append(fields,
			ui3270.FormField{Name: screens.FieldMFARequired, Label: "MFA required", Length: 1, Value: mfaReq, Suffix: "Y/N"},
			ui3270.FormField{Name: screens.FieldMFAStatus, Label: "MFA status", Length: 10, Value: status, ReadOnly: true, SameRow: true},
		)
		// Account policy.
		lock := "N"
		if u.UserSettingsLocked {
			lock = "Y"
		}
		fields = append(fields,
			ui3270.FormField{Name: screens.FieldUserSettingsLocked, Label: "Block self-service", Length: 1, Value: lock, Section: "Account policy", Suffix: "(blocks user-initiated changes)"},
		)
		// Account actions: a Clear-MFA wipe requires the toggle AND a typed CLEAR.
		fields = append(fields,
			ui3270.FormField{Name: screens.FieldMFAClear, Label: "Clear MFA", Length: 1, Value: "", Section: "Account actions", Suffix: "Y/N"},
			ui3270.FormField{Name: screens.FieldMFAClearConfirm, Label: "Confirm: type CLEAR", Length: 7, Value: ""},
		)
		// Hint: when MFA is required but no secret is enrolled, locking makes
		// mfa_required a no-op (a locked account is never force-enrolled).
		if u.UserSettingsLocked && u.MFARequired && u.MFASecret == "" {
			fields = append(fields,
				ui3270.FormField{Name: "lockhint", Label: "Note", Length: 40,
					Value: "MFA REQ INERT WHILE LOCKED W/O SECRET", ReadOnly: true},
			)
		}
	}
```

- [ ] **Step 3: Enable the compact + dot-leader layout and fix the re-render index references**

The `Submit` closure preserves typed input by index into `fields` (`fields[1]`, `fields[2]`, `fields[0]`). The field order above is unchanged for those three (0=User ID, 1=Full name, 2=Email), so those index references remain correct. In `internal/server/admin_users.go`, update the `RunForm` config (lines 204-226) to set the new flags — replace the `return ui3270.RunForm(ctx, r, ui3270.FormConfig{` opening and its `Title`/`Fields` lines with:

```go
	return ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title:   title,
		Fields:  fields,
		Compact: true,
		DotLeader: true,
```

Leave the existing `Submit: func(...) {...}` body unchanged.

- [ ] **Step 4: Build and run the server package tests**

Run: `go build ./... && go test ./internal/server/ ./internal/screens/`
Expected: build OK; tests PASS (the CLEAR-confirm guard test comes in Task 8 — existing tests should still pass since a blank Clear-MFA toggle is a no-op).

- [ ] **Step 5: Commit**

```bash
git add internal/screens/admin.go internal/server/admin_users.go
git commit -m "feat(server): sectioned compact Edit/Add User form (#130)"
```

---

## Task 8: Clear-MFA typed-confirm guard

**Files:**
- Modify: `internal/server/admin_users.go:315-322` (`applyMFAEdit` clear branch)
- Test: `internal/server/admin_users_test.go`

- [ ] **Step 1: Write the failing test**

Append to the existing `internal/server/admin_users_test.go` (imports `context`, `testing`, `auth`, `screens`, `store` are already present). Mirror the existing `TestApplyMFAEditTogglesAndClears` setup pattern exactly:

```go
func TestApplyMFAEditClearRequiresTypedConfirm(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "h")
	st.StoreMFAEnrollment(ctx, uid, "ct", "t", 3) // give alice an enrolled secret
	u, _ := st.GetUserByUsername(ctx, "alice")

	f := &adminFlow{store: st, identity: auth.Identity{UserID: 99, Username: "admin"}}

	// Toggle Y but confirm field blank → rejected, secret retained.
	msg, err := f.applyMFAEdit(ctx, u, map[string]string{
		screens.FieldMFAClear: "Y",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "TYPE CLEAR TO CONFIRM MFA WIPE" {
		t.Errorf("msg = %q, want the confirm prompt", msg)
	}
	if got, _ := st.GetUserByUsername(ctx, "alice"); got.MFASecret == "" {
		t.Errorf("secret was wiped without confirmation")
	}

	// Toggle Y + typed CLEAR (case-insensitive) → wiped.
	msg, err = f.applyMFAEdit(ctx, u, map[string]string{
		screens.FieldMFAClear:        "Y",
		screens.FieldMFAClearConfirm: "clear",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "" {
		t.Errorf("msg = %q, want empty (wipe succeeded)", msg)
	}
	if got, _ := st.GetUserByUsername(ctx, "alice"); got.MFASecret != "" {
		t.Errorf("secret not wiped after confirmed clear")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run TestApplyMFAEditClearRequiresTypedConfirm`
Expected: FAIL — currently a bare `Y` wipes the secret (no confirm gate), so the first assertion fails.

- [ ] **Step 3: Add the guard**

In `internal/server/admin_users.go`, replace the clear branch (lines 315-322) with:

```go
	if strings.ToUpper(strings.TrimSpace(vals[screens.FieldMFAClear])) == "Y" {
		if !strings.EqualFold(strings.TrimSpace(vals[screens.FieldMFAClearConfirm]), "CLEAR") {
			return "TYPE CLEAR TO CONFIRM MFA WIPE", nil
		}
		if err := f.store.ClearMFA(ctx, u.ID); err != nil {
			return f.storeErr("clear mfa", err), nil
		}
		if f.audit != nil {
			f.audit(ctx, store.AuditEvent{Kind: store.AuditMFACleared, Username: u.Username})
		}
	}
```

- [ ] **Step 4: Update the existing test that clears MFA without the confirm**

The pre-existing `TestApplyMFAEditTogglesAndClears` (top of `internal/server/admin_users_test.go`) clears with just `screens.FieldMFAClear: "Y"` and expects success — the new guard now rejects that. Update its second `applyMFAEdit` call (the "clear MFA" step) to include the confirm:

```go
	if msg, err := f.applyMFAEdit(ctx, u, map[string]string{
		screens.FieldMFARequired: "Y", screens.FieldMFAClear: "Y", screens.FieldMFAClearConfirm: "CLEAR",
	}); err != nil || msg != "" {
		t.Fatalf("clear MFA: msg=%q err=%v", msg, err)
	}
```

- [ ] **Step 5: Run both tests to verify they pass**

Run: `go test ./internal/server/ -run TestApplyMFAEdit`
Expected: PASS (both `TestApplyMFAEditClearRequiresTypedConfirm` and `TestApplyMFAEditTogglesAndClears`).

- [ ] **Step 6: Run the full server package**

Run: `go test ./internal/server/ -race`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/server/admin_users.go internal/server/admin_users_test.go
git commit -m "feat(server): require typed CLEAR to confirm an MFA wipe (#130)"
```

---

## Task 9: Verification — gofmt, full suite, smoke test, live QA

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/` (assertions for the new layout)

- [ ] **Step 1: gofmt gate**

Run: `gofmt -l internal/`
Expected: no output (all formatted). If any file is listed, run `go fmt ./...` and re-commit.

- [ ] **Step 2: Full test suite with race detector**

Run: `go test ./... -race`
Expected: all packages PASS.

- [ ] **Step 3: Rebuild + restart the QA server**

Run: `./qa/restart-proxy.sh`
Expected: "QA proxy restarted (pid …), listening on :2323".

- [ ] **Step 4: Update the s3270 smoke test for the new Edit User layout**

Open `.claude/skills/s3270-smoke-testing/smoke.sh` and locate the Edit User assertions (checks "14a"–"14d" near line 617, plus the cursor check at line 620). Update them to the new layout:
- **Cursor column changes 17 → 24.** Full name stays on **row 5** (User ID row 2, gutter row 3, `--- Identity` banner row 4, Full name row 5), but the input attribute byte is now at col 23 (longest label "Confirm: type CLEAR" = 19 runes → `formInputCol(19)` = 23), so the cursor homes to **(5,24)**. Update the "14d edit-user cursor on Full name (5,17)" check to **(5,24)** (both the assertion and its PASS message text).
- Add checks that the section banners `--- Identity`, `--- Authentication`, `--- Account policy`, `--- Account actions` are present in the captured screen.
- The Clear-MFA path now needs the `Confirm: type CLEAR` field typed `CLEAR`; assert a bare `Y` is rejected with `TYPE CLEAR TO CONFIRM MFA WIPE` and that `Y` + `CLEAR` succeeds.

Run the smoke script per the skill (it manages s3270 + a throwaway instance):
Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all checks pass, including the updated Edit User assertions.

> If the smoke script's field-Tab navigation depends on field order/row offsets, adjust the Tab hops to the new single-spaced rows (the editor/documents-list precedent in memory: layout shifts move Tab targets).

- [ ] **Step 5: Commit the smoke updates**

```bash
git add .claude/skills/s3270-smoke-testing/
git commit -m "test(smoke): assert sectioned Edit User layout + CLEAR confirm"
```

- [ ] **Step 6: Final human QA pass (manual)**

Connect with `c3270 127.0.0.1:2323` as `admin/admin123`, go to `A` → Users → `S` on a user. Confirm visually:
- Sections, single-spacing, dot-leader labels, suffixes render as in the spec mockup.
- `MFA required` and `MFA status` share one row.
- The red message line sits near the bottom; PF3 returns to the user list; Enter saves.
- Clear MFA requires both `Y` and typed `CLEAR`.

This human pass is the final word on visual polish (per CLAUDE.md).

---

## Self-Review Notes

- **Spec coverage:** engine knobs (Task 1), layout helpers (Task 2), compact render + single-spacing + bottom message (Task 3), banners/gutters (Task 4), suffix (Task 5), same-row (Task 6), user-form wiring with full labels + new confirm field + standalone User ID (Task 7), dual Clear-MFA guard (Task 8), tests + smoke + QA (Task 9). All spec sections map to a task.
- **Decisions honored:** PF3-only footer (hardcoded `Enter=Save    PF3=Cancel`); NONE/PENDING/ENROLLED retained; both Clear-MFA guards; User ID standalone; compact error line at bottom.
- **Type consistency:** `FieldMFAClearConfirm` defined in Task 7 (admin.go) and used in Tasks 7-8; `placeFormField`/`buildCompactFormScreen`/`sectionBanner`/`compactMessageRow` and the `sameRow*`/`compactBodyTopRow`/`sectionBannerWidth` constants are defined before use.
- **No regression:** legacy `buildFormScreen` path is refactored to share `placeFormField` but the existing column tests (`TestBuildFormScreen*`) prove byte-identical output; the other admin forms (Group/Service/sysparms) leave `Compact` false and are untouched.
- **Existing-test updates flagged:** Task 8 Step 4 updates `TestApplyMFAEditTogglesAndClears` (the new CLEAR guard would otherwise break it); Task 9 Step 4 updates the smoke cursor assertion (5,17)→(5,24). These are the only behavior-visible breaks of existing assertions.
- **Verified against real code:** `adminFlow{store, identity}` + `store.Open(t.TempDir())` + `StoreMFAEnrollment` is the actual test pattern (no invented helpers); the smoke script already targets Full name at row 5 (only the column moves).
