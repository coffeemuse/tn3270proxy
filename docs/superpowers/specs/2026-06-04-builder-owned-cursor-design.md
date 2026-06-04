# Builder-owned initial cursor (GH #33)

**Date:** 2026-06-04
**Issue:** [#33](https://github.com/coffeemuse/tn3270proxy/issues/33) — sub-issue of #15, item 1.
**Scope:** #33 only. Independent of #15b / #15c.

## Problem

The initial input-cursor coordinates are hardcoded in the presenters, while the
field positions they depend on are owned by the `screens` package:

| Screen | Presenter cursor | Input field (builder) | `Col+1`? |
|---|---|---|---|
| Login | `3, 17` (`presenter.go:81`) | username `Row 3, Col 16` (`login.go:42`) | ✓ |
| Menu | `geom.InputRow(), 8` (`presenter.go:103`) | selection `InputRow, Col 7` (`menu.go:73`) | ✓ |
| Admin menu | `geom.InputRow(), 8` (`presenter_admin.go:68`) | option `InputRow, Col 7` (`admin.go:143`) | ✓ |
| Admin list | `4, 3` / `0,0` empty (`presenter_admin.go:93`) | cmd0 `Row 4, Col 2` (`admin.go:71`) | ✓ |
| Admin form | `3, 17` (`presenter_admin.go:118`) | field0 `Row 3, Col 16` (`admin.go:122`) | ✓ |

Every current value is **correct** — this is a *preventive* refactor, not a
bugfix. The risk it removes: the cursor literal lives in the presenter while the
field it targets lives in `screens`, and nothing fails a unit test if they
drift. `admin.go:103-105` even documents `(3, 17)` in prose with nothing
enforcing it. This is precisely the emulator-only bug class CLAUDE.md warns
about — the `(field.Row, field.Col+1)` rule is only truly checked by a real
emulator / s3270's status line.

## Fix

Have each screen builder return its initial cursor, applying the
`(field.Row, field.Col+1)` rule **once**, in the package that owns field
geometry. Presenters consume the returned cursor instead of re-deriving it.

### 1. The `Cursor` type and the single rule

New file `internal/screens/cursor.go`:

```go
// Cursor is a screen builder's initial input-cursor position, already
// adjusted for the 3270 attribute-byte offset. Cursor{0,0} means "home"
// (no input field, e.g. an empty admin list).
type Cursor struct{ Row, Col int }

// cursorAt applies the go3270 attribute-byte rule: a field's Col is its
// attribute byte, so input begins one column right.
func cursorAt(f go3270.Field) Cursor { return Cursor{f.Row, f.Col + 1} }
```

The `+1` rule lives in exactly one function. Each builder finds its primary
input field (which it already constructs) and passes it to `cursorAt`, rather
than hand-writing the arithmetic.

**Empty-list home:** `Cursor{0,0}`. Real input cursors are always `Col+1 >= 1`,
so `(0,0)` is unambiguous as the conventional 3270 home position. No extra flag
or pointer indirection.

### 2. Builder signature changes

Each builder returns its cursor as the last return value:

- `LoginScreen(...) (go3270.Screen, go3270.Rules, Cursor)` → `cursorAt(usernameField)`
- `MenuScreen(...) (go3270.Screen, map[string]store.Service, Cursor)` → `cursorAt(selectionField)`
- `AdminMenuScreen(...) (go3270.Screen, Cursor)` → `cursorAt(optionField)`
- `AdminListScreen(...) (go3270.Screen, Cursor)` → `cursorAt(cmd0)` when rows exist, else `Cursor{0,0}`
- `AdminFormScreen(...) (go3270.Screen, Cursor)` → `cursorAt(field0)`

The empty-list branch is deleted from `presenter_admin.go:94-96` and moves into
`AdminListScreen` — the one place that knows `len(v.Rows) == 0`. The prose
comment at `admin.go:103-105` is replaced by the actual returned value.

### 3. Presenter changes

Each presenter captures the returned cursor and passes `cur.Row, cur.Col` to
`HandleScreenAlt` in place of the literals at `presenter.go:81,103` and
`presenter_admin.go:68,93,118`.

## Testing (TDD order)

1. **Builder unit tests first.** Assert each builder's returned `Cursor` equals
   `(primaryField.Row, primaryField.Col+1)` by *finding the input field in the
   returned screen by name and computing the expectation from it* — never by
   hardcoding `{3,17}`. The test then follows the field if it moves, which is
   the whole point. Include the empty-admin-list `→ Cursor{0,0}` case.
2. Rewire presenters. `go build ./...` and `go test ./... -race` green.
3. **s3270 smoke assertion.** Extend the smoke script to assert cursor row/col
   on each screen (login, menu, admin menu, admin list populated + empty, admin
   form), closing the unit-test gap permanently so the coupling cannot silently
   rot again.

## Out of scope

- #15b / #15c (other sub-issues of #15).
- Any change to field positions themselves.
