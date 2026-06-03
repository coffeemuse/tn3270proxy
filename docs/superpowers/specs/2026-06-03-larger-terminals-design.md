# Larger Terminal Models (MOD 3/4/5) — Design

**Date:** 2026-06-03
**Status:** Approved
**Roadmap item:** #7 (`docs/superpowers/ROADMAP.md`)

## Goal

Render the proxy's own screens correctly on terminals larger than the 24×80
default (MOD 2). Bottom-anchored elements (error line, PF-key help, input
line, admin list legend) move to the actual last rows; list screens use the
extra rows for more content per page.

3270 model geometries (rows × cols): MOD 2 = 24×80 (current hard-coded
assumption), MOD 3 = 32×80, MOD 4 = 43×80, MOD 5 = 27×132.

## Scope decisions

- **All proxy screens adapt:** login, service menu, admin menu, admin lists,
  admin forms. (The roadmap entry predates the admin UI; the admin lists are
  the biggest beneficiary — 14 → 33 rows per page on a MOD 4.)
- **Rows only; columns stay 0–79.** On a MOD 5 (27×132) content is
  left-aligned in the first 80 columns, classic-ISPF style. No column math,
  no widening, no centering in v1.
- **Codepage fix-along:** start passing `dev.Codepage()` into the screen
  calls (the go3270-recommended practice the presenter currently skips). It
  rides the same plumbing and fixes potential mojibake for non-1047
  terminals. Strictly additive — nil codepage means the current 1047 default.
- **Targeted improvement:** the menu builder now explicitly truncates the
  service list so it can never collide with the input/error/help rows
  (previously a silent pre-existing overflow at row 19). Menu pagination
  stays out of scope.
- **No config knobs** (no "force MOD 2" override) until a real need appears.

## Key library facts (go3270 v0.9.13)

- `NegotiateTelnet(conn)` returns a `DevInfo` exposing
  `AltDimensions() (rows, cols)`, `TerminalType()`, and `Codepage()`.
- `HandleScreenAlt(..., dev DevInfo, codepage...)` renders to the alternate
  screen size; **nil dev is defined as exactly `HandleScreen`'s 24×80
  behavior** — this doubles as our fallback path.
- `DevInfo` has a private method, so it **cannot be implemented or faked**
  outside go3270. The real object from negotiation must be retained and
  passed back; test fakes never construct one.

## Architecture

Approach chosen: **thread an opaque `Term` value through the seams**
(alternatives — a stateful per-connection presenter, or a conn-keyed map —
were rejected for hidden state / ordering dependencies / lifecycle races).

Two small types, one per layer:

```go
// internal/server — the seam value returned by Presenter.Negotiate.
type Term struct {
    Type string          // negotiated terminal type, e.g. "IBM-3278-4"
    Rows, Cols int       // alternate screen size from negotiation
    dev  go3270.DevInfo  // unexported: set only by real negotiation; nil in fakes
}

// internal/screens — what builders consume.
type Geometry struct{ Rows, Cols int }
```

Data flow:

```
NegotiateTelnet(conn) ─► DevInfo ─► Term{Type, Rows, Cols, dev}
        (go3270Presenter.Negotiate)        │
                                           ├─► Session.Run: term.Type → Bridger.Bridge (unchanged)
                                           ├─► Presenter.Login/Menu(conn, term, ...)
                                           ├─► adminFlow{term} → AdminPresenter.AdminMenu/List/Form(conn, term, ...)
                                           │       └─ paging math from term.Rows
                                           └─► inside go3270Presenter:
                                                 screens.XxxScreen(geom, ...) →
                                                 HandleScreenAlt(..., term.dev, term.dev.Codepage())
```

The real presenter stays **stateless**; all per-connection state rides in
`Term`. The bridge is untouched: it already echoes the negotiated terminal
type to the backend, so bridged-session geometry remains the backend's
concern.

## Geometry rules

All bottom-anchored rows are functions of the row count R, chosen so
**R = 24 reproduces today's layout exactly** (regression invariant):

| Element                                  | Formula   | R=24 | MOD 3 (32) | MOD 4 (43) |
|------------------------------------------|-----------|------|------------|------------|
| PF help line                             | R−1       | 23   | 31         | 42         |
| Error line                               | R−3       | 21   | 29         | 40         |
| Admin list legend                        | R−4       | 20   | 28         | 39         |
| Input/selection line (menu, admin menu)  | R−5       | 19   | 27         | 38         |
| Admin list last data row                 | R−7       | 17   | 25         | 36         |
| Admin list page size                     | R−10      | 14   | 22         | 33         |
| Menu service capacity                    | rows 4..R−7 (truncated) | 14 | 22 | 33 |
| Admin form max fields                    | rows 3,5,…,R−5 → (R−8)/2+1 | 9 | 13 | 18 |

