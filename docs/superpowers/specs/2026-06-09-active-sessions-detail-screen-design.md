# Active Sessions: `S` detail screen (untruncated IP, reverse-DNS PTR, disconnect-from-detail)

**Issue:** GH #93
**Date:** 2026-06-09
**Status:** Design — approved, pending implementation plan
**Follows:** GH #91 / #92 (the Active Sessions list + in-memory session registry, now on `main`)

## Goal

Give the admin **Active Sessions** list (GH #91) an `S` (Select) line command that
opens a read-only **session detail** screen — modeled on the audit-log detail
(`ui3270.DetailView` / `Renderer.Detail`). The list row is space-constrained
(72 cols, CLIENT clipped to 21), so a bracketed IPv6 `[addr]:port` is truncated.
The detail screen shows the full picture untruncated, adds a reverse-DNS PTR hint,
and is where **Disconnect** now lives.

This also **moves disconnect off the list entirely**: the list becomes pure
read-only navigation (`S` to drill in), and the confirm-gated disconnect lives
only on the detail screen. Cleaner separation — the list is for scanning, the
detail is for acting on one session.

## Scope

**In scope:**
- An `S` line command on the Active Sessions list that opens a read-only detail
  screen for one session: full (untruncated) client address, reverse-DNS PTR,
  both timestamps (connected / logged-in) with Julian stamp + elapsed age, user,
  and service.
- A confirm-gated **Disconnect** on the detail screen (PF11), with the same
  self-veto and `session_disconnect` audit the list disconnect had.
- **Removal** of the `D` disconnect line command from the Active Sessions list.
- A small new `ui3270` driver (`RunDetail`) that renders a detail screen with one
  optional confirm-gated PF-key action, mirroring `RunSnapshotList`'s two-press
  gate.

**Out of scope:**
- Per-row PTR on the list (would mean N blocking DNS lookups per page). PTR is a
  single lookup per drill-down only.
- Live auto-refresh of the detail screen. It is a point-in-time snapshot, like
  the list.
- Geolocation or forward-confirmed reverse DNS (FCrDNS). PTR is advisory only.
- A second reverse-DNS toggle. The existing `AUDIT_REVERSE_DNS` sysparam governs
  both admin screens (see Decisions).

## Decisions (locked during brainstorming)

1. **Reverse-DNS toggle: reuse `AUDIT_REVERSE_DNS`.** It collapses to a single
   "are we doing reverse lookups in admin screens or not" flag. The detail screen
   reuses the existing `adminFlow.auditReverseDNS` helper and the `ptr` / `Resolver`
   seam (`internal/server/resolver.go`) with its `(none)` / `(unavailable)`
   vocabulary and 2s timeout.
2. **Disconnect lives only on the detail screen.** The list's `ActCmd` / `Confirm`
   / `OnAct` wiring is deleted; the list's `Legend` becomes `S = detail`.
3. **Two-press confirm**, reusing the existing gate semantics: first action press
   arms a prompt on the message line; second press commits; a self-session press
   vetoes and never commits.
4. **Disconnect is a PF key (PF11), not a typed `D`.** A detail screen has no
   line-command grid; a PF key is the 3270-native trigger for a single-record
   screen — no input field added, the cursor keeps homing to `{0,0}`, and the
   renderer seam just gains one more exit AID. Legend: `PF11=Disconnect   PF3=Back`.
5. **After a successful disconnect, stay on the detail screen** showing a
   `DISCONNECTED` status on the message line, with the action disarmed (PF11
   inert, PF help reduced to `PF3=Back`). PF3 then returns to a **refreshed** list
   (the booted session is gone). The same flow applies to the `already-ended` race
   (`SESSION ALREADY ENDED` status).

## What the admin sees

On the Active Sessions list, `S` on a row opens (PF3 returns to the list):

```
 SESSION DETAIL

 Session     14
 Client      [2001:db8:85a3:8d3:1319:8a2e:370:7348]:65535      <- full, untruncated
 PTR         host.example.de                                    <- reverse DNS (advisory; only when enabled)
 Connected   2026-06-09 (2026.160) 14:02:11 UTC   (00:18:43)    <- wall clock + age
 Logged in   2026-06-09 (2026.160) 14:03:05 UTC   (00:17:49)    <- or "(not logged in)"
 User        DARROW                                              <- or "(login)" when pre-auth
 Service     DEMO                                                <- or "-" when not bridged

                                                       PF11=Disconnect   PF3=Back
```

- **Full client address** — no truncation (the list's 21-char clip doesn't apply).
- **PTR** — present only when `AUDIT_REVERSE_DNS=Y`; resolved value, or
  `(none)` / `(unavailable)` per the audit vocabulary. Advisory, not
  forward-confirmed. One lookup per drill-down, never per-row.
- **Both timestamps** — `julianStamp` + `HH:MM:SS UTC` + an elapsed age via the
  `hhmmss` helper (both already in `admin_sessions.go` / `admin_audit.go`).
  `LoggedInAt` zero ⇒ `(not logged in)`.
- **User** — the username; `(login)` when pre-auth (not yet logged in).
- **Service** — the bridged service NAME; `-` when not bridged.

Disconnect flow on the detail screen:
- **PF11** (first press): self-session ⇒ `CANNOT DISCONNECT YOUR OWN SESSION`
  (veto, no arm). Otherwise ⇒ `CONFIRM DISCONNECT <who> - PRESS PF11 AGAIN`
  (armed). `<who>` is the username, or the client address when pre-auth.
- **PF11** (second press): commit. `DISCONNECTED` on success (or `SESSION ALREADY
  ENDED` on the race), action disarmed, PF help → `PF3=Back`.
- **PF3**: return to the list, re-fetched if a disconnect committed.

## Architecture

Layering holds: the lookup + formatting live in `internal/server`; `internal/ui3270`
stays pure-render and is handed resolved strings.

### 1. `internal/server/admin_sessions.go` — list changes + detail builder + wiring

- **List config:** remove `ActCmd` / `Confirm` / `OnAct`. Set `Legend: "S = detail"`.
  Drop the disconnect hint from `PFHelp`. The row formatter (`fmtSessionRow`,
  `clipField`, `sessionHeader`, `hhmmss`) is unchanged — the row still clips
  CLIENT to 21; the detail screen is where the full address lives. The `*YOU*`
  marker on the row stays (it identifies the admin's own session at a glance).
- **`sessionDetail(ctx, v SessionView) ui3270.DetailView`** — builds the field
  list above. Reuses `julianStamp`, `hhmmss`, `auditReverseDNS`, and the `ptr` /
  `Resolver` seam (resolver `nil` ⇒ `net.DefaultResolver`, matching `auditDetail`).
  PTR field is appended only when reverse DNS is enabled and the lookup returns a
  non-empty string. `DotLeader: true`, `PFHelp: "PF11=Disconnect   PF3=Back"`.
- **`OnSelect`** (closing over `f.selfSessionID`, `f.sessions`, `f.audit`): builds
  the `DetailConfig`, calls `ui3270.RunDetail`, and returns its `refresh` flag.
  - `Confirm`: `v.ID == f.selfSessionID` ⇒ `("", "CANNOT DISCONNECT YOUR OWN
    SESSION")`; else `("CONFIRM DISCONNECT <who> - PRESS PF11 AGAIN", "")`.
  - `OnAct`: `f.sessions.Disconnect(v.ID)`. On `ok` ⇒ audit
    `store.AuditSessionDisconnect` (same record as the old list path: `Username`
    = booted user, `Detail` = `disconnected session N (addr)`), return
    `("DISCONNECTED", true)`. On `!ok` ⇒ return `("SESSION ALREADY ENDED", true)`
    (no audit). `refresh=true` in both cases so the list re-fetches on PF3.

### 2. `internal/ui3270` — `RunDetail` driver + action-capable detail render

- **`DetailView` gains an optional `Message string`** (and the field stays unset
  for the audit detail). `buildDetailScreen` renders `Message` on **row 2** in red
  when non-empty, and **moves the field block to row 3** (always — uniform layout).
  This aligns the detail screen with the documented three-band ISPF layout
  (title row 0 / message row 2 / body row 3) and shifts the **audit** detail
  fields down one cosmetic row. Cursor still homes to `{0,0}`.
- **New renderer method** for an action-capable detail, e.g.
  `DetailAct(v DetailView, actPF int) (ListAction, error)` — exits on PF3 **or**
  the `actPF` AID (PF11); `actPF == 0` ⇒ PF3 only. Returns a `ListAction` carrying
  the PF. The existing `Detail(DetailView) error` is unchanged and the audit
  screen keeps using it.
- **`DetailConfig` + `RunDetail`:**

  ```go
  type DetailConfig struct {
      View       DetailView
      ActPF      int                                       // 11; 0 ⇒ read-only
      DonePFHelp string                                     // PF help after the action commits (e.g. "PF3=Back")
      Confirm    func() (prompt, blocked string)            // first ActPF press
      OnAct      func(ctx context.Context) (status string, refresh bool) // second press
  }

  // RunDetail renders cfg.View until PF3. Returns refresh=true when an action
  // committed (the caller should re-fetch its list on return).
  func RunDetail(ctx context.Context, r Renderer, cfg DetailConfig) (refresh bool, err error)
  ```

  Loop: render `View` with the current `Message`; read the action.
  - `PF3` ⇒ return `(refresh, nil)`.
  - `ActPF` press while not yet acted:
    - armed ⇒ `OnAct` commits; set `Message = status`, `refresh = result`, mark
      acted (so `ActPF` is no longer an exit key — pass `actPF = 0` to the
      renderer afterwards) and swap `View.PFHelp = DonePFHelp`.
    - not armed ⇒ consult `Confirm`; `blocked != ""` sets `Message = blocked`;
      else `Message = prompt` and arm.
  - any other AID ⇒ disarm, clear `Message`.

  `Confirm` and `OnAct` must both be non-nil when `ActPF != 0` (mirrors the
  `RunSnapshotList` contract: a confirm-gated action has no immediate-commit
  path).

### 3. `internal/ui3270/snapshotlist.go` — `OnSelect` refresh contract

Widen `OnSelect` from `func(ctx, r, item) error` to
`func(ctx, r, item) (refresh bool, fatal error)`. In `RunSnapshotList`'s `'S'`
branch, when `refresh` is true set `loaded = false; page = 0` so the next loop
re-fetches the snapshot (the booted session disappears). Two call sites updated:
the audit viewer returns `(false, nil)`; the sessions viewer returns the
`RunDetail` refresh flag.

## Data flow

```
Active Sessions list (RunSnapshotList)
  └─ 'S' on a row → OnSelect(ctx, r, SessionView)
       └─ sessionDetail(...) builds DetailView (+ PTR lookup if enabled)
       └─ RunDetail(ctx, r, DetailConfig{ActPF:11, Confirm, OnAct})
            ├─ PF11 ×1 → Confirm → prompt | veto on message line
            ├─ PF11 ×2 → OnAct → sessions.Disconnect + audit → "DISCONNECTED", refresh=true
            └─ PF3 → return refresh
       └─ OnSelect returns refresh → 'S' branch sets loaded=false, page=0
  └─ list re-fetches; booted session gone
```

`SessionView` already carries `ConnectedAt` + `LoggedInAt`, so ages need no new
registry data. No store/schema changes.

## Error handling

- **PTR lookup** — bounded by the existing `ptrTimeout` (2s); errors render as
  `(unavailable)`, no record as `(none)`, empty address omits the field. A slow or
  hostile resolver can never hang the admin session.
- **Disconnect race** — `Disconnect` returns `ok=false` when the id is already
  gone; the detail shows `SESSION ALREADY ENDED` and still refreshes the list on
  return.
- **Self-disconnect** — vetoed at the `Confirm` step; never reaches `OnAct`.
- **Dead connection** — `RunDetail` / `RunSnapshotList` propagate a non-nil
  renderer error up as a fatal (dead client), unchanged from the existing drivers.

## Testing

- **Unit (`internal/server`):** `sessionDetail` field content (full untruncated
  address, both Julian stamps + ages, `(login)` / `-` / `(not logged in)`); PTR
  present/absent under the flag (fake `Resolver`); `OnAct` audits + returns
  `refresh`; self-veto via `Confirm`; `Now` seam for deterministic ages.
- **Unit (`internal/ui3270`):** `RunDetail` two-press commit, veto path,
  disarm-after-action (PF11 inert post-commit), PF3 refresh propagation,
  read-only (`ActPF == 0`) path; `buildDetailScreen` renders `Message` on row 2
  and shifts fields to row 3. Scripted fake renderer.
- **`-race`** across all of it (the bridge/registry are concurrent).
- **Smoke (`s3270`, `.claude/skills/s3270-smoke-testing/smoke.sh`):** `S` drills
  into a session → full client address visible on the detail; PF11 ×2 disconnects
  → `DISCONNECTED` status → PF3 returns to a list no longer listing that session.
  Re-verify the audit detail still renders correctly after the row-2 message-line
  shift.

## Conventions

- Build to `docs/ispf-style-guide.md` (read-only field panel like the audit
  detail; message line row 2; PF help on the last row).
- TDD; small single-responsibility changes; `store` owns all SQL (none changes
  here); `screens` / `ui3270` stay pure-render.
- No credential logging. PTR is the only network call and is admin-triggered.
