# Documents: DB-backed MOTD/branding with an ISPF-style editor — design

**Date:** 2026-06-11
**Status:** Approved (brainstormed with Robert)

## Problem

The MOTD/NEWS text and the login-screen branding art live as plain files on the
server's disk, located via two `sysconfig` runtime params (`MOTD_FILE`,
`BRANDING_FILE`). This was always interim: editing them requires shell access to
the box, they sit outside the DB (so they miss backups, audit, and the
"all day-to-day admin through the 3270 UI" north star), and the System
Parameters form only edits the *paths*, not the content.

## Goals

- The database is the single source of truth for MOTD and branding content.
- Admins edit both documents inside the 3270 admin UI with a lightweight
  ISPF-style editor (overstrike/insert natively, plus I/D/R line commands).
- Complex full-width ASCII art keeps an authoring path through local tools:
  an **import** flow (3270 screen + CLI sibling) pulls a server-side file into
  the DB.
- Existing deployments cut over automatically: a schema migration imports the
  currently-configured files once. No flag day.
- Admin documentation is updated as part of the work, not after.

## Non-goals

- A full ISPF EDIT clone (no C/M block commands, no COMMAND ===> primary
  commands, no PF10/PF11 horizontal scroll, no UNDO).
- Editing arbitrary files on disk. The editor edits DB documents only.
- New document types beyond MOTD and BRANDING (the design extends to them, but
  none ship now).

## Design

### 1. Data model (`internal/store`)

New `documents` table, created via the migration ledger:

| column       | type | notes                                          |
|--------------|------|------------------------------------------------|
| `name`       | TEXT PRIMARY KEY, COLLATE NOCASE | canonical UPPERCASE (`MOTD`, `BRANDING`) |
| `content`    | TEXT | LF-joined lines; `''` = empty/disabled         |
| `updated_at` | TEXT | UTC RFC3339; `''` until first save             |
| `updated_by` | TEXT | actor username; `''` until first save          |

Store methods (`internal/store/documents.go`):

- `GetDocument(ctx, name) (Document, error)`
- `SetDocument(ctx, name, content, actor) error` — validates the 8 KiB content
  cap (moved from read time to write time) and stamps `updated_at`/`updated_by`.
- `ListDocuments(ctx) ([]Document, error)`

The two known names are seeded as empty rows in `reconcileDefaults` (NOT a
migration) so the member list always shows both rows, including on DBs that
predate the table gaining new known names later. Name normalization goes
through the existing uppercase choke point.

### 2. Cutover migration

One new ledger step (next free version):

1. Create the `documents` table.
2. For each of `MOTD_FILE` / `BRANDING_FILE`: read the param value from
   `system_config`; if non-empty, read that path through the existing capped
   reader (8 KiB, absolute-path guard) and insert the contents as the
   document's initial content with `updated_by = 'migration'`. A missing or
   unreadable file is skipped **non-fatally** (document stays empty — the same
   user-visible behavior as an unset param today).

The sysconfig params **stay** in the catalog with new semantics: they are now
the *default import source path* for each document. Labels change to
`MOTD Import Path:` / `Branding Import Path:` (catalog `Label` field only; keys
are unchanged so no data migration is needed). Their values pre-fill the
import screen.

### 3. Admin UI: Documents member list

New admin menu option **Documents** (next free number on the admin menu),
opening an `AdminListScreen` styled like an ISPF PDS member list:

```
Name        Lines  Changed (UTC)      ID
MOTD           12  2026-06-11 14:02   ADMIN
BRANDING       18  2026-06-10 09:30   MIGRATION
```

Line commands:

- `E` — open the editor on that document.
- `I` — open the import form for that document.

The list is fixed at the two seeded rows (no create/delete line commands).
PF3 returns to the admin menu, per the uniform back-one-level rule.

### 4. Import flow

**3270 screen:** a small `AdminFormScreen`-style form with one input: the
server-side absolute path, pre-filled from the matching sysconfig param. Enter
reads the file (absolute-path guard + 8 KiB cap), replaces the document
content, and returns to the member list with a confirmation message
(`MOTD IMPORTED (12 LINES)`). Errors (relative path, unreadable, over cap)
re-render the form with the red message line. Audited as `doc_import` with the
path and line count in `Detail`; `username` stays empty (the subject is in
Detail, matching generic admin CRUD).

Trust model note: an admin importing an arbitrary server path can display any
readable file's first 8 KiB. This is the same exposure as today's
`MOTD_FILE` param (which already let an admin point the MOTD at any path), so
no new guard is added beyond absolute-path + cap.

**CLI sibling** (`cmd/tn3270proxy`):

```
tn3270proxy doc import -db proxy.db -name BRANDING -file art.txt
tn3270proxy doc export -db proxy.db -name BRANDING -file art.txt
```

`import` shares the store method and caps; `export` writes the document
content to a file (refusing to overwrite without `-force`), making offline
art editing a true round trip. CLI imports are audited as `doc_import` with
actor `cli` (there is no logged-in principal to attribute).

### 5. The editor

