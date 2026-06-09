# Branding-forward login screen — design

**Issue:** [#101](https://github.com/coffeemuse/TN3270Proxy/issues/101)
**Date:** 2026-06-09
**Status:** Approved (brainstorm) — pending implementation plan

## Goal

Re-architect the TN3270 login screen into a **branding-forward** layout so operators
can display custom ASCII/EBCDIC art or a logo (MVS, z/OS, a site banner) as the
centerpiece of the first screen a user sees — while preserving every existing login
function: credential entry, the status block (Date / Time / System ID / Release), the
error line, and `PF3=Disconnect`.

The art is supplied by an operator-managed text file whose path is a runtime system
parameter, read **fresh on every login paint** so content can be updated live with no
restart or session interruption. The mechanism deliberately mirrors the existing
MOTD/NEWS file feature.

## Non-goals

- No horizontal centering or auto-indent of the art. The branding file author owns
  column layout; the proxy renders columns 0–79 verbatim and hard-cuts at 79.
- No paging/scrolling of branding (unlike MOTD, which paginates). Branding is a
  single fixed region; overflow is clipped.
- No admin **upload** of the file through the 3270 UI. The admin only sets the *path*
  (System Parameters form); the file itself is managed out-of-band (or seeded by
  `quickstart`).
- No new config-file (`tn3270proxy.json`) field. The path lives in the DB-backed
  system parameters, exactly like `MOTD_FILE`.

## Layout (0-based; MOD 2 / 24 rows shown)

```
Row 0:  ............TN3270 GATEWAY LOGIN............Date . . : 26.160......   title centered │ Date  col 60
Row 1:  Invalid credentials........................Time . . : 21:14........   error col 2    │ Time  col 60
Row 2:  ..........................................System ID: SYS1..........                  │ SysID col 60
Row 3:  ..........................................Release. : v1.2.3........                  │ Rel   col 60
Row 4:  ┐
  ...   │  branding file — cols 0–79 verbatim, no indent, hard-cut at 79,
Row 21: ┘  VERTICALLY CENTERED in rows 4..(BodyBottomRow-1)
Row 22: User ID . . : ________  Password . . : ____________________________   inputs; pw field ends col 78
Row 23: PF3=Disconnect..........................................................
```

### Header band (rows 0–3)

The old contiguous right-hand status block is replaced by four **individually placed**
rows at `Geometry.StatusBlockCol()` (column 60), same labels and values as today:

| Row | Left (col 2)                  | Right (col 60)        |
|-----|-------------------------------|-----------------------|
| 0   | centered title `TN3270 GATEWAY LOGIN` (white, intense) | `Date . . :` + `YY.DDD` |
| 1   | error line (red, intense)     | `Time . . :` + `HH:MM` |
| 2   | —                             | `System ID:` + value (≤7) |
| 3   | —                             | `Release. :` + value (≤7) |

- No `User ID` value (pre-login) and no `Terminal` row — unchanged from today's login
  status block, just relocated/split.
- The clock fields (`Date`, `Time`) are stamped at paint time by the presenter
  (`status.Now = time.Now()`), exactly as today.
- **Error truncation:** the error line shares row 1 with the Time block at col 60, so
  the error text is hard-cut to fit columns 2..57 (≤56 runes) to guarantee no overlap.
  Current generic errors ("User ID is required", "invalid credentials") are well under
  this; the cut is defensive.

### Branding region (rows 4 .. `BodyBottomRow()-1`)

- Region top is fixed at row 4 (immediately below the header band). Region bottom is
  `BodyBottomRow()-1` — i.e. the row just above the credential row. On MOD 2 that is
  rows 4..21 (height 18); on taller models the region grows because `BodyBottomRow()`
  grows.
- **Vertical placement rule.** Let `L` = number of branding lines, `H` = region height.
  - `L ≤ H` → vertically centered: top padding `= (H - L) / 2` (integer division),
    lines start at `regionTop + topPad`.
  - `L > H` → top-aligned clip: render the **first `H`** lines starting at `regionTop`,
    drop the remainder. (Centering is impossible when it overflows; the admin owns
    validating that the art fits.)
  - `L == 0` (missing/empty/disabled) → region renders blank; chrome + inputs still work.
- **Horizontal rule.** Each line is placed at column 0 verbatim, hard-cut at column 79
  (`newsMaxCols`-style). No indent, no centering. Trailing `\r` stripped. Trailing
  whitespace-only lines are dropped before measuring `L` (so a conventional EOF newline
  doesn't skew centering); leading/interior blank lines are preserved (author owns
  vertical spacing within the art).
- Each line is a **protected** field (read-only), colored — default color matches the
  MOTD convention (the art is informational chrome). Color choice is a builder detail;
  the file content is plain text (no embedded attributes).

### Credential row (`BodyBottomRow()`, = 22 on MOD 2)

Both fields share the second-to-last row; **dot-leader labels** (app-consistent, chosen
over `===>`). Illustrative (exact columns finalized in implementation):

```
User ID . . : [u_____________]  Password . . : [p_____________________ … col 78]
```

- `User ID . . :` label (turquoise) at col 2; username input field (≈16 wide); stop field
  closes it.
- `Password . . :` label (turquoise); password input field (hidden) extends to **col 78**
  per the requirement (stop/closing field at col 79). The exact column split between the
  two labels/fields is finalized during implementation; the binding constraints are:
  password field's last input column is 78, both labels use the dot-leader style, and the
  layout stays within cols 2..79.
- Field names unchanged: `FieldUsername`, `FieldPassword`. Validation rule unchanged
  (`FieldUsername` NonBlank → "User ID is required").
- **Cursor** homes to the username field via the existing `cursorAt(field)` helper
  (`(field.Row, field.Col+1)`).

### PF-key help (`HelpRow()`, = 23 on MOD 2)

`PF3=Disconnect` (turquoise), unchanged.

### Larger models (MOD 3/4/5)

All header rows stay fixed at 0–3. `BodyBottomRow()` and `HelpRow()` grow with the
geometry, so the credential row and PF-help row ride the bottom; the branding region
(rows 4 .. `BodyBottomRow()-1`) grows and re-centers automatically. Content stays within
columns 0–79 (rows-only adaptation, consistent with the rest of the app).

## Architecture & data flow

Mirror the MOTD/NEWS feature precisely. Three layers change.

### 1. `internal/sysconfig` — new catalog entry

```go
// KeyBrandingFile is the system_config key whose value is the absolute path to
// the login branding/art file. Empty disables the feature (blank region).
const KeyBrandingFile = "BRANDING_FILE"
```

Add an `Entry` to `Catalog`, placed in the identity group next to `MOTD_FILE`:

```go
{
    Key:     KeyBrandingFile,
    Label:   "Branding File:",
    Default: "",
    // Empty disables; any non-empty path accepted. Existence/readability is
    // checked at read time, not here (matches MOTD_FILE).
    Validate: func(_ string) string { return "" },
},
```

The store seeds the default via `reconcileDefaults`/`INSERT OR IGNORE` (reaches existing
DBs — no migration needed; it is a new code-defined default, per CLAUDE.md). The admin
System Parameters form builds the field from the catalog automatically.

### 2. `internal/server` — fresh per-paint read in the session

- Add a `BrandingRead func(path string) ([]byte, error)` seam to `Session` (parallel to
  `MOTDRead`); nil selects `readBrandingCapped`, an 8 KiB-capped reader cloned from
  `readMOTDCapped` (reuse the existing `motdReadCap` constant, or a sibling
  `brandingReadCap = 8 << 10`).
- In `doLogin`'s prompt loop (`internal/server/session.go`), **before each**
  `s.Presenter.Login(...)` call, resolve the branding lines:
  1. `path, err := s.Store.GetConfig(ctx, sysconfig.KeyBrandingFile)` — error → warn,
     nil lines.
  2. Empty path → nil lines (feature disabled).
  3. Relative path → warn ("branding path not absolute; skipping"), nil lines
     (same guard as MOTD).
  4. Read via the (capped) reader; unreadable → warn, nil lines.
  5. Split into lines with the branding rules (strip `\r`, hard-cut col 79, drop
     trailing blank lines). A small pure helper in `internal/screens`
     (e.g. `SplitBranding(raw) []string`, analogous to `PaginateNews`) keeps the parse
     testable and shared.
- Reading inside the loop (not once before it) is what makes edits go live per paint.

### 3. Presenter & screen signatures

- `Presenter.Login(conn, term, status, errMsg)` → add a `branding []string` parameter:
  `Login(conn net.Conn, term Term, status screens.MenuStatus, branding []string, errMsg string) (...)`.
- `go3270Presenter.Login` passes `branding` straight through to `screens.LoginScreen`.
  The presenter remains a thin go3270 driver with **no Store handle** — the session owns
  the config/file read (preserves the existing layering).
- `screens.LoginScreen(geom, status, errMsg)` → `screens.LoginScreen(geom, status,
  branding []string, errMsg string)`. The builder lays out the header band, the centered/
  clipped branding region, the credential row, and the PF-help row, and returns the
  cursor on the username field.

### Geometry helpers

Add small formula methods to `internal/screens/geometry.go` to keep the branding math
out of the builder and unit-testable:

- branding region top = constant 4 (a named helper or const).
- branding region height = `BodyBottomRow() - 1 - 4 + 1` = `BodyBottomRow() - 4`.

(Exact helper names are an implementation detail; the formulas above are the contract.)

### Rejected alternatives

- **(B) Read branding in the presenter** (give `go3270Presenter` a Store reference):
  breaks the established boundary where the session owns config/file reads and the
  presenter is a thin go3270 driver. Rejected.
- **(C) Store the art as a sysconfig *value* in the DB** (not a file): the requirement is
  a file editable live with an admin-defined path; an inline DB value can't be edited
  with a text editor out-of-band and bloats the params row. Rejected.

## Quickstart seeding

`quickstart` is the **only** command that creates an example branding file (mirrors
`motd.txt`):

- `internal/quickstart/paths.go`: add `Branding string` to `Layout` and
  `Branding: filepath.Join(dir, "branding.txt")` in `NewLayout`.
- New `internal/quickstart/branding.go`: `DefaultBranding() string` returning a small
  ASCII banner sized to fit comfortably inside the region (e.g. a boxed
  `TN3270 GATEWAY` title + `QUICK-START DEMO` subtitle), authored pre-centered
  horizontally by the file content itself.
- `internal/quickstart/provision.go`: write `l.Branding` (0644) and
  `st.SetConfig(ctx, sysconfig.KeyBrandingFile, l.Branding)`, alongside the existing
  MOTD provisioning.

Hand-run `serve` deployments author their own art and point `BRANDING_FILE` at it via
the System Parameters form.

## Testing

**`internal/screens` (pure, deterministic — assert content/color/field presence and the
placement *logic*, not arbitrary row numbers):**
- `SplitBranding`: `\r` strip, col-79 cut, trailing-blank-line trim, empty → nil.
- Vertical centering when `L < H` (top padding correct); top-aligned clip when `L > H`
  (first `H` lines, rest dropped); blank region when `L == 0`.
- Header band: Date/Time/System ID/Release present on rows 0–3 at the status column;
  title centered on row 0; error field present on row 1 and truncated when over-long.
- Credential row: both `FieldUsername` and `FieldPassword` present, password field's last
  input column is 78, username field hidden=false, password hidden=true.
- Cursor returned on the username field (`cursorAt`).
- MOD 3/4/5 geometry: region height grows; credential + help rows bottom-anchored.

**`internal/server` (clone the MOTD test shape):**
- Branding read fresh per `Login` paint (reader invoked each loop iteration).
- 8 KiB cap enforced.
- Relative path / empty / unreadable / GetConfig error → blank region + warn, login still
  paints.

**`internal/sysconfig`:** `BRANDING_FILE` present in `Catalog` with empty default and a
permissive validator.

**`internal/quickstart`:** provisioning writes `branding.txt` and sets `BRANDING_FILE` to
its path; `DefaultBranding()` fits within the MOD 2 region.

**s3270 smoke test** (`.claude/skills/s3270-smoke-testing/`): update the login-screen
assertions to the new layout — status fields on rows 0–3, branding lines present (seed a
known file), credential row field positions, cursor row/col on the username field, PF3.

## Documentation

- `docs/ispf-style-guide.md`: document the login screen as a second deliberate exception
  to the three-band convention (alongside MOTD/NEWS) — header band on rows 0–3, branding
  body, credential row second-to-last, PF help last.
- `CLAUDE.md`: update the `internal/screens` (login.go new layout + branding region),
  `internal/sysconfig` (new `BRANDING_FILE` key), `internal/server` (per-paint branding
  read seam), and `internal/quickstart` (branding seeding) entries in the package map;
  note the login screen as a layout exception in the Gotchas section.

## Implementation order (for the plan)

1. `sysconfig.KeyBrandingFile` + catalog entry + test.
2. `screens.SplitBranding` + `LoginScreen` rewrite (header band, branding region,
   credential row) + geometry helpers + tests. (TDD: tests first.)
3. `Presenter.Login` / `go3270Presenter.Login` signature + branding pass-through; update
   all fakes/callers.
4. Session: `BrandingRead` seam + `readBrandingCapped` + per-paint read in `doLogin`
   + tests.
5. Quickstart: `Layout.Branding`, `DefaultBranding()`, provisioning + tests.
6. Docs (style guide, CLAUDE.md) + s3270 smoke-test update.
7. `go test ./... -race`, then a live c3270 / s3270 pass.
