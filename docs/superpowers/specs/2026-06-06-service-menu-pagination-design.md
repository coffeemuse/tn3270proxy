# Service Menu Pagination (PF7/PF8) — Design

**GitHub issue:** #77
**Related:** #65 (ISPF screen polish — three-band layout, style guide), `ui3270.RunList`
paging engine used by the admin lists.
**Date:** 2026-06-06

## Goal

The group-filtered service menu must page through the user's full service list instead of
silently truncating it to the first screenful. Today
[`screens.MenuScreen`](../../../internal/screens/menu.go) hard-cuts the slice to
`geom.MenuCapacity(admin)` and the presenter registers no paging AIDs, so the overflow is
unreachable and PF7/PF8 flash a red `PF8: unknown key`. This is a release-blocking access
gap for any account that can see more than one screenful of services (~17 on a MOD 2).

## Behaviour summary

- The menu pages forward (PF8) and back (PF7) through the complete group-filtered list; no
  service is unreachable.
- PF7 at the first page and PF8 at the last page are **no-ops**, not errors.
- An `ITEMS x TO y OF z` indicator is shown on row 0, right-aligned, mirroring the admin
  lists' `ROW x TO y OF z` slot (style guide top band, r0-right). Shown always (even for a
  single page), matching the admin-list convention. An empty list reads `ITEMS 0 OF 0`.
- The `0 User Settings` / `A Administration` meta entries render on **every** page, in a
  **fixed** position anchored to the bottom band.
- Selection numbering is **global and stable**: service #18 is always `18`, on whatever page
  it falls. Paging affects only *visibility*, not *selectability*.
- Works across MOD 2–5: page size derives from `Geometry`.

## Key decisions

### 1. Global (stable) numbering, full mapping

`MenuScreen` returns a mapping covering **all** services, keyed by global index
(`"1".."N"`). Only the current page's slice is rendered, but each rendered row shows its
**global** number (`start + i + 1`), not a per-page `i + 1`. `classifyMenuSubmit`
(unchanged) resolves any typed number against the full mapping, so a user can even select a
service that lives on another page by typing its number. This sidesteps any coupling between
the active page and what a submitted number means, and gives services stable numbers for
muscle memory.

### 2. Bespoke menu paging, not `ui3270.RunList`

The admin lists delegate paging to the generic [`ui3270.RunList`](../../../internal/ui3270/list.go)
engine, but the service menu is a different interaction model and cannot reuse it:

| | Admin lists (`RunList`) | Service menu |
|---|---|---|
| Selection | line commands (`S`/`D`) beside rows | number typed into `Option ===>` |
| Layout | full-width rows | grid + right-hand status block (cols 60+) + `0`/`A` meta band |
| Page size | `listPageSize` (`rows-10`) | `MenuCapacity(admin)` |

We keep the bespoke `MenuScreen` builder + `go3270Presenter.Menu` loop and add paging to
them. The *pattern* is mirrored; the *code* is not shared, because the page-size math
(`MenuCapacity`, not `listPageSize`) and the layout differ.

### 3. Menu-specific page-bounds helper

`ui3270.pageBounds` is keyed on `listPageSize` and is package-private, so it is not reusable
here. Add a small, unit-testable helper in `internal/screens` that clamps a page against a
total and a per-page size and returns `(clampedPage, start, end, indicator)`. Both
`MenuScreen` (to render the window + indicator) and `go3270Presenter.Menu` (to know when PF8
should no-op at the last page) use it, so the clamp logic lives in exactly one place.

### 4. Page state lives in the `Menu` loop

The page integer is local to the `for` loop in `go3270Presenter.Menu`. It therefore:
- **survives** an invalid-selection reprompt (a typo does not bounce you to page 1), and
- **resets to page 0** on every fresh entry to the menu (login, return from admin /
  UserSettings, PA3 back from a bridged session).

Resetting to page 0 after a bridge round-trip is the intended behaviour; preserving page
across a bridge would require lifting the state into `Session` and is out of scope.

## Layout change: one blank separator row above the PF legend (menu only)

Shorten the menu body by one row, leaving a blank line between the last menu content and the
PF-key legend. This is scoped to the **menu only** — `Geometry.BodyBottomRow()` is shared by
the login / admin / MFA screens and is **not** changed. The menu uses an effective bottom of
`BodyBottomRow() - 1`; the blank separator sits at `BodyBottomRow()`.

MOD 2 (24 rows) bottom-of-menu layout:

```
 r0   <centered TITLE>                                   ITEMS x TO y OF z
 r1   Option ===> _
 r2   <red message line>
 r3   Select a service and press ENTER:
 r4 ..              <service grid (page window)>        <status block, cols 60+>
 ...
 r20  (admin) "  0  User Settings"          ── meta band, fixed, anchored to r21
 r21  (admin) "  A  Administration"  / (non-admin) "  0  User Settings"
 r22  <blank separator>                                              ← new
 r23  PF3=Logoff … PF7=PgUp  PF8=PgDn   (PA3 returns here from a session)
```

The meta band is bottom-anchored at the menu's effective bottom (`BodyBottomRow() - 1`):
non-admin `0` on r21; admin `0` on r20 and `A` on r21. On a partially-filled page there is a
gap between the last service row and the meta band — that is expected with a fixed anchor.

### Effect on capacity

`MenuCapacity(admin)` drops by exactly one page row (the blank separator), so per-page
capacity becomes **17 non-admin / 16 admin** on MOD 2 (was 18 / 17). The formula keeps its
existing structure, substituting the effective bottom for `BodyBottomRow()`. Capacity scales
on MOD 3–5 because the separator is always one row above the legend.

## Components touched

- **`internal/screens/geometry.go`** — `MenuCapacity` uses the menu's effective bottom
  (`BodyBottomRow() - 1`) so the blank separator is reserved; add the menu page-bounds
  helper (clamp + window + `ITEMS x TO y OF z`).
- **`internal/screens/menu.go`** — `MenuScreen` gains a `page` parameter; renders the page
  window with global row numbers; returns the full mapping; renders the bottom-anchored meta
  band and the row-0 `ITEMS` indicator.
- **`internal/server/presenter.go`** — `go3270Presenter.Menu` tracks `page`; adds
  `AIDPF7`/`AIDPF8` to the valid AID set; on PF7 decrements, on PF8 increments only when a
  next page exists (no-op at the ends); preserves `page` across reprompts.
- **Smoke test** (`.claude/skills/s3270-smoke-testing/`) — fixture with ≥18 services for one
  account; assert page-1 content + indicator, PF8 → page 2 content, PF7 → page 1, PF8 at
  last page is a no-op, cursor position per page.

## Data flow

`Session.Run` → `go3270Presenter.Menu(page=0)` → `MenuScreen(geom, svcs, admin, status,
errMsg, page)` renders window + indicator and returns the **full** mapping → user submits.
PF7/PF8 adjust `page` and re-render in the same loop; ENTER with a number routes through the
unchanged `classifyMenuSubmit` against the full mapping; PF3 logs off.

## Error / edge handling

- **Empty service list:** indicator reads `ITEMS 0 OF 0`; the
  `(no services available…)` line and the `0`/`A` meta band still render; paging is a no-op;
  selection still routes `0`/`A` (checked before the mapping in `classifyMenuSubmit`).
- **PF7 on page 0 / PF8 on last page:** clamp leaves the page unchanged → silent re-render,
  no error message.
- **Selecting a service on another page:** allowed; the full mapping resolves it. No
  auto-flip to that page.
- **MOD 3–5:** larger page size; the separator and legend stay bottom-anchored.

## Testing (TDD)

Unit tests assert field *names / content / color* and the mapping, not row numbers (per the
repo convention), plus the page-bounds helper directly:

- **page-bounds helper:** clamp below 0 → 0; clamp above max → last page; `start/end`/
  indicator strings for first / middle / last / empty / single-page.
- **`MenuScreen`:** page 0 shows items 1..cap with global numbers; page 1 shows
  cap+1..; indicator content; meta band present on every page at the fixed rows; full
  mapping covers all services regardless of page; blank separator row is unoccupied.
- **`go3270Presenter.Menu`** (fake presenter / handleScreen): PF8 advances and PF7 retreats;
  PF8 at last page and PF7 at page 0 are no-ops; page preserved across an invalid-selection
  reprompt; a number for an off-page service still selects it.
- **Smoke test** as above — the protocol-surface regression guard (content + cursor +
  PF7/PF8), the final word per CLAUDE.md.

## Out of scope (YAGNI)

- Preserving page across a bridge round-trip (would require `Session`-level state).
- Auto-flipping to the page of an off-page selection.
- Generalizing `ui3270.RunList` to host the menu's selection-by-number + status-block model.
- Any change to the shared `Geometry.BodyBottomRow()` or non-menu screens.
