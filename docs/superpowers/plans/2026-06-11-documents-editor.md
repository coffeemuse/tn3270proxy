# DB-Backed Documents + ISPF-Style Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move MOTD and login-branding content from disk files into a `documents` DB table, edited via a new admin "Documents" member list with an ISPF-style line editor (I/D/R prefix commands) and an import-from-server-file flow (3270 screen + CLI).

**Architecture:** A new `documents` store file + migration v3 (one-time file import) make the DB the source of truth; the session render path reads documents instead of files. A new `ui3270` editor widget (EditorView/EditorAction/RunEditor) follows the existing List/Form widget pattern. The admin flow gains `admin_documents.go` riding RunList/RunForm/RunEditor. The two sysconfig path params survive, relabeled as *import paths*.

**Tech Stack:** Go, modernc SQLite, go3270, existing internal packages (`store`, `sysconfig`, `ui3270`, `screens`, `server`, `quickstart`).

**Spec:** `docs/superpowers/specs/2026-06-11-documents-editor-design.md`

**House rules that apply to every task:** TDD (test first, watch it fail, then implement), `go test ./... -race` before each commit, `go fmt ./...` (CI gate), GPL header comment block at the top of every NEW `.go` file (copy the 18-line header verbatim from `internal/store/config.go`), conventional commit prefixes.

---

## File structure (what gets created/modified)

```
internal/store/documents.go            NEW   Document type, CRUD, caps, ReadDocumentFile
internal/store/documents_test.go       NEW
internal/store/migrate.go              MOD   migration v3 (table + one-time import), reconcileDefaults seeds rows
internal/store/migrate_test.go         MOD   v3 import tests
internal/store/audit.go                MOD   AuditDocUpdate/AuditDocImport kinds
internal/sysconfig/catalog.go          MOD   relabel MOTD_FILE/BRANDING_FILE as import paths
internal/ui3270/editor.go              NEW   applyPrefix, RunEditor, EditorConfig
internal/ui3270/editor_test.go         NEW
internal/ui3270/editorscreen.go        NEW   buildEditorScreen + editorAction
internal/ui3270/editorscreen_test.go   NEW
internal/ui3270/types.go               MOD   EditorLine/EditorView/EditorAction, Renderer.Editor
internal/ui3270/renderer.go            MOD   go3270Renderer.Editor
internal/server/session.go             MOD   maybeShowNews/loginBranding read documents; delete file seams
internal/server/session_motd_test.go   MOD   seed documents instead of fake readers
internal/server/session_branding_test.go MOD same
internal/screens/admin.go              MOD   menu option 8 + FieldPath constant
internal/server/presenter_admin.go     MOD   AdminMenu accepts "8"
internal/server/admin.go               MOD   AdminStore + case 8 + record() helper
internal/server/admin_documents.go     NEW   member list, editor wiring, import form
internal/server/admin_documents_test.go NEW
cmd/tn3270proxy/doc.go                 NEW   doc import|export subcommand
cmd/tn3270proxy/doc_test.go            NEW
cmd/tn3270proxy/main.go                MOD   dispatch "doc"
internal/quickstart/provision.go       MOD   seed documents rows at provision time
internal/quickstart/provision_test.go  MOD
docs/admin/08-motd.md                  MOD   rewritten as the Documents chapter
docs/admin/02-admin-ui.md              MOD   menu map
docs/admin/07-system-parameters.md     MOD   param semantics
docs/admin/14-cli.md                   MOD   doc import/export
docs/admin/15-operations-upgrades.md   MOD   v3 migration note
docs/dev/ispf-style-guide.md           MOD   editor screen conventions
CLAUDE.md                              MOD   package map + commands
.claude/skills/s3270-smoke-testing/smoke.sh MOD  editor/import smoke steps
```

Layout constants the editor relies on (decided in the spec):
- Row layout per editor line: prefix attr **col 0**, prefix input **cols 1–2**, text attr **col 3**, text **cols 4–79** → **76 editable text columns** (`editorTextMax = 76`).
- Lines wider than 76 runes render **protected, yellow**, content never truncated in storage (only the display clips); the `D`/`I`/`R` prefix still works on them.
- Editable text fields are written as content with **no space padding** — go3270 leaves the rest of the field as NULs, which is what makes native 3270 insert mode work. Never pad lines to width with spaces.
- A **stop field at `{row: 4+len(lines), col: 0}`** terminates the last text field (otherwise it would wrap on through the blank rows into the legend).

---

### Task 1: store — Document type, CRUD, caps, file reader, audit kinds

**Files:**
- Create: `internal/store/documents.go`
- Create: `internal/store/documents_test.go`
- Modify: `internal/store/audit.go` (two new kind constants)
- Modify: `internal/store/migrate.go` (only the bare CREATE TABLE in this task — see step 3; the import logic is Task 2)

- [ ] **Step 1: Write the failing tests**

`internal/store/documents_test.go` (GPL header, then):

```go
package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newDocStore opens a fresh store in a temp dir (mirrors the helper style used
// by the other store tests; reuse an existing helper if one fits).
func newDocStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestNormalizeDocName(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"motd", DocMOTD, true},
		{" Branding ", DocBranding, true},
		{"MOTD", DocMOTD, true},
		{"bogus", "", false},
		{"", "", false},
	} {
		got, err := NormalizeDocName(tc.in)
		if tc.ok && (err != nil || got != tc.want) {
			t.Errorf("NormalizeDocName(%q) = %q, %v; want %q, nil", tc.in, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Errorf("NormalizeDocName(%q): want error", tc.in)
		}
	}
}

func TestSetGetDocument(t *testing.T) {
	st := newDocStore(t)
	ctx := context.Background()
	if err := st.SetDocument(ctx, "motd", "HELLO\nWORLD", "ADMIN"); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDocument(ctx, DocMOTD)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "HELLO\nWORLD" || d.UpdatedBy != "ADMIN" {
		t.Errorf("got %+v", d)
	}
	if _, err := time.Parse(time.RFC3339, d.UpdatedAt); err != nil {
		t.Errorf("UpdatedAt %q not RFC3339: %v", d.UpdatedAt, err)
	}
	if d.LineCount() != 2 {
		t.Errorf("LineCount = %d, want 2", d.LineCount())
	}
}

func TestSetDocumentRejectsUnknownAndOversize(t *testing.T) {
	st := newDocStore(t)
	ctx := context.Background()
	if err := st.SetDocument(ctx, "NOPE", "x", "A"); err == nil {
		t.Error("unknown name: want error")
	}
	big := strings.Repeat("x", MaxDocumentBytes+1)
	if err := st.SetDocument(ctx, DocMOTD, big, "A"); !errors.Is(err, ErrDocumentTooLarge) {
		t.Errorf("oversize: got %v, want ErrDocumentTooLarge", err)
	}
}

func TestListDocumentsAlwaysShowsKnownDocs(t *testing.T) {
	st := newDocStore(t)
	docs, err := st.ListDocuments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Name != DocBranding || docs[1].Name != DocMOTD {
		t.Errorf("got %+v, want [BRANDING, MOTD] (alphabetical)", docs)
	}
	if docs[0].Content != "" || docs[0].LineCount() != 0 {
		t.Errorf("fresh doc should be empty: %+v", docs[0])
	}
}

func TestReadDocumentFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "art.txt")
	if err := os.WriteFile(p, []byte("LINE1\nLINE2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDocumentFile(p)
	if err != nil || got != "LINE1\nLINE2\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := ReadDocumentFile("relative/path.txt"); err == nil {
		t.Error("relative path: want error")
	}
	if _, err := ReadDocumentFile(filepath.Join(dir, "absent.txt")); err == nil {
		t.Error("missing file: want error")
	}
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", MaxDocumentBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDocumentFile(big); !errors.Is(err, ErrDocumentTooLarge) {
		t.Errorf("oversize file: got %v, want ErrDocumentTooLarge", err)
	}
}
```

Note for `TestListDocumentsAlwaysShowsKnownDocs`: the empty rows come from `reconcileDefaults` (step 3 wires both the table and the seeding — without them every test here fails, which is the point).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'Document|NormalizeDoc' -v`
Expected: FAIL — `undefined: DocMOTD`, `undefined: NormalizeDocName`, etc.

- [ ] **Step 3: Implement**

`internal/store/documents.go` (GPL header, then):

```go
// Package store: documents.go owns the documents table — the DB-resident text
// documents (MOTD, login branding) edited via the admin Documents screen.
// Content is LF-joined lines; the 8 KiB cap is enforced at WRITE time so reads
// never need a cap.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaxDocumentBytes caps a document's content (matches the old file read cap).
const MaxDocumentBytes = 8 << 10 // 8 KiB

// ErrDocumentTooLarge is returned when content (or an imported file) exceeds
// MaxDocumentBytes.
var ErrDocumentTooLarge = errors.New("document too large (max 8 KiB)")

// Known document names. The set is code-defined: reconcileDefaults seeds one
// empty row per name so the admin member list always shows them all.
const (
	DocMOTD     = "MOTD"
	DocBranding = "BRANDING"
)

// KnownDocuments lists every valid document name.
var KnownDocuments = []string{DocMOTD, DocBranding}