New screen builder `screens.EditorScreen` + a server-side edit loop driven
through the existing presenter seams (a new `ui3270` runner,
`RunEditor`, alongside RunForm/RunList).

**Row layout (the width trade-off).** Each body row is:
attribute + 2-char unprotected **prefix field** + attribute + unprotected
**text field** to column 79 → **76 editable text columns**. Document lines may
legitimately be up to 80 columns wide (branding renders verbatim at col 0;
news truncates at col 79). Rule:

- Lines **≤ 76 chars** render as editable fields, padded with **nulls, not
  spaces** (3270 insert mode needs trailing nulls; space padding locks the
  keyboard on insert — see the cursor/insert gotchas in CLAUDE.md).
- Lines **> 76 chars** render **protected** with a distinguishing highlight
  (yellow). They can still be deleted (`D`) or have lines inserted around them
  (`I`), but their content can only be changed by re-import. The editor never
  silently truncates wide art.

**Prefix commands**, processed ISPF-style on Enter (commands first, then text
changes from the same transmission, then re-render):

- `I` — insert one blank editable line after this one.
- `D` — delete this line.
- `R` — repeat (duplicate) this line after itself.

Invalid prefix input re-renders with the red message line and the offending
prefix preserved.

**Keys:** Enter = apply commands/changes and stay; PF7/PF8 = page; PF3 = END
(save to DB + return to member list, audited as `doc_update` with line count
in Detail); PF12 = CANCEL (discard, return to list). Idle/disconnect discards
unsaved changes (the session's existing idle regime applies unchanged).

**Caps:** saving enforces the 8 KiB content cap; the editor refuses to grow
past it with a message rather than truncating.

The editor edits an in-memory `[]string` working copy; the DB is touched only
at PF3. Concurrent edits are last-writer-wins (two simultaneous admins is not
a scenario worth locking for; the audit trail records both writes).

### 6. Render path

`Session.MOTDRead` / `Session.BrandingRead` (file-reading seams) are replaced
by document reads through the store (a `DocumentStore` seam: `GetDocument`),
still read fresh per paint — one SQLite row read, no caching. Empty content =
feature disabled (blank branding region / MOTD skipped), exactly as an unset
path behaves today. The `screens` builders (news, login) are unchanged.
Existing fake-injection tests move from fake file readers to a fake document
store.

### 7. Quickstart

`internal/quickstart` keeps writing `motd.txt` / `branding.txt` as starter
files (they remain useful as import-path defaults and offline-editing seeds),
and continues setting the two sysconfig params to those paths — but now also
imports their contents into the `documents` rows at provision time, so a fresh
quickstart serves DB truth from the first connection.

### 8. Documentation deliverables (in-scope, not follow-up)

- `docs/admin/08-motd.md` — rewritten as the **Documents** chapter: member
  list, editor (including the 76-column rule and insert-mode behavior),
  import screen, and the offline-art workflow (`scp` + import, or CLI).
- `docs/admin/02-admin-ui.md` — add the Documents option to the admin menu map.
- `docs/admin/07-system-parameters.md` — re-document the two params as import
  paths.
- `docs/admin/14-cli.md` — `doc import` / `doc export`.
- `docs/admin/15-operations-upgrades.md` — the one-time migration import and
  its non-fatal-skip behavior.
- `docs/user/04-motd.md` — touch only if user-visible wording changes (none
  expected).
- `docs/dev/ispf-style-guide.md` — add the editor screen conventions (prefix
  area, protected wide lines, PF3=END/PF12=CANCEL).
- `CLAUDE.md` — package map and commands updates.

### 9. Testing

TDD throughout. Unit coverage:

- store: documents CRUD, cap enforcement, migration import (present, missing,
  unreadable, over-cap files), reconcileDefaults seeding.
- editor core: prefix-command parsing/application, wide-line protection,
  paging windows, cap refusal — all pure functions over `[]string`.
- screens: field names/colors/content for the member list, import form, and
  editor (per convention, not row numbers); null-padding of editable fields
  asserted at the builder level.
- server: edit loop save/cancel/idle paths, import flow errors, audit events,
  render-path switch — via the existing fake presenter/store seams.

Protocol surface: extend the s3270 smoke script — member list renders, editor
cursor lands on the first text field, **insert mode works mid-line** (the
trailing-nulls regression guard), I/D/R round trip, PF3 saves and the new MOTD
shows on next login. Final visual pass in c3270.

## Alternatives considered

- **Simple full-screen editor (no prefix area):** wider editing fields,
  but no line insert/delete — restructuring a document means retyping
  everything below the change. Rejected: line-level editing was the point.
- **Full ISPF EDIT (block commands, primary commands, horizontal scroll):**
  authentic but a large editor engine for two small documents. Rejected as
  YAGNI; the I/D/R subset plus import covers the real workflows.
- **Hybrid source of truth (file overrides DB when param set):** rejected —
  two sources of truth and a "why isn't my edit showing up" failure mode.
- **Dropping the sysconfig params entirely** (the `trusted_cidrs` precedent):
  rejected in favor of repurposing them as default import paths, which keeps
  the import screen pre-filled and gives the migration its import source.
