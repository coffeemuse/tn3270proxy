# Active Sessions view on the admin menu (with Disconnect)

**Issue:** GH #91
**Date:** 2026-06-09
**Status:** Design — approved, pending implementation plan

## Goal

Put a 3270 face on the proxy's live connection state: an admin screen that lists
every connected client in real time (who, from where, how long, into what) and
lets an admin **disconnect** any of them. This is operational visibility and
control the gateway has never had, and another step toward the north star —
*almost all day-to-day admin functions should have a 3270 UI.*

Unlike the audit-log viewer (GH #40), the **data layer does not exist yet**:
there is no central registry of live sessions. Each accepted TCP connection runs
its own independent `Session` goroutine, and the only shared connection state is
`connLimiter`'s per-IP counters (`internal/server/limiter.go`). The bulk of this
work is therefore a new in-memory session registry; the screen on top reuses the
existing snapshot-list machinery.

## Scope

**In scope:**
- An in-memory, process-wide registry of live sessions, updated as each session
  transitions (accept → login → bridge → logoff → disconnect).
- A read-only, paged admin viewer listing active sessions with a live session
  clock, modeled on the `RECENT ACTIVITY` audit viewer.
- A `D` (Disconnect) line command with a confirm step that hard-closes the
  target connection.
- A self-disconnect guardrail (the admin cannot drop their own session).
- An audit event for each disconnect, reusing the actor/subject split (GH #73).

**Out of scope:**
- Auto-refreshing / server-push updates. 3270 screens are static; the list is a
  snapshot, refreshed on plain Enter (same model as the audit viewer).
- Persisting session state or history to SQLite. Live session state is ephemeral
  and process-local; the audit trail already records login/logout/bridge history.
- Cross-process / clustered session views. Single process only.
- A filter screen (by user / IP / service). The active-session set is small and
  fits the paged snapshot; revisit only if it proves necessary.

## Background: why an in-memory registry, not a DB table

Three alternatives were considered:

1. **In-memory registry (chosen).** A mutex-guarded map of live sessions held by
   the handler, created once per process and shared across all listeners —
   exactly how `authThrottle` is wired today (`NewSessionHandler`).
2. **SQLite `active_sessions` table.** Rejected: persistence buys nothing here. A
   disconnect *must* reach the in-process `net.Conn` to close it, which a DB row
   cannot do; rows would have to be reconciled against live goroutines anyway. A
   crash or `kill -9` leaves stale rows that misreport reality, and the write
   traffic (every transition, every connection) is pure overhead.
3. **Hybrid (memory + audit for history).** This is effectively the chosen
   option: live state in memory, history already covered by the audit trail. No
   third component is needed.

The registry is the natural fit: session liveness is exactly as ephemeral as the
goroutine that owns the connection, and the disconnect action needs the
in-process handle regardless.

## Architecture

```
admin menu (option 7)
  └─ adminFlow.activeSessions()                    internal/server/admin_sessions.go (new)
       ├─ ui3270.RunSnapshotList[SessionView]      internal/ui3270/snapshotlist.go (extended)
       │    fetch snapshot → page held slice → Enter refreshes
       │    → 'D' confirm → disconnect → refresh
       ├─ SessionRegistry seam (Snapshot / Disconnect)   internal/server (new)
       │    └─ *sessionRegistry                    internal/server/registry.go (new)
       └─ audit: disconnect event (actor=admin, subject=booted user)

connection lifecycle (every session):
  sessionHandler.sessionFor → registry.register(entry)   (at accept)
  Session transitions        → entry.setUser / setService / clearLogin
  Session teardown (defer)   → registry.deregister(id)
  admin Disconnect           → entry.close()  (idempotent; unblocks the goroutine)
```

Layering is preserved. All concurrency lives in `internal/server`. `internal/ui3270`
stays pure render and never learns about `net.Conn` or the registry — the flow
passes it pre-formatted `SessionView` rows and a disconnect callback. No SQL is
involved at all (the registry is not backed by `store`).

## Components

### 1. `sessionRegistry` (new — `internal/server/registry.go`)

A mutex-guarded `map[uint64]*sessionEntry` plus a monotonic id counter. One
instance is created in `NewSessionHandler` (alongside `newAuthThrottle()`) and
shared by every connection across every listener.

```go
type sessionEntry struct {
    id          uint64
    remoteAddr  string        // client IP:port (conn.RemoteAddr().String())
    connectedAt time.Time     // TCP accept
    loggedInAt  time.Time     // zero until login; reset to zero on logoff
    username    string        // "" until login
    service     string        // "" unless actively bridged (service NAME)
    close       func()        // closes the underlying net.Conn, idempotent (sync.Once)
}
```

Methods (all concurrency-safe):
- `register(remoteAddr string, connectedAt time.Time, close func()) (id uint64, entry *sessionEntry)` —
  assigns the next id, inserts, returns the handle the session updates.
- Per-entry mutators guarded by the registry mutex: `setLogin(id, username)`,
  `clearLogin(id)` (logoff / idle-logout — clears username + `loggedInAt`),
  `setService(id, name)`, `clearService(id)`.
- `deregister(id)` — removes the entry (called from the session's teardown defer).
- `snapshot(now time.Time) []SessionView` — returns a sorted (by id), copied
  slice of display rows; never hands out live pointers.
- `disconnect(id uint64) (booted SessionView, ok bool)` — looks up the entry,
  calls its `close()`, returns the entry's last-known view for the audit record.
  `ok=false` if the id is already gone (lost a race with natural disconnect).

`close` is built by the handler as `sync.Once`-wrapped `conn.Close()`, so a
double-disconnect — or a race between an admin `D` and the client hanging up — is
safe. Closing the conn unblocks the session goroutine's pending `Read` (in
`Negotiate`, `HandleScreen`, or the bridge relay); the goroutine then errors out
and tears down through its existing `defer`, which calls `deregister`.

`SessionView` is the flattened, copied snapshot row (`id`, `remoteAddr`,
`connectedAt`, `loggedInAt` (zero ⇒ not logged in), `username`, `service`) — a
plain value type the `server` flow formats and the `ui3270` layer renders. It
carries no `net.Conn` and no `close`.

### 2. Lifecycle integration (`internal/server/server.go`, `session.go`)

`sessionHandler` gains a `*sessionRegistry` field, set in `NewSessionHandler`.
`sessionFor` (or `Handle`) registers the entry at accept time, threads the
returned id into the `Session`, and `defer`s `deregister`. The `close` func wraps
the same `net.Conn` the session reads from.

The session updates its entry at the transitions it already tracks:

| Transition | Registry call |
|------------|---------------|
| login success | `setLogin(id, username)` — sets username + `loggedInAt = now` |
| logoff (PF3 at service menu) / idle-logout | `clearLogin(id)` — clears username + `loggedInAt`; entry persists (same TCP conn) |
| bridge start | `setService(id, serviceName)` |
| bridge end (back to menu) | `clearService(id)` |
| connection teardown | `deregister(id)` (defer) |

Keeping the entry alive across logoff is why we track **both** `connectedAt` and
`loggedInAt`: a client that logs in, logs off, and sits at the login screen is
still one connected session, and the list must show it with an accurate
connection age.

The `Session` gets a registry seam + its own id so the admin screen can mark the
admin's own row and reject self-disconnect. To keep the existing `Session`
zero-value-friendly for unit tests, the registry hooks are nil-safe (a `Session`
constructed without a registry simply skips the updates).

### 3. `SessionRegistry` seam + `adminFlow` wiring

The admin flow reaches the registry through a small interface (so admin tests use
a fake, and the screen layer never sees concurrency internals):

```go
type SessionRegistry interface {
    Snapshot(now time.Time) []SessionView
    Disconnect(id uint64) (booted SessionView, ok bool)
}
```

`*sessionRegistry` satisfies it. `adminFlow` gains a `sessions SessionRegistry`
field and a `selfSessionID uint64` (the acting admin's own session id, threaded
from the `Session`). A new admin-menu `case 7:` dispatches to
`f.activeSessions(ctx, conn)`.

### 4. `RunSnapshotList` extension — one confirm-capable line command

The existing `RunSnapshotList` (`internal/ui3270/snapshotlist.go`) is read-only:
it supports a single inert-or-detail `S` command via `OnSelect` and has no
confirm step. `RunList` has the confirm machinery (`pendingConfirm`, the
`Confirm func(item) (prompt, blocked string)` veto) but re-fetches on every
render, which defeats stable paging and the as-of stamp.

The active-sessions screen needs **snapshot semantics + a confirm**, so
`RunSnapshotList` is extended with an optional action command mirroring
`RunList`'s confirm pattern:

```go
type SnapshotConfig[T any] struct {
    // ... existing fields ...
    ActCmd     byte                                   // e.g. 'D'; 0 disables
    Confirm    func(item T) (prompt, blocked string)  // veto/confirm; blocked!="" refuses
    OnAct      func(ctx context.Context, item T) (refresh bool, errMsg string)
}
```

Behaviour added to the driver loop:
- An `ActCmd` keystroke on a row, with no pending confirm, calls `Confirm(item)`.
  A non-empty `blocked` string is shown and nothing happens (the self-disconnect
  veto). Otherwise the `prompt` is shown and the action is held pending, exactly
  like `RunList`'s delete dance.
- The pending action is resolved by the next keystroke: re-issuing `ActCmd` on the
  same row confirms (calls `OnAct`); any other action cancels it.
- `OnAct` returning `refresh=true` re-fetches the snapshot (a disconnected row
  disappears). An `errMsg` is shown.

**Message-line rendering (style-guide conformance).** The `prompt`, the `blocked`
veto, and every `errMsg` all flow into the **single `errormsg` field on the
message line at row 2**, rendered Red-intense — exactly as `RunList` and the
audit viewer already do (`internal/ui3270/screen.go`, `snapshotscreen.go`:
`messageRow() == 2`). The ISPF style guide's palette nominally colours *confirm
prompts* Yellow (WASL caution), but the established `ui3270` convention is one
shared red message field for prompts, vetoes, and errors alike; this screen
**follows the existing convention** rather than introducing a second message
field, keeping all snapshot/list panels uniform. (If a yellow caution band is
ever wanted, it is a guide-wide `ui3270` change, out of scope here.)

This keeps the read-only fetch-once/Enter-refresh/PF7-PF8 paging intact and adds
the minimum needed for a guarded mutating command. `OnSelect`/`S` remains
available but is unused in v1 (all fields fit one row; no detail drill-down).

**Second `ui3270` change — a wider row for 6 columns.** The audit viewer's
`SnapshotRow` is a fixed three-segment model (Left cols 8–27 = 20 wide, Mid
29–40 = 12, Right 42–79 = 38) whose Mid segment exists to carry its own colour.
Active Sessions has six columns and an `IP:port` that alone can reach 21 chars,
so the 20-wide Left segment is too tight. Since this screen needs **no
per-segment colour**, the row is rendered as a **single full-width content field
(cols 8–79, 72 chars)** packed by the `server`-layer formatter. This is a small,
additive `snapshotscreen.go` change (a full-width row variant selected when the
`SnapshotRow` carries no Mid colour); the audit viewer keeps the three-segment
coloured model unchanged. The Blue column-heading row (r3) uses the same
full-width field so headings align with the packed columns.

### 5. List screen layout (MOD 2; self-normalising for larger models)

The screen follows the ISPF three-band layout (`docs/ispf-style-guide.md`) and
reuses the snapshot builder's band rows verbatim — exactly the placement
`buildSnapshotScreen` already produces:

- **r0** — centered White-intense title; right-justified `ROW x TO y OF z`
  indicator at col 60.
- **r1** — turquoise, centered `AS OF …` snapshot stamp (snapshot lists carry the
  as-of stamp here; no `Command ===>` line, per style-guide deviation 7 — list
  panels have no command line yet).
- **r2** — the **message line**: Red-intense `errormsg` field, blank unless there
  is a prompt / veto / error.
- **r3** — Blue column-heading row.
- **r4 … rN−3** — data rows; the line-command (`CMD`) input is the green field at
  col 3.
- **rN−2** — turquoise legend row (`D=Disconnect`).
- **rN−1** — turquoise PF-key help row.

```
 r0   ACTIVE SESSIONS                                        ROW 1 TO 3 OF 3
 r1                     AS OF 2026-06-09 (2026.160)  14:32 UTC
 r2   <message line — Red, blank unless prompt/veto/error>
 r3   CMD ID    CLIENT             CONNECTED SESSION  USER     SERVICE
 r4   _   12    203.0.113.7:51234  14:02:11  00:18:43 DARROW   DEMO
 r5   _   13    203.0.113.9:44120  14:19:55  00:00:59 (login)  -
 r6   D   14    10.0.0.4:5050      13:40:02  00:39:52 *YOU*    -
 ...
 r20  D=Disconnect
 r23  PF3=Admin Menu  PF7=Up  PF8=Down  Enter=Refresh
```

Each data row is one full-width content field (cols 8–79, 72 chars; see §4),
packed by the `server`-layer formatter to this column budget:

| Column | Cols (within 8–79) | Width | Notes |
|--------|--------------------|-------|-------|
| ID | 8–12 | 5 | per-process monotonic session id |
| CLIENT | 14–34 | 21 | remote `IP:port`; IPv6 clips at 21 (full value in a future detail view) |
| CONNECTED | 36–43 | 8 | wall-clock `HH:MM:SS` of TCP accept, UTC |
| SESSION | 45–52 | 8 | elapsed `HH:MM:SS` since `connectedAt` |
| USER | 54–61 | 8 | logged-in username; `(login)` pre-auth; own row `*YOU*` |
| SERVICE | 63–79 | 17 | bridged service NAME (≤8 in practice); `-` when not bridged |

- **SESSION** is `now - connectedAt`, formatted `HH:MM:SS` (the requested session
  length). Because it is computed at snapshot time, plain Enter advances the
  clock. `loggedInAt` is carried in `SessionView` for a possible future "login
  age" column / detail but is not shown in v1.
- **`*YOU*`** marks the acting admin's own session (matched by `selfSessionID`).
- **PF-key help** follows style-guide §3 (`PFn=Verb`, no spaces around `=`, two
  spaces between entries); `PF3=Admin Menu` matches the sibling audit viewer
  (this is an admin sub-screen returning to the admin menu).
- **UTC everywhere**; the empty/overflow conventions match the audit viewer.
  Empty state: `(no active sessions)` — not reachable in practice (the viewing
  admin is themselves a session) but handled.

### 6. Disconnect flow + guardrails

- `Confirm(view)` returns `blocked = "CANNOT DISCONNECT YOUR OWN SESSION"` when
  `view.id == f.selfSessionID` — the self-disconnect guardrail, consistent with
  the existing no-self-delete / no-self-demotion guardrails. Otherwise it returns
  a prompt like `CONFIRM DISCONNECT <USER-OR-CLIENT> - PRESS D AGAIN` (ASCII only;
  this renders on a 3270 screen). Both the veto and the prompt surface on the
  **row-2 message line** via the shared `errormsg` field (§4).
- On confirm, `OnAct` calls `registry.Disconnect(view.id)`:
  - `ok=true` → record the audit event, return `refresh=true` (row vanishes).
  - `ok=false` (already gone) → benign; `errMsg = "SESSION ALREADY ENDED"`,
    `refresh=true`.
- The disconnect is a **hard close** — uniform whether the target is sitting on a
  proxy screen or actively bridged to a backend (the chosen behaviour; a notice
  screen cannot be painted reliably mid-bridge).

### 7. Auditing

A new audit kind records each admin-initiated disconnect, reusing the actor/
subject split (GH #73): `actor` auto-fills to the acting admin (via
`auditTrail`), `username` is the booted subject (the entry's last-known
username; empty if the target had not logged in), and `Detail` carries the client
address and session id, e.g. `disconnected session 14 (10.0.0.4:5050)`.

Proposed kind constant: `AuditSessionDisconnect = "session_disconnect"` in
`internal/store` (alongside the other `Audit*` kinds), coloured yellow in the
audit viewer's `auditEventColor` (a security-state action, like `admin`).

## Data flow (one refresh)

1. `activeSessions` calls `registry.Snapshot(now)` → copied `[]SessionView`,
   sorted by id.
2. Format each row (`SESSION = now - connectedAt`; `(login)` / `*YOU*` / `-`
   substitutions); stamp `AS OF now`.
3. `RunSnapshotList` pages the held slice; PF7/PF8 page, Enter re-fetches.
4. `D` on a row → `Confirm` (self-disconnect vetoed) → prompt → `D` again →
   `OnAct` → `registry.Disconnect(id)` → audit → `refresh=true`.

## Wiring touchpoints

- `internal/screens/admin.go` — add option `7` to `AdminMenuScreen` on the
  tri-color keyword grid (style-guide §2): turquoise name `Sessions`, green
  description `Active client sessions`.
- `internal/server/presenter_admin.go` — accept `"7"` in the `AdminMenu` parse.
- `internal/server/admin.go` — `case 7:` dispatch; add `sessions SessionRegistry`
  and `selfSessionID uint64` to `adminFlow`; (no `AdminStore` change — the
  registry is a separate seam, not store-backed).
- `internal/server/admin_sessions.go` (new) — the flow: snapshot, format rows,
  run the snapshot list, confirm + disconnect + audit.
- `internal/server/registry.go` (new) — `sessionRegistry`, `sessionEntry`,
  `SessionView`, `SessionRegistry` seam.
- `internal/server/server.go` — `*sessionRegistry` on `sessionHandler`; build it
  in `NewSessionHandler`; register/deregister + `sync.Once` close in the accept/
  handle path; thread id + registry into the `Session`.
- `internal/server/session.go` — registry field + id on `Session`; nil-safe
  `setLogin`/`clearLogin`/`setService`/`clearService` calls at the existing
  transitions.
- `internal/ui3270/snapshotlist.go` — extend `SnapshotConfig` with
  `ActCmd`/`Confirm`/`OnAct` and the confirm dance (all messages via the
  existing row-2 `errormsg` field).
- `internal/ui3270/snapshotscreen.go` — add a full-width row variant (cols 8–79)
  selected when a `SnapshotRow` carries no Mid colour, used for the packed
  6-column rows and the heading row; the audit viewer's three-segment coloured
  layout is untouched.
- `internal/store` — `AuditSessionDisconnect` kind constant; colour it in
  `auditEventColor`.

## Testing

- **Unit (`internal/server`, no live sockets):**
  - `sessionRegistry`: concurrent register / setLogin / setService / clearLogin /
    deregister under `-race`; `snapshot` returns copies (not live pointers) sorted
    by id; `disconnect` calls `close` once and is idempotent (double-disconnect,
    disconnect-after-deregister both safe); `ok=false` on a missing id.
  - Lifecycle: a driven fake-conn session registers on entry, reflects login/
    bridge/logoff in the snapshot, and deregisters on teardown.
  - `activeSessions` flow through a **fake `SessionRegistry`**: row formatting
    (`(login)`, `*YOU*`, `-`, `HH:MM:SS` session length with an injected clock),
    self-disconnect veto, confirm → disconnect → audit emission (actor/subject),
    `ok=false` benign path.
- **Unit (`internal/ui3270`):** the extended `RunSnapshotList` — `ActCmd` confirm
  dance (prompt held, re-issue confirms, other key cancels), `blocked` veto shows
  the error and does nothing, `OnAct` refresh re-fetches. Existing `OnSelect`/PF/
  Enter behaviour unchanged (regression).
- **Protocol surface (s3270 smoke skill):** the screen renders within 80 cols,
  cursor lands on the command field, PF3/PF7/PF8/Enter semantics, and a `D`-confirm
  drops a *second* connected client (the smoke harness opens two connections).
- `go test ./... -race` throughout — the registry is concurrent by construction.
- A human pass in a live emulator (c3270) remains the final word on visual polish.

## Open questions

None outstanding; all design decisions are settled above.
