# PF1=Help on the Service Menu Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** PF1 at the service menu opens a pageable, read-only help viewer showing the DB-resident `HELP-MENU` document (admin-editable like MOTD/BRANDING), seeded with stock help text on every install.

**Architecture:** A third entry in `store.KnownDocuments` seeded with default content via `reconcileDefaults` (INSERT OR IGNORE — lands once, admin edits stick). A new three-band ISPF screen builder `screens.HelpScreen` + `HelpPageBounds` paging math. `Presenter.Menu` gains a lazy `help func() ([]string, error)` fetcher; PF1 is handled inside the presenter's menu loop (so the menu page survives the round-trip), running `runHelpViewer`; empty/error shows an inline `NO HELP AVAILABLE` message. The session builds the fetcher over `GetDocument` (fresh read per PF1 press).

**Tech Stack:** Go, go3270, modernc SQLite (via internal/store), s3270 smoke script.

**Spec:** `docs/superpowers/specs/2026-06-12-pf1-help-service-menu-design.md`

**Conventions that govern this work** (from CLAUDE.md — read it first):
- TDD; run `go test ./... -race`; `go fmt ./...` before every commit (CI has a gofmt gate).
- Unit tests assert field names/content/color, NOT row numbers.
- All screen titles are ASCII and uppercase (the viewer title is `SERVICE MENU HELP` — the spec's `HELP — Service Menu` em dash is avoided for EBCDIC codepage safety).
- New code-defined defaults go in `reconcileDefaults`, never a migration.
- Commit messages: conventional prefixes (`feat:`/`test:`/`docs:`), small and focused.

**Ripple effects discovered during planning (don't skip):**
- `HELP-MENU` sorts alphabetically BETWEEN `BRANDING` and `MOTD`, so the admin Documents member list gains a middle row. This breaks (a) `const motdRow = 1` in `internal/server/admin_documents_test.go` and (b) the smoke script's single-`Tab()` navigation to the MOTD row in scenarios t13set, t15off, and t19. Tasks 1 and 6 fix these.
- `TestListDocumentsAlwaysShowsKnownDocs` in `internal/store/documents_test.go` asserts exactly 2 documents. Task 1 updates it.

---

### Task 1: store — `HELP-MENU` document with stock default content

**Files:**
- Modify: `internal/store/documents.go` (constants block, ~line 43)
- Modify: `internal/store/migrate.go` (`reconcileDefaults`, ~line 378)
- Modify: `internal/server/admin_documents_test.go:34` (`motdRow` constant)
- Test: `internal/store/documents_test.go`, `internal/store/migrate_test.go`, `internal/quickstart/provision_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/store/documents_test.go`, add a row to the `NormalizeDocName` table test (the table near the top with `{"motd", DocMOTD, true}` entries):

```go
		{"help-menu", DocHelpMenu, true},
```

Replace `TestListDocumentsAlwaysShowsKnownDocs` (currently asserts 2 docs) with:

```go
func TestListDocumentsAlwaysShowsKnownDocs(t *testing.T) {
	st := newTestStore(t)
	docs, err := st.ListDocuments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 3 || docs[0].Name != DocBranding || docs[1].Name != DocHelpMenu || docs[2].Name != DocMOTD {
		t.Errorf("got %+v, want [BRANDING, HELP-MENU, MOTD] (alphabetical)", docs)
	}
	if docs[0].Content != "" || docs[0].LineCount() != 0 {
		t.Errorf("fresh BRANDING should be empty: %+v", docs[0])
	}
	if docs[1].Content != DefaultHelpMenuContent {
		t.Errorf("fresh HELP-MENU should hold the stock text, got %q", docs[1].Content)
	}
}
```

Add a new test (same file — it already imports `path/filepath` and `os` for the file tests; add them if the compiler complains):

```go
// The stock HELP-MENU seed lands exactly once: a fresh DB gets it, and an
// admin edit — including deliberately blanking the document — survives a
// reopen (the seed is INSERT OR IGNORE, never an update).
func TestHelpMenuStockSeedAndEditDurability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d, err := st.GetDocument(ctx, DocHelpMenu)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != DefaultHelpMenuContent || d.UpdatedBy != "" {
		t.Errorf("fresh seed: content match=%v updated_by=%q", d.Content == DefaultHelpMenuContent, d.UpdatedBy)
	}
	if err := st.SetDocument(ctx, DocHelpMenu, "", "ADMIN"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	d, err = st2.GetDocument(ctx, DocHelpMenu)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "" {
		t.Errorf("blank edit did not survive reopen: %q", d.Content)
	}
}
```

In `internal/store/migrate_test.go`, add (uses the existing `buildV2DB` helper; pass an empty config map):

```go
// A DB that predates the HELP-MENU entry gains the stock seed on Open via
// reconcileDefaults — no migration step involved.
func TestExistingDBGainsHelpMenuDocument(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	buildV2DB(t, dbPath, map[string]string{})
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d, err := st.GetDocument(context.Background(), DocHelpMenu)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != DefaultHelpMenuContent {
		t.Errorf("pre-existing DB should gain the stock HELP-MENU on open, got %q", d.Content)
	}
}
```

In `internal/quickstart/provision_test.go`, append inside `TestProvisionSeedsDocuments` (after the existing map loop):

```go
	// HELP-MENU is seeded by reconcileDefaults (stock text), not by quickstart.
	h, err := st.GetDocument(context.Background(), store.DocHelpMenu)
	if err != nil {
		t.Fatal(err)
	}
	if h.Content == "" {
		t.Error("HELP-MENU should hold the stock help text after provisioning")
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ ./internal/quickstart/ -run 'Document|HelpMenu|Provision' 2>&1 | tail -20`
Expected: compile FAILURE — `undefined: DocHelpMenu`, `undefined: DefaultHelpMenuContent`.

- [ ] **Step 3: Implement the store change**

In `internal/store/documents.go`, replace the constants block:

```go
// Known document names. The set is code-defined: reconcileDefaults seeds one
// row per name so the admin member list always shows them all.
const (
	DocMOTD     = "MOTD"
	DocBranding = "BRANDING"
	// DocHelpMenu is the service-menu help text shown by PF1. The HELP-<panel>
	// prefix is the naming convention for future per-panel help documents.
	DocHelpMenu = "HELP-MENU"
)

// KnownDocuments lists every valid document name.
var KnownDocuments = []string{DocMOTD, DocBranding, DocHelpMenu}

// DefaultHelpMenuContent is the stock HELP-MENU text seeded when the row is
// first created (see reconcileDefaults). The menu conventions it documents are
// app-defined, not site-specific, so every install gets working PF1 help out
// of the box; admins may edit or blank it and the edit sticks. Lines stay
// within the editor's 76 editable columns.
const DefaultHelpMenuContent = `The menu lists the services your account may use, one per row:
a selection number, the service name, and a description.

SELECTING A SERVICE

  Type the service's number in the Option field and press ENTER.
  Your terminal is connected to that service in a bridged session.

WHILE CONNECTED TO A SERVICE

  PA3  ends the bridged session and returns to this menu.
       Every other key belongs to the service you are using.

MENU KEYS

  PF1  shows this help.
  PF3  logs off and returns to the sign-on screen.
  PF7  pages up when the service list spans multiple pages.
  PF8  pages down.

OTHER MENU ENTRIES

  0    User Settings - change your password or manage MFA.
  A    Administration - shown only to administrators.

Ask your administrator if you need access to another service.`

// documentDefaults maps a document name to the content seeded when its row is
// first created. MOTD and BRANDING are site-specific and seed empty (absent
// from the map); HELP-MENU seeds the stock help text.
var documentDefaults = map[string]string{DocHelpMenu: DefaultHelpMenuContent}
```

(The stock text is 26 lines — two pages at MOD 2's 20-line help page size; the smoke test in Task 6 depends on it paginating, and on the literal strings `SELECTING A SERVICE` / `OTHER MENU ENTRIES`.)

In `internal/store/migrate.go` `reconcileDefaults`, replace the documents loop:

```go
	for _, name := range KnownDocuments {
		if _, err := s.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO documents (name, content) VALUES (?, ?)",
			name, documentDefaults[name]); err != nil {
			return fmt.Errorf("seed document %s: %w", name, err)
		}
	}
```

In `internal/server/admin_documents_test.go`, update the row constant (HELP-MENU now sorts between BRANDING and MOTD):

```go
// Documents list orders by name: BRANDING row 0, HELP-MENU row 1, MOTD row 2.
const motdRow = 2
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ ./internal/quickstart/ ./internal/server/ 2>&1 | tail -5`
Expected: `ok` for all three packages.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./... && git add -A && git commit -m "feat(store): HELP-MENU document seeded with stock service-menu help (#132)"
```

---

### Task 2: screens — help viewer builder + menu help-row text

**Files:**
- Create: `internal/screens/help.go`
- Create: `internal/screens/help_test.go`
- Modify: `internal/screens/geometry.go` (add `HelpPageSize`)
- Modify: `internal/screens/menu.go:178` (help-row text)
- Test: `internal/screens/geometry_test.go`, `internal/screens/menu_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/screens/help_test.go` (copy the GPL header comment block from `internal/screens/news.go` verbatim onto every new file in this plan):

```go
package screens

import (
	"fmt"
	"testing"

	"github.com/racingmars/go3270"
)

// helpTestLines yields n distinct lines; zero-padded so "LINE01" can never
// substring-match "LINE10".
func helpTestLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("LINE%02d CONTENT", i+1)
	}
	return out
}

func TestHelpPageBounds(t *testing.T) {
	g := DefaultGeometry // HelpPageSize 20 on MOD 2
	tests := []struct {
		name                          string
		total, page                   int
		wantPage, wantStart, wantEnd  int
		wantInd                       string
	}{
		{"empty", 0, 0, 0, 0, 0, "PAGE 1 OF 1"},
		{"single page", 5, 0, 0, 0, 5, "PAGE 1 OF 1"},
		{"negative clamps", 5, -3, 0, 0, 5, "PAGE 1 OF 1"},
		{"second page", 26, 1, 1, 20, 26, "PAGE 2 OF 2"},
		{"beyond end clamps", 26, 9, 1, 20, 26, "PAGE 2 OF 2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, start, end, ind := HelpPageBounds(g, tc.total, tc.page)
			if page != tc.wantPage || start != tc.wantStart || end != tc.wantEnd || ind != tc.wantInd {
				t.Errorf("got (%d,%d,%d,%q), want (%d,%d,%d,%q)",
					page, start, end, ind, tc.wantPage, tc.wantStart, tc.wantEnd, tc.wantInd)
			}
		})
	}
}