// Document is one DB-resident text document.
type Document struct {
	Name      string
	Content   string // LF-joined lines; "" = empty (feature disabled)
	UpdatedAt string // UTC RFC3339; "" until first save
	UpdatedBy string // actor username; "" until first save
}

// Lines splits Content for the editor; nil for an empty document.
func (d Document) Lines() []string {
	if d.Content == "" {
		return nil
	}
	return strings.Split(d.Content, "\n")
}

// LineCount is the member-list Lines column.
func (d Document) LineCount() int { return len(d.Lines()) }

// NormalizeDocName folds name to canonical uppercase and rejects names outside
// KnownDocuments (the single choke point, mirroring usernames/service names).
func NormalizeDocName(name string) (string, error) {
	n := strings.ToUpper(strings.TrimSpace(name))
	for _, k := range KnownDocuments {
		if n == k {
			return n, nil
		}
	}
	return "", fmt.Errorf("unknown document %q (want %s)", name, strings.Join(KnownDocuments, " or "))
}

// GetDocument returns the named document, or ErrNotFound for a known name whose
// row is missing (cannot happen after reconcileDefaults; defensive).
func (s *Store) GetDocument(ctx context.Context, name string) (Document, error) {
	n, err := NormalizeDocName(name)
	if err != nil {
		return Document{}, err
	}
	var d Document
	err = s.db.QueryRowContext(ctx,
		"SELECT name, content, updated_at, updated_by FROM documents WHERE name = ?", n).
		Scan(&d.Name, &d.Content, &d.UpdatedAt, &d.UpdatedBy)
	if err == sql.ErrNoRows {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	return d, nil
}

// SetDocument replaces the named document's content, stamping who and when.
func (s *Store) SetDocument(ctx context.Context, name, content, actor string) error {
	n, err := NormalizeDocName(name)
	if err != nil {
		return err
	}
	if len(content) > MaxDocumentBytes {
		return ErrDocumentTooLarge
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO documents (name, content, updated_at, updated_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET content=excluded.content,
		   updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		n, content, time.Now().UTC().Format(time.RFC3339), actor)
	return err
}

// ListDocuments returns all documents ordered by name.
func (s *Store) ListDocuments(ctx context.Context) ([]Document, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, content, updated_at, updated_by FROM documents ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.Name, &d.Content, &d.UpdatedAt, &d.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ReadDocumentFile reads a server-side file for import: absolute path required,
// rejected (not truncated) over MaxDocumentBytes. Used by the admin import
// screen and the CLI; the v3 migration has its own truncating reader to match
// the legacy render behavior.
func ReadDocumentFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxDocumentBytes {
		return "", ErrDocumentTooLarge
	}
	return string(data), nil
}
```

In `internal/store/migrate.go`, append to the `migrations` ledger (full import logic comes in Task 2 — this step ships the table so the CRUD tests pass):

```go
var migrations = []migration{
	{1, "baseline schema", migrateV1Baseline},
	{2, "audit actor column", migrateV2AuditActor},
	{3, "documents table + file import", migrateV3Documents},
}
```

and (temporary minimal body, replaced in Task 2):

```go
// migrateV3Documents creates the documents table and imports the MOTD/branding
// files configured in system_config (one-time cutover; see Task 2).
func migrateV3Documents(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS documents (
		name       TEXT PRIMARY KEY COLLATE NOCASE NOT NULL,
		content    TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT ''
	)`)
	return err
}
```

In `reconcileDefaults` (same file), after the sysconfig loop, add:

```go
	for _, name := range KnownDocuments {
		if _, err := s.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO documents (name) VALUES (?)", name); err != nil {
			return fmt.Errorf("seed document %s: %w", name, err)
		}
	}
```

In `internal/store/audit.go`, extend the kinds const block:

```go
	AuditDocUpdate         = "doc_update"         // admin saved a document from the editor (detail = name + line count)
	AuditDocImport         = "doc_import"         // document replaced from a server file (detail = name + path + line count)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -race`
Expected: PASS (including all pre-existing store tests — the new migration must not break `migrate_test.go`).

- [ ] **Step 5: Commit**

```bash
git add internal/store/documents.go internal/store/documents_test.go internal/store/migrate.go internal/store/audit.go
git commit -m "feat(store): documents table with CRUD, caps, and import file reader"
```

---

### Task 2: store — migration v3 one-time file import

**Files:**
- Modify: `internal/store/migrate.go` (flesh out `migrateV3Documents`)
- Modify: `internal/store/migrate_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/migrate_test.go` (check the file's existing helpers first — if it already has a "build a legacy DB by hand" helper, reuse it instead of the raw-SQL blocks below):

```go
// buildV2DB writes a v2-schema database by hand (baseline schema + audit.actor
// + user_version=2) so Open exercises exactly the v2→v3 step.
func buildV2DB(t *testing.T, dbPath string, configRows map[string]string) {
	t.Helper()
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("ALTER TABLE audit ADD COLUMN actor TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatal(err)
	}
	for k, v := range configRows {
		if _, err := raw.Exec("INSERT INTO system_config (key, value) VALUES (?, ?)", k, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateV3ImportsConfiguredFiles(t *testing.T) {
	dir := t.TempDir()
	motd := filepath.Join(dir, "motd.txt")
	if err := os.WriteFile(motd, []byte("HELLO\nWORLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "test.db")
	buildV2DB(t, dbPath, map[string]string{
		"MOTD_FILE":     motd,
		"BRANDING_FILE": filepath.Join(dir, "missing.txt"), // unreadable → non-fatal skip
	})

	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	d, err := st.GetDocument(context.Background(), DocMOTD)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "HELLO\nWORLD\n" || d.UpdatedBy != "migration" {
		t.Errorf("MOTD after migration: %+v", d)
	}
	b, err := st.GetDocument(context.Background(), DocBranding)
	if err != nil {
		t.Fatal(err)
	}
	if b.Content != "" {
		t.Errorf("BRANDING should be empty after unreadable-file skip, got %q", b.Content)
	}
}

func TestMigrateV3TruncatesOversizeFile(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", MaxDocumentBytes+100)), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "test.db")
	buildV2DB(t, dbPath, map[string]string{"MOTD_FILE": big})

	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d, err := st.GetDocument(context.Background(), DocMOTD)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Content) != MaxDocumentBytes {
		t.Errorf("oversize import: len=%d, want truncated to %d", len(d.Content), MaxDocumentBytes)
	}
}
```

Add any missing imports (`os`, `strings`, `filepath`, `context`, `database/sql`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run MigrateV3 -v`
Expected: FAIL — MOTD content is empty (the minimal v3 from Task 1 creates the table but does not import).

- [ ] **Step 3: Implement the import in `migrateV3Documents`**

Replace the Task 1 body in `internal/store/migrate.go`:

```go
// migrateV3Documents creates the documents table and performs the one-time
// cutover import: for each legacy path param (MOTD_FILE / BRANDING_FILE) whose
// value points at a readable absolute path, the file's contents become the
// document's initial content. Missing/unreadable/relative paths are skipped
// NON-FATALLY (the document starts empty — the same user-visible behavior as an
// unset param before the cutover). Over-cap files are truncated, matching the
// legacy capped render readers. The params themselves survive as the default
// import source paths (see internal/sysconfig).
func migrateV3Documents(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS documents (
		name       TEXT PRIMARY KEY COLLATE NOCASE NOT NULL,
		content    TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return err
	}
	imports := []struct{ doc, key string }{
		{DocMOTD, sysconfig.KeyMOTDFile},
		{DocBranding, sysconfig.KeyBrandingFile},
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, im := range imports {
		var path string
		err := tx.QueryRowContext(ctx,
			"SELECT value FROM system_config WHERE key = ?", im.key).Scan(&path)
		if err == sql.ErrNoRows {
			continue // fresh DB: param not seeded yet at migration time
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", im.key, err)
		}
		path = strings.TrimSpace(path)
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		content, rerr := readLegacyDocFile(path)
		if rerr != nil {
			slog.Default().Warn("documents migration: configured file unreadable; document starts empty",
				"key", im.key, "path", path, "error", rerr)
			continue
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT OR REPLACE INTO documents (name, content, updated_at, updated_by) VALUES (?, ?, ?, ?)",
			im.doc, content, now, "migration"); err != nil {
			return fmt.Errorf("import %s: %w", im.doc, err)
		}
	}
	return nil
}

// readLegacyDocFile reads at most MaxDocumentBytes (truncating, like the old
// render-time readers — an over-cap legacy file rendered clipped, so it
// migrates clipped rather than failing the upgrade).
func readLegacyDocFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
```

Add `io`, `os`, `path/filepath`, `strings` to migrate.go's imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrate.go internal/store/migrate_test.go
git commit -m "feat(store): migration v3 imports configured MOTD/branding files into documents"
```

---

### Task 3: sysconfig — relabel the path params as import paths

**Files:**
- Modify: `internal/sysconfig/catalog.go`

- [ ] **Step 1: Update the two entries and their doc comments**

In `internal/sysconfig/catalog.go`, change the `KeyMOTDFile` const comment to:

```go
// KeyMOTDFile is the system_config key whose value is the DEFAULT IMPORT PATH
// for the MOTD document: the admin Documents import screen pre-fills from it,
// and the v3 migration did its one-time cutover import from it. The rendered
// content itself lives in the documents table (store.DocMOTD), not on disk.
const KeyMOTDFile = "MOTD_FILE"
```

and `KeyBrandingFile` to:

```go
// KeyBrandingFile is the system_config key whose value is the DEFAULT IMPORT
// PATH for the BRANDING document (mirrors KeyMOTDFile). Content lives in the
// documents table (store.DocBranding).
const KeyBrandingFile = "BRANDING_FILE"
```

In `Catalog`, change the two labels and inline comments:

```go
	{
		Key:     KeyMOTDFile,
		Label:   "MOTD Import Path:",
		Default: "",
		// Default source path for the admin Documents import screen. Empty just
		// leaves the import form blank; existence is checked at import time.
		Validate: func(_ string) string { return "" },
	},
	{
		Key:     KeyBrandingFile,
		Label:   "Branding Import Path:",
		Default: "",
		// Mirrors MOTD Import Path for the BRANDING document.
		Validate: func(_ string) string { return "" },
	},
```

- [ ] **Step 2: Run the package tests**

Run: `go test ./internal/sysconfig/ ./internal/server/ -race`
Expected: PASS (catalog tests assert key presence and non-empty labels, not label text; if a server admin_system test asserts the old literal label, update that assertion to the new text).

- [ ] **Step 3: Commit**

```bash
git add internal/sysconfig/catalog.go
git commit -m "feat(sysconfig): relabel MOTD/branding path params as import paths"
```

---

### Task 4: server — render path reads documents; delete the file seams

**Files:**
- Modify: `internal/server/session.go`
- Modify: `internal/server/session_motd_test.go`
- Modify: `internal/server/session_branding_test.go`

- [ ] **Step 1: Update the tests first**

Both test files currently configure a path param and inject a fake `MOTDRead`/`BrandingRead`. Read them, then for every case rewrite the arrange step to seed the store directly:

- "shows news / branding renders" cases: replace `st.SetConfig(ctx, sysconfig.KeyMOTDFile, ...)` + fake reader with `st.SetDocument(ctx, store.DocMOTD, "LINE ONE\nLINE TWO", "TEST")` (and `store.DocBranding` for the branding tests).
- "disabled" cases (empty param) → simply do NOT seed the document (fresh = empty = skip). Keep the assertion that the news screen is skipped / branding is nil.
- "relative path" and "unreadable file" cases → DELETE (no file path exists anymore at render time; path errors are now import-time errors covered by store tests).
- Remove every reference to `MOTDRead`/`BrandingRead` from session construction in all server tests (grep: `grep -rn "MOTDRead\|BrandingRead" internal/server/`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'MOTD|News|Branding' -v`
Expected: FAIL — tests now seed documents, but the session still reads the config param + file.

- [ ] **Step 3: Implement the render-path switch in `session.go`**

1. Delete the `MOTDRead` and `BrandingRead` fields from `Session` (and their doc comments).
2. Delete `motdReadCap`, `readMOTDCapped`, `brandingReadCap`, `readBrandingCapped`.
3. Replace the body of `maybeShowNews` up to the `pages :=` line with:

```go
	doc, err := s.Store.GetDocument(ctx, store.DocMOTD)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log().Warn("MOTD document unreadable; skipping", "error", err)
		}
		return true, nil
	}
	pages := screens.PaginateNews(term.Geometry(), doc.Content)
```

(the rest of the function — empty-pages skip, Presenter.News, idle handling — is unchanged). Update its doc comment: "It returns (true, nil) to proceed into the menu — including every skip case (document empty or unreadable)."

4. Replace `loginBranding`'s body with:

```go
// loginBranding resolves the login branding lines fresh for one paint from the
// BRANDING document (one SQLite row read; admin edits take effect on the next
// paint, no restart). nil means "render a blank body".
func (s *Session) loginBranding(ctx context.Context) []string {
	doc, err := s.Store.GetDocument(ctx, store.DocBranding)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log().Warn("branding document unreadable; skipping", "error", err)
		}
		return nil
	}
	return screens.SplitBranding(doc.Content)
}
```

5. Remove now-unused imports (`io`, `os`, `path/filepath`, `sysconfig` if no other use — check; `sysconfig` is still used by `mfaIssuer`/`systemID`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/session.go internal/server/session_motd_test.go internal/server/session_branding_test.go
git commit -m "feat(server): MOTD and login branding render from DB documents"
```

---

### Task 5: ui3270 — editor core (pure prefix-command engine)

**Files:**
- Create: `internal/ui3270/editor.go` (this task: constants + `applyPrefix` only; `RunEditor` is Task 7)
- Create: `internal/ui3270/editor_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/ui3270/editor_test.go` (GPL header, then):

```go
package ui3270

import (
	"slices"
	"testing"
)

func TestApplyPrefix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      []string
		cmds    map[int]byte
		want    []string
		wantErr bool
	}{
		{"insert", []string{"a", "b"}, map[int]byte{0: 'I'}, []string{"a", "", "b"}, false},
		{"delete", []string{"a", "b", "c"}, map[int]byte{1: 'D'}, []string{"a", "c"}, false},
		{"repeat", []string{"a", "b"}, map[int]byte{0: 'R'}, []string{"a", "a", "b"}, false},
		{"multiple same transmission", []string{"a", "b", "c"}, map[int]byte{0: 'D', 2: 'I'},
			[]string{"b", "c", ""}, false},
		{"delete last line leaves one empty", []string{"a"}, map[int]byte{0: 'D'}, []string{""}, false},
		{"invalid command vetoes all", []string{"a", "b"}, map[int]byte{0: 'D', 1: 'X'},
			[]string{"a", "b"}, true},
		{"out of range ignored", []string{"a"}, map[int]byte{5: 'D'}, []string{"a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, errMsg := applyPrefix(slices.Clone(tc.in), tc.cmds)
			if (errMsg != "") != tc.wantErr {
				t.Fatalf("errMsg = %q, wantErr=%v", errMsg, tc.wantErr)
			}
			if !tc.wantErr && !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui3270/ -run ApplyPrefix -v`
Expected: FAIL — `undefined: applyPrefix`.

- [ ] **Step 3: Implement**

`internal/ui3270/editor.go` (GPL header, then):

```go
// Package ui3270: editor.go is the ISPF-style line editor driver — a prefix
// command area (I/D/R) plus full-width editable text lines. The pure command
// engine lives here; the screen builder is editorscreen.go.

package ui3270

import (
	"slices"
	"sort"
)

// editorTextMax is the editable text width: each editor row spends an
// attribute byte (col 0), a 2-char prefix input (cols 1-2), and the text
// attribute (col 3), leaving cols 4-79 = 76 text columns. Lines wider than
// this render protected (display-clipped, content preserved) — full-width art
// is the import workflow's job.
const editorTextMax = 76

// applyPrefix applies one transmission's prefix commands to lines, keyed by
// GLOBAL line index. ISPF semantics: an invalid command vetoes the whole set
// (lines returned unchanged + errMsg); valid sets apply in DESCENDING index
// order so structural shifts never move a line a later command targets.
// A document can never become empty: deleting the last line leaves one "".
func applyPrefix(lines []string, cmds map[int]byte) ([]string, string) {
	for _, c := range cmds {
		if c != 'I' && c != 'D' && c != 'R' {
			return lines, "INVALID LINE COMMAND: " + string(c)
		}
	}
	idxs := make([]int, 0, len(cmds))
	for i := range cmds {
		if i >= 0 && i < len(lines) {
			idxs = append(idxs, i)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(idxs)))
	for _, i := range idxs {
		switch cmds[i] {
		case 'I':
			lines = slices.Insert(lines, i+1, "")
		case 'D':
			lines = slices.Delete(lines, i, i+1)
		case 'R':
			lines = slices.Insert(lines, i+1, lines[i])
		}
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines, ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui3270/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/editor.go internal/ui3270/editor_test.go
git commit -m "feat(ui3270): editor prefix-command engine (I/D/R)"
```

---

### Task 6: ui3270 — editor view types, screen builder, renderer method

**Files:**
- Modify: `internal/ui3270/types.go` (EditorLine/EditorView/EditorAction + `Editor` on `Renderer`)
- Create: `internal/ui3270/editorscreen.go`
- Create: `internal/ui3270/editorscreen_test.go`
- Modify: `internal/ui3270/renderer.go` (go3270Renderer.Editor)
- Modify: every fake Renderer in tests (compile breakage tells you where: expect `internal/ui3270/*_test.go` and `internal/server/admin_test.go` — run `go build ./... && go vet ./...` to find them all)

- [ ] **Step 1: Write the failing tests**

`internal/ui3270/editorscreen_test.go` (GPL header, then). Follow the assertion style of `screen_test.go` (assert field names/content/colors/Write flags, not absolute rows — except the structural columns, which ARE the contract here):

```go
package ui3270

import (
	"strings"
	"testing"

	"github.com/racingmars/go3270"
)

func findFieldByName(s go3270.Screen, name string) (go3270.Field, bool) {
	for _, f := range s {
		if f.Name == name {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestBuildEditorScreenEditableLine(t *testing.T) {
	v := EditorView{
		Title:  "EDIT MOTD",
		Lines:  []EditorLine{{Text: "HELLO"}, {Text: "WORLD"}},
		PFHelp: "PF3=Save  PF7/PF8=Page  PF12=Cancel",
	}
	screen, cur := buildEditorScreen(24, v)

	pfx, ok := findFieldByName(screen, "pfx0")
	if !ok || !pfx.Write || pfx.Col != 0 {
		t.Fatalf("pfx0: %+v ok=%v (want writable at col 0)", pfx, ok)
	}
	txt, ok := findFieldByName(screen, "txt0")
	if !ok || !txt.Write || txt.Col != 3 || txt.Content != "HELLO" {
		t.Fatalf("txt0: %+v ok=%v (want writable at col 3, content HELLO)", txt, ok)
	}
	// Content must NOT be space-padded: trailing field positions stay NUL so
	// native 3270 insert mode works (CLAUDE.md gotcha).
	if strings.HasSuffix(txt.Content, " ") || len(txt.Content) != len("HELLO") {
		t.Errorf("txt0 content padded: %q", txt.Content)
	}
	// Cursor on the first prefix input (attribute byte + 1).
	if cur != (Cursor{Row: pfx.Row, Col: pfx.Col + 1}) {
		t.Errorf("cursor %+v, want {%d %d}", cur, pfx.Row, pfx.Col+1)
	}
	// Stop field terminates the LAST text field: an unnamed protected field at
	// col 0 on the row after the last line.
	last, _ := findFieldByName(screen, "txt1")
	foundStop := false
	for _, f := range screen {
		if f.Name == "" && f.Col == 0 && f.Row == last.Row+1 && !f.Write {
			foundStop = true
		}
	}
	if !foundStop {
		t.Error("missing stop field after last editor line")
	}
}

func TestBuildEditorScreenProtectedWideLine(t *testing.T) {
	wide := strings.Repeat("X", 80)
	v := EditorView{Title: "EDIT BRANDING", Lines: []EditorLine{{Text: wide, Protected: true}}}
	screen, _ := buildEditorScreen(24, v)
	if _, ok := findFieldByName(screen, "txt0"); ok {
		t.Error("protected line must not have a writable text field")
	}
	// The display clips at editorTextMax; storage is untouched (driver's job).
	found := false
	for _, f := range screen {
		if f.Col == 3 && !f.Write && f.Content == wide[:editorTextMax] && f.Color == go3270.Yellow {
			found = true
		}
	}
	if !found {
		t.Error("protected line not rendered as yellow clipped static text")
	}
	// Prefix input still present so D/I/R work on protected lines.
	if pfx, ok := findFieldByName(screen, "pfx0"); !ok || !pfx.Write {
		t.Error("protected line must keep its prefix input")
	}
}

func TestEditorAction(t *testing.T) {
	resp := go3270.Response{
		AID: go3270.AIDEnter,
		Values: map[string]string{
			"pfx0": " d ",
			"pfx1": "",
			"txt0": "  indented art   ",
			"txt1": "unchanged",
		},
	}
	act := editorAction(resp, 2)
	if act.PF != 0 {
		t.Errorf("PF = %d, want 0 (Enter)", act.PF)
	}
	if act.Prefix[0] != 'D' || len(act.Prefix) != 1 {
		t.Errorf("Prefix = %v, want {0:'D'}", act.Prefix)
	}
	// Leading spaces preserved (art!), trailing spaces/NULs stripped.
	if act.Text[0] != "  indented art" {
		t.Errorf("Text[0] = %q", act.Text[0])
	}
	for _, aid := range []struct {
		aid go3270.AID
		pf  int
	}{{go3270.AIDPF3, 3}, {go3270.AIDPF7, 7}, {go3270.AIDPF8, 8}, {go3270.AIDPF12, 12}} {
		if got := editorAction(go3270.Response{AID: aid.aid}, 0); got.PF != aid.pf {
			t.Errorf("AID %v → PF %d, want %d", aid.aid, got.PF, aid.pf)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui3270/ -run 'EditorScreen|EditorAction' -v`
Expected: FAIL — `undefined: EditorView`, `buildEditorScreen`, `editorAction`.

- [ ] **Step 3: Implement**

In `internal/ui3270/types.go`, add after the Form types:

```go
// EditorLine is one document line in the editor. Protected lines (wider than
// the editable width) render as static yellow text: D/I/R still work via the
// prefix, but content changes require re-import — the editor never truncates.
type EditorLine struct {
	Text      string
	Protected bool
}

// EditorView is what to paint for the line editor.
type EditorView struct {
	Title, RowInfo, ErrMsg, PFHelp string
	Lines                          []EditorLine
}

// EditorAction is what the user did on an editor screen. Maps are keyed by
// VISIBLE row index (the driver adds the page offset). Text holds only the
// fields the device returned (protected lines never appear). PF: 3 save+end,
// 7/8 page, 12 cancel; 0 = plain Enter.
type EditorAction struct {
	Prefix map[int]byte
	Text   map[int]string
	PF     int
}
```

and add to the `Renderer` interface:

```go
	Editor(EditorView) (EditorAction, error)
```

`internal/ui3270/editorscreen.go` (GPL header, then):

```go
package ui3270

import (
	"fmt"
	"strings"

	"github.com/racingmars/go3270"
)

const (
	fieldPfxPrefix = "pfx" // per-row 2-char prefix command inputs: pfx0, pfx1, …
	fieldTxtPrefix = "txt" // per-row text inputs: txt0, txt1, …
)

// editorRuler is the ISPF-style column ruler over the 76-col text area
// (exactly editorTextMax chars).
const editorRuler = "----+----1----+----2----+----3----+----4----+----5----+----6----+----7----+-"

// buildEditorScreen renders one editor page. Row layout per line: prefix
// attribute col 0, prefix input cols 1-2, text attribute col 3, text cols
// 4-79. Editable text is written WITHOUT padding so the field's trailing
// positions stay NUL — that is what makes native 3270 insert mode work; do not
// "fix" this by padding with spaces (locks the keyboard on Insert).
func buildEditorScreen(rows int, v EditorView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		// Attribute byte at col 3 puts the ruler's first char at col 4,
		// aligned with the text columns below it.
		{Row: bodyTopRow(), Col: 3, Color: go3270.Blue, Content: editorRuler},
	}
	cur := Cursor{Row: 0, Col: 0}
	row := 4
	for i, ln := range v.Lines {
		row = 4 + i
		pfx := go3270.Field{Row: row, Col: 0, Name: fmt.Sprintf("%s%d", fieldPfxPrefix, i),
			Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = Cursor{Row: pfx.Row, Col: pfx.Col + 1}
		}
		screen = append(screen, pfx)
		if ln.Protected {
			screen = append(screen, go3270.Field{Row: row, Col: 3, Color: go3270.Yellow,
				Content: truncRunes(ln.Text, editorTextMax)})
		} else {
			screen = append(screen, go3270.Field{Row: row, Col: 3,
				Name: fmt.Sprintf("%s%d", fieldTxtPrefix, i), Write: true,
				Color: go3270.Green, Content: ln.Text})
		}
	}
	// Stop field: the last text field would otherwise run through the blank
	// rows below it into the legend (a 3270 field ends only at the next
	// attribute byte).
	screen = append(screen, go3270.Field{Row: row + 1, Col: 0})
	screen = append(screen,
		go3270.Field{Row: legendRow(rows), Col: 2, Color: go3270.Turquoise,
			Content: "Prefix: I=Insert  D=Delete  R=Repeat    Yellow lines: re-import to change"},
		go3270.Field{Row: messageRow(), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 2, Color: go3270.Turquoise, Content: v.PFHelp},
	)
	return screen, cur
}

// editorAction maps a HandleScreen response to an EditorAction. Text trims
// trailing spaces/NULs only — leading whitespace is significant (art).
// A field absent from resp.Values is treated as unchanged (defensive).
func editorAction(resp go3270.Response, nLines int) EditorAction {
	act := EditorAction{Prefix: map[int]byte{}, Text: map[int]string{}}
	switch resp.AID {
	case go3270.AIDPF3:
		act.PF = 3
	case go3270.AIDPF7:
		act.PF = 7
	case go3270.AIDPF8:
		act.PF = 8
	case go3270.AIDPF12:
		act.PF = 12
	}
	for i := 0; i < nLines; i++ {
		if v, ok := resp.Values[fmt.Sprintf("%s%d", fieldPfxPrefix, i)]; ok {
			if c := strings.ToUpper(strings.TrimSpace(v)); c != "" {
				act.Prefix[i] = c[0]
			}
		}
		if v, ok := resp.Values[fmt.Sprintf("%s%d", fieldTxtPrefix, i)]; ok {
			act.Text[i] = strings.TrimRight(v, " \x00")
		}
	}
	return act
}
```

Handle the empty-lines edge in the builder: if `len(v.Lines) == 0` the loop never runs and `row` stays 4 — the stop field at `{5, 0}` is harmless. (The driver always supplies ≥1 line, but the builder must not panic.)

In `internal/ui3270/renderer.go`, add:

```go
var editorExitKeys = []go3270.AID{go3270.AIDPF3, go3270.AIDPF7, go3270.AIDPF8, go3270.AIDPF12}

func (g *go3270Renderer) Editor(v EditorView) (EditorAction, error) {
	screen, cur := buildEditorScreen(g.rows, v)
	resp, err := g.call(screen, editorExitKeys, cur)
	if err != nil {
		return EditorAction{}, err
	}
	return editorAction(resp, len(v.Lines)), nil
}
```

- [ ] **Step 4: Fix the fake renderers**

Run `go build ./... 2>&1 | head -30`. Every fake `Renderer` now fails to satisfy the interface. For each (expect one scripted fake in `internal/ui3270`'s tests and one in `internal/server`'s admin tests), add an `Editor` method following that fake's existing scripting pattern — e.g. for a queue-of-actions fake:

```go
func (f *fakeRenderer) Editor(v EditorView) (EditorAction, error) {
	f.editorViews = append(f.editorViews, v)
	if len(f.editorActs) == 0 {
		return EditorAction{PF: 12}, nil // default: cancel out
	}
	a := f.editorActs[0]
	f.editorActs = f.editorActs[1:]
	return a, nil
}
```

(adapt names/fields to the fake you find; the server fake may just need a stub returning `EditorAction{PF: 12}, nil` until Task 9 scripts it).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./... -race`
Expected: PASS across the repo.

- [ ] **Step 6: Commit**

```bash
git add internal/ui3270/ internal/server/
git commit -m "feat(ui3270): editor screen builder and Renderer.Editor"
```

---

### Task 7: ui3270 — RunEditor driver

**Files:**
- Modify: `internal/ui3270/editor.go`
- Modify: `internal/ui3270/editor_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui3270/editor_test.go` a scripted-fake-driven test set (reuse/extend the fake from Task 6 so it records `EditorView`s and replays queued `EditorAction`s):

```go
func TestRunEditorTypeAndSave(t *testing.T) {
	f := &fakeRenderer{editorActs: []EditorAction{
		{Text: map[int]string{0: "EDITED"}, PF: 3}, // type over line 0, PF3=save
	}}
	var saved []string
	err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: []string{"HELLO", "WORLD"},
		Save: func(_ context.Context, lines []string) (string, error) {
			saved = slices.Clone(lines)
			return "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(saved, []string{"EDITED", "WORLD"}) {
		t.Errorf("saved %q", saved)
	}
}

func TestRunEditorPrefixThenCancelDiscards(t *testing.T) {
	f := &fakeRenderer{editorActs: []EditorAction{
		{Prefix: map[int]byte{0: 'D'}}, // Enter: delete line 0
		{PF: 12},                       // PF12: cancel
	}}
	saveCalled := false
	err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: []string{"A", "B"},
		Save: func(_ context.Context, _ []string) (string, error) {
			saveCalled = true
			return "", nil
		},
	})
	if err != nil || saveCalled {
		t.Errorf("err=%v saveCalled=%v; cancel must not save", err, saveCalled)
	}
	// Second paint reflects the delete (working copy mutated before cancel).
	if got := f.editorViews[1].Lines; len(got) != 1 || got[0].Text != "B" {
		t.Errorf("second paint lines: %+v", got)
	}
}

func TestRunEditorEmptyDocGetsOneLine(t *testing.T) {
	f := &fakeRenderer{editorActs: []EditorAction{{PF: 12}}}
	if err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: nil,
		Save: func(_ context.Context, _ []string) (string, error) { return "", nil },
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.editorViews[0].Lines; len(got) != 1 || got[0].Text != "" {
		t.Errorf("first paint of empty doc: %+v, want one empty line", got)
	}
}

func TestRunEditorWideLineProtectedAndTextIgnored(t *testing.T) {
	wide := strings.Repeat("W", 80)
	f := &fakeRenderer{editorActs: []EditorAction{
		// A hostile/buggy client returns text for the protected line anyway.
		{Text: map[int]string{0: "clobber"}, PF: 3},
	}}
	var saved []string
	if err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT BRANDING", Rows: 24, Lines: []string{wide},
		Save: func(_ context.Context, lines []string) (string, error) {
			saved = slices.Clone(lines)
			return "", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if !f.editorViews[0].Lines[0].Protected {
		t.Error("wide line not marked Protected")
	}
	if saved[0] != wide {
		t.Errorf("wide line content changed: %q", saved[0])
	}
}

func TestRunEditorSaveErrStaysOnScreen(t *testing.T) {
	f := &fakeRenderer{editorActs: []EditorAction{{PF: 3}, {PF: 12}}}
	calls := 0
	if err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: []string{"A"},
		Save: func(_ context.Context, _ []string) (string, error) {
			calls++
			return "DOCUMENT TOO LARGE (MAX 8 KIB)", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(f.editorViews) != 2 || f.editorViews[1].ErrMsg == "" {
		t.Errorf("calls=%d paints=%d errmsg=%q; save error must re-present",
			calls, len(f.editorViews), f.editorViews[1].ErrMsg)
	}
}

func TestRunEditorPaging(t *testing.T) {
	// 20 lines, page size 14 on a 24-row screen: PF8 shows lines 14..19.
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("L%02d", i)
	}
	f := &fakeRenderer{editorActs: []EditorAction{{PF: 8}, {PF: 12}}}
	if err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: lines,
		Save: func(_ context.Context, _ []string) (string, error) { return "", nil },
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.editorViews[1].Lines[0].Text; got != "L14" {
		t.Errorf("page 2 first line %q, want L14", got)
	}
	if f.editorViews[1].RowInfo == "" {
		t.Error("RowInfo empty; want ROW x TO y OF z")
	}
}
```

Add imports as needed (`context`, `fmt`, `slices`, `strings`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui3270/ -run RunEditor -v`
Expected: FAIL — `undefined: RunEditor`, `EditorConfig`.

- [ ] **Step 3: Implement in `internal/ui3270/editor.go`**

```go
// EditorConfig parameterizes RunEditor. Lines is the document's working copy
// seed; Save commits the full line set ("" ⇒ saved, non-"" ⇒ stay with that
// error). The driver mutates only its own copy — cancel (PF12) discards.
type EditorConfig struct {
	Title  string
	Rows   int // terminal row count → page-size math
	Lines  []string
	Save   func(ctx context.Context, lines []string) (errMsg string, fatal error)
}

// editorPFHelp is the editor's fixed key map (ISPF: PF3=END saves).
const editorPFHelp = "Enter=Apply    PF3=Save+End    PF7=PgUp    PF8=PgDn    PF12=Cancel"

// RunEditor drives the line editor until PF3 (save + return) or PF12 (cancel).
// Per transmission, ISPF order: text changes apply first, then prefix commands
// (I/D/R), then navigation/save. Text changes for protected (over-wide) lines
// are ignored — those lines change only by re-import. A non-nil error is a
// dead connection.
func RunEditor(ctx context.Context, r Renderer, cfg EditorConfig) error {
	lines := slices.Clone(cfg.Lines)
	if len(lines) == 0 {
		lines = []string{""}
	}
	page, errMsg := 0, ""
	for {
		var start, end int
		var rowInfo string
		page, start, end, rowInfo = pageBounds(page, len(lines), cfg.Rows)
		view := EditorView{Title: cfg.Title, RowInfo: rowInfo, ErrMsg: errMsg, PFHelp: editorPFHelp}
		for i := start; i < end; i++ {
			view.Lines = append(view.Lines, EditorLine{
				Text:      lines[i],
				Protected: len([]rune(lines[i])) > editorTextMax,
			})
		}
		act, err := r.Editor(view)
		if err != nil {
			return err
		}
		errMsg = ""

		// 1. Text changes (editable lines only; visible index + page offset).
		for vi, txt := range act.Text {
			gi := start + vi
			if gi >= 0 && gi < len(lines) && len([]rune(lines[gi])) <= editorTextMax {
				lines[gi] = txt
			}
		}
		// 2. Prefix commands (an invalid one vetoes the set and re-presents).
		if len(act.Prefix) > 0 {
			global := make(map[int]byte, len(act.Prefix))
			for vi, c := range act.Prefix {
				global[start+vi] = c
			}
			lines, errMsg = applyPrefix(lines, global)
		}
		// 3. Navigation / save.
		switch act.PF {
		case 3:
			if errMsg != "" {
				continue // bad prefix on the save press: show it, don't save
			}
			msg, ferr := cfg.Save(ctx, lines)
			if ferr != nil {
				return ferr
			}
			if msg != "" {
				errMsg = msg
				continue
			}
			return nil
		case 12:
			return nil
		case 7:
			page--
		case 8:
			if end < len(lines) {
				page++
			}
		}
	}
}
```

(`pageBounds` is reused from layout.go — the editor's page size deliberately equals the list page size.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui3270/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui3270/editor.go internal/ui3270/editor_test.go
git commit -m "feat(ui3270): RunEditor line-editor driver"
```

---

### Task 8: server — admin menu option 8 + AdminStore seam

**Files:**
- Modify: `internal/screens/admin.go` (option row + `FieldPath`)
- Modify: `internal/screens/admin_test.go` (if it asserts the option list)
- Modify: `internal/server/presenter_admin.go`
- Modify: `internal/server/admin.go`
- Modify: `internal/server/admin_test.go` (fake store gains the two methods)

- [ ] **Step 1: Write the failing test**

In `internal/screens/admin_test.go`, add (mirroring its existing AdminMenu assertions style):

```go
func TestAdminMenuHasDocumentsOption(t *testing.T) {
	screen, _ := AdminMenuScreen(NewGeometry(24, 80), "")
	found := false
	for _, f := range screen {
		if strings.Contains(f.Content, "Documents") {
			found = true
		}
	}
	if !found {
		t.Error("admin menu missing Documents option")
	}
}
```

(Check the geometry constructor name in `geometry.go` — use whatever the existing tests use to build a 24×80 Geometry.)

- [ ] **Step 2: Run it, verify FAIL, then implement**

Run: `go test ./internal/screens/ -run AdminMenu -v` → FAIL.

In `internal/screens/admin.go`:
- Append to `opts`: `{"8", "Documents", "MOTD and login branding text"},`
- Append to the field-name const block: `FieldPath = "path" // documents import form: server file path`

In `internal/server/presenter_admin.go`:
- Update the `AdminMenu` doc comment to `choice 1-8 (… /active sessions/documents)`.
- Add `case "8": return 8, false, nil` to the switch.

In `internal/server/admin.go`:
- Add to `AdminStore` (after the trusted-networks block):

```go
	ListDocuments(ctx context.Context) ([]store.Document, error)
	SetDocument(ctx context.Context, name, content, actor string) error
```

- Add `case 8: err = f.documents(ctx, conn)` to `Run`'s switch.
- Add next to `recordAdmin`:

```go
// record emits one audit event of an explicit kind (the documents flow uses
// doc_update/doc_import instead of the generic admin kind). Actor auto-fills.
func (f *adminFlow) record(ctx context.Context, kind, detail string) {
	if f.audit == nil {
		return
	}
	f.audit(ctx, store.AuditEvent{Kind: kind, Detail: detail})
}
```

- At the bottom comment block add: `// documents is implemented in admin_documents.go.`
- `Run`'s new `case 8` needs the method to exist to compile, so create the file now with a stub body (replaced wholesale in Task 9):

`internal/server/admin_documents.go` (GPL header, then):

```go
package server

import (
	"context"
	"net"
)

// documents drives the Documents member list (MOTD/BRANDING) — see Task 9.
func (f *adminFlow) documents(ctx context.Context, conn net.Conn) error {
	return nil
}
```

- In `internal/server/admin_test.go`, the fake AdminStore now fails to compile; add:

```go
func (s *fakeAdminStore) ListDocuments(ctx context.Context) ([]store.Document, error) {
	return s.documents, nil
}

func (s *fakeAdminStore) SetDocument(ctx context.Context, name, content, actor string) error {
	for i := range s.documents {
		if s.documents[i].Name == name {
			s.documents[i].Content, s.documents[i].UpdatedBy = content, actor
			return nil
		}
	}
	s.documents = append(s.documents, store.Document{Name: name, Content: content, UpdatedBy: actor})
	return nil
}
```

(adapt the receiver/field names to the actual fake; add a `documents []store.Document` field to it).

- [ ] **Step 3: Run tests, verify PASS, commit**

Run: `go test ./internal/screens/ ./internal/server/ -race` → PASS.

```bash
git add internal/screens/ internal/server/
git commit -m "feat(server): admin menu Documents option and store seam"
```

---

### Task 9: server — Documents member list, editor wiring, import form

**Files:**
- Modify: `internal/server/admin_documents.go` (replace the stub)
- Create: `internal/server/admin_documents_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/server/admin_documents_test.go` (GPL header). Mirror the structure of `admin_sessions_test.go`/`admin_networks` tests: build an `adminFlow` with the fake store + a scripted fake renderer, drive `documents`, assert store/audit effects. Cover:

```go
// TestDocumentsListShowsMembers: fake store seeded with both documents
// (MOTD content "A\nB", updated_by "ADMIN"); script: [List action PF3].
// Assert the rendered ListView rows contain "MOTD", "2" (line count), and
// "ADMIN"; title contains "DOCUMENTS".

// TestDocumentsEditSaves: script: [List 'E' on row MOTD, Editor action
// {Text:{0:"NEW"}, PF:3}, List PF3]. Assert store document content == "NEW\nB",
// updated_by is the flow identity's username, and ONE audit event of kind
// doc_update with detail containing "MOTD" was recorded.

// TestDocumentsEditCancelDoesNotSave: script: [List 'E', Editor {PF:12},
// List PF3]. Assert content unchanged, no audit event.

// TestDocumentsImport: write a real temp file "ART LINE\n"; fake store's
// GetConfig returns its path for MOTD_FILE (so the form pre-fills); script:
// [List 'I' on MOTD, Form submit {path: <temp path>}, List PF3]. Assert
// content == "ART LINE\n", audit kind doc_import with the path in detail,
// and the confirmation message ("IMPORTED") appeared as the list errMsg on
// the following List paint.

// TestDocumentsImportBadPath: script: [List 'I', Form submit
// {path: "relative.txt"}, Form PF3 (cancel), List PF3]. Assert the form
// re-presented with errMsg "PATH MUST BE ABSOLUTE", content unchanged,
// no audit event.

// TestDocumentsEditorOversizeSaveRejected: fake store SetDocument returns
// store.ErrDocumentTooLarge; script: [List 'E', Editor {PF:3}, Editor {PF:12},
// List PF3]. Assert the second Editor paint shows errMsg
// "DOCUMENT TOO LARGE (MAX 8 KIB)".
```

Write these as real tests against the actual fakes in the package (the fake renderer from Task 6 needs its `editorActs`/`editorViews` script plumbing now if Task 6 left it a stub). The audit hook: `adminFlow.audit` is a func field — point it at a slice-appending recorder as the existing admin tests do.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run Documents -v`
Expected: FAIL — the stub `documents` does nothing.

- [ ] **Step 3: Implement `internal/server/admin_documents.go`**

Replace the stub file's contents (keep the GPL header):

```go
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/coffeemuse/tn3270proxy/internal/screens"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
)

// documents drives the Documents member list (an ISPF PDS-member-list feel):
// the two DB documents with E=edit (line editor) and I=import (server file).
func (f *adminFlow) documents(ctx context.Context, conn net.Conn) error {
	r := f.renderer(conn)
	return ui3270.RunList(ctx, r, ui3270.ListConfig[store.Document]{
		Title:  "TN3270 GATEWAY ADMIN: DOCUMENTS",
		Header: "CMD  NAME       LINES  CHANGED (UTC)     ID",
		Legend: "E = edit   I = import from server file",
		PFHelp: "PF3=Admin Menu    PF7=PgUp    PF8=PgDn",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.Row[store.Document], string) {
			docs, err := f.store.ListDocuments(ctx)
			if err != nil {
				return nil, f.storeErr("list documents", err)
			}
			rows := make([]ui3270.Row[store.Document], len(docs))
			for i, d := range docs {
				rows[i] = ui3270.Row[store.Document]{
					Display: fmt.Sprintf("%-10s %5d  %-16s  %s",
						d.Name, d.LineCount(), docStamp(d.UpdatedAt), d.UpdatedBy),
					Item: d,
				}
			}
			return rows, ""
		},
		Cmds: []ui3270.Command[store.Document]{
			{Key: 'E', Commit: func(ctx context.Context, r ui3270.Renderer, d store.Document) (string, error) {
				return f.documentEditor(ctx, r, d)
			}},
			{Key: 'I', Commit: func(ctx context.Context, r ui3270.Renderer, d store.Document) (string, error) {
				return f.documentImport(ctx, r, d)
			}},
		},
	})
}

// docStamp renders an RFC3339 stamp as the member list's "2026-06-11 14:02"
// Changed column ("" until first save).
func docStamp(rfc3339 string) string {
	s := strings.Replace(rfc3339, "T", " ", 1)
	if len(s) > 16 {
		s = s[:16]
	}
	return s
}

// documentEditor runs the line editor on d. PF3 saves (audited doc_update);
// PF12 discards. The store enforces the size cap — the editor surfaces it.
func (f *adminFlow) documentEditor(ctx context.Context, r ui3270.Renderer, d store.Document) (string, error) {
	saved := ""
	err := ui3270.RunEditor(ctx, r, ui3270.EditorConfig{
		Title: "EDIT " + d.Name,
		Rows:  f.term.Rows,
		Lines: d.Lines(),
		Save: func(ctx context.Context, lines []string) (string, error) {
			content := strings.Join(lines, "\n")
			if err := f.store.SetDocument(ctx, d.Name, content, f.identity.Username); err != nil {
				if errors.Is(err, store.ErrDocumentTooLarge) {
					return "DOCUMENT TOO LARGE (MAX 8 KIB)", nil
				}
				return f.storeErr("save document", err), nil
			}
			f.record(ctx, store.AuditDocUpdate, fmt.Sprintf("%s %d lines", d.Name, len(lines)))
			saved = d.Name + " SAVED"
			return "", nil
		},
	})
	return saved, err
}

// documentImport prompts for a server-side absolute path (pre-filled from the
// document's import-path sysconfig param) and replaces d's content with the
// file's contents. Audited as doc_import with the path in the detail.
func (f *adminFlow) documentImport(ctx context.Context, r ui3270.Renderer, d store.Document) (string, error) {
	defaultPath, _ := f.store.GetConfig(ctx, docImportPathKey(d.Name)) // "": blank pre-fill
	imported := ""
	fields := []ui3270.FormField{
		{Name: screens.FieldPath, Label: "Server file path. .", Value: defaultPath, Length: 56},
	}
	err := ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title:  "IMPORT " + d.Name + " FROM SERVER FILE",
		Fields: fields,
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			path := strings.TrimSpace(vals[screens.FieldPath])
			fields[0].Value = path
			content, err := store.ReadDocumentFile(path)
			if err != nil {
				return strings.ToUpper(err.Error()), nil
			}
			if err := f.store.SetDocument(ctx, d.Name, content, f.identity.Username); err != nil {
				return f.storeErr("import document", err), nil
			}
			n := store.Document{Content: content}.LineCount()
			f.record(ctx, store.AuditDocImport, fmt.Sprintf("%s from %s (%d lines)", d.Name, path, n))
			imported = fmt.Sprintf("%s IMPORTED (%d LINES)", d.Name, n)
			return "", nil
		},
	})
	return imported, err
}

