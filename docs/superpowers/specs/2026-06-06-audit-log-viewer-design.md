# Audit Log viewer on the admin menu (RECENT view, v1)

**Issue:** GH #40
**Date:** 2026-06-06
**Status:** Design — approved, pending implementation plan

## Goal

Put a 3270 face on the existing audit trail so day-to-day "what happened to
this user?" support requests don't require shell access to `audit list`. This
is the first concrete step toward the north star: *almost all day-to-day admin
functions should have a 3270 UI.*

The data layer already exists — the `audit` table plus `RecordAudit`/`ListAudit`/
`PruneAudit` in `internal/store` and the `audit list|prune` CLI. This work is
mostly a screen on top, plus a thin snapshot list driver and two new system
parameters.

## Scope

**In scope (v1 — the RECENT view):**
- A read-only, newest-first, paged viewer over the last 72 hours, with a
  drill-down detail screen.
- Derived-severity colour-coding of the event column.
- Reverse-DNS (PTR) enrichment on the detail screen.
- Two new System Parameters (the safety cap and the reverse-DNS toggle).

**Out of scope (next iteration):** a filter screen (by username / session /
event / time range), which is also how arbitrary older history gets reached.
`store.AuditFilter` already carries the fields a filter screen needs
(`Username`, `Kind`, `Since`, `Limit`), so v1 does not need to anticipate it.

## Background: why a snapshot, not a live re-fetch

The generic list driver `ui3270.RunList` re-runs its `Fetch` callback on **every
iteration** by design, so mutations in the users/groups/services lists show up
immediately. For those screens that is correct.

For a newest-first audit list it is wrong, for two reasons:

1. **Paging a moving target.** New events land at the *top* of a newest-first
   list. If the admin is on page 2 and three `auth_fail` events arrive, pressing
   PF8 shifts every row and the admin skips or re-sees records. Paging only makes
   sense over a *stable* snapshot.
2. **It defeats the staleness stamp.** The header carries an "as of last query"
   timestamp; the gap between that stamp and the wall clock is the staleness
   signal. If every render re-queries, the stamp always equals "now" and the
   signal is meaningless.

Therefore the viewer fetches a **snapshot** once and pages over the held slice;
**plain Enter is the explicit refresh** (re-query + re-stamp). `RunList` cannot
express this (its `Fetch` callback receives no signal distinguishing a page turn
from a refresh request), so v1 adds a dedicated read-only driver.

## Architecture

```
admin menu (option 6)
  └─ adminFlow.auditLog()                         internal/server/admin_audit.go (new)
       ├─ ui3270.RunSnapshotList[store.AuditEvent] internal/ui3270/snapshotlist.go (new)
       │    fetch once → page held slice → Enter refreshes → S returns a row
       ├─ store.ListAudit(Since: now-72h, Limit: cap+1)   (existing — no change)
       ├─ sysconfig AUDIT_MAX_ROWS / AUDIT_REVERSE_DNS     internal/sysconfig/catalog.go
       └─ detail screen + PTR lookup
            ├─ screens.AuditDetailScreen(...)      internal/screens/audit.go (new)
            └─ Resolver seam (net.Resolver)        internal/server (new, testable)
```

Layering is preserved: `screens` and `ui3270` stay pure render (no DB, no
network). The PTR lookup and the snapshot fetch live in the `server` layer; the
resolved hostname and the formatted rows are passed *into* the screen builders as
plain data. No SQL leaves `internal/store`.

## Components

### 1. `ui3270.RunSnapshotList[T]` (new)

A thin read-only viewer with its own small screen builder, reusing the existing
`pageBounds` helper for paging math.

Behaviour:
- **Fetch once** into a held slice on entry; record the fetch time (drives the
  `AS OF` header stamp).
- **PF7 / PF8** page within the held slice — no re-query.
- **Plain Enter** = re-fetch + re-stamp (the explicit refresh).
- **`S` on a row** = return that row's item to the caller (which paints the
  detail screen); on return, resume the *same* snapshot at the *same* page.
- **PF3** = return to the caller (admin menu).
- **Empty window** = a `(none)` state; cursor homes to `{0,0}`.
- PA1/PA2/PA3/Clear are silent no-ops (same convention as `RunList`).