func TestHelpScreenTitleIndicatorAndHelpRow(t *testing.T) {
	screen, _ := HelpScreen(DefaultGeometry, "SERVICE MENU HELP", helpTestLines(3), 0)
	for _, want := range []string{"SERVICE MENU HELP", "PAGE 1 OF 1", "PF3=Return", "PF7=PgUp", "PF8=PgDn"} {
		if !screenContains(screen, want) {
			t.Errorf("help screen missing %q", want)
		}
	}
}

func TestHelpScreenPagesWindowAndColor(t *testing.T) {
	lines := helpTestLines(26) // 2 pages at MOD 2's 20-line page size
	p1, _ := HelpScreen(DefaultGeometry, "T", lines, 0)
	if !screenContains(p1, "LINE01 CONTENT") || screenContains(p1, "LINE21 CONTENT") {
		t.Error("page 1 should show lines 1..20 only")
	}
	if !screenContains(p1, "PAGE 1 OF 2") {
		t.Error("page 1 indicator wrong")
	}
	p2, _ := HelpScreen(DefaultGeometry, "T", lines, 1)
	if !screenContains(p2, "LINE21 CONTENT") || screenContains(p2, "LINE01 CONTENT") {
		t.Error("page 2 should show lines 21..26 only")
	}
	f, ok := fieldByContent(p2, "LINE21 CONTENT")
	if !ok || f.Color != go3270.Green {
		t.Errorf("body line field = %+v, want green protected text", f)
	}
}