// docImportPathKey maps a document name to its import-path sysconfig key.
func docImportPathKey(doc string) string {
	if doc == store.DocBranding {
		return sysconfig.KeyBrandingFile
	}
	return sysconfig.KeyMOTDFile
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin_documents.go internal/server/admin_documents_test.go
git commit -m "feat(server): Documents admin flow — member list, editor, import"
```

---

### Task 10: CLI — `doc import` / `doc export`

**Files:**
- Create: `cmd/tn3270proxy/doc.go`
- Create: `cmd/tn3270proxy/doc_test.go`
- Modify: `cmd/tn3270proxy/main.go`

- [ ] **Step 1: Write the failing tests**

`cmd/tn3270proxy/doc_test.go` (GPL header). Follow the style of `audit_test.go`/`mfa_test.go` (table tests driving `run()` or the sub-runner with a temp DB):

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

func TestDocImportExportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	src := filepath.Join(dir, "art.txt")
	if err := os.WriteFile(src, []byte("ART\nLINES\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"doc", "import", "-db", db, "-name", "branding", "-file", src}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDocument(context.Background(), store.DocBranding)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "ART\nLINES\n" || d.UpdatedBy != "cli" {
		t.Errorf("imported doc: %+v", d)
	}
	st.Close()

	out := filepath.Join(dir, "out.txt")
	if err := run([]string{"doc", "export", "-db", db, "-name", "BRANDING", "-file", out}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if string(got) != "ART\nLINES\n" {
		t.Errorf("exported %q", got)
	}
	// Existing file refused without -force.
	if err := run([]string{"doc", "export", "-db", db, "-name", "BRANDING", "-file", out}); err == nil ||
		!strings.Contains(err.Error(), "-force") {
		t.Errorf("overwrite without -force: err=%v", err)
	}
	if err := run([]string{"doc", "export", "-db", db, "-name", "BRANDING", "-file", out, "-force"}); err != nil {
		t.Errorf("export -force: %v", err)
	}
}

func TestDocImportRejectsBadArgs(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	if err := run([]string{"doc", "import", "-db", db, "-name", "BOGUS", "-file", "/tmp/x"}); err == nil {
		t.Error("unknown doc name: want error")
	}
	if err := run([]string{"doc", "import", "-db", db}); err == nil {
		t.Error("missing flags: want error")
	}
	if err := run([]string{"doc"}); err == nil {
		t.Error("missing verb: want error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/tn3270proxy/ -run Doc -v`
Expected: FAIL — `doc` falls through to serve and errors on flags (or compile error once doc.go exists half-done).

- [ ] **Step 3: Implement**

In `cmd/tn3270proxy/main.go`: add to the package doc comment's verb list `doc        import/export the MOTD and branding documents`, and to `run`:

```go
	if len(args) > 0 && args[0] == "doc" {
		return runDoc(args[1:])
	}
```

`cmd/tn3270proxy/doc.go` (GPL header, then):

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

// runDoc dispatches the document verbs (DB-resident MOTD/branding text).
func runDoc(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("doc: usage: doc import|export [flags]")
	}
	switch args[0] {
	case "import":
		return runDocImport(args[1:])
	case "export":
		return runDocExport(args[1:])
	default:
		return fmt.Errorf("doc: unknown subcommand %q (want import or export)", args[0])
	}
}

