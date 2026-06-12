# Service menu meta-band flow (GH #133) — design

**Issue:** https://github.com/coffeemuse/tn3270proxy/issues/133
**Date:** 2026-06-12
**Status:** approved

## Goal

Stop bottom-anchoring the service menu's conditional meta entries (`0 User
Settings`, `A Administration`). Render them as a continuation of the service
list — directly below the last service row, separated by a single blank row —
so a sparse menu (2–3 services) no longer shows a large empty gap between the
list and the meta entries.

## Current behavior

`MenuScreen` (`internal/screens/menu.go`) anchors the band at
`geom.menuBottomRow()` (= `BodyBottomRow()-1`): `A` on that row (admins), `0`
one row above (or on it for non-admins), regardless of how many services the
page shows. `Geometry.MenuCapacity(admin)` reserves the band rows (plus the
blank row excluded via `menuBottomRow`) so service rows never collide with it.
`internal/screens/menu_paging_test.go` asserts this bottom anchoring on every
page.

## New rendering contract

All rows below are 0-based; MOD 2 examples assume `BodyTopRow()=3`,
`BodyBottomRow()=22`, `menuBottomRow()=21`.

1. **Service rows are unchanged:** the page window renders from
   `BodyTopRow()+1` down, one service per row, global stable numbering,
   `MenuCapacity` page size. Paging (PF7/PF8, `MenuPageBounds`) is untouched.
2. **Band anchor flows with the list.** Let `next` be the row after the last
   rendered service row:
   - **≥1 service rendered on the page:** one blank separator row at `next`;
     the band starts at `next+1`.
   - **Empty list (zero services for the user):** the
     `(no services available for your account)` placeholder renders on
     `BodyTopRow()+1` as today; the band starts directly beneath it at
     `BodyTopRow()+2` — **no separator** (the placeholder is a message, not a
     service row).
3. **Band composition is unchanged:** `0 User Settings` (omitted when
   `settingsLocked`) first, `A Administration` (admins only) below it. When
   `0` is hidden, `A` takes the anchor row. When neither renders (locked
   non-admin), nothing renders — the separator is a blank row, so the rule is
   moot.
4. **`MenuCapacity` is unchanged.** The row budget already reserves one row
   for `0` (even when hidden), one for `A` (admin), and one blank (via
   `menuBottomRow`). Only the band's *position* changes; page size, paging
   math, and the stable-numbering mapping do not move.
5. **Full-page consequence (accepted):** on a full page the band shifts down
   one row from today, because the blank row that used to sit *below* the band
   (above the PF legend) becomes the separator *above* it. The band's last row
   lands on `BodyBottomRow()`, flush above the PF-key legend — legal per the
   ISPF style guide (the body extends through `BodyBottomRow`; other screens
   use it). No clamping.

### MOD 2 examples

| Scenario                          | Rows                                                      |
|-----------------------------------|-----------------------------------------------------------|
| 3 services, admin, unlocked       | svcs 4–6, blank 7, `0` 8, `A` 9                           |
| 3 services, non-admin, locked     | svcs 4–6, nothing else (band empty)                       |
| Empty list, admin, unlocked       | placeholder 4, `0` 5, `A` 6                               |
| Full page, non-admin (17 svcs)    | svcs 4–20, blank 21, `0` 22                               |
| Full page, admin (16 svcs)        | svcs 4–19, blank 20, `0` 21, `A` 22                       |

## Implementation (approach: flow-anchor inside MenuScreen)

`MenuScreen`'s render loop already tracks a running `row` cursor. After the
loop:

- services rendered → band anchor = `row + 1` (skipping the separator row);
- empty list → band anchor = placeholder row + 1.

`menuBottomRow()` survives purely as `MenuCapacity` input; its doc comment (and
`MenuCapacity`'s, and `MenuScreen`'s) change from "bottom-anchored band" to
"page-window row budget". No `Geometry` API changes, no presenter/session
changes, no store/server changes.

Rejected alternatives: a `Geometry`-level anchor method (would be the only
Geometry method depending on page *content*, muddying that file's pure-formula
contract); rendering the band as pseudo-list rows (entangles the selection
mapping and paging for no benefit).

## Testing (TDD)

1. **Rewrite the bottom-anchor assertions** in
   `internal/screens/menu_test.go` (the `us.Row ==
   menuBottomRow()-1` / `admin.Row == menuBottomRow()` checks) to the new
   contract: band row = last service row + 2 on pages with services.
2. **New unit cases** (assert rows relative to the geometry formulas, not
   literals): sparse page (3 services) admin/non-admin × locked/unlocked;
   empty list with and without admin; full page non-admin (`0` on
   `BodyBottomRow()`) and admin; second page of a paged menu (full page rules
   apply to mid-stack pages; a sparse final page hugs its short list).
3. Watch the new assertions fail, then make the `MenuScreen` change.
4. `go test ./... -race`, `go fmt ./...`.
5. **Smoke:** `.claude/skills/s3270-smoke-testing/smoke.sh` — existing menu
   checks are content-presence only and should pass unchanged; re-run to
   confirm. Final visual QA in c3270 (sparse menu, full menu, empty menu).

## Out of scope

- Any change to paging math, selection dispatch, or the status block.
- MOD 3/4/5-specific layout work (the formulas adapt automatically; #130
  covers the broader CUA verification pass).