Config carries: title, the as-of stamp, column-heading row, a fetch callback
returning `(rows []Row[T], asOf time.Time, overflow bool, errMsg string)`, a
per-row display formatter that can assign a colour to the event segment, and the
`S`-row callback. No add/delete/confirm machinery (read-only).

The row builder lays each data row as **three fields** so the event can carry its
own colour attribute: `[date/time/user] [EVENT coloured] [detail]`. Each field's
attribute byte occupies one blank cell, which doubles as the column separator.

### 2. List screen layout (MOD 2; self-normalising for larger models)

72 usable display columns (cols 8–79); 14 data rows per page on MOD 2.

```
 RECENT ACTIVITY                                            ROW 1 OF 312
                                                            (page counter, col 60)
 AS OF 2026-06-06 (2026.157)  14:32 UTC
 CMD MM/DD HH:MM USERNAME EVENT        DETAIL
 _   06/06 14:31 ADMIN    ADMIN        created user JSMITH      (EVENT yellow)
 _   06/06 14:30 JSMITH   AUTH_OK                               (EVENT default)
 _   06/06 14:28 BADGUY   AUTH_FAIL    delay=4s count=2         (EVENT red)
 ...
 S=Detail
 <error line>
 PF3=Back   PF7=Bkwd  PF8=Fwd   Enter=Refresh
```

Column budget (the 72 chars from col 8):

| Field    | Width | Notes |
|----------|-------|-------|
| MM/DD    | 5     | date |
| HH:MM    | 5     | time, UTC (label only in header) |
| USERNAME | 8     | truncated/padded; full value on detail |
| EVENT    | 12    | longest kind (`bridge_start`/`mfa_enrolled`/`mfa_enforced`) is exactly 12; uppercased; coloured |
| DETAIL   | 37    | summary; full value on detail (3 cols lost to field attribute bytes vs a single-field row) |

- **As-of stamp** (`AS OF YYYY-MM-DD (YYYY.DDD)  HH:MM UTC`) is the snapshot time,
  re-stamped only on refresh. Includes the Julian day-of-year.
- **Overflow marker.** When the 72h window holds more than the cap, the header
  shows `…(newest N)` so the screen never implies it showed everything.
- **UTC everywhere.** Stored UTC is displayed as-is — no local-time conversion.
  Only the header carries the `UTC` label; bare rows keep the columns.

### 3. Event colour map (derived severity)

There is no severity column in the schema; severity is derived from `Kind` in the
presentation layer (a pure map; no data-model change, and compatible with a future
severity filter). **Only the EVENT text is coloured** — most rows stay plain, so
the exceptions pop.

| Tier | Colour | Kinds |
|------|--------|-------|
| Error / security-fail | Red | `auth_fail`, `auth_error`, `mfa_failed` |
| Notable / security-state change | Yellow | `admin`, `mfa_cleared`, `mfa_enforced`, `mfa_enrolled` |
| Routine | default (no colour) | `connect`, `auth_ok`, `mfa_success`, `bridge_start`, `bridge_end`, `logout`, `disconnect` |

### 4. Detail screen (`screens.AuditDetailScreen`, new)

Read-only, custom builder (not the generic single-line form) so the detail text
can wrap full-width untruncated.

```
 AUDIT DETAIL

 Date/Time   2026-06-06 (2026.157) 14:28:07 UTC
 Session     7f3a9c2e1b4d
 Username    BADGUY
 Event       AUTH_FAIL                              (same colour map)
 Remote      203.0.113.9:54221
 PTR         host.example.de
 Service

 Detail
 delay=4s count=2
 ...wraps full-width across continuation lines if long...

 PF3=Back
```

- `Date/Time` shows full `YYYY-MM-DD (YYYY.DDD) HH:MM:SS UTC` (Julian day in
  parens, matching the list).
- Includes `Remote` and `Service` (both already in `store.AuditEvent`): exactly
  what a "what happened / from where / into what" lookup wants. `Service` is
  blank for non-bridge events.
- `Detail` is rendered untruncated, wrapped across continuation lines.
- PF3 returns to the list snapshot.

### 5. Reverse DNS (PTR)

- **Detail-only.** A per-row lookup on the list would mean 14 blocking DNS
  queries per page; one lookup per drill-down is acceptable.
- **Resolver seam** in the `server` layer: an interface with
  `LookupAddr(ctx, ip) ([]string, error)`; the real impl wraps `net.Resolver`,
  tests inject a fake. Keeps live DNS out of unit tests and `screens` pure.
