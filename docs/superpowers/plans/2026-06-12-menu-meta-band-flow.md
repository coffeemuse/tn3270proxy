# Menu Meta-Band Flow (GH #133) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the service menu's `0 User Settings` / `A Administration` meta entries directly below the service list (with a conditional blank separator) instead of bottom-anchoring them, per the approved spec `docs/superpowers/specs/2026-06-12-menu-meta-band-flow-design.md` and GH issue #133.

**Architecture:** Single behavioral change inside `screens.MenuScreen`: the meta band's anchor row becomes `row + 1` where `row` is the render loop's running cursor (one past the last service row, or sitting on the empty-list placeholder row — the same expression yields "blank separator after services" and "no separator after the placeholder"). When `0` is hidden (settings-locked), `A` takes the anchor row. `Geometry.MenuCapacity` and all paging math are untouched; `menuBottomRow()` survives purely as capacity input. On a full page the band's last row lands on `BodyBottomRow()` (one row lower than today — accepted in the spec).

**Tech Stack:** Go, `go3270` screen builders, stdlib `testing`. No new dependencies.

**Key row math (MOD 2, 0-based, `BodyTopRow()=3`, `BodyBottomRow()=22`):** first service row is `BodyTopRow()+1` = 4; with N services on the page, last service row = `3+N`, separator = `4+N`, `0` = `5+N`, `A` = `6+N`. Empty list: placeholder at 4, `0` at 5, `A` at 6. `MenuCapacity` = 17 non-admin / 16 admin.

---

### Task 1: Rewrite the existing bottom-anchor tests to the flow contract (RED)

**Files:**
- Modify: `internal/screens/menu_test.go` (replace `TestMenuScreenMetaBandOnEveryPage`, lines ~375–399, and `TestMenuScreenBlankSeparatorAboveHelp`, lines ~401–414)

Test helpers already exist: `fieldByContent` (exact-match, `login_test.go`), `screenContains` (substring, `admin_test.go`), `hasContent` (`menu_test.go`). Do not redefine them.

- [ ] **Step 1: Replace `TestMenuScreenMetaBandOnEveryPage`**

Replace the whole function (it currently asserts `us.Row == g.menuBottomRow()-1` / `admin.Row == g.menuBottomRow()`) with:

```go
func TestMenuScreenMetaBandOnEveryPage(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry // admin capacity 16: page 0 renders 16 services, page 1 renders 6
	for _, c := range []struct {
		page    int
		svcRows int
	}{
		{0, 16},
		{1, 6},
	} {
		screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", c.page)
		if !screenContains(screen, "User Settings") {
			t.Errorf("page %d missing '0 User Settings' meta entry", c.page)
		}
		if !screenContains(screen, "Administration") {
			t.Errorf("page %d missing 'A Administration' meta entry", c.page)
		}
		// The band flows below the list (GH #133): last service row, one blank
		// separator row, then "0", then "A". On the full page 0 that puts "A"
		// on BodyBottomRow, flush above the PF legend.
		lastSvc := g.BodyTopRow() + c.svcRows // first service row is BodyTopRow()+1
		us, _ := fieldByContent(screen, "User Settings")
		if us.Row != lastSvc+2 {
			t.Errorf("page %d: User Settings row = %d, want %d", c.page, us.Row, lastSvc+2)
		}
		admin, _ := fieldByContent(screen, "Administration")
		if admin.Row != lastSvc+3 {
			t.Errorf("page %d: Administration row = %d, want %d", c.page, admin.Row, lastSvc+3)
		}
	}
}
```

- [ ] **Step 2: Replace `TestMenuScreenBlankSeparatorAboveHelp`**

The old test asserts `BodyBottomRow()` is blank on a full admin page; in the new layout `A` legitimately occupies that row and the blank sits between the list and the band. Replace the whole function with:

```go
func TestMenuScreenSeparatorBetweenListAndBand(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry
	screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", 0)
	// Full admin page: services fill rows 4..19, so the blank separator is row
	// 20 and the band occupies 21 ("0") and 22 ("A" = BodyBottomRow). Row 20 is
	// below the right-hand status block (rows 4..9), so the whole row is blank.
	sep := g.BodyTopRow() + g.MenuCapacity(true) + 1
	for _, f := range screen {
		if f.Row == sep && f.Content != "" {
			t.Errorf("separator row %d should be blank, found %+v", sep, f)
		}
	}
}
```

- [ ] **Step 3: Run the two tests and verify they FAIL**

Run: `go test ./internal/screens/ -run 'TestMenuScreenMetaBandOnEveryPage|TestMenuScreenSeparatorBetweenListAndBand' -v`
Expected: both FAIL — `User Settings row = 20, want 21` style mismatches on page 0 (band still bottom-anchored at rows 20/21) and a non-blank field on the old band row. Page-1 mismatches will be larger (band at 20/21, want 11/12).

Do NOT commit yet (tree is red).

### Task 2: Add the new flow-behavior tests (RED)

**Files:**
- Modify: `internal/screens/menu_test.go` (append at end of file)

- [ ] **Step 1: Append the four new tests**

```go
// GH #133: the meta band flows directly below the service list with one blank
// separator row, instead of bottom-anchoring near the PF legend.
func TestMenuScreenMetaBandFlowsBelowSparseList(t *testing.T) {
	g := DefaultGeometry
	svcs := []store.Service{
		{ID: 1, Name: "PROD", Description: "Production CICS", Host: "h", Port: 23},
		{ID: 2, Name: "TEST", Description: "Test CICS", Host: "h", Port: 23},
		{ID: 3, Name: "DEMO", Description: "Demo backend", Host: "h", Port: 23},
	}
	lastSvc := g.BodyTopRow() + 3 // service rows 4..6 on MOD 2

	// admin, unlocked: blank separator, then "0", then "A".
	screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", 0)
	us, ok := fieldByContent(screen, "User Settings")
	if !ok || us.Row != lastSvc+2 {
		t.Errorf("admin: User Settings row = %d ok=%v, want %d", us.Row, ok, lastSvc+2)
	}
	adm, ok := fieldByContent(screen, "Administration")
	if !ok || adm.Row != lastSvc+3 {
		t.Errorf("admin: Administration row = %d ok=%v, want %d", adm.Row, ok, lastSvc+3)
	}

	// non-admin, unlocked: "0" on the same anchor row, no "A".
	screen, _, _ = MenuScreen(g, svcs, false, false, MenuStatus{}, "", 0)
	us, ok = fieldByContent(screen, "User Settings")
	if !ok || us.Row != lastSvc+2 {
		t.Errorf("non-admin: User Settings row = %d ok=%v, want %d", us.Row, ok, lastSvc+2)
	}
}

// GH #133: when "0" is hidden (settings-locked), "A" takes the anchor row —
// no gap where "0" would have been.
func TestMenuScreenLockedAdminBandTakesAnchorRow(t *testing.T) {
	g := DefaultGeometry
	svcs := []store.Service{{ID: 1, Name: "PROD", Description: "Production CICS", Host: "h", Port: 23}}
	lastSvc := g.BodyTopRow() + 1
	screen, _, _ := MenuScreen(g, svcs, true, true, MenuStatus{}, "", 0)
	if screenContains(screen, "User Settings") {
		t.Error("locked admin must not see User Settings entry")
	}
	adm, ok := fieldByContent(screen, "Administration")
	if !ok || adm.Row != lastSvc+2 {
		t.Errorf("locked admin: Administration row = %d ok=%v, want %d (anchor row)", adm.Row, ok, lastSvc+2)
	}
}

// GH #133: with an empty list the band starts directly beneath the
// placeholder — no blank separator (the placeholder is a message, not a
// service row).
func TestMenuScreenMetaBandHugsEmptyPlaceholder(t *testing.T) {
	g := DefaultGeometry
	screen, _, _ := MenuScreen(g, nil, true, false, MenuStatus{}, "", 0)
	ph, ok := fieldByContent(screen, "(no services available for your account)")
	if !ok || ph.Row != g.BodyTopRow()+1 {
		t.Fatalf("placeholder row = %d ok=%v, want %d", ph.Row, ok, g.BodyTopRow()+1)
	}
	us, ok := fieldByContent(screen, "User Settings")
	if !ok || us.Row != ph.Row+1 {
		t.Errorf("User Settings row = %d ok=%v, want %d (directly below placeholder)", us.Row, ok, ph.Row+1)
	}
	adm, ok := fieldByContent(screen, "Administration")
	if !ok || adm.Row != ph.Row+2 {
		t.Errorf("Administration row = %d ok=%v, want %d", adm.Row, ok, ph.Row+2)
	}
}

// GH #133: a sparse final page hugs its short list just like a sparse
// single-page menu.
func TestMenuScreenMetaBandFlowsOnSparseFinalPage(t *testing.T) {
	g := DefaultGeometry
	svcs := make([]store.Service, g.MenuCapacity(false)+5) // page 1 renders 5
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	screen, _, _ := MenuScreen(g, svcs, false, false, MenuStatus{}, "", 1)
	lastSvc := g.BodyTopRow() + 5
	us, ok := fieldByContent(screen, "User Settings")
	if !ok || us.Row != lastSvc+2 {
		t.Errorf("page 1: User Settings row = %d ok=%v, want %d", us.Row, ok, lastSvc+2)
	}
}
```

