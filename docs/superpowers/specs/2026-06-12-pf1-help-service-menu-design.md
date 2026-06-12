# PF1=Help on the service menu, backed by a DB HELP document — design

**Date:** 2026-06-12
**Status:** Approved (brainstormed with Robert)
**Issue:** #132

## Problem

First-time users land on the service menu with no in-band explanation of the
conventions: selection by number, PA3-returns-from-bridge, PF3 logoff, PF7/PF8
paging, the `0` User Settings entry. Operators should be able to write and
maintain that guidance themselves — without a rebuild or filesystem access.

The documents infrastructure from #126 (the `documents` table, ISPF line
editor, import form, audit events, `doc import|export` CLI verbs) already
provides storage and admin editing for free; this feature adds a third
document and a viewer.

## Goals

- **PF1 at the service menu** opens a full-screen, pageable, read-only help
  viewer showing the contents of a `HELP-MENU` document stored in the DB.
- Admins edit/import the document through the existing **admin menu → 8
  Documents** member list, exactly like MOTD and BRANDING (same editor,
  same audits, same 8 KiB / 76-column rules).
- Every install gets useful help **out of the box**: the document is seeded
  with stock text describing the menu conventions.
- Returning from the viewer lands on the **same menu page** (no logoff, no
  page reset).
- The viewer is structured so wiring PF1 on another panel later is one
  dispatch line + one `KnownDocuments` entry (`HELP-<panel>` naming
  convention).

## Non-goals

- Help on any panel other than the service menu.
- Field-level / context-sensitive help (ISPF PF1-on-a-field semantics).
- A sysconfig default-import-path entry for the help document (the admin
  import form's path field pre-fills empty; MOTD_FILE/BRANDING_FILE stay
  special-cased).
- Any registry/abstraction for per-panel help beyond the viewer's
  `(fetcher, title)` shape.

## Design

### 1. Storage (`internal/store`)

**New document name.** `DocHelpMenu = "HELP-MENU"` joins the constants and
`KnownDocuments` in `documents.go`, establishing the `HELP-<panel>` naming
convention for future panel help documents (`HELP-ADMIN`, `HELP-SETTINGS`, …).
`NormalizeDocName` remains the single choke point; no other validation
changes.

**Seeding with default content.** Unlike MOTD/BRANDING (site-specific,
seeded empty), the service-menu conventions are app-defined and identical on
every install, so `HELP-MENU` seeds with **stock help text**:

- A `documentDefaults` map in `documents.go` (name → default content; MOTD
  and BRANDING map to `""`). The stock text is a package const: short
  paragraphs covering select-by-number, PA3 returns from a bridged session,
  PF3 logoff, PF7/PF8 paging, and the `0` User Settings entry. Every line
  ≤ 76 columns (editor-editable), total well under the 8 KiB cap.
- The `reconcileDefaults` document loop (`migrate.go`) changes from
  `INSERT OR IGNORE INTO documents (name)` to
  `INSERT OR IGNORE INTO documents (name, content) VALUES (?, ?)`.
  `INSERT OR IGNORE` preserves once-only semantics: fresh **and** existing
  DBs get the stock text exactly once (when the row is first created), and
  any admin edit — including deliberately blanking the document — survives
  restarts.

No migration (the `documents` table exists since v3; reconcile runs on every
Open). No sysconfig entry. Quickstart needs no changes: `reconcileDefaults`
runs during its provisioning `store.Open` like everywhere else.

**Admin editing comes free.** The Documents member list renders whatever
`ListDocuments` returns, so `HELP-MENU` appears with E=edit and I=import
automatically, including `doc_update`/`doc_import` audits. The CLI verbs
operate by name, so `doc import|export -name HELP-MENU` works unchanged.

### 2. Help viewer screen (`internal/screens`, new file `help.go`)

`HelpScreen(geom Geometry, title string, lines []string, page int)` — a
read-only **three-band ISPF screen** per `docs/dev/ispf-style-guide.md`
(deliberately NOT the chrome-less MOTD/news pager; the news pager stays the
documented exception):

- Row 0: centered title — `HELP — Service Menu` — with a `PAGE x OF y`
  indicator right-aligned on the same row (mirroring the menu's
  `ITEMS x TO y OF z` convention).
- Row 2: red message line, reserved (unused by this screen).
- Body from row 3: the current page's lines, protected text, default color,
  truncated at column 79.
- `geom.HelpRow()`: `PF3=Return  PF7=PgUp  PF8=PgDn`.

Paging math via a page-bounds helper following the established
clamp-at-ends convention (PF7 on page 1 / PF8 on the last page re-present
the same page; no drift, no error). Page size derives from `Geometry` so
MOD 3/4/5 clients get taller pages. The builder returns a `screens.Cursor`
like every other builder; with no input fields the cursor homes to `{0,0}`
(the empty-admin-list precedent).

### 3. Presenter wiring (`internal/server/presenter.go`)

`Presenter.Menu` gains a **lazy fetcher** parameter:

```go
Menu(conn net.Conn, term Term, svcs []store.Service, admin, settingsLocked bool,
    status screens.MenuStatus, errMsg string,
    help func() ([]string, error)) (*store.Service, menuChoice, error)
```

Inside `go3270Presenter.Menu`'s loop, `go3270.AIDPF1` joins the silent-exit
AIDs and is handled **inside the loop**, like PF7/PF8 — so the menu's local
`page` variable survives a help round-trip and no new `menuChoice` is
needed:

- **PF1 pressed** → call `help()`. On error or zero lines, set
  `errMsg = "NO HELP AVAILABLE"` and re-present the menu (same page) — the
  inline red message line, matching existing invalid-selection handling.
  One path covers both "empty document" and "fetch failed".
- **Non-empty** → run an internal viewer loop: render `HelpScreen` pages;
  PF7/PF8 page with clamping; **PF3 or Enter returns** (Enter from any
  page — there is no input to submit); PA keys and other AIDs silently
  re-present via the existing `withSilentExits` mechanism (PA3 inert on
  proxy screens, per convention). On return, fall back into the menu loop
  with `page` intact and no error message.

The menu help row text in `screens/menu.go` becomes:

```
PF1=Help  PF3=Logoff  PF7=PgUp  PF8=PgDn  (PA3 returns here from a session)
```

The text is static — PF1 is always advertised; an empty document answers
with the inline message rather than dynamic key-row suppression (which
would force an emptiness check on every menu paint).

### 4. Session wiring (`internal/server/session.go`)

The menu loop builds the closure over the store and passes it to
`Presenter.Menu`:

```go
help := func() ([]string, error) {
    doc, err := s.Store.GetDocument(ctx, store.DocHelpMenu)
    if err != nil {
        return nil, err
    }
    return doc.Lines(), nil
}
```

Fresh read on each PF1 press — consistent with `loginBranding` and
`maybeShowNews` (#126's fresh-on-use convention) — and **no DB read at all
unless PF1 is pressed**. No new `menuChoice`, no new dispatch case.

**Idle handling:** the viewer runs inside `Presenter.Menu`, so the post-auth
idle regime already covers it; a timeout surfaces as the existing
`isTimeoutErr` path (idle logout to the login screen). No new arming.

**Future panels:** another screen wires help by passing its own
`func() ([]string, error)` + title into the same viewer loop (extracted as a
small presenter helper, e.g. `runHelpViewer(conn, term, title, lines)`), plus
one `KnownDocuments`/`documentDefaults` entry. Nothing else.

### 5. Testing

Per repo convention, unit tests assert field names/content/color — not row
numbers; positioning is verified in a real emulator.

- **screens:** `HelpScreen` builder — title text, `PAGE x OF y` indicator,
  body content per page, PF-row text, paging clamp math, cursor value,
  geometry-driven page size (MOD 2 vs MOD 4).
- **presenter:** PF1 flow against a scripted connection/fake — fetcher
  invoked only on PF1; empty/error → menu re-presented with
  `NO HELP AVAILABLE`; non-empty → viewer renders; PF8/PF7 page; PF3 and
  Enter return; menu page preserved across the help round-trip.
- **store:** `HELP-MENU` in `KnownDocuments`; reconcile seeds the stock
  content on a fresh DB **and** on an existing DB that predates the entry;
  reconcile twice is idempotent; an admin edit (including blanking)
  survives a re-open; `NormalizeDocName("help-menu")` folds and accepts.
- `go test ./... -race` passes (bridge is concurrent).
- **s3270 smoke script** grows a help step: at the menu press PF1 → assert
  title + stock body content + cursor; PF8 → page 2 (stock text long enough
  to paginate on MOD 2, or the step pads via `doc import`); PF3 → back at
  the service menu.

## Alternatives considered

- **PF1 as a new `menuChoice` dispatched by the session** (matching the
  admin/user-settings pattern): cleaner seam separation, but the menu's
  page state is local to `Presenter.Menu`, so returning through the session
  resets to page 0 — violating "return to the same page" — unless page
  state is threaded in/out of the seam. Rejected for signature churn.
- **Pre-fetched help lines passed into `Menu`:** simpler signature than a
  closure, but costs a DB read on every menu render even when help is never
  used, and content staleness depends on the last menu paint. Rejected;
  the lazy fetcher reads fresh exactly when needed.
- **Suppressing PF1 when the document is empty:** most honest key row, but
  makes the help-row text dynamic and reintroduces a per-render DB read for
  the emptiness check. Rejected in favor of the inline
  `NO HELP AVAILABLE` message.
- **Empty seed + quickstart-only starter content** (the issue as filed):
  consistent with MOTD/BRANDING, but the menu conventions are app-defined,
  not site-specific — most sites would write the same generic text. Seeding
  stock content at row creation gives every install working PF1 help while
  keeping admin edits (and blanks) durable.

## Acceptance criteria (from #132, amended by the seeding decision)

- [ ] `HELP-MENU` appears in the admin Documents list; E opens the ISPF
      editor, I imports, both audited; `doc import`/`doc export` round-trip it.
- [ ] Fresh DBs and existing DBs both get the seeded row **with stock help
      content**, exactly once (reconcileDefaults, no migration); admin edits
      and blanks survive restarts.
- [ ] PF1 at the service menu opens the help viewer; PF7/PF8 page; PF3 (or
      Enter) returns to the menu with state intact (same page, no logoff).
- [ ] Menu PF-key help row advertises PF1=Help.
- [ ] Empty document handled gracefully: inline `NO HELP AVAILABLE` message
      on the menu, no blank screen.
- [ ] Unit tests assert field names/content/color (not row numbers);
      `go test ./... -race` passes.
- [ ] s3270 smoke script grows a help-screen step (open via PF1, assert
      content + cursor, PF8 page, PF3 return).