// runDocImport replaces a document's content with a local file's contents —
// the CLI sibling of the admin Documents import screen (handy when nobody is
// at a 3270: provisioning, docker, CI).
func runDocImport(args []string) error {
	fs := flag.NewFlagSet("doc import", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	name := fs.String("name", "", "document name: MOTD or BRANDING (required)")
	file := fs.String("file", "", "path to the text file to import (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *file == "" {
		return fmt.Errorf("doc import: -name and -file are required")
	}
	docName, err := store.NormalizeDocName(*name)
	if err != nil {
		return fmt.Errorf("doc import: %w", err)
	}
	abs, err := filepath.Abs(*file)
	if err != nil {
		return fmt.Errorf("doc import: %w", err)
	}
	content, err := store.ReadDocumentFile(abs)
	if err != nil {
		return fmt.Errorf("doc import: %w", err)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SetDocument(ctx, docName, content, "cli"); err != nil {
		return err
	}
	// Best-effort audit: the import succeeded either way.
	_ = st.RecordAudit(ctx, store.AuditEvent{
		At: time.Now(), SessionID: "cli", Kind: store.AuditDocImport, Actor: "cli",
		Detail: fmt.Sprintf("%s from %s (%d lines)", docName, abs, store.Document{Content: content}.LineCount()),
	})
	fmt.Printf("imported %s (%d lines)\n", docName, store.Document{Content: content}.LineCount())
	return nil
}

// runDocExport writes a document's content to a file (round-trips offline art
// editing). Refuses to overwrite an existing file without -force.
func runDocExport(args []string) error {
	fs := flag.NewFlagSet("doc export", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	name := fs.String("name", "", "document name: MOTD or BRANDING (required)")
	file := fs.String("file", "", "destination file path (required)")
	force := fs.Bool("force", false, "overwrite the destination if it exists")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *file == "" {
		return fmt.Errorf("doc export: -name and -file are required")
	}
	docName, err := store.NormalizeDocName(*name)
	if err != nil {
		return fmt.Errorf("doc export: %w", err)
	}
	if !*force {
		if _, err := os.Stat(*file); err == nil {
			return fmt.Errorf("doc export: %s exists (use -force to overwrite)", *file)
		}
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	d, err := st.GetDocument(context.Background(), docName)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*file, []byte(d.Content), 0o644); err != nil {
		return err
	}
	fmt.Printf("exported %s to %s (%d lines)\n", docName, *file, d.LineCount())
	return nil
}
```

- [ ] **Step 4: Run tests, verify PASS, commit**

Run: `go test ./cmd/tn3270proxy/ -race` → PASS.

```bash
git add cmd/tn3270proxy/doc.go cmd/tn3270proxy/doc_test.go cmd/tn3270proxy/main.go
git commit -m "feat(cli): doc import/export subcommand"
```

---

### Task 11: quickstart — seed the documents at provision time

**Files:**
- Modify: `internal/quickstart/provision.go`
- Modify: `internal/quickstart/provision_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/quickstart/provision_test.go` (match its existing Provision test setup):

```go
func TestProvisionSeedsDocuments(t *testing.T) {
	dir := t.TempDir()
	if _, err := Provision(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(NewLayout(dir).DB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for name, want := range map[string]string{
		store.DocMOTD:     DefaultMOTD(),
		store.DocBranding: DefaultBranding(),
	} {
		d, err := st.GetDocument(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if d.Content != want || d.UpdatedBy != "quickstart" {
			t.Errorf("%s: content match=%v updated_by=%q", name, d.Content == want, d.UpdatedBy)
		}
	}
}
```

- [ ] **Step 2: Run it, verify FAIL, then implement**

Run: `go test ./internal/quickstart/ -run SeedsDocuments -v` → FAIL (documents empty).

In `internal/quickstart/provision.go`, directly after the branding `SetConfig` block, add:

```go
	// Documents: the DB is the render source of truth; the starter files above
	// remain as the import-path defaults and offline-editing seeds.
	if err := st.SetDocument(ctx, store.DocMOTD, DefaultMOTD(), "quickstart"); err != nil {
		return nil, fmt.Errorf("seed motd document: %w", err)
	}
	if err := st.SetDocument(ctx, store.DocBranding, DefaultBranding(), "quickstart"); err != nil {
		return nil, fmt.Errorf("seed branding document: %w", err)
	}
```

- [ ] **Step 3: Run tests, verify PASS, commit**

Run: `go test ./internal/quickstart/ -race` → PASS.

```bash
git add internal/quickstart/provision.go internal/quickstart/provision_test.go
git commit -m "feat(quickstart): seed MOTD/branding documents into the DB"
```

---

### Task 12: documentation

**Files:**
- Modify: `docs/admin/08-motd.md`, `docs/admin/02-admin-ui.md`, `docs/admin/07-system-parameters.md`, `docs/admin/14-cli.md`, `docs/admin/15-operations-upgrades.md`, `docs/admin/README.md` (if it lists chapter titles), `docs/dev/ispf-style-guide.md`

Read each file before editing to match its heading/nav conventions. Content:

- [ ] **Step 1: Rewrite `docs/admin/08-motd.md`** as the Documents chapter. Replacement body (keep/adapt any nav header-footer the file family uses):

```markdown
# Documents: MOTD and Login Branding

The post-login **MOTD/NEWS** text and the **login-screen branding** art are
stored in the database and managed from the admin UI: admin menu →
**8 Documents**. The screen is a member list (think ISPF PDS member list):

    Name        Lines  Changed (UTC)      ID
    MOTD           12  2026-06-11 14:02   ADMIN
    BRANDING       18  2026-06-10 09:30   MIGRATION

Line commands: **E** = edit in place, **I** = import from a server file.
Changes take effect on the next render — the MOTD on the next login, branding
on the next login-screen paint. No restart. An **empty document disables the
feature** (no MOTD gate; blank branding region).

## The editor (E)

An ISPF-style line editor: type over text, and use the 2-character prefix area
at the left of each line for structural commands —

- **I** — insert a blank line after this one
- **D** — delete this line
- **R** — repeat (duplicate) this line

Press **Enter** to apply, **PF3** to save and return, **PF12** to cancel
(discard all changes), **PF7/PF8** to page. Within a line, your emulator's
**Insert** key works natively.

Two width rules, both inherited from the 3270 screen:

- Rendered output truncates at **column 79** (the documents are composed
  fixed-width banners — you own line breaks and indentation).
- The editor itself can edit lines up to **76 characters** (the prefix area
  costs 4 columns). Wider lines show in **yellow, protected**: you can still
  delete them or insert around them, but changing their content requires
  re-import. The editor never truncates your art.

Documents are capped at **8 KiB**; the editor refuses to save past the cap.

## Import (I)

For full-width or complex ASCII art, author with your favorite editor and
import. The import screen asks for an **absolute server-side path** — pre-filled
from the matching system parameter (**MOTD Import Path** / **Branding Import
Path**, admin menu → Sysparms), so the usual flow is: copy the file to the
configured path (`scp`, a volume mount, …) and press Enter. Import replaces the
document's entire content. Files over 8 KiB are rejected.

The CLI sibling works without a 3270 session and can also round-trip content
back out for offline editing:

    tn3270proxy doc import -db proxy.db -name BRANDING -file art.txt
    tn3270proxy doc export -db proxy.db -name BRANDING -file art.txt

## Authoring conventions

- Fixed width: compose to **80 columns or less**; anything past column 79 is
  not rendered.
- Keep the MOTD short — realistically no more than ~3 screens; longer notices
  get paged past unread. Press ENTER to page; PA3/PF3 do nothing on the MOTD.
- Write maintenance windows in **UTC** by convention.
- Branding lines render verbatim from column 0 on the login screen body;
  vertical centering is automatic when the art is shorter than the body.

Saves and imports are audited (`doc_update` / `doc_import`) with the actor,
line count, and (for imports) the source path.
```

- [ ] **Step 2: Touch the supporting pages**

- `02-admin-ui.md`: add option **8 Documents — MOTD and login branding text** to the admin menu listing, in the same format as options 1–7.
- `07-system-parameters.md`: update the two parameter descriptions: "**MOTD Import Path** / **Branding Import Path** — default source path pre-filled on the Documents import screen. The displayed content itself lives in the database (admin menu → Documents); these parameters only say where imports come from."
- `14-cli.md`: add a `doc import` / `doc export` section using the two command lines from step 1's draft, noting `-force` for export overwrite and that names are `MOTD` or `BRANDING` (case-insensitive).
- `15-operations-upgrades.md`: add a v3 migration note: "Upgrading to schema v3 performs a one-time import of the files configured in MOTD File/Branding File into the new `documents` table (actor `migration`). A missing or unreadable file is skipped with a warning and the document starts empty — re-import it from the admin UI or CLI. As with every migration, a `*.pre-migrate-*.bak` backup is written first."
- `docs/admin/README.md`: update the chapter 08 title/link text if it lists one.
- `docs/dev/ispf-style-guide.md`: add a short "Editor screens" subsection documenting: prefix area cols 1–2 + text cols 4–79 (76 editable), I/D/R command set, PF3=END(save)/PF12=CANCEL, protected-yellow over-wide lines, the trailing-NUL (never space-pad) rule, and the col-ruler header row.

- [ ] **Step 3: Commit**

```bash
git add docs/
git commit -m "docs: Documents admin chapter + editor/import/CLI/migration notes"
```

---

### Task 13: CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update**

- **What this is**: add a sentence after the User Settings paragraph: "MOTD and login branding live in the DB (`documents` table), managed via the admin **Documents** member list — an ISPF-style line editor (I/D/R prefix commands, 76-col editable width, wider lines read-only) plus import-from-server-file (3270 screen + `doc import` CLI)."
- **Commands**: add `./bin/tn3270proxy doc import -db proxy.db -name MOTD -file motd.txt   # import a document (export round-trips)`.
- **Package map**: update the `internal/store` entry (documents.go: DB-resident MOTD/BRANDING documents, 8 KiB write-time cap, migration v3 one-time file import), `internal/sysconfig` (MOTD_FILE/BRANDING_FILE are now *import paths*, not render sources), `internal/screens`/`internal/ui3270` (editor widget: EditorView/RunEditor, prefix I/D/R, editorTextMax=76, protected wide lines), `internal/server` (admin_documents.go; loginBranding/maybeShowNews read documents; MOTDRead/BrandingRead seams are GONE), `cmd/tn3270proxy` (doc subcommand), `internal/quickstart` (seeds documents rows; .txt files remain as import seeds).
- **Gotchas**: append to the go3270 cursor/insert-mode area: "Editor text fields are written unpadded so trailing positions stay NUL — 3270 insert mode needs trailing NULs; space-padding locks the keyboard on Insert. The smoke script asserts insert works mid-line in the editor."

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: CLAUDE.md updates for DB documents + editor"
```

---

### Task 14: protocol verification (smoke) + final full pass

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh` (follow the skill's own conventions — invoke the s3270-smoke-testing skill / read its SKILL.md before editing)

- [ ] **Step 1: Extend the smoke script**

Add a Documents section after the existing admin-screen steps:

1. From the admin menu, enter `8` → assert title `TN3270 GATEWAY ADMIN: DOCUMENTS` and both `MOTD` and `BRANDING` rows render.
2. `E` on MOTD → assert title `EDIT MOTD`, the column ruler row, and **cursor at the first prefix field (row 4, col 1)**.
3. Type text into line 1's text field, press PF3 → back at the member list with `MOTD SAVED`; Lines column updated.
4. **Insert-mode regression guard:** in the editor, move mid-line, toggle s3270 Insert mode, type a character, assert no keyboard lock and the shifted content reads back (this asserts the NUL-padding contract).
5. Prefix `I` on line 1 + Enter → assert a blank line 2 appears; `D` it back out.
6. PF12 → member list with no save message.
7. Log off, log back in → assert the saved MOTD text renders on the NEWS screen.

- [ ] **Step 2: Full verification**

```bash
go fmt ./...
go build ./...
go test ./... -race
.claude/skills/s3270-smoke-testing/smoke.sh
```

Expected: all PASS. If smoke fails on cursor position or insert mode, the screen builder column math is wrong — fix the builder, not the assertions.

- [ ] **Step 3: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/
git commit -m "test: smoke coverage for the Documents editor and import flow"
```

- [ ] **Step 4: Human pass**

Run `./bin/tn3270proxy serve` + `c3270 127.0.0.1:2323`, visually QA: member list alignment, editor feel (type-over, Insert key, I/D/R, paging), wide-line yellow protection, import confirmation, MOTD/branding rendering after edits. Final word on visual polish per CLAUDE.md.

---

## Self-review notes (already applied)

- Spec §1–§9 each map to tasks: data model→1, cutover→2 (+param relabel→3), member list→8/9, import→9/10, editor→5/6/7, render path→4, quickstart→11, docs→12/13, testing→every task + 14.
- Type consistency: `store.Document{Name, Content, UpdatedAt, UpdatedBy}` + `Lines()/LineCount()`; `ui3270.EditorView/EditorLine/EditorAction/EditorConfig/RunEditor`; `screens.FieldPath`; audit kinds `AuditDocUpdate`/`AuditDocImport` — used with these exact names throughout.
- The editor deliberately reuses `pageBounds`/`listPageSize` (14 lines/page on MOD 2) rather than inventing a second paging system.
- `RunList` re-fetches every iteration, so the member list's Lines/Changed columns refresh after each E/I command — no extra plumbing needed.