- [ ] **Step 2: Run the new tests and verify they FAIL**

Run: `go test ./internal/screens/ -run 'TestMenuScreenMetaBandFlowsBelowSparseList|TestMenuScreenLockedAdminBandTakesAnchorRow|TestMenuScreenMetaBandHugsEmptyPlaceholder|TestMenuScreenMetaBandFlowsOnSparseFinalPage' -v`
Expected: all four FAIL with row mismatches (band still at rows 20/21).

Do NOT commit yet (tree is red).

### Task 3: Implement the flow anchor in MenuScreen (GREEN) and update doc comments

**Files:**
- Modify: `internal/screens/menu.go` (meta-band block, lines ~146–166, and the `MenuScreen` doc comment lines ~105–106)
- Modify: `internal/screens/geometry.go` (doc comments on `menuBottomRow`, lines ~60–63, and `MenuCapacity`, lines ~65–69 — comments only, no code change)

- [ ] **Step 1: Replace the meta-band block in `MenuScreen`**

Replace this (current code after the `(no services available...)` placeholder append):

```go
	// Meta band: bottom-anchored. Non-admin unlocked: "0" on menuBottomRow.
	// Admin: "A" on menuBottomRow, "0" one row above. A locked user has no "0"
	// row at all (self-service is hidden). The page window is capped at
	// MenuCapacity, so service rows never reach the meta band.
	metaRow := geom.menuBottomRow()
	if admin {
		metaRow-- // leave the bottom row for the "A" entry
	}
	if !settingsLocked {
		screen = append(screen,
			go3270.Field{Row: metaRow, Col: 0, Intense: true, Content: "  0"},
			go3270.Field{Row: metaRow, Col: 17, Color: go3270.Green, Content: "User Settings"},
		)
	}
	if admin {
		adminRow := metaRow + 1
		screen = append(screen,
			go3270.Field{Row: adminRow, Col: 0, Intense: true, Content: "  A"},
			go3270.Field{Row: adminRow, Col: 17, Color: go3270.Green, Content: "Administration"},
		)
	}
```

with:

```go
	// Meta band: flows directly below the page's service rows (GH #133) — one
	// blank separator row after the last service row, none after the
	// empty-list placeholder (a message, not a service row: `row` still sits
	// on it, so row+1 lands directly beneath). "0 User Settings" renders first
	// unless settingsLocked; "A Administration" (admins) takes the next row,
	// or the anchor row itself when "0" is hidden. MenuCapacity reserves the
	// separator + band rows, so a full page's band ends exactly on
	// BodyBottomRow and never collides with the page window or PF legend.
	metaRow := row + 1
	if !settingsLocked {
		screen = append(screen,
			go3270.Field{Row: metaRow, Col: 0, Intense: true, Content: "  0"},
			go3270.Field{Row: metaRow, Col: 17, Color: go3270.Green, Content: "User Settings"},
		)
		metaRow++
	}
	if admin {
		screen = append(screen,
			go3270.Field{Row: metaRow, Col: 0, Intense: true, Content: "  A"},
			go3270.Field{Row: metaRow, Col: 17, Color: go3270.Green, Content: "Administration"},
		)
	}
```

