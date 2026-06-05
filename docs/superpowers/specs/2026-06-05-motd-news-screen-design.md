# MOTD / NEWS Screen — Design (GH #45)

**Status:** Approved design, ready for implementation plan.
**Depends on:** #44 (System Parameters) — *merged*. The `MOTD_FILE` key already
exists in `internal/sysconfig.Catalog` and is seeded into `system_config`,
readable live via `store.GetConfig(ctx, "MOTD_FILE")`. No restart is involved.

## Goal

A TSO/READY-style Message-of-the-Day / NEWS screen shown once after a successful
login, before the menu, for runtime notices ("systems offline Tuesday
09:00–10:00 UTC for maintenance"). Editing the *message* is live: post a new file,
the next login sees it — no proxy restart.

This mimics classic mainframe logon UX: between logon and ISPF, messages display
raw — no screen title, no PF-key legend, just the text and a `***` page gate.

## Behaviour

1. Login/auth succeeds.
2. Read `MOTD_FILE` from `system_config`. If unset or empty-after-trim → go
   straight to the menu.
3. The path must be **absolute**. A relative path is treated as a
   misconfiguration → log once, go to the menu (never a user-facing error).
4. Read the file **fresh every login**, capped at **8 KB** (see Read cap). On any
   read error (missing, unreadable, permissions, device/FIFO) → log once, go to
   the menu.
5. Paginate **in memory**. If the content is empty/whitespace-only after parsing
   → go to the menu.
6. Display the news page by page. **ENTER** advances; on the last page ENTER
   proceeds to the menu.

### Rendering (per page)

- Text shown in **red**, protected (non-input) fields.
- Up to `geom.Rows − 2` text lines per page (geometry-driven: MOD 2 = 22 lines,
  more on MOD 3/4/5). Then a blank row, then `***` on the last row — the classic
  "press ENTER to continue" pause.
- **No chrome:** no title row, no PF-key help row. This is a deliberate departure
  from the app's "title on row 0, PF help on last row" convention, for TSO
  fidelity. It must not be "fixed" to add chrome.
- Lines **truncated at column 79** (all MOD types, including MOD 5, use 79). The
  file is a deliberately composed fixed-width banner — the author owns line
  breaks and indentation; anything past column 79 is silently dropped.
- The `***` page gate is **uniform on every page**, including the last (and the
  single-page case). It is not a semantic "more follows" marker — it is the
  page-advance affordance. The final ENTER simply lands on the menu, acting as a
  pause/acknowledge before the screen is repainted with the menu.

### Navigation — pure ENTER gate (force-the-read)

- **ENTER** = next page, or proceed to the menu after the last page.
- **PA3** = nothing. (PA3 is only ever the bridge-connection escape; it does
  nothing on any proxy-owned screen.)
- **PF3** = nothing on this screen (no skip in v1; the news is meant to be seen).

## Architecture

Three independently testable units plus one wiring point. The MOTD is a plain
protected-text screen, so it uses the simpler `internal/screens` builder pattern
(like `LoginScreen`/`MenuScreen`), not the `internal/ui3270` form/list framework.

### 1. `internal/screens/news.go` — pure builders, no IO

- `func PaginateNews(geom Geometry, raw string) [][]string`
  Splits raw file text into pages. Each line is truncated at column 79; pages
  hold up to `geom.NewsLinesPerPage()` (= `Rows − 2`) lines. Returns `nil` for
  empty/whitespace-only input (caller treats `nil` as "nothing to show").
  Line splitting is on `\n`; a trailing `\r` (CRLF files) is stripped.

- `func NewsScreen(geom Geometry, page []string) (go3270.Screen, go3270.Rules, Cursor)`
  Renders one page: red, protected fields for the text lines, a blank row, then
  `***`. No title, no PF help. Cursor homes to `{0, 0}` (no input field).
  Returns the `(screen, rules, cursor)` triple in the same shape as the other
  `screens` builders.

- `func (g Geometry) NewsLinesPerPage() int { return g.norm().Rows - 2 }`
  Added to `internal/screens/geometry.go` alongside the other formula methods.

### 2. `Presenter.News` — render/paging loop (real impl in `internal/server/presenter.go`)

New method on the `Presenter` seam:

```go
News(conn net.Conn, term Term, pages [][]string) error
```

`go3270Presenter.News` loops the pages, rendering each with `HandleScreenAlt`,
accepting **ENTER** to advance and using `withSilentExits` for PF3/PA3 so they do
nothing. Returns `nil` after the last-page ENTER, or an `error` on
disconnect/idle-timeout. Like `Login`/`Menu`, the real implementation is verified
by the smoke test, not unit tests; unit testing happens against a fake `Presenter`
at the session layer.

A `nil`/empty `pages` slice must never reach `News` — the session decides whether
to show anything (see below) and only calls `News` with at least one page.

### 3. Session wiring — `maybeShowNews` in `internal/server/session.go`

Inserted in `Session.Run` right after `s.armPostAuth(conn)` and before the
`menu:` label (currently session.go:195–199). Runs once per successful login;
because PF3-logoff returns to the login screen, a re-login re-runs it.

```
maybeShowNews(ctx, conn, term, identity, aud) (enterMenu bool, err error)
```

The three outcomes are encoded as: `(true, nil)` → fall through into the menu
loop; `(false, nil)` → idle-logout handled internally (audited, `armPreAuth`),
caller `break`s back to the login screen; `(_, err)` → fatal, caller sets
`endDetail` and returns (disconnect). This mirrors how the inline menu render
error is classified at session.go:208–218.

Logic:
- `val, err := GetConfig(ctx, "MOTD_FILE")`; if `ErrNotFound` or
  `strings.TrimSpace(val) == ""` → return (skip).
- If `!filepath.IsAbs(val)` → `log.Printf` once → skip.
- `data, err := s.MOTDRead(val)`; on error → `log.Printf` once → skip.
- `pages := screens.PaginateNews(term.Geometry(), string(data))`; if `len(pages)
  == 0` → skip.
- `err := s.Presenter.News(conn, term, pages)`; classify the error exactly like
  the menu render error:
  - `isTimeoutErr(err)` → audit `logout{idle logout}`, clear `currentUser`,
    `s.armPreAuth(conn)`, return to the login screen (do **not** enter the menu).
  - other non-nil err → propagate as a disconnect (set `endDetail`, return).
  - `nil` → fall through into the menu loop.

No audit event is recorded for showing the MOTD itself (login success and the
menu render are already audited; the MOTD is a screen between two audited states,
and "shown" cannot prove "read").

### 4. File-read seam (testability + safety)

`Session` gains a field:

```go
MOTDRead func(path string) ([]byte, error)
```

Default: a capped wrapper over `os.ReadFile` that reads at most **8 KB**; a larger
file is truncated (clipped banner), not rejected. 8 KB is roughly 4–5 MOD-2
screens — generous for any legitimate notice while bounding a misconfigured path.
Tests inject a fake to exercise the skip-logic branches without touching disk.

## Data flow

```
doLogin success → armPostAuth
  → maybeShowNews:
        GetConfig(MOTD_FILE) ──unset/empty/relative──▶ skip → menu
              │ absolute path
              ▼
        MOTDRead(path, ≤8KB) ──error──▶ log once → skip → menu
              │ bytes
              ▼
        PaginateNews(geom, text) ──nil──▶ skip → menu
              │ pages (≥1)
              ▼
        Presenter.News(conn, term, pages)   (ENTER through ***; PA3/PF3 inert)
              │ nil            │ timeout            │ other err
              ▼                ▼                    ▼
            menu        idle-logout → login       disconnect
```

## Idle regime

The MOTD gate inherits the post-auth idle window already armed at session.go:195.
A user who walks away mid-news idle-*logs out* to the login screen (re-arming
pre-auth), identical to menu idle — not a disconnect. This falls out of the
timeout-error classification above; no new idle regime is introduced.

## Testing (TDD order)

1. `PaginateNews` table tests — truncation at column 79; page splitting at
   `Rows − 2`; MOD 2 vs MOD 3/4/5 line counts; empty/whitespace → `nil`;
   CRLF handling; exact-fit and off-by-one page boundaries.
2. `NewsScreen` builder tests — field count/content per page; red + protected
   attributes; `***` present on the last row; cursor `{0, 0}`.
3. `NewsLinesPerPage` geometry test — `Rows − 2` across MOD types, sub-MOD-2
   normalization to 22.
4. Session `maybeShowNews` tests with a fake `Presenter` + injected `MOTDRead` —
   each skip branch (unset, empty/whitespace, relative path, read error, nil
   pages), the happy path (News called with the correct pages), and the
   idle-timeout → logout vs. other-error → disconnect classification.
5. Smoke test (`s3270-smoke-testing`) — a new step between login and menu: real
   paging through `***`, the red attribute, PA3/PF3 inert, and the last ENTER
   landing on the menu.

## Documentation (in scope for #45)

A note for operators who author the MOTD file, in the admin System Parameters
help text and the operator docs:

- The MOTD is fixed-width: lines truncate at **column 79** — compose to fit
  ("80-column record length").
- Use an **absolute path**; a relative path is ignored.
- Maintenance windows should be written in **UTC** by convention.
- Keep it short — realistically no more than ~3 screens; longer notices get
  page-skipped rather than read.

## Out of scope (deferred, per the issue)

- PF3-skip-to-menu.
- Per-user "already seen this" suppression.
- Word-wrap instead of truncate-at-79.
- A hard page-count cap (the 8 KB read cap is a safety guard, not a feature; a
  legitimate multi-screen notice is paged in full).
