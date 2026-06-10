# Branding-forward Login Screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-architect the TN3270 login screen so a custom operator-supplied ASCII/EBCDIC art file fills the body, while preserving credential entry, the status block, the error line, and PF3.

**Architecture:** Mirror the existing MOTD/NEWS file feature. A new `BRANDING_FILE` system parameter holds an absolute path; the session reads it fresh on every login paint (8 KiB cap + absolute-path guard) and threads the split lines through `Presenter.Login` into the `screens.LoginScreen` builder, which lays out a four-row status header (rows 0–3), a vertically-centered/clipped branding region (rows 4..input−1), a shared credential row (second-to-last), and the PF-help row.

**Tech Stack:** Go 1.25, `github.com/racingmars/go3270`, `modernc.org/sqlite`. Test-first throughout. Run `go test ./... -race`.

**Spec:** `docs/superpowers/specs/2026-06-09-branding-login-screen-design.md` · **Issue:** [#101](https://github.com/coffeemuse/TN3270Proxy/issues/101)

---

## File Structure

| File | Responsibility | Change |
|------|----------------|--------|
| `internal/sysconfig/catalog.go` | `BRANDING_FILE` key + catalog entry | modify |
| `internal/sysconfig/catalog_test.go` | assert the new entry | modify |
| `internal/screens/news.go` | `SplitBranding` helper (reuses `newsMaxCols`) | modify |
| `internal/screens/news_test.go` | `SplitBranding` tests | modify |
| `internal/screens/geometry.go` | `LoginBrandingTop` / `LoginBrandingHeight` | modify |
| `internal/screens/geometry_test.go` | geometry helper tests | modify |
| `internal/screens/login.go` | new layout (header band, branding region, credential row) | rewrite |
| `internal/screens/login_test.go` | update existing + new layout tests | modify |
| `internal/server/session.go` | `BrandingRead` seam, `readBrandingCapped`, `loginBranding`, thread into `doLogin` | modify |
| `internal/server/presenter.go` | `go3270Presenter.Login` branding param | modify |
| `internal/server/session_test.go` | `Presenter.Login` interface + `fakePresenter` + per-paint test | modify |
| `internal/quickstart/paths.go` | `Layout.Branding` | modify |
| `internal/quickstart/branding.go` | `DefaultBranding()` art | create |
| `internal/quickstart/provision.go` | write file + set `BRANDING_FILE` | modify |
| `internal/quickstart/paths_test.go`, `provision_test.go`, `setup_test.go` | assertions | modify |
| `docs/ispf-style-guide.md`, `CLAUDE.md` | document login as a layout exception | modify |
| `.claude/skills/s3270-smoke-testing/smoke.sh` | login-screen assertions | modify |

Note: a new `sysconfig.Catalog` entry reaches existing DBs via `reconcileDefaults` on `store.Open` (CLAUDE.md) — **no migration** is needed.

---

## Task 1: `BRANDING_FILE` system parameter

**Files:**
- Modify: `internal/sysconfig/catalog.go`
- Test: `internal/sysconfig/catalog_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/sysconfig/catalog_test.go`:

```go
func TestCatalogHasBrandingFile(t *testing.T) {
	var e *Entry
	for i := range Catalog {
		if Catalog[i].Key == KeyBrandingFile {
			e = &Catalog[i]
			break
		}
	}
	if e == nil {
		t.Fatalf("Catalog missing %q entry", KeyBrandingFile)
	}
	if e.Default != "" {
		t.Errorf("BRANDING_FILE default = %q, want empty (disabled)", e.Default)
	}
	if e.Validate == nil || e.Validate("/any/path") != "" || e.Validate("") != "" {
		t.Errorf("BRANDING_FILE validator should accept any value")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sysconfig/ -run TestCatalogHasBrandingFile -v`
Expected: FAIL — `undefined: KeyBrandingFile`.

- [ ] **Step 3: Add the key constant and catalog entry**

In `internal/sysconfig/catalog.go`, add the constant next to `KeyMOTDFile`:

```go
// KeyBrandingFile is the system_config key whose value is the absolute path to
// the login branding/art file rendered in the login screen body. Empty disables
// the feature (blank region). Mirrors KeyMOTDFile.
const KeyBrandingFile = "BRANDING_FILE"
```

Add this `Entry` to `Catalog`, immediately after the `KeyMOTDFile` entry (keeps the identity group together):

```go
	{
		Key:     KeyBrandingFile,
		Label:   "Branding File:",
		Default: "",
		// Empty disables (blank region); any non-empty path is accepted.
		// Existence/readability is checked at read time, not here (matches MOTD).
		Validate: func(_ string) string { return "" },
	},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sysconfig/ -run TestCatalogHasBrandingFile -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sysconfig/catalog.go internal/sysconfig/catalog_test.go
git commit -m "feat(sysconfig): add BRANDING_FILE system parameter (#101)"
```

---

## Task 2: `SplitBranding` line parser

**Files:**
- Modify: `internal/screens/news.go`
- Test: `internal/screens/news_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/screens/news_test.go`:

```go
func TestSplitBranding(t *testing.T) {
	if got := SplitBranding(""); got != nil {
		t.Errorf("empty input = %v, want nil", got)
	}
	if got := SplitBranding("  \n\t\n"); got != nil {
		t.Errorf("whitespace-only = %v, want nil", got)
	}
	got := SplitBranding("ALPHA\r\nBETA\n\n")
	want := []string{"ALPHA", "BETA"} // trailing blank lines trimmed, \r stripped
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	// Lines hard-cut at column 79 (80 runes -> 79).
	long := SplitBranding(strings.Repeat("X", 100))
	if len(long) != 1 || len([]rune(long[0])) != 79 {
		t.Errorf("long line len = %d, want 79", len([]rune(long[0])))
	}
	// Interior blank lines are preserved (author owns vertical spacing).
	if got := SplitBranding("A\n\nB"); len(got) != 3 || got[1] != "" {
		t.Errorf("interior blank not preserved: %v", got)
	}
}
```

(`news_test.go` already imports `strings`; if not, add it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestSplitBranding -v`
Expected: FAIL — `undefined: SplitBranding`.

- [ ] **Step 3: Implement `SplitBranding`**

Add to `internal/screens/news.go` (reuses the existing `newsMaxCols = 79`):

```go
// SplitBranding turns raw branding-file text into the lines the login screen
// renders. It mirrors PaginateNews's line handling but does not paginate: lines
// split on "\n" (a trailing "\r" is stripped) and truncate at column 79.
// Trailing whitespace-only lines are dropped (so a conventional EOF newline
// doesn't skew vertical centering); leading and interior blank lines are
// preserved (the author owns vertical spacing). Returns nil when the text is
// empty or whitespace-only.
func SplitBranding(raw string) []string {
	lines := strings.Split(raw, "\n")
	for i, ln := range lines {
		ln = strings.TrimSuffix(ln, "\r")
		if r := []rune(ln); len(r) > newsMaxCols {
			ln = string(r[:newsMaxCols])
		}
		lines[i] = ln
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/screens/ -run TestSplitBranding -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/news.go internal/screens/news_test.go
git commit -m "feat(screens): add SplitBranding line parser (#101)"
```

---

## Task 3: Login geometry helpers

**Files:**
- Modify: `internal/screens/geometry.go`
- Test: `internal/screens/geometry_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/screens/geometry_test.go`:

```go
func TestLoginBrandingGeometry(t *testing.T) {
	cases := []struct {
		g          Geometry
		wantTop    int
		wantHeight int
	}{
		{Geometry{24, 80}, 4, 18},  // BodyBottomRow 22 -> height 22-4
		{Geometry{}, 4, 18},        // normalizes to 24x80
		{Geometry{32, 80}, 4, 26},  // BodyBottomRow 30 -> 26
		{Geometry{43, 80}, 4, 37},  // BodyBottomRow 41 -> 37
	}
	for _, c := range cases {
		if got := c.g.LoginBrandingTop(); got != c.wantTop {
			t.Errorf("%+v LoginBrandingTop = %d, want %d", c.g, got, c.wantTop)
		}
		if got := c.g.LoginBrandingHeight(); got != c.wantHeight {
			t.Errorf("%+v LoginBrandingHeight = %d, want %d", c.g, got, c.wantHeight)
		}
		// Region must sit strictly between the header band and the input row.
		if c.g.LoginBrandingTop()+c.g.LoginBrandingHeight() != c.g.BodyBottomRow() {
			t.Errorf("%+v region bottom should be input row %d", c.g, c.g.BodyBottomRow())
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/screens/ -run TestLoginBrandingGeometry -v`
Expected: FAIL — `g.LoginBrandingTop undefined`.

- [ ] **Step 3: Add the geometry helpers**

Append to `internal/screens/geometry.go`:

```go
// Login-screen branding region (login is a documented layout exception; see
// docs/ispf-style-guide.md). The region sits below the rows 0-3 status header
// and above the credential row (BodyBottomRow). LoginBrandingTop is fixed at 4;
// the region runs LoginBrandingTop .. BodyBottomRow-1, so its height is
// BodyBottomRow-4 (18 on MOD 2). The credential row rides BodyBottomRow and the
// PF-key help rides HelpRow, so both bottom-anchor on taller models.

// LoginBrandingTop is the first row of the login branding region.
func (g Geometry) LoginBrandingTop() int { return 4 }

// LoginBrandingHeight is how many rows the login branding region spans.
func (g Geometry) LoginBrandingHeight() int { return g.BodyBottomRow() - g.LoginBrandingTop() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/screens/ -run TestLoginBrandingGeometry -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/screens/geometry.go internal/screens/geometry_test.go
git commit -m "feat(screens): add login branding region geometry helpers (#101)"
```

---

## Task 4: Rewrite `LoginScreen`

**Files:**
- Rewrite: `internal/screens/login.go` (the `LoginScreen` func body + signature)
- Modify: `internal/screens/login_test.go`
- Modify: `internal/server/presenter.go:82` (keep build green — pass `nil` branding for now)

- [ ] **Step 1: Update existing tests for the new layout + signature, and add new tests**

In `internal/screens/login_test.go`:

**(a)** Every existing `LoginScreen(...)` call gains a `branding` arg before `errMsg`. Apply these exact edits:

- `TestLoginScreenFields`: `LoginScreen(DefaultGeometry, MenuStatus{}, nil, "")`
- `TestLoginScreenShowsError`: `LoginScreen(DefaultGeometry, MenuStatus{}, nil, "Invalid credentials")`
- `TestLoginScreenBands`: `LoginScreen(g, MenuStatus{}, nil, "err")` and change the error-row assertion from `f.Row != g.MessageRow()` to `f.Row != 1` (error now lives on row 1), updating the message text accordingly:

```go
		screen, _, _ := LoginScreen(g, MenuStatus{}, nil, "err")
		f, ok := fieldByName(screen, FieldError)
		if !ok || f.Row != 1 {
			t.Errorf("%+v: error row = %d, want row 1", g, f.Row)
		}
```

- `TestLoginScreenStatusBlock`: `LoginScreen(g, status, nil, "")` (assertions unchanged — labels are still at `StatusBlockCol`, just on rows 0–3).
- `TestLoginScreenCursor`: `LoginScreen(DefaultGeometry, MenuStatus{}, nil, "")` (unchanged otherwise — it compares against `cursorAt`).

**(b)** Replace `TestLoginScreenPalette` entirely with:

```go
func TestLoginScreenPalette(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, cur := LoginScreen(g, MenuStatus{}, nil, "bad creds")

	title, ok := fieldByContent(screen, "TN3270 GATEWAY LOGIN")
	if !ok {
		t.Fatal("missing title field")
	}
	if title.Row != 0 || !title.Intense || title.Color != go3270.White {
		t.Errorf("title = %+v, want row 0 white intense", title)
	}
	if title.Col != g.CenterCol(len("TN3270 GATEWAY LOGIN")) {
		t.Errorf("title not centered: col %d", title.Col)
	}
	label, ok := fieldByContent(screen, "User ID . . :")
	if !ok {
		t.Fatal("missing userid label")
	}
	if label.Row != g.BodyBottomRow() || label.Color != go3270.Turquoise {
		t.Errorf("userid label = %+v, want row %d turquoise", label, g.BodyBottomRow())
	}
	user, ok := fieldByName(screen, FieldUsername)
	if !ok {
		t.Fatal("missing username field")
	}
	if user.Color != go3270.Green || !user.Write || user.Row != g.BodyBottomRow() {
		t.Errorf("userid input = %+v, want green writable on row %d", user, g.BodyBottomRow())
	}
	msg, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatal("missing error field")
	}
	if msg.Row != 1 || msg.Color != go3270.Red || !msg.Intense {
		t.Errorf("message field = %+v, want row 1 red intense", msg)
	}
	if want := cursorAt(user); cur != want {
		t.Errorf("cursor = %+v, want %+v", cur, want)
	}
}
```

**(c)** Add a helper and three new tests:

```go
func fieldAt(s go3270.Screen, row, col int) (go3270.Field, bool) {
	for _, f := range s {
		if f.Row == row && f.Col == col {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestLoginScreenCredentialRow(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := LoginScreen(g, MenuStatus{}, nil, "")
	row := g.BodyBottomRow()

	pf, ok := fieldByName(screen, FieldPassword)
	if !ok || pf.Row != row {
		t.Fatalf("password field = %+v, want row %d", pf, row)
	}
	if !pf.Hidden {
		t.Errorf("password field must be Hidden")
	}
	// Password input runs from pf.Col+1 to the column before its closing stop
	// field; the requirement is the input reaches column 78 (stop field at 79).
	if _, ok := fieldAt(screen, row, 79); !ok {
		t.Errorf("missing password stop field at col 79 (input must reach col 78)")
	}
	if pf.Col >= 79 {
		t.Errorf("password attribute col = %d, leaves no input before col 79", pf.Col)
	}
	if _, ok := fieldByContent(screen, "Password . . :"); !ok {
		t.Errorf("missing password label")
	}
}

func TestLoginScreenBrandingCentered(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := LoginScreen(g, MenuStatus{}, []string{"AAA", "BBB"}, "")
	// 2 lines in an 18-row region (rows 4..21): top padding = (18-2)/2 = 8,
	// so the first line lands on row 4+8 = 12.
	a, ok := fieldByContent(screen, "AAA")
	if !ok || a.Row != 12 || a.Col != 0 {
		t.Errorf("AAA = %+v, want row 12 col 0", a)
	}
	b, ok := fieldByContent(screen, "BBB")
	if !ok || b.Row != 13 {
		t.Errorf("BBB = %+v, want row 13", b)
	}
}

func TestLoginScreenBrandingClipsTopAligned(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80} // region height 18
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = "L" + string(rune('A'+i%26))
	}
	screen, _, _ := LoginScreen(g, MenuStatus{}, lines, "")
	// Overflow: first line top-aligned at LoginBrandingTop (row 4)...
	first, ok := fieldByContent(screen, lines[0])
	if !ok || first.Row != g.LoginBrandingTop() {
		t.Errorf("first branding line = %+v, want row %d", first, g.LoginBrandingTop())
	}
	// ...and nothing renders past the row above the credential row.
	for _, f := range screen {
		if f.Col == 0 && f.Content != "" && f.Row >= g.BodyBottomRow() {
			t.Errorf("branding line %q on row %d overruns input row %d", f.Content, f.Row, g.BodyBottomRow())
		}
	}
}

func TestLoginScreenBrandingBlankWhenNil(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := LoginScreen(g, MenuStatus{}, nil, "")
	for _, f := range screen {
		if f.Col == 0 && f.Row >= g.LoginBrandingTop() && f.Row < g.BodyBottomRow() && f.Content != "" {
			t.Errorf("unexpected body content with nil branding: %+v", f)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/screens/ -run 'TestLoginScreen' -v`
Expected: FAIL — wrong arg count to `LoginScreen` / undefined behavior.

- [ ] **Step 3: Rewrite `LoginScreen`**

Replace the `LoginScreen` function in `internal/screens/login.go` with:

```go
// LoginScreen returns the branding-forward login screen, its validation rules,
// and the initial cursor (on the username field), sized for geom. Layout
// (0-based; login is a documented exception to the three-band convention — see
// docs/ispf-style-guide.md):
//
//	row 0     centered title              | Date  (col StatusBlockCol)
//	row 1     error line (col 2)          | Time
//	row 2                                 | System ID
//	row 3                                 | Release
//	rows 4..  branding (cols 0-79 verbatim, vertically centered; first-N
//	  input-1   top-aligned clip when taller than the region)
//	BodyBottomRow  User ID + Password on one line (password input reaches col 78)
//	HelpRow        PF3=Disconnect
//
// branding holds the already-split file lines (see SplitBranding); nil/empty
// renders a blank body. status supplies the right-hand info; the presenter
// stamps status.Now at paint time. errMsg, if non-empty, shows on row 1
// (truncated so it cannot collide with the Time block at StatusBlockCol).
func LoginScreen(geom Geometry, status MenuStatus, branding []string, errMsg string) (go3270.Screen, go3270.Rules, Cursor) {
	title := "TN3270 GATEWAY LOGIN"
	row := geom.BodyBottomRow() // credential row (second-to-last)
	username := go3270.Field{Row: row, Col: 15, Name: FieldUsername, Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		// Error on row 1; truncated to cols 2..57 so it never overlaps the
		// Time block at StatusBlockCol (60). Generic errors are far shorter.
		{Row: 1, Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: truncateRunes(errMsg, 56)},
		// Credential row: both fields share one line.
		{Row: row, Col: 2, Color: go3270.Turquoise, Content: "User ID . . :"},
		username,
		{Row: row, Col: 28}, // stop field: closes the username input (cols 16-27)
		{Row: row, Col: 30, Color: go3270.Turquoise, Content: "Password . . :"},
		{Row: row, Col: 44, Name: FieldPassword, Write: true, Hidden: true, Color: go3270.Green, Highlighting: go3270.Underscore},
		{Row: row, Col: 79}, // stop field: password input runs cols 45-78
		{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Disconnect"},
	}
	// Status header on rows 0-3 at StatusBlockCol (startRow 0 places the four
	// rows consecutively). No User ID (pre-login) and no Terminal, by design.
	screen = append(screen, statusBlock(geom, 0, []statusRow{
		{"Date . . :", julianDate(status.Now)},
		{"Time . . :", clockHM(status.Now)},
		{"System ID:", truncateRunes(status.SystemID, 7)},
		{"Release. :", truncateRunes(status.Release, 7)},
	})...)
	// Branding region: rows LoginBrandingTop .. (credential row - 1). Vertically
	// centered when it fits; first-N top-aligned clip when taller.
	top, h := geom.LoginBrandingTop(), geom.LoginBrandingHeight()
	lines := branding
	if len(lines) > h {
		lines = lines[:h]
	}
	pad := (h - len(lines)) / 2
	for i, ln := range lines {
		if ln == "" {
			continue // blank line: spacing only, no protected field
		}
		screen = append(screen, go3270.Field{Row: top + pad + i, Col: 0, Color: go3270.White, Content: ln})
	}
	rules := go3270.Rules{
		FieldUsername: {Validator: go3270.NonBlank, ErrorText: "User ID is required"},
	}
	return screen, rules, cursorAt(username)
}
```

- [ ] **Step 4: Keep the build green — update the lone caller**

In `internal/server/presenter.go`, update the `LoginScreen` call (currently line ~82) to pass `nil` branding (real wiring lands in Task 6):

```go
	screen, rules, cur := screens.LoginScreen(term.Geometry(), status, nil, errMsg)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/screens/ ./internal/server/ -v -run 'Login'`
Expected: PASS (screens layout tests green; server still compiles).

- [ ] **Step 6: Commit**

```bash
git add internal/screens/login.go internal/screens/login_test.go internal/server/presenter.go
git commit -m "feat(screens): branding-forward login layout (#101)"
```

---

## Task 5: Session branding read seam (`loginBranding`)

**Files:**
- Modify: `internal/server/session.go` (add `BrandingRead` field, `brandingReadCap`, `readBrandingCapped`, `loginBranding`)
- Test: `internal/server/session_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/session_test.go`:

```go
func TestLoginBranding(t *testing.T) {
	ctx := context.Background()
	mk := func(t *testing.T) *Session {
		st, err := store.Open(t.TempDir() + "/s.db")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		return &Session{Store: st}
	}

	t.Run("disabled when empty", func(t *testing.T) {
		s := mk(t)
		if got := s.loginBranding(ctx); got != nil {
			t.Errorf("empty path = %v, want nil", got)
		}
	})

	t.Run("relative path skipped", func(t *testing.T) {
		s := mk(t)
		s.Store.SetConfig(ctx, sysconfig.KeyBrandingFile, "branding.txt")
		s.BrandingRead = func(string) ([]byte, error) { t.Fatal("should not read a relative path"); return nil, nil }
		if got := s.loginBranding(ctx); got != nil {
			t.Errorf("relative path = %v, want nil", got)
		}
	})

	t.Run("unreadable skipped", func(t *testing.T) {
		s := mk(t)
		s.Store.SetConfig(ctx, sysconfig.KeyBrandingFile, "/abs/missing.txt")
		s.BrandingRead = func(string) ([]byte, error) { return nil, errors.New("nope") }
		if got := s.loginBranding(ctx); got != nil {
			t.Errorf("unreadable = %v, want nil", got)
		}
	})

	t.Run("valid splits lines", func(t *testing.T) {
		s := mk(t)
		s.Store.SetConfig(ctx, sysconfig.KeyBrandingFile, "/abs/branding.txt")
		s.BrandingRead = func(string) ([]byte, error) { return []byte("ALPHA\nBETA\n"), nil }
		got := s.loginBranding(ctx)
		if len(got) != 2 || got[0] != "ALPHA" || got[1] != "BETA" {
			t.Errorf("got %v, want [ALPHA BETA]", got)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestLoginBranding -v`
Expected: FAIL — `s.loginBranding undefined`.

- [ ] **Step 3: Implement the seam, capped reader, and `loginBranding`**

In `internal/server/session.go`, add the seam field to the `Session` struct, right after the `MOTDRead` field:

```go
	// BrandingRead reads the login branding file for loginBranding; nil selects
	// the capped os.ReadFile default (readBrandingCapped). Tests inject a fake.
	BrandingRead func(path string) ([]byte, error)
```

Add the cap constant and reader next to `readMOTDCapped`:

```go
// brandingReadCap bounds how much of the branding file is read (a few screens of
// art); an over-cap file is truncated, not rejected.
const brandingReadCap = 8 << 10 // 8 KiB

// readBrandingCapped is the default BrandingRead: it reads at most
// brandingReadCap bytes.
func readBrandingCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, brandingReadCap))
}
```

Add `loginBranding` (mirrors `maybeShowNews`'s config/guard logic) near `doLogin`:

```go
// loginBranding resolves the login branding lines fresh for one paint: it reads
// the BRANDING_FILE path, applies the same guards as the MOTD reader (missing
// key, empty, relative path, or unreadable -> nil + warn), and splits the file
// into lines (SplitBranding). nil means "render a blank body". Called once per
// login render so live file edits take effect without a restart.
func (s *Session) loginBranding(ctx context.Context) []string {
	path, err := s.Store.GetConfig(ctx, sysconfig.KeyBrandingFile)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log().Warn("branding config key unreadable; skipping", "error", err)
		}
		return nil
	}
	if strings.TrimSpace(path) == "" {
		return nil // disabled
	}
	if !filepath.IsAbs(path) {
		s.log().Warn("branding path not absolute; skipping", "path", path)
		return nil
	}
	read := s.BrandingRead
	if read == nil {
		read = readBrandingCapped
	}
	data, err := read(path)
	if err != nil {
		s.log().Warn("branding file unreadable; skipping", "path", path, "error", err)
		return nil
	}
	return screens.SplitBranding(string(data))
}
```

(`session.go` already imports `context`, `errors`, `io`, `os`, `path/filepath`, `strings`, `store`, `sysconfig`, and `screens`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestLoginBranding -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/session_test.go
git commit -m "feat(server): per-paint login branding read seam (#101)"
```

---

## Task 6: Thread branding through `Presenter.Login`

**Files:**
- Modify: `internal/server/session.go` (interface + `doLogin`)
- Modify: `internal/server/presenter.go` (`go3270Presenter.Login`)
- Modify: `internal/server/session_test.go` (`fakePresenter` + per-paint test)

- [ ] **Step 1: Write the failing test**

Add a `gotBranding` field to the `fakePresenter` struct in `internal/server/session_test.go` (next to `loginStatuses`):

```go
	gotBranding [][]string
```

Add the per-paint test:

```go
func TestSessionReadsBrandingPerLoginPaint(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "bad"},  // render 1
			{user: "alice", pass: "good"}, // render 2
			{quit: true},                  // render 3 (after menu logoff)
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	ctx := context.Background()
	s.Store.SetConfig(ctx, sysconfig.KeyBrandingFile, "/abs/branding.txt")
	var reads int
	s.BrandingRead = func(string) ([]byte, error) {
		reads++
		return []byte("HELLO\nWORLD"), nil
	}

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if reads < 2 {
		t.Errorf("branding read %d times, want once per login paint (>=2)", reads)
	}
	if len(p.gotBranding) < 2 {
		t.Fatalf("login painted %d times, want >=2", len(p.gotBranding))
	}
	for i, b := range p.gotBranding {
		if len(b) != 2 || b[0] != "HELLO" || b[1] != "WORLD" {
			t.Errorf("paint %d branding = %v, want [HELLO WORLD]", i, b)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestSessionReadsBrandingPerLoginPaint -v`
Expected: FAIL — `fakePresenter` has no branding, and `Presenter.Login` signature lacks it.

- [ ] **Step 3: Add the branding param to the interface, real presenter, fake, and `doLogin`**

In `internal/server/session.go`, change the `Presenter` interface `Login` line:

```go
	Login(conn net.Conn, term Term, status screens.MenuStatus, branding []string, errMsg string) (username, password string, quit bool, err error)
```

In `doLogin`, read branding fresh each iteration and pass it (replace the `s.Presenter.Login(...)` call):

```go
	for {
		branding := s.loginBranding(ctx)
		user, pass, quit, err := s.Presenter.Login(conn, term, status, branding, errMsg)
```

In `internal/server/presenter.go`, update `go3270Presenter.Login` signature and pass branding through:

```go
func (go3270Presenter) Login(conn net.Conn, term Term, status screens.MenuStatus, branding []string, errMsg string) (string, string, bool, error) {
	status.Now = time.Now() // paint-time clock, matching the menu status block
	screen, rules, cur := screens.LoginScreen(term.Geometry(), status, branding, errMsg)
```

(Leave the rest of the function body unchanged.)

In `internal/server/session_test.go`, update `fakePresenter.Login`:

```go
func (f *fakePresenter) Login(conn net.Conn, term Term, status screens.MenuStatus, branding []string, errMsg string) (string, string, bool, error) {
	f.gotTerms = append(f.gotTerms, term)
	f.loginStatuses = append(f.loginStatuses, status)
	f.loginErrors = append(f.loginErrors, errMsg)
	f.gotBranding = append(f.gotBranding, branding)
	r := f.logins[0]
	f.logins = f.logins[1:]
	return r.user, r.pass, r.quit, r.err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -v -run 'Login|Session'`
Expected: PASS (new per-paint test + all existing session tests).

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/presenter.go internal/server/session_test.go
git commit -m "feat(server): thread login branding through Presenter.Login (#101)"
```

---

## Task 7: Quickstart seeds an example branding file

**Files:**
- Modify: `internal/quickstart/paths.go`
- Create: `internal/quickstart/branding.go`
- Modify: `internal/quickstart/provision.go`
- Test: `internal/quickstart/paths_test.go`, `internal/quickstart/provision_test.go`, `internal/quickstart/setup_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/quickstart/paths_test.go`, add `"branding": l.Branding` to the path map being asserted in the first subtest, and `"branding": "/data/branding.txt"` to the expected map in the second (mirror the existing `"motd"` entries).

In `internal/quickstart/provision_test.go`, add `l.Branding` to the existence loop list (line ~24) and add this assertion after the MOTD config check (line ~62):

```go
	// BRANDING_FILE sysconfig points at the branding file.
	bv, err := st.GetConfig(context.Background(), sysconfig.KeyBrandingFile)
	if err != nil {
		t.Fatalf("get branding config: %v", err)
	}
	if bv != l.Branding {
		t.Errorf("BRANDING_FILE = %q, want %q", bv, l.Branding)
	}
```

In `internal/quickstart/setup_test.go`, add:

```go
func TestDefaultBrandingFitsRegion(t *testing.T) {
	lines := screens.SplitBranding(DefaultBranding())
	if len(lines) == 0 {
		t.Fatal("DefaultBranding produced no lines")
	}
	if h := screens.DefaultGeometry.LoginBrandingHeight(); len(lines) > h {
		t.Errorf("DefaultBranding has %d lines, exceeds MOD 2 region height %d", len(lines), h)
	}
	for i, ln := range lines {
		if len([]rune(ln)) > 79 {
			t.Errorf("line %d is %d cols, exceeds 79", i, len([]rune(ln)))
		}
	}
}
```

(Add `"github.com/coffeemuse/tn3270proxy/internal/screens"` to `setup_test.go`'s imports — the module path is case-sensitive; matches `go.mod`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/quickstart/ -v -run 'Branding|Path|Provision'`
Expected: FAIL — `l.Branding` / `DefaultBranding` undefined.

- [ ] **Step 3: Add the path, the art, and provisioning**

In `internal/quickstart/paths.go`, add `Branding string` to the `Layout` struct (after `MOTD`) and to `NewLayout`:

```go
		MOTD:      filepath.Join(dir, "motd.txt"),
		Branding:  filepath.Join(dir, "branding.txt"),
```

Create `internal/quickstart/branding.go`:

```go
package quickstart

// DefaultBranding is the example login art seeded on first run. It is authored
// pre-centered (the login screen renders columns 0-79 verbatim, no auto-indent)
// and sized to fit inside the MOD 2 branding region (18 rows). Rendered through
// the login screen body (see internal/screens/login.go).
func DefaultBranding() string {
	return "" +
		"            ____________________________________________            \n" +
		"           |                                            |           \n" +
		"           |        T N 3 2 7 0   G A T E W A Y         |           \n" +
		"           |                                            |           \n" +
		"           |            QUICK-START DEMO HOST           |           \n" +
		"           |____________________________________________|          \n" +
		"\n" +
		"                  Edit BRANDING_FILE to customize this art.\n"
}
```

In `internal/quickstart/provision.go`, after the MOTD block (line ~95), add:

```go
	// Branding file + sysconfig pointer (mirrors MOTD).
	if err := os.WriteFile(l.Branding, []byte(DefaultBranding()), 0o644); err != nil {
		return nil, fmt.Errorf("write branding: %w", err)
	}
	if err := st.SetConfig(ctx, sysconfig.KeyBrandingFile, l.Branding); err != nil {
		return nil, fmt.Errorf("set branding config: %w", err)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/quickstart/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/quickstart/
git commit -m "feat(quickstart): seed example login branding file (#101)"
```

---

## Task 8: Docs + smoke-test update

**Files:**
- Modify: `docs/ispf-style-guide.md`
- Modify: `CLAUDE.md`
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`

- [ ] **Step 1: Document the login layout exception**

In `docs/ispf-style-guide.md`, add a subsection alongside the MOTD/NEWS exception, describing the login screen's deliberate departure:

```markdown
### Login screen (branding-forward) — layout exception

The login screen deviates from the three-band convention to make room for
operator branding art:

- Rows 0–3 are a status header: centered title (row 0) and the
  Date / Time / System ID / Release block at the status column (rows 0–3).
- The red error line moves to **row 1** (col 2), truncated so it cannot collide
  with the Time block at the status column.
- Rows 4 .. `BodyBottomRow()-1` render the `BRANDING_FILE` contents (cols 0–79
  verbatim, no indent), vertically centered when shorter than the region and
  top-aligned/clipped when taller.
- The `User ID` and `Password` fields share `BodyBottomRow()` (the password
  field's input reaches col 78); `PF3=Disconnect` stays on `HelpRow()`.

Like MOTD/NEWS, this is an intentional, documented exception — not a model for
new ISPF-layer screens.
```

- [ ] **Step 2: Update CLAUDE.md package map**

Make these edits in `CLAUDE.md`:

- In the `internal/sysconfig` blurb, add `BRANDING_FILE` to the list of catalog params (next to MOTD/MFA issuer/System ID).
- In the `internal/screens` blurb, note that `LoginScreen` takes a `branding []string` and renders the branding-forward layout (status header rows 0–3, centered/clipped branding body, credential row second-to-last).
- In the `internal/server` blurb, note `loginBranding`/`BrandingRead` reads `BRANDING_FILE` fresh on each login paint (mirrors the MOTD reader).
- In the `internal/quickstart` blurb, add `branding.txt` to the provisioned artifacts.
- In the Gotchas "Screen layout convention" bullet, add the login screen next to MOTD/NEWS as a documented exception.

- [ ] **Step 3: Update the s3270 smoke test**

Read `.claude/skills/s3270-smoke-testing/smoke.sh` and update its login-screen section:

- Seed a known `BRANDING_FILE` for the test instance (write a small `branding.txt` and set the sysconfig key, or point at a fixture) so a branding line is assertable on screen.
- Update the login cursor assertion to the username field's new position: row = `Rows-2` (22 on MOD 2), col = 16.
- Update any hardcoded login field-row assertions (User ID / Password now share row `Rows-2`; error on row 1; status labels on rows 0–3).

- [ ] **Step 4: Commit**

```bash
git add docs/ispf-style-guide.md CLAUDE.md .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "docs: document branding-forward login layout + smoke assertions (#101)"
```

---

## Task 9: Full verification

- [ ] **Step 1: Race-enabled full test run**

Run: `go test ./... -race`
Expected: all packages PASS.

- [ ] **Step 2: Build both binaries**

Run: `go build ./... && go build -o bin/tn3270proxy ./cmd/tn3270proxy`
Expected: clean build.

- [ ] **Step 3: Live protocol pass (s3270 smoke)**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: PASS — login screen renders the branding art, status header on rows 0–3, credential row second-to-last, cursor on the username field, PF3 disconnects.

- [ ] **Step 4: Manual c3270 spot-check (final word on visual polish)**

Provision a quickstart data dir, `serve`, connect with `c3270`, and confirm: branding art is vertically centered, edits to `branding.txt` appear on the next paint without restart, long password input reaches the right edge, MOD 3/4/5 clients keep the credential row bottom-anchored.

- [ ] **Step 5: Final commit (if any doc/polish fixups)**

```bash
git add -A
git commit -m "chore: branding login screen verification fixups (#101)"
```

---

## Self-Review notes

- **Spec coverage:** sysconfig key (T1), SplitBranding (T2), geometry (T3), layout incl. status header/error row 1/credential row to col 78/centering+clip/cursor (T4), per-paint read + guards + cap (T5), threading + fresh-per-paint (T6), quickstart seeding (T7), docs + style-guide exception + smoke (T8), race + live verify (T9). All spec sections map to a task.
- **Type consistency:** `LoginScreen(geom, status, branding []string, errMsg string)` and `Presenter.Login(conn, term, status, branding []string, errMsg)` are consistent across T4/T6; `loginBranding(ctx) []string`, `BrandingRead func(string)([]byte,error)`, `readBrandingCapped`, `brandingReadCap`, `LoginBrandingTop()`, `LoginBrandingHeight()`, `KeyBrandingFile`, `Layout.Branding`, `DefaultBranding()` are used identically everywhere referenced.
- **Build-green between tasks:** T4 keeps `internal/server` compiling by passing `nil` branding; T5 adds an unused-but-valid method/func/const (legal at package scope); T6 flips the interface + caller + fake together.
- **No migration:** the new catalog entry reaches existing DBs via `reconcileDefaults` on `store.Open`.