(`row` is the existing render-loop cursor: after the loop it is one past the last service row; with zero services it equals `BodyTopRow()+1`, the placeholder row.)

- [ ] **Step 2: Update the `MenuScreen` doc comment**

In the function comment (~lines 104–108), replace the sentence:

```
The
"0 User Settings" meta row (omitted when settingsLocked) and, when admin,
"A Administration" are bottom-anchored on every page.
```

with:

```
The
"0 User Settings" meta row (omitted when settingsLocked) and, when admin,
"A Administration" flow directly below the page's service rows on every page,
after one blank separator row (no separator below the empty-list placeholder).
```

- [ ] **Step 3: Update the two geometry.go doc comments (no code changes)**

`menuBottomRow` (~lines 60–63) — replace the comment with:

```go
// menuBottomRow is the menu page window's row-budget floor used by
// MenuCapacity: one above BodyBottomRow, reserving the blank separator row
// between the service list and the meta band (GH #133). It no longer anchors
// the band — MenuScreen flows the band below the list, so on a full page the
// band ends exactly on BodyBottomRow (menu-only; other screens use
// BodyBottomRow directly).
func (g Geometry) menuBottomRow() int { return g.BodyBottomRow() - 1 }
```

`MenuCapacity` (~lines 65–69) — replace the last comment line:

```
// The blank separator above the PF legend is excluded via menuBottomRow.
```

with:

```
// The blank separator between the list and the meta band is excluded via
// menuBottomRow.
```

- [ ] **Step 4: Run the screens package tests — all PASS**

Run: `go test ./internal/screens/ -v`
Expected: PASS, including `TestMenuScreenAdminEntryClampedWithManyServices` and `TestMenuScreenAdminEntryNeverCollidesWhenFull` (the band's last row is `BodyBottomRow()`, which both allow) and all Task 1/2 tests.

- [ ] **Step 5: Commit (tests + implementation together — never commit red)**

```bash
go fmt ./internal/screens/
git add internal/screens/menu.go internal/screens/geometry.go internal/screens/menu_test.go
git commit -m "feat: flow menu meta band below the service list (#133)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 4: Full-tree verification

**Files:** none (verification only)

- [ ] **Step 1: Race-enabled full test run**

Run: `go test ./... -race`
Expected: all packages PASS (the server/presenter layers consume `MenuScreen` output without asserting band rows; failures here mean an unexpected coupling — investigate, do not paper over).

- [ ] **Step 2: gofmt gate + build**

Run: `gofmt -l . && go build ./...`
Expected: `gofmt -l` prints nothing; build succeeds.

- [ ] **Step 3: Commit (only if fixes were needed)**

If Steps 1–2 required changes, commit them with a `fix:`-prefixed message; otherwise nothing to commit.

### Task 5: Protocol-surface verification (s3270 smoke)

**Files:** none expected (`smoke.sh` menu checks are content-presence only — 15a/16e/16j check the string "User Settings", no rows)

- [ ] **Step 1: Run the smoke script**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all checks PASS. The script provisions its own throwaway instance (`-config` with TLS disabled).

- [ ] **Step 2: If a smoke check fails on meta-band placement**

Read the failing check's saved screen dump under the script's `$WORK` dir, confirm whether the assertion is stale (pinned to the old bottom-anchored rows) or the implementation is wrong. Fix accordingly (stale assertion → update `smoke.sh` and commit `test: update smoke assertions for flowed meta band (#133)`); implementation bug → return to Task 3.

- [ ] **Step 3: Note for the human pass**

c3270 visual QA (sparse menu, full menu, empty menu, locked user) remains the final word per CLAUDE.md; flag it in the PR description as done/pending.