- **Bounded by a 2s context timeout** so a slow or hostile resolver can't hang
  the session. The lookup blocks only the admin's own connection.
- `RemoteAddr` is `host:port`; `net.SplitHostPort` extracts the IP. The first
  PTR name is shown.
- **States:** no PTR record → `(none)`; timeout/error → `(unavailable)`; no
  `RemoteAddr` on the event (e.g. `admin` mutations) → omit both Remote and PTR
  lines; reverse-DNS toggle `OFF` → omit the PTR line.
- **Caveat (documented, not enforced):** PTR is advisory, not authenticated —
  the owner of the IP's reverse zone controls it. It is a triage hint
  ("why is this US user arriving from a `.de` host?"), not proof. No
  forward-confirmed rDNS (FCrDNS) in v1.

### 6. System Parameters (new Catalog entries)

Both auto-appear on the System Parameters form (it builds from `sysconfig.Catalog`)
and are read at runtime via `store.GetConfig`.

```go
KeyAuditMaxRows     = "AUDIT_MAX_ROWS"
DefaultAuditMaxRows = 1000

KeyAuditReverseDNS     = "AUDIT_REVERSE_DNS"
DefaultAuditReverseDNS = "ON"
```

| Key | Label | Default | Validator |
|-----|-------|---------|-----------|
| `AUDIT_MAX_ROWS` | `Audit View Max Rows:` | `1000` | `intInRange(1, 10000)` (new) |
| `AUDIT_REVERSE_DNS` | `Audit Reverse DNS:` | `ON` | `onOff` (new; Normalize = trim+upper) |

- `intInRange(min, max)` and `onOff` are new shared validators alongside the
  existing `nonNegativeInt` / `positiveInt`.
- The viewer reads `AUDIT_MAX_ROWS`, parses it, and **clamps to [1,10000] with a
  fallback to 1000** if a hand-edited DB holds garbage — the same defensive
  pattern `auththrottle.go`'s `throttleInt` already uses.

## Data flow (one refresh)

1. Viewer reads `AUDIT_MAX_ROWS` (cap, clamped) and `AUDIT_REVERSE_DNS`.
2. `store.ListAudit(ctx, AuditFilter{Since: now-72h, Limit: cap+1})` — newest
   first. **No store change**; the `+1` detects window overflow.
3. If `len > cap`: trim to `cap`, set the overflow marker.
4. Format rows (uppercased, coloured event); record `asOf = now`.
5. `RunSnapshotList` pages the held slice until PF3.
6. On `S`: paint `AuditDetailScreen`; if reverse-DNS is on and the event has a
   `RemoteAddr`, resolve the PTR (2s timeout) and pass the result in.

## Wiring touchpoints

- `internal/screens/admin.go` — add `6.  Audit Log` to `AdminMenuScreen`.
- `internal/server/presenter_admin.go` — accept `"6"` in the `AdminMenu` parse.
- `internal/server/admin.go` — `case 6:` dispatch to `f.auditLog(ctx, conn)`.
- `internal/server/admin_audit.go` (new) — the flow: read params, fetch, run the
  snapshot list, handle `S` → detail + PTR.
- `internal/ui3270/snapshotlist.go` (new) — `RunSnapshotList` + its row builder.
- `internal/screens/audit.go` (new) — `AuditDetailScreen` builder.
- `internal/sysconfig/catalog.go` — two new entries + `intInRange`/`onOff`.

## Testing

- **Unit (no live 3270 / DNS):**
  - `RunSnapshotList`: fetch-once, page the held slice, Enter re-fetches, `S`
    returns the row and resumes the same page, empty → `(none)` + home cursor.
  - Event colour mapping (each kind → expected tier).
  - `intInRange` / `onOff` validators; the cap clamp/fallback.
  - Detail flow with a **fake resolver**: PTR present / `(none)` / `(unavailable)`
    / omitted when toggle off or no RemoteAddr.
- **Protocol surface (s3270 smoke skill):** column budget on 80 cols, cursor
  position per screen, colour attributes on the event field, PF3/PF7/PF8/Enter
  semantics, the empty-window home cursor. Unit tests do not catch positioning.
- A human pass in a live emulator (c3270) remains the final word on visual polish.

## Open questions

None outstanding; all design decisions are settled above.