func TestHelpScreenCursorHomes(t *testing.T) {
	_, cur := HelpScreen(DefaultGeometry, "T", helpTestLines(2), 0)
	if cur != (Cursor{}) {
		t.Errorf("cursor = %+v, want home {0,0} (no input field)", cur)
	}
}

func TestHelpScreenTruncatesWideLines(t *testing.T) {
	wide := []string{fmt.Sprintf("%080d", 1)} // 80 runes, must cut to 79
	screen, _ := HelpScreen(DefaultGeometry, "T", wide, 0)
	f, ok := fieldByContent(screen, wide[0][:79])
	if !ok {
		t.Fatal("missing truncated body line")
	}
	if len(f.Content) != 79 {
		t.Errorf("line length = %d, want 79", len(f.Content))
	}
}
```

In `internal/screens/geometry_test.go`, add:

```go
func TestHelpPageSize(t *testing.T) {
	if got := DefaultGeometry.HelpPageSize(); got != 20 {
		t.Errorf("MOD 2 help page size = %d, want 20", got)
	}
	if got := (Geometry{Rows: 43, Cols: 80}).HelpPageSize(); got != 39 {
		t.Errorf("MOD 4 help page size = %d, want 39", got)
	}
}
```

In `internal/screens/menu_test.go`, replace `TestMenuScreenHelpSaysLogoff` with:

```go
func TestMenuScreenHelpAdvertisesKeys(t *testing.T) {
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	for _, want := range []string{"PF1=Help", "PF3=Logoff", "PF7=PgUp", "PF8=PgDn", "PA3 returns here"} {
		if !screenContains(screen, want) {
			t.Errorf("menu help row should contain %q", want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/screens/ 2>&1 | tail -10`
Expected: compile FAILURE — `undefined: HelpPageBounds`, `undefined: HelpScreen`, `undefined: (Geometry).HelpPageSize`.

- [ ] **Step 3: Implement**

In `internal/screens/geometry.go`, after `NewsLinesPerPage`:

```go
// HelpPageSize is how many help-viewer text lines fit on one page: the full
// body band, rows BodyTopRow..BodyBottomRow (20 on MOD 2).
func (g Geometry) HelpPageSize() int { return g.BodyBottomRow() - g.BodyTopRow() + 1 }
```

Create `internal/screens/help.go` (GPL header omitted here — include it):

```go
package screens

import (
	"fmt"

	"github.com/racingmars/go3270"
)

// HelpPageBounds clamps page against total lines and the per-page help
// capacity, returning the clamped page, the [start,end) slice bounds for that
// page, and the "PAGE x OF y" indicator. It is the single source of help
// paging math, mirroring MenuPageBounds: HelpScreen uses it to render the
// window, and the viewer loop uses it to clamp its stored page so PF7/PF8 are
// no-ops at the ends.
func HelpPageBounds(geom Geometry, total, page int) (clamped, start, end int, indicator string) {
	if total == 0 {
		return 0, 0, 0, "PAGE 1 OF 1"
	}
	size := geom.HelpPageSize()
	maxPage := (total - 1) / size
	if page > maxPage {
		page = maxPage
	}
	if page < 0 {
		page = 0
	}
	start = page * size
	end = min(start+size, total)
	return page, start, end, fmt.Sprintf("PAGE %d OF %d", page+1, maxPage+1)
}

// HelpScreen renders one page of a read-only help document as a three-band
// ISPF screen (docs/dev/ispf-style-guide.md): centered title and right-aligned
// PAGE x OF y indicator on the title row, text lines (protected, green,
// truncated at column 79) filling the body band, and the PF-key help on the
// last row. This is deliberately NOT the chrome-less MOTD/news pager — help is
// an ISPF-layer screen. There is no input field; the cursor homes to {0,0}
// (the empty-admin-list precedent). The caller drives paging with PF7/PF8;
// out-of-range pages clamp (see HelpPageBounds).
func HelpScreen(geom Geometry, title string, lines []string, page int) (go3270.Screen, Cursor) {
	_, start, end, indicator := HelpPageBounds(geom, len(lines), page)
	// Right-align the indicator so its content ends at col 79 (a Field's Col
	// is the attribute byte, so content starts at Col+1) — the menu convention.
	indicatorCol := max(79-len(indicator), 0)
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: geom.TitleRow(), Col: indicatorCol, Color: go3270.Turquoise, Content: indicator},
	}
	row := geom.BodyTopRow()
	for i := start; i < end; i++ {
		screen = append(screen, go3270.Field{
			Row: row, Col: 0, Color: go3270.Green, Content: truncateRunes(lines[i], 79),
		})
		row++
	}
	screen = append(screen, go3270.Field{
		Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise,
		Content: "PF3=Return   PF7=PgUp  PF8=PgDn",
	})
	return screen, Cursor{Row: 0, Col: 0}
}
```

In `internal/screens/menu.go`, change the help-row field content (currently `"PF3=Logoff   PF7=PgUp  PF8=PgDn   (PA3 returns here from a session)"`):

```go
		go3270.Field{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF1=Help  PF3=Logoff  PF7=PgUp  PF8=PgDn  (PA3 returns here from a session)"},
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/screens/ 2>&1 | tail -3`
Expected: `ok  	github.com/coffeemuse/tn3270proxy/internal/screens`

- [ ] **Step 5: Format and commit**

```bash
go fmt ./... && git add -A && git commit -m "feat(screens): help viewer screen + PF1 advertised on the menu help row (#132)"
```

---

### Task 3: server — PF1 in the menu loop, lazy fetcher through the Presenter seam

**Files:**
- Modify: `internal/server/session.go` (Presenter interface ~line 48; menu loop ~line 402; new `helpMenuLines` method near `loginBranding` ~line 229)
- Modify: `internal/server/presenter.go` (`go3270Presenter.Menu` ~line 101; new `runHelpViewer`)
- Modify: `internal/server/session_test.go:115` (`fakePresenter.Menu` signature)
- Create: `internal/server/session_help_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/server/session_help_test.go` (GPL header; mirrors `session_branding_test.go`):

```go
package server

import (
	"context"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

// helpMenuLines is the session-side PF1 fetcher: a fresh DB read per call (the
// fresh-on-use convention), nil/empty when the document is blanked.
func TestHelpMenuLines(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Session{Store: st}

	// Out of the box: the stock seed renders (non-empty).
	lines, err := s.helpMenuLines(ctx)
	if err != nil || len(lines) == 0 {
		t.Fatalf("stock seed: lines=%d err=%v, want non-empty", len(lines), err)
	}

	// Fresh read: an admin edit shows up on the very next PF1.
	if err := st.SetDocument(ctx, store.DocHelpMenu, "ONE\nTWO", "TEST"); err != nil {
		t.Fatal(err)
	}
	lines, err = s.helpMenuLines(ctx)
	if err != nil || len(lines) != 2 || lines[0] != "ONE" || lines[1] != "TWO" {
		t.Errorf("after edit: got %v (err %v), want [ONE TWO]", lines, err)
	}

	// Blanked: empty result → the presenter shows NO HELP AVAILABLE inline.
	if err := st.SetDocument(ctx, store.DocHelpMenu, "", "TEST"); err != nil {
		t.Fatal(err)
	}
	lines, err = s.helpMenuLines(ctx)
	if err != nil || len(lines) != 0 {
		t.Errorf("blanked: got %v (err %v), want empty", lines, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run TestHelpMenuLines 2>&1 | tail -5`
Expected: compile FAILURE — `s.helpMenuLines undefined`.

- [ ] **Step 3: Implement the session side**

In `internal/server/session.go`, add next to `loginBranding` (~line 240):

```go
// helpMenuLines resolves the service-menu help text fresh for one PF1 press
// from the HELP-MENU document (one SQLite row read; admin edits take effect on
// the next press, no restart). Empty means "no help available" — the presenter
// renders an inline message instead of the viewer.
func (s *Session) helpMenuLines(ctx context.Context) ([]string, error) {
	doc, err := s.Store.GetDocument(ctx, store.DocHelpMenu)
	if err != nil {
		return nil, err
	}
	return doc.Lines(), nil
}
```

Update the `Presenter` interface's `Menu` method (session.go ~line 48):

```go
	// Menu renders the service menu and returns the user's selection. help is
	// the lazy PF1 fetcher: invoked only when PF1 is pressed, returning the
	// help-document lines; nil/empty lines or an error render an inline
	// NO HELP AVAILABLE message on the menu instead of opening the viewer.
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, settingsLocked bool, status screens.MenuStatus, errMsg string, help func() ([]string, error)) (selected *store.Service, choice menuChoice, err error)
```

In the menu loop (session.go, just above `errMsg := ""` / the `menu:` label, ~line 387), build the closure and thread it:

```go
		help := func() ([]string, error) { return s.helpMenuLines(ctx) }
		errMsg := ""
	menu:
```

and change the call site (~line 402):

```go
			selected, choice, err := s.Presenter.Menu(conn, term, services, isAdmin, settingsLocked, status, errMsg, help)
```

- [ ] **Step 4: Implement the presenter side**

In `internal/server/presenter.go`, update `go3270Presenter.Menu`: new signature, `go3270.AIDPF1` added to the silent exits, and a PF1 case:

```go
func (go3270Presenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, settingsLocked bool, status screens.MenuStatus, errMsg string, help func() ([]string, error)) (*store.Service, menuChoice, error) {
```

silent-exits line inside the loop becomes:

```go
				withSilentExits([]go3270.AID{go3270.AIDPF1, go3270.AIDPF3, go3270.AIDPF7, go3270.AIDPF8}),
```

and in the `switch resp.AID` block, after the PF8 case:

```go
		case go3270.AIDPF1:
			lines, herr := fetchHelp(help)
			if herr != nil || len(lines) == 0 {
				errMsg = "NO HELP AVAILABLE"
				continue
			}
			if verr := runHelpViewer(conn, term, helpMenuTitle, lines); verr != nil {
				return nil, menuReprompt, verr // disconnect/idle, classified by the session
			}
			errMsg = ""
			continue
```

Add to the same file (below `Menu`):

```go
// helpMenuTitle is the service-menu help panel's title; per-panel titles
// arrive with future per-panel help documents.
const helpMenuTitle = "SERVICE MENU HELP"

// fetchHelp guards the lazy fetcher (a nil func means no help is wired).
func fetchHelp(help func() ([]string, error)) ([]string, error) {
	if help == nil {
		return nil, nil
	}
	return help()
}

// runHelpViewer drives the read-only help viewer over an already-fetched
// document: PF7/PF8 page (clamped by HelpPageBounds), Enter or PF3 return to
// the caller, PA keys are silent no-ops (handleScreen). A non-nil error is a
// disconnect or an idle timeout — the caller classifies it like any other
// menu render error.
func runHelpViewer(conn net.Conn, term Term, title string, lines []string) error {
	geom := term.Geometry()
	page := 0
	for {
		// Clamp the stored page each render so PF7 at the top / PF8 at the
		// bottom re-present the same page — the menu's paging convention.
		page, _, _, _ = screens.HelpPageBounds(geom, len(lines), page)
		screen, cur := screens.HelpScreen(geom, title, lines, page)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3, go3270.AIDPF7, go3270.AIDPF8}),
				"", cur.Row, cur.Col, conn, term.dev, term.codepage(),
			)
		})
		if err != nil {
			return err
		}
		switch resp.AID {
		case go3270.AIDPF7:
			page--
		case go3270.AIDPF8:
			page++
		default: // Enter or PF3 — back to the menu (no input to submit)
			return nil
		}
	}
}
```

Update the fake in `internal/server/session_test.go` (line 115) to the new signature, recording the fetcher so tests can assert it is threaded and callable:

```go
func (f *fakePresenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, settingsLocked bool, status screens.MenuStatus, errMsg string, help func() ([]string, error)) (*store.Service, menuChoice, error) {
	f.gotTerms = append(f.gotTerms, term)
	f.menuErrors = append(f.menuErrors, errMsg)
	f.gotAdminFlag = append(f.gotAdminFlag, admin)
	f.gotSettingsLocked = append(f.gotSettingsLocked, settingsLocked)
	f.gotStatus = append(f.gotStatus, status)
	f.gotHelpFns = append(f.gotHelpFns, help)
	r := f.menuPicks[0]
	f.menuPicks = f.menuPicks[1:]
	ch := r.choice
	if r.quit {
		ch = menuQuit
	}
	return r.sel, ch, r.err
}
```

and add the field to the `fakePresenter` struct (top of session_test.go):

```go
	gotHelpFns []func() ([]string, error) // help fetcher passed to each Menu call
```

- [ ] **Step 5: Run the full server tests (race) to verify everything passes**

Run: `go test ./internal/server/ -race 2>&1 | tail -3`
Expected: `ok  	github.com/coffeemuse/tn3270proxy/internal/server`. The interface change is compile-enforced everywhere; the only other `Presenter` implementation is `go3270Presenter` and the only fake is `fakePresenter`.

Then: `go build ./... && go test ./... -race 2>&1 | tail -8`
Expected: all packages `ok`.

- [ ] **Step 6: Format and commit**

```bash
go fmt ./... && git add -A && git commit -m "feat(server): PF1 help viewer in the menu loop with lazy DB fetcher (#132)"
```

---

### Task 4: admin import form — no pre-filled path for HELP-MENU

**Files:**
- Modify: `internal/server/admin_documents.go` (`docImportPathKey` ~line 137, `documentImport` ~line 110)
- Test: `internal/server/admin_documents_test.go`

`docImportPathKey` currently falls through to `KeyMOTDFile` for any non-BRANDING name, which would wrongly pre-fill HELP-MENU's import form with the MOTD path. Per the spec, HELP-MENU's path field pre-fills empty.

- [ ] **Step 1: Write the failing test**

Add to `internal/server/admin_documents_test.go`:

```go
func TestDocImportPathKey(t *testing.T) {
	if k := docImportPathKey(store.DocMOTD); k != sysconfig.KeyMOTDFile {
		t.Errorf("MOTD key = %q, want %q", k, sysconfig.KeyMOTDFile)
	}
	if k := docImportPathKey(store.DocBranding); k != sysconfig.KeyBrandingFile {
		t.Errorf("BRANDING key = %q, want %q", k, sysconfig.KeyBrandingFile)
	}
	if k := docImportPathKey(store.DocHelpMenu); k != "" {
		t.Errorf("HELP-MENU key = %q, want \"\" (blank pre-fill, no sysconfig param)", k)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run TestDocImportPathKey 2>&1 | tail -5`
Expected: FAIL — `HELP-MENU key = "MOTD_FILE", want ""`.

- [ ] **Step 3: Implement**

Replace `docImportPathKey` in `internal/server/admin_documents.go`:

```go
// docImportPathKey maps a document name to its import-path sysconfig key, or
// "" when the document has no configured default path (HELP-MENU; only the
// legacy MOTD/BRANDING cutover params exist — see internal/sysconfig).
func docImportPathKey(doc string) string {
	switch doc {
	case store.DocBranding:
		return sysconfig.KeyBrandingFile
	case store.DocMOTD:
		return sysconfig.KeyMOTDFile
	}
	return ""
}
```

and in `documentImport`, replace the `defaultPath` line so an empty key skips the config read:

```go
	defaultPath := ""
	if key := docImportPathKey(d.Name); key != "" {
		defaultPath, _ = f.store.GetConfig(ctx, key) // "": blank pre-fill
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/server/ 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./... && git add -A && git commit -m "fix(admin): blank import-path pre-fill for documents without a path param (#132)"
```

---### Task 5: documentation

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/admin/08-motd.md`
- Modify: `docs/user/07-navigation.md`

No code. Read each file before editing and integrate at the natural spot; keep each doc's existing voice. Refer to docs by name, not URL.

- [ ] **Step 1: Update CLAUDE.md**

Three precise edits:

1. In the **"What this is"** paragraph, extend the MOTD/branding sentence: after `MOTD and login branding live in the DB (`documents` table, names `MOTD` / `BRANDING`)` add the third name `MOTD / `BRANDING` / `HELP-MENU`` and append a sentence: `PF1 at the service menu opens a pageable help viewer rendering the `HELP-MENU` document (stock text seeded at first run; the `HELP-<panel>` name prefix is the convention for future per-panel help).`
2. In the **package map**, `internal/store` documents.go description: note that `HELP-MENU` seeds with stock content via `documentDefaults` (INSERT OR IGNORE — admin edits/blanks stick), unlike the empty MOTD/BRANDING rows.
3. In the **package map**, `internal/screens` entry: mention `HelpScreen/HelpPageBounds (PF1 help viewer, three-band ISPF, PAGE x OF y)` ; `internal/server` entry: mention PF1 handled inside `go3270Presenter.Menu` via a lazy `help` fetcher param + `runHelpViewer`, empty → inline `NO HELP AVAILABLE`.

- [ ] **Step 2: Update docs/admin/08-motd.md**

Add a short section introducing the third document:

> ## HELP-MENU: the service-menu help text
>
> `HELP-MENU` is the text behind **PF1=Help** on the service menu. Unlike MOTD
> and BRANDING it ships with stock content describing the menu conventions, so
> PF1 works out of the box. Edit it like any other document (option **8
> Documents**, `E` to edit, `I` to import — the import form's path field starts
> blank; there is no import-path system parameter for it), or from the command
> line with `doc export`/`doc import -name HELP-MENU`. Blanking the document
> disables help: PF1 then shows `NO HELP AVAILABLE` on the menu message line.
> Your edits (including blanking) survive restarts and upgrades.

- [ ] **Step 3: Update docs/user/07-navigation.md**

Document the key where the menu keys are described:

> **PF1** on the service menu opens a help screen describing the menu. PF7/PF8
> page through it; PF3 (or ENTER) returns to the menu exactly where you left it.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "docs: PF1 service-menu help — admin, user, and CLAUDE.md notes (#132)"
```

---

### Task 6: s3270 smoke coverage (+ fix Tab navigation broken by the new list row)

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`

Two parts: (a) repair the three scenarios that reach the MOTD row in the Documents member list with a single `Tab()` — HELP-MENU now sits between BRANDING (cursor home) and MOTD, so those need TWO `Tab()`s; (b) add scenario 20 for the help viewer.

- [ ] **Step 1: Fix the member-list Tab navigation (4 spots)**

Each Documents member list row has exactly one input (the 2-char CMD field), so one `Tab()` = one row down from the cursor home (BRANDING, row 0). MOTD moved from row 1 to row 2:

1. **t13set** (~line 517): the lone `Tab()` between `String(8)`/`Enter` and `String(I)` becomes `Tab()` `Tab()` (two lines).
2. **t15off** (~line 713): same — the `Tab()` before `String(I)` becomes two `Tab()`s.
3. **t19** (~line 946): the `Tab()` before the FIRST `String(E)` becomes two `Tab()`s.
4. **t19** (~line 956): the `Tab()` before the SECOND `String(E)` (after `MOTD SAVED` returns to the list) becomes two `Tab()`s.

Do NOT touch the `Tab()`s inside the editor itself (text-field navigation after `EDIT MOTD` opens) — only the member-list → MOTD-row hops. Update each scenario's walk comment ("Tab to the MOTD row" → "Tab twice to the MOTD row (HELP-MENU sits between BRANDING and MOTD)").

Also add one member-list assertion next to 19b/19c:

```bash
check "19c2 member list shows HELP-MENU row" "HELP-MENU" "$WORK/t19.out"
```

- [ ] **Step 2: Add scenario 20 at the end of the script (after scenario 19's checks, before the summary block)**

```bash
# --- 20. PF1 help viewer (GH #132): the stock HELP-MENU document (26 lines →
# 2 pages at MOD 2's 20-line help page size) opens from the service menu. The
# viewer is all protected text with the cursor homed to {0,0} — no input field
# — so use Wait(Unlock)+Wait(1,seconds) after AID keys, like the MOTD gate.
# Uses the pager user ON MENU PAGE 2 so the return check proves the menu page
# survives the help round-trip. NOTE: scenario 19 re-populated the MOTD (1
# line = a 1-page gate), so the login needs one ENTER to clear it. ---
s3 t20 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(5,InputField)
PF(8)
Wait(5,InputField)
PF(1)
Wait(Unlock)
Wait(1,seconds)
Ascii()
PF(8)
Wait(Unlock)
Wait(1,seconds)
Ascii()
PF(8)
Wait(Unlock)
Wait(1,seconds)
Ascii()
PF(3)
Wait(5,InputField)
Ascii()
Quit()
EOF
check  "20a PF1 opens the help viewer"      "SERVICE MENU HELP"    "$WORK/t20.out"
check  "20b help page 1 indicator"          "PAGE 1 OF 2"          "$WORK/t20.out"
check  "20c help page 1 stock content"      "SELECTING A SERVICE"  "$WORK/t20.out"
check  "20d help cursor homed (0,0)"        "I 2 24 80 0 0 "       "$WORK/t20.out"
check  "20e PF8 pages to help page 2"       "PAGE 2 OF 2"          "$WORK/t20.out"
check  "20f help page 2 stock content"      "OTHER MENU ENTRIES"   "$WORK/t20.out"
ncheck "20g PF8 clamps at the last page"    "PAGE 3 OF"            "$WORK/t20.out"
# The menu was on page 2 (ITEMS 18 TO 22) before PF1; PF3 must restore it.
check  "20h PF3 returns to menu page 2"     "ITEMS 18 TO 22 OF 22" "$WORK/t20.out"
check  "20i menu help row advertises PF1"   "PF1=Help"             "$WORK/t20.out"
```

(20h is order-safe: the earlier captures in t20.out are help screens, which never contain an `ITEMS` indicator.)

- [ ] **Step 3: Run the smoke script**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh 2>&1 | tail -40`
Expected: every existing scenario still PASSes (especially 13z `MOTD IMPORTED`, 15f, and all of 19 — these prove the Tab fixes), plus `PASS: 20a` … `PASS: 20i`, and a `FAIL=0` style summary (the script prints PASS/FAIL counts at the end). If s3270 is not installed, the skill (`.claude/skills/s3270-smoke-testing/SKILL.md`) documents the prerequisite — do not skip this step silently; report it.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "test(smoke): PF1 help viewer scenario; fix Documents-list Tab hops for the new row (#132)"
```

---

### Task 7: final verification

- [ ] **Step 1: Full build, tests with race detector, vet, gofmt**

```bash
go build ./... && go vet ./... && go test ./... -race && gofmt -l .
```

Expected: build/vet/test clean; `gofmt -l` prints NOTHING (CI gate).

- [ ] **Step 2: Acceptance-criteria sweep (from the spec)**

Verify each, citing the test/smoke check that proves it:
- HELP-MENU in admin Documents list, E/I audited, CLI round-trip → Task 1 store tests + scenario 19c2 + (CLI verbs operate by name, no change — `cmd/tn3270proxy/doc_test.go` still green).
- Fresh AND existing DBs seeded once, edits/blanks durable → `TestHelpMenuStockSeedAndEditDurability`, `TestExistingDBGainsHelpMenuDocument`.
- PF1 opens viewer, PF7/PF8 page, PF3/Enter returns, same menu page → scenario 20, `TestHelpPageBounds`.
- Menu help row advertises PF1 → `TestMenuScreenHelpAdvertisesKeys`, 20i.
- Empty document → inline message → `TestHelpMenuLines` (blanked case) + the presenter's `NO HELP AVAILABLE` branch (smoke-only path; exercised implicitly if you want by blanking via the editor — optional).
- Tests assert names/content/color not rows → review the new tests against the convention.

- [ ] **Step 3: Use the superpowers:finishing-a-development-branch skill to wrap up**
