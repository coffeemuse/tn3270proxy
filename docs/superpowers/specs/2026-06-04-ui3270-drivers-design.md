# Design: `internal/ui3270` generic list/form drivers

**Issue:** [#34](https://github.com/coffeemuse/tn3270proxy/issues/34) (sub-issue of #15, items 2 + 4)
**Date:** 2026-06-04
**Status:** Approved design, pre-implementation
**Depends on:** #33 / PR #37 (builder-owned cursor) — **merged**

> Note: issue #34 names the package `internal/us3270`; that was an autocorrect
> typo. The agreed name is **`internal/ui3270`**. Fix the issue title during
> migration PR2.

## Problem

The admin flow carries ~500 duplicated lines across two loop families in
`internal/server`:

- `users()` / `groups()` / `services()` — each an ~85-line skeleton: page +
  `pendingDelete` state, render, identical delete-confirm dance, PF3/4/7/8
  dispatch.
- `userGroups()` / `groupMembers()` / `serviceGroups()` — each an ~75-line
  stateless membership-toggle loop (A/R, no add, no confirm).
- The PF7/PF8 paging pair is copy-pasted into all six loops.

The three copies in each family are currently **behaviorally identical**. The
refactor's job is to collapse the duplication while keeping them identical.

Two per-command hook points exist in the live code and must be preserved:

- a **press-time veto** — e.g. `groups()` blocks `D` on a reserved `ZZ*` name
  *before* showing the confirm prompt (`admin_groups.go:116`);
- a **commit-time guard** — e.g. `deleteUser` runs the last-admin guard *after*
  Enter confirms (`admin_users.go:134`).

Plus immediate toggles (`A`/`R`, no confirm) and sub-screen openers
(`S`/`G`/`M`/`E` that take over the connection).

## Approach (chosen: "A" — driver owns rendering)

Introduce a new package **`internal/ui3270`**: generic, proxy-agnostic 3270 UI
drivers built directly on `go3270` + stdlib. It owns the view models, the
generic screen builders, the driver loops, **and** the concrete go3270-backed
renderer (the `HandleScreenAlt` wrapper — this absorbs #15 item 4). The
`server/admin_*.go` files shrink to glue: map `store` rows → display strings,
wire commands, set titles/legends/PF-help, hand off to the driver.

**Why A (over keeping rendering behind the existing `AdminPresenter` seam):**
liftability is the issue's stated goal, and the only thing that would keep
`ui3270` from one day being its own module is whether it owns the go3270
binding. The existing test suite (`admin_test.go`) scripts at the **action
level** with `conn == nil` and never touches go3270, so moving rendering into
`ui3270` is a mechanical type-rename in the tests, not a rewrite of the safety
net. (Full coupling analysis: the suite asserts on real-store side effects,
captured view content, and audit events — all preserved under A.)

## Package surface

New package `internal/ui3270`. Imports **only** `go3270`, `net`, `context`, and
stdlib. Never imports `store`, `auth`, `server`, or `screens`.

```go
package ui3270

// ── view models (what to paint) ──
type Cursor struct{ Row, Col int }

type ListView struct {
    Title, RowInfo, Header, Legend, ErrMsg, PFHelp string
    Rows []string
}

type FormField struct {
    Name, Label, Value string
    Hidden bool
    Length int
}
type FormView struct {
    Title, ErrMsg string
    Fields []FormField
}

// ── actions (what came back) ──
type ListAction struct { Cmd byte; Row int; PF int } // Cmd==0 && PF==0 ⇒ plain Enter
type FormAction struct { Values map[string]string; Cancel bool }

// ── render seam ──
type Renderer interface {
    List(ListView) (ListAction, error)
    Form(FormView) (FormAction, error)
}

// ── generic drivers ──
type Row[T any] struct { Display string; Item T }

type Command[T any] struct {
    Key     byte
    Commit  func(ctx context.Context, r Renderer, item T) (errMsg string, fatal error)
    Confirm func(item T) (prompt, blocked string) // nil ⇒ immediate command
}

type ListConfig[T any] struct {
    Title, Header, Legend, PFHelp string
    Rows  int // terminal rows → page-size math (no screens.Geometry dependency)
    Fetch func(ctx context.Context) (rows []Row[T], errMsg string)
    Cmds  []Command[T]
    Add   func(ctx context.Context, r Renderer) (errMsg string, fatal error) // nil ⇒ no PF4
}

type FormConfig struct {
    Title  string
    Fields []FormField
    Submit func(ctx context.Context, values map[string]string) (errMsg string, fatal error)
}

func RunList[T any](ctx context.Context, r Renderer, cfg ListConfig[T]) error
func RunForm(ctx context.Context, r Renderer, cfg FormConfig) error

// ── production renderer (owns the HandleScreenAlt tail = #15 item 4) ──
// dev's concrete type is whatever Term.dev is; confirm against the code at
// plan time rather than guessing.
func NewGo3270Renderer(conn net.Conn, dev <go3270 device type>, codepage string, rows, cols int) Renderer
```

### Command model

A line command is modeled by `Command[T]` with the confirm *capability*
expressed as an optional function (presence = capability), not a boolean flag —
this is what keeps it from being the "flag-laden monster" the issue warned
against. One dispatch path covers all four shapes:

- **Immediate toggle** (`A`/`R`): `Confirm == nil`; `Commit` runs on the
  keypress. The last-admin guard for `R` lives inside `Commit`.
- **Sub-screen opener** (`S`/`G`/`M`/`E`): `Confirm == nil`; `Commit` takes the
  `Renderer` and recursively calls `RunForm`/`RunList`.
- **Press-time veto** (reserved-group `D`): `Confirm` returns `("", "ZZ*…")`;
  the driver shows the block message and does not arm the confirm.
- **Confirm-then-commit** (user `D`): `Confirm` returns `(prompt, "")`; driver
  arms pending-delete; the next plain Enter runs `Commit` (whose own
  commit-time guard, e.g. last-admin, lives inside).

`Add` (PF4) is a separate optional field; `nil` ⇒ no PF4 and PF4 is not
advertised as an exit key.

## Data flow

`RunList` owns the loop (this deletes the ~85-line skeleton ×3). One iteration:

1. `cfg.Fetch(ctx)` → rows + optional `errMsg`. Fetch re-runs every iteration so
   mutations/deletes show up (same as today). On fetch error: clear pending
   delete, blank the list, keep looping.
2. Compute page bounds from `cfg.Rows`; slice the page; build `ListView`
   (Title/Header/Legend/PFHelp from cfg; `RowInfo` from bounds; `Rows` = the
   page's `Display` strings; `ErrMsg`).
3. `r.List(view)` → `ListAction`.
4. **Pending-delete resolution first** (if armed): plain Enter → run the armed
   `Commit`; PF3 → cancel; anything else → cancel and process normally.
5. Dispatch: PF3 → return; PF4 → `cfg.Add` (if non-nil); PF7/PF8 → page∓ (PF8
   guarded by `end < len`); `Cmd` → match `Command`, then run immediate or arm
   confirm per the command model.

`RunForm` loops: render → on `Cancel` return → else `Submit(values)`; `errMsg`
⇒ continue, success ⇒ return.

### Glue after the collapse

Each glue function becomes a mapping + wiring function, e.g.:

```go
func (f *adminFlow) users(ctx context.Context, conn net.Conn) error {
    r := f.renderer(conn)
    return ui3270.RunList(ctx, r, ui3270.ListConfig[store.User]{
        Title:  "TN3270 GATEWAY ADMIN: USERS",
        Header: "CMD  USERNAME         GROUPS",
        Legend: "S = set password   G = groups   D = delete   PF4 = add user",
        PFHelp: "Enter = process   PF7/PF8 = page   PF3 = admin menu",
        Rows:   f.term.Rows,
        Fetch:  f.fetchUsers,
        Add:    f.userAdd,
        Cmds: []ui3270.Command[store.User]{
            {Key: 'S', Commit: f.openSetPassword},
            {Key: 'G', Commit: f.openUserGroups},
            {Key: 'D', Confirm: confirmDelete("USER"), Commit: f.commitDeleteUser},
        },
    })
}
```

All policy — guards, duplicate pre-checks, hashing, audit, `logStoreErr` —
stays in glue methods. The store and the `screens.Field*` constants stay
server-side (the glue passes field-name strings into `ui3270` as data).
Sub-screens compose by recursion. `adminFlow` gains a `renderer(conn)` helper
returning `ui3270.NewGo3270Renderer(conn, f.term.dev, f.term.codepage(),
f.term.Rows, f.term.Cols)` in production; tests inject a fake `ui3270.Renderer`.

### What moves, what stays

- **Moves into `ui3270`:** today's `screens.AdminListScreen` /
  `AdminFormScreen` builders (as the renderer's private helpers), the
  page-size/anchored-row formulas (re-expressed as small unexported functions of
  `(rows, cols)` — minor, accepted duplication of `screens.Geometry`'s math),
  and the `HandleScreenAlt` wrapper.
- **Stays in `screens`:** `LoginScreen`, `MenuScreen`, `AdminMenuScreen`, and
  the app field-name constants (`FieldUsername`, `FieldHost`, …), which are also
  used by the login/menu screens.

### Dependency direction

`server → ui3270`, `server → screens`, `screens → go3270`, `ui3270 → go3270`.
No cycles; `ui3270` is a leaf on the proxy side.

## Error handling

Two channels, kept distinct (mirrors today's code):

- **`fatal error`** — a dead connection. Only `r.List`/`r.Form`
  (`HandleScreenAlt`) produce these. Propagated unchanged through
  `Commit`/`Add`/`Submit` → `RunList`/`RunForm` → glue → `Session`; ends the
  session.
- **`errMsg string`** — domain/validation/store failures. Shown on the error
  line next render, never fatal. Store errors are logged glue-side via
  `logStoreErr` (returns the generic `"TEMPORARY ERROR; TRY AGAIN"`). `ui3270`
  never sees store errors and logs nothing.

Behavior-preserving invariants the driver must hold (each already true today):

- Fetch error → clear pending-delete, blank list, keep looping.
- PF8 paging guarded by `end < len`; PF7 underflow clamped.
- Empty list homes the cursor `(0,0)`; populated list cursors to the first CMD
  field (lives in the lifted builder, post-#33 builder-owns-cursor rule).
- One line-command per Enter: first non-blank CMD field wins.
- Hidden (password) form fields never trimmed; visible fields trimmed.
- Audit emitted only *after* a successful mutation, inside the glue handler.

`ui3270` adds no logging and no credential handling; passwords pass through
`FormAction.Values` to the glue's `Submit`, which owns hashing — so "no
credential logging, ever" holds structurally.

## Testing

**New `ui3270` unit tests** (driver logic in isolation; no store, no live
terminal). A scripted fake `Renderer` pops actions and captures views;
`Fetch`/`Commit` are in-test closures over a `[]Row[T]`. Cases: immediate
toggle, press-time veto (`blocked`), confirm→commit, confirm→PF3 cancel, PF7/PF8
bounds, empty-list home cursor, fetch-error clears pending-delete,
one-command-per-Enter, sub-screen recursion, `Add == nil` ⇒ PF4 inert.

**`server/admin_test.go` migration is mechanical; the suite stays the
behavioral net.** Because it scripts at the action level with `conn == nil`:

- `fakeAdminPresenter` → a fake `ui3270.Renderer` (same pop-and-capture).
- `screens.AdminListView/AdminFormView` → `ui3270.ListView/FormView`;
  `AdminListAction/AdminFormAction` → `ui3270.ListAction/FormAction`.
- **Unchanged:** all scripted action sequences, real-store side-effect
  assertions, audit assertions, and the `screens.Field*` constants.

**The go3270 binding** (`NewGo3270Renderer` — builders + cursor +
`HandleScreenAlt`) is verified by the **s3270 smoke test** (screen content,
cursor row/col, attributes, PF3/4/7/8), not unit tests — same protocol-surface
coverage as today, relocated.

**Verification gate:** `go test ./... -race` plus the s3270 smoke run before
declaring done (per CLAUDE.md — the protocol surface is only truly verified
against a real emulator).

## Migration sequence

Two PRs, for reviewability (not for risk — the net isn't at risk):

1. **PR1 — introduce `ui3270` + collapse the loops.** New package (drivers,
   view/action types, lifted builders, `NewGo3270Renderer`). Rewrite the six
   glue functions + five forms to call it. Migrate `admin_test.go` (the renames
   above). Delete the dead `AdminPresenter.AdminList/AdminForm` paths.
   `go test ./... -race` green; smoke test green.
2. **PR2 — tidy.** Remove the now-dead `screens.AdminListScreen/AdminFormScreen`
   + their tests (superseded by the `ui3270` builders); fix the issue title typo
   (`us3270` → `ui3270`).

Both behavior-preserving; the three copies in each family stay byte-for-byte
identical because they share one driver path.

## Out of scope

- Extracting `ui3270` to its own module — gated on a real second consumer
  (per the issue). We design for eventual extraction (no proxy types) but do
  not extract.
- The other #15 sub-issues: #33 (cursor — done) and #35 (store hygiene).
