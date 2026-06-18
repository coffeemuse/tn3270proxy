# TN3270Proxy ISPF Screen Style Guide

The single source of truth for how the proxy's 3270 screens look and behave.
Every screen in `internal/screens` and `internal/ui3270` follows it; the planned
screens (#63 User Settings, #40 Audit Log viewer) are built to it.

Origin: issue #65 ("Audit & polish all 3270 screens toward consistent ISPF-style
UI/UX"). This guide is the keystone deliverable; the per-screen audit decisions
and the deviation log live here.

The goal is an **authentic ISPF panel feel** so operators with z/OS muscle memory
feel at home: centered panel title, a top command line, a top message band, and a
consistent CUA color vocabulary. Conscious departures are recorded in the
[Deviation Log](#deviation-log) — deviations are deliberate and documented, never
accidental.

> **One exception, settled:** the MOTD/NEWS screen (`internal/screens/news.go`) is
> deliberately chrome-less — it mimics the raw TSO/READY logon-message screen
> *before* ISPF launches. It is **out of scope** for this guide and must not be
> brought under these panel conventions.

---

## 1. Color palette (CUA)

The standard ISPF CUA attribute categories mapped onto go3270's seven colors. This
is the house palette; apply it uniformly across all ISPF-layer screens.

| Semantic role | CUA category | Color | Attribute | Appears on |
|---|---|---|---|---|
| Panel title | PT (Panel Title) | **White** | Intense | row 0, centered |
| Command-line prompt (`Option ===>`) | PIN | **Turquoise** | — | row 1 |
| Input / entry fields | EE (Entry, unprotected) | **Green** | Write + Underscore | user-typed fields |
| Field prompts / labels | FP (Field Prompt) | **Turquoise** | — | `Userid . . .`, `Issuer:` |
| Column headings (lists) | CH | **Blue** | — | list heading row |
| Selectable option key (digit/letter) | PS (point-and-shoot) | **White** | Intense | `1`, `0`, `A` |
| Normal text / values | NT (Normal Text) | **Green** | — | descriptions, status values |
| Instruction text | PIN (Panel Instruction) | **Turquoise** | — | "Select a service and press ENTER" |
| Caution / warning | WASL | **Yellow** | Intense | "MFA now required", confirm prompts |
| Error / urgent message | (short/long message) | **Red** | Intense | message line (row 2) |

### go3270 implementation cheat-sheet

Exact `go3270.Field` attributes per role, so every builder paints identically:

| Role | `Color` | `Intense` | `Write` | `Highlighting` | `Hidden` |
|---|---|---|---|---|---|
| Title | `White` | `true` | — | — | — |
| Command prompt text | `Turquoise` | — | — | — | — |
| Input field | `Green` | — | `true` | `Underscore` | — |
| Password / secret input | `Green` | — | `true` | `Underscore` | `true` |
| Label / prompt | `Turquoise` | — | — | — | — |
| Column heading | `Blue` | — | — | — | — |
| Option key (digit/letter) | `White` | `true` | — | — | — |
| Value / description / normal text | `Green` | — | — | — | — |
| Instruction text | `Turquoise` | — | — | — | — |
| Caution / warning | `Yellow` | `true` | — | — | — |
| Error message | `Red` | `true` | — | — | — |
| PF-key help row | `Turquoise` | — | — | — | — |

Notes:
- `DefaultColor` is avoided for semantic text: it leaves the emulator to apply
  base-color rules (protected→green, intense→white), which *happens* to look right
  but is implicit. Set the color explicitly so intent is in the source.
- Non-display fields use `Hidden: true` — only passwords. The MFA enrollment key
  (`Key:`) and the confirmation-code field are intentionally visible (the key must be
  read for manual entry); the secret-at-rest is never rendered.

---

## 2. Layout: the three-band panel

Every ISPF-layer screen uses the same vertical structure. Rows are 0-based; a MOD-2
screen is rows 0..23. Taller models (MOD 3/4/5) stretch the **body** only — the top
and bottom bands stay anchored. Content always stays within columns 0–79.

```
 col: 0         1         2         3         4         5         6         7
      0123456789012345678901234567890123456789012345678901234567890123456789012345678
 r0 │                       <centered PANEL TITLE>                  <RowInfo, lists>  │  TOP BAND
 r1 │ Option ===> ______________________________________________________             │
 r2 │ <red message line — blank when no error>                                        │
 r3 │ <body begins: instruction line, or column headings on a list>                   │  BODY
 .. │ ...                                                                              │
 rN-2│ <legend row — list line-command help only>                                     │
 rN-1│ PF3=Back  PF7=Up  PF8=Down                                                      │  BOTTOM BAND
```

### Bands

- **Top band (rows 0–2)** — fixed, every screen:
  - **r0** Panel title, centered, White-intense. Lists also show a right-justified
    `ROW x TO y OF z` row indicator on r0.
  - **r1** Command line. Menus use `Option ===>`; list panels use `Command ===>`.
    **Entry panels (login, MFA, forms) leave r1 blank** — they have no command line
    (see deviation 1).
  - **r2** Message line: Red-intense, blank unless there is an error/message. Field
    name remains `errormsg` (and `screens.FieldError`) so presenters and unit tests
    are unaffected by the move from the old bottom position.
- **Body (rows 3 … N−3)** — screen-specific. On lists, **r3 is the Blue column
  heading row** and data rows follow.
- **Bottom band**:
  - **rN−2** Legend row (list line-command help, e.g. `S=Select  D=Delete`). Lists
    only; other screens leave it blank.
  - **rN−1** PF-key help row, Turquoise (see §3).

### Geometry helpers

The band model replaces the old all-bottom-anchored helpers. Target helper set
(names indicative; see the implementation plan for exact arithmetic):

| Helper | MOD-2 row | Meaning |
|---|---|---|
| `TitleRow()` | 0 | centered title |
| `CommandRow()` | 1 | `Option`/`Command ===>` |
| `MessageRow()` | 2 | red message line |
| `BodyTopRow()` | 3 | first body row (column headings on lists) |
| `LegendRow()` | N−2 | list line-command legend |
| `HelpRow()` | N−1 | PF-key help |

Moving the message band to the top **frees the two bottom rows** the old error line
occupied; list page size and form capacity are recomputed from the new bands
(roughly neutral to slightly larger versus today). Title centering is computed from
`Cols` (centered within 0–79).

### The menu option grid (tri-color)

All three **option menus** — service, admin, and User Settings — render their
selectable rows on one shared three-field grid, matching the ISPF Primary Option
Menu look:

| Field | Column | Color | Attr | Example |
|---|---|---|---|---|
| Option key | 0 (right-aligned in cols 0–2) | **White** | Intense | `1`, `0`, `A` |
| Name (short keyword) | 6 | **Turquoise** | — | `Users`, `Sysparms` |
| Description | 17 | **Green** | — | `User accounts and group membership` |

(Three blank columns separate number↔name and name↔description — names are hard-cut
to 8, so the description column clears the widest name. The trailing `A Administration`
meta-row carries only the option key + a description-column label; the leading
`0 Settings` row (see below) is a full three-field grid row. On the service menu the
right-hand status block sits at col 60, clearing a full 40-char description.)

The service menu already renders this exactly (number/`Service.Name`/
`Service.Description`). The admin and User Settings menus adopt the same grid using
short ISPF-style keywords as the turquoise name and the full meaning in the green
description.

**Admin menu keyword mapping** (turquoise name → green description):

| Key | Name | Description |
|---|---|---|
| 1 | `Users` | User accounts and group membership |
| 2 | `Groups` | Group definitions |
| 3 | `Services` | Backend TN3270 services |
| 4 | `Sysparms` | Runtime system parameters |
| 5 | `Networks` | Trusted networks (DoS allow-list) |
| 6 | `Audit` | Browse the audit trail |
| 7 | `Sessions` | Active client sessions |
| 8 | `Documents` | MOTD and login branding text |

The User Settings menu is adaptive (its rows depend on MFA state), so its
`UserSettingsRow` carries a short `Name` plus a `Description` rendered on the same
grid — e.g. `Password / Change your sign-on password`, `MFA / Enroll in multi-factor
authentication`.

**`0 Settings` leads the service list (GH #130).** Matching the real ISPF Primary
Option Menu (`0  Settings   Terminal and user parameters`), the self-service entry
renders as a full grid row — `0` / `Settings` (turquoise name, col 6) / `User and
security parameters` (green description, col 17) — at the **top** of the list,
immediately above service `1`, on the **first page only** (it is "before 1", which
lives only on page 0; `0` stays typeable from any page). It is omitted entirely for
settings-locked users. `A Administration` keeps the letters-at-the-end convention and
flows in the trailing meta-row on every page. The capacity math (`MenuCapacity`)
reserves the `0` slot on every page so global numbering and PF7/PF8 paging stay
stable regardless of which page renders it.

---

## 3. PF-key help row

- **Format:** `PFn=Verb` — **no spaces** around `=`, **two spaces** between entries,
  terse **Title-case** verb. (Fixes today's `Enter = save    PF3 = cancel` outlier.)
- State `Enter=...` only when the Enter action is non-obvious — `Enter=Save` on
  forms, `Enter=Confirm` on MFA enroll. A bare menu selection needs no `Enter=`.
- **PF3 keeps a contextual label** — the word reflects what stepping back actually
  does at that level. We standardize the *format*, not the word:

  | Screen | PF3 label | Effect |
  |---|---|---|
  | Login | `PF3=Disconnect` | drops the connection |
  | Service menu | `PF3=Logoff` | logoff; re-login re-evaluates groups |
  | Admin menu / User Settings | `PF3=Main Menu` / `PF3=Service Menu` | up one level |
  | Admin sub-screen (list/form) | `PF3=Back` / `PF3=Cancel` | up one level / abandon edit |
  | MFA enroll / verify | `PF3=Cancel` | return to login |

- **Paging:** `PF7=Up  PF8=Down` on paged lists. **Add:** `PF4=Add` where the list
  supports it. (`PF1=Help` is reserved for if/when contextual help lands.)

---

## 4. Per-screen audit decisions

Keep / change / deviate for every existing screen. ✔ = conforms after this work.

| Screen | File | Command line | Key changes from pre-#65 | Deviation |
|---|---|---|---|---|
| **Login** | `screens/login.go` | none | center title; message→r2; turquoise labels; green inputs; keep `Userid . . .` dot-leader | entry panel: no command line |
| **Service menu** | `screens/menu.go` | `Option ===>` r1 | command line→top; message→r2; instruction line turquoise; already on the tri-color grid; **`0 Settings` leads the list on page 1 (GH #130)** | — |
| **Admin menu** | `screens/admin.go` | `Option ===>` r1 | center title; command line→top; message→r2; **adopt tri-color grid** + ISPF keywords (see §2 mapping) | — |
| **User Settings** | `screens/usersettings.go` | `Option ===>` r1 | center title; command line→top; message→r2; **adopt tri-color grid** (name + description per row) | — |
| **MFA enroll** | `screens/mfa.go` | none | center title; "MFA now required" → **yellow caution**; `Key:` intense; message→r2 | entry panel: no command line |
| **MFA verify** | `screens/mfa.go` | none | center title; message→r2; code field green | entry panel: no command line |
| **List view** | `ui3270/screen.go` `buildListScreen` | `Command ===>` r1 | center title; **blue column headings**; RowInfo→r0 right; message→r2; legend near bottom | per-row line commands retained |
| **Form view** | `ui3270/screen.go` `buildFormScreen` | none | center title; **turquoise labels** (was monochrome); message→r2; standard PF help | entry panel: no command line |
| **MOTD / NEWS** | `screens/news.go` | n/a | **no work** — exempt | chrome-less pre-ISPF layer |

The form view drives **System Parameters**, user/group/service edit, and user
details; the list view drives the users / groups / services lists. Polishing the
two `ui3270` builders polishes all of them at once.

### Planned screens (build to this guide)

- **#63 User Settings** — the self-service sub-menu uses the menu pattern
  (`Option ===>`); its Change Password form uses the form pattern; it reuses the MFA
  enroll/verify screens. All already covered above.
- **#40 Audit Log viewer** — the RECENT ACTIVITY list uses the list pattern
  (`Command ===>`, blue headings, `S` line command, RowInfo on r0); the record
  detail uses a read-only field panel (DetailView). Built to this guide from day one.

### Editor screens

The `ui3270.RunEditor` driver and `buildEditorScreen` implement an ISPF-style
line editor (Documents — MOTD and login branding). Key layout and rules:

**Column layout per data row:**

| Columns | Role | Color / attr |
|---|---|---|
| 0 (attribute byte) | Separates rows; never visible | — |
| 1–2 | Prefix command input (2 chars) | Green, underscored, writable |
| 3 (attribute byte) | Separates prefix from text area | — |
| 4–79 | Editable text (76 columns) | Green, writable; Yellow + protected when over-wide |

**Ruler row (body top row, r3):** a Blue protected field showing the classic
ISPF column ruler (`----+----1----+----2----+----3…`) aligned with the text
columns at col 4.

**Prefix commands (I / D / R):** typed in the 2-char prefix area then Enter
to apply. An invalid command vetoes the PREFIX SET (all commands apply or none
do); text changes from the same transmission still apply — they were made
against the lines as displayed and are kept. Commands apply in descending
line-index order so structural shifts do not move targets. The document can
never become empty:
deleting the last line leaves one blank line.

**Key map:** `Enter=Apply  PF3=Save+End  PF7=PgUp  PF8=PgDn  PF12=Cancel`
(PF3 is ISPF's `END` — save and return; PF12 is ISPF's `CANCEL` — discard all
changes). If a prefix error is pending when PF3 is pressed, the error is shown
and the save is deferred (re-press PF3 after the screen re-presents).

**Over-wide lines:** lines whose rune count exceeds 76 (editorTextMax) render
Yellow and protected. Text changes to them are silently ignored — only
structural prefix commands (I/D/R) and re-import can change them.

**Trailing-NUL rule:** editable text fields are written to the screen WITHOUT
trailing space padding. The field's unused trailing positions stay NUL, which
is what enables native 3270 Insert mode (padding with spaces would lock the
keyboard). Never add trailing space padding to editor text fields.

---

## 5. Cursor home

Cursor placement stays builder-owned via `screens.cursorAt` / the `Cursor` returned
by each `ui3270` builder — `(field.Row, field.Col + 1)` to clear the attribute byte
(see the CLAUDE.md "go3270 cursor" gotcha). With the command line at the top, menus
and lists home to the **command-line input** (r1); entry panels home to their first
entry field; an empty list homes to `{0,0}`. The s3270 smoke script asserts cursor
row/col per screen as the regression guard.

---

## 6. Deviation Log

Conscious departures from textbook ISPF, with rationale:

1. **Entry panels carry no command line.** Login, MFA enroll/verify, and the
   `ui3270` forms are logon/data-entry panels; ISPF logon and data-entry panels also
   omit the `Option/Command ===>` line. The top command-line band (r1) is left blank
   on these screens.
2. **PF-key help is a single descriptive row,** not the live two-row PFK display a
   real 3270 session can show. Terminal-budget pragmatism; the one row advertises the
   active keys per §3.
3. **MOTD/NEWS is chrome-less** — the pre-ISPF TSO/READY layer. Settled in #65; not
   brought under these conventions.
4. **The menu status block is fixed at column 57** (rows-only adaptation). It does
   not widen on taller/wider models; content stays within cols 0–79.
5. **Command line at the top only.** Real ISPF can place the command line at top or
   bottom (a user setting). We fix it at the top (r1) and do not offer the bottom
   option, for layout simplicity.
6. **No action bar.** Later ISPF versions show a pull-down action bar on row 0
   (`Menu  Utilities  …`). We omit it: it post-dates the classic look we target and
   is overkill for a single-purpose gateway. The title owns row 0.
7. **List panels have no command line yet.** The guide calls for `Command ===>` on
   list panels, but the `RunList` driver has no primary-command processing — adding
   a functional command line is deferred to a follow-up. Lists use line commands
   (S/D) + PF keys; row 1 stays blank. (The `Option`/`Command` split still governs
   the label once it lands.)
8. **Login screen (branding-forward) — layout exception.** The login screen deviates
   from the three-band convention to make room for operator branding art:

   - Rows 0–3 are a status header: centered title (row 0) and the
     Date / Time / System ID / Release block at the status column (rows 0–3).
   - The red error line moves to **row 1** (col 2), truncated so it cannot collide
     with the Time block at the status column.
   - Rows 4 .. `BodyBottomRow()-1` render the BRANDING document content (cols 0–79
     verbatim, no indent), vertically centered when shorter than the region and
     top-aligned/clipped when taller.
   - The `User ID` and `Password` fields share `BodyBottomRow()` (the password
     field's input reaches col 78); `PF3=Disconnect` stays on `HelpRow()`.

   Like MOTD/NEWS, this is an intentional, documented exception — not a model for
   new ISPF-layer screens.

---

## 7. Verifying conformance

Unit tests assert field **names and content**, not row numbers — so they survive
layout moves. The 3270 *protocol surface* (exact placement, color, cursor) is only
truly verified against a real emulator: run the **s3270-smoke-testing** skill
(`.claude/skills/s3270-smoke-testing/`) before declaring screen work done, and do a
human pass in c3270 for visual polish. Update the smoke script's asserted
cursor/positions when a screen's layout changes.