Top-anchored content does not move: titles row 0, login fields rows 3/5,
list data starting row 4, admin form fields from row 3. The menu's admin
`A` entry cap becomes R−7 (today's hard-coded 17); when the user is an
admin, the service list truncates one row earlier (R−8) so the `A` entry
always has a non-colliding row (today an over-full list can overlap the
clamped admin entry — this rule removes that case).

The formulas live in one place: `Geometry` methods (`HelpRow()`,
`ErrorRow()`, `LegendRow()`, `InputRow()`, `ListPageSize()`,
`FormMaxFields()`, …). The `screens.AdminListPageSize` and
`AdminFormMaxFields` constants are replaced by these methods.

## Interface & signature changes

`internal/server/session.go`:

```go
type Presenter interface {
    Negotiate(conn net.Conn) (Term, error)                           // was (string, error)
    Login(conn net.Conn, term Term, errMsg string) (...)             // + term
    Menu(conn net.Conn, term Term, svcs []store.Service, ...) (...)  // + term
}
```

- `Session.Run` keeps `term` and passes `term.Type` to `Bridger.Bridge`.
- `AdminPresenter` methods (`AdminMenu`, `AdminList`, `AdminForm`) each gain
  `term Term`; `adminFlow` gains a `term` field set at construction in
  `Session.Run`, and the `adminPageSize` const becomes a per-flow value
  computed from `term.Rows`.
- Every `internal/screens` builder takes `Geometry` first:
  `LoginScreen(geom, errMsg)`, `MenuScreen(geom, svcs, admin, errMsg)`,
  `AdminMenuScreen(geom, errMsg)`, `AdminListScreen(geom, v)`,
  `AdminFormScreen(geom, v)`.
- Initial cursor positions: menu and admin menu become
  `(geom.InputRow(), col+1)`; login and admin forms stay `(3, 17)`
  (top-anchored). The `(field.Row, field.Col+1)` cursor rule is unchanged.
- Test fakes (`fakePresenter` in `session_test.go`, admin-flow fakes) adopt
  the new signatures and build `Term{Type: ..., Rows: 24, Cols: 80}`
  directly; `dev` stays nil in fakes (only the real presenter uses it).

## Fallback, normalization, codepage

- **Single choke point — `Term.Geometry()`:** if the negotiated alternate
  dimensions have `Rows < 24 || Cols < 80` (including zero/unknown),
  normalize to 24×80 **and drop `dev`** (nil dev → go3270's plain 24×80
  path). Never render a layout larger than the buffer the terminal
  advertised.
- Valid alt ≥ 24×80 (including MOD 5's 27×132): pass the real `dev`; build
  screens with the real row count; builders cap content at column 79.
- **Codepage:** the presenter passes `term.dev.Codepage()` as the trailing
  argument on every `HandleScreenAlt` call when `dev` is non-nil.

## Error handling

- `Negotiate` failure: unchanged (log + drop connection).
- Geometry edge cases are absorbed by the normalization rule above — there
  is no error path for "weird dimensions", only the 24×80 fallback.
- Oversized data (more services/list rows than fit) is truncated by the
  builders at the formula rows; admin lists already paginate via PF7/PF8.

## Testing

1. **`internal/screens` table tests** over {24×80, 32×80, 43×80, 27×132,
   zero-value}: error/help/legend/input rows track the formulas; page size,
   menu capacity, and form capacity grow with R; zero-value normalizes to
   the current layout; menu truncation never reaches the input row. Existing
   name/content assertions keep passing with `Geometry{24, 80}`.
2. **`internal/server` session tests**: fakes verify `term` threads from
   `Negotiate` through `Login`/`Menu`/admin calls; an admin paging test with
   `Term{Rows: 32}` asserts 22-row pages.
3. **Live emulator smoke test** (unit tests cannot verify the 3270 protocol
   surface — CLAUDE.md): run `c3270` advertising different models, e.g.
   `c3270 -model 3279-4 127.0.0.1:2323`. Checklist: login → menu → admin
   list paging on MOD 2 (regression), MOD 4 (row growth), MOD 5 (132 cols,
   left-aligned 80-col content); bottom lines on the true last rows; cursor
   lands on the first input field everywhere.

`go test ./... -race` throughout. Housekeeping: `go mod tidy` (go3270 is
currently marked `// indirect` despite direct imports).

## Out of scope

- Menu pagination (truncation is explicit now; paging can come later).
- Using the full 132-column width on MOD 5 (widen/center later if wanted).
- Dynamic re-negotiation mid-session (geometry is fixed at connect).
- Any bridge/TN3270E changes (roadmap #4).
