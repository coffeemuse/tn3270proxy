# Operator-configurable menu status `System ID` — design

**Issue:** GH #64 — *System Parameters: make menu status block 'System ID' operator-configurable (replace placeholder)*
**Date:** 2026-06-05
**Status:** Approved design, ready for implementation plan
**Builds on:** GH #53 (menu status block) — done and on `main`. Completes the "System ID source" follow-up #53 deferred to the System Parameters work.

## Problem

The menu status block (#53) renders a **System ID** row, but the value is a
hardcoded placeholder:

- `internal/server/session.go` — `const systemIDPlaceholder = "PROXY"`, carrying a
  `TODO(#53)` pointing at this issue.
- The menu loop sets `status.SystemID = systemIDPlaceholder`.

It should be operator-configurable, following the same pattern already used for
`MOTD_FILE` and `MFA_ISSUER`: declare one entry in `sysconfig.Catalog` (which
seeds the default and auto-builds the admin edit field), then read the value at
menu-paint time.

## Key decisions

The issue left three things open. The settled answers:

1. **Over-length input.** The admin form field is physically capped at 7 columns
   (a 3270 field's width is bounded by the next attribute byte — see
   `internal/ui3270/screen.go`), so the operator cannot type more than 7 chars.
   A length check is *also* kept in validation as defense-in-depth (the value
   could be set through other paths, e.g. seed or direct DB writes).

2. **Normalization + charset.** The stored value is canonical: **trim whitespace,
   uppercase regardless of input**, then validate the character set. Allowed:
   `A-Z`, `0-9`, and `-`; a dash may not be the first character. Classic mainframe
   system IDs are uppercase alphanumeric, so this matches operator expectation and
   means the render side needs no `ToUpper`.

3. **Required, default `PROXY`.** The status row always shows *a* system ID (a
   mainframe always does). The value is required (empty rejected), seeded to
   `"PROXY"` to preserve current behavior, and the menu read falls back to
   `"PROXY"` on any read error or empty value so the menu never fails to render.

## Contract change: extend `sysconfig.Entry`

Decision (2) needs a transform hook the catalog does not have today — `Validate`
only returns an error string, it cannot fold a value to canonical form. Decision
(1) needs a per-entry input width; the admin form currently hardcodes
`Length: 64` for every field. Both are small, generalizing additions to the
shared `Entry` struct, and both are backward-compatible (zero values preserve
current behavior for the five existing entries):

```go
type Entry struct {
    Key       string
    Label     string
    Default   string
    Length    int                  // input field width; 0 ⇒ default 64
    Normalize func(string) string  // canonicalize before validate+store; nil ⇒ identity
    Validate  func(string) string
}
```

- `Length` makes the admin form's input width data-driven (fallback 64 when 0).
- `Normalize` is applied **before** `Validate` and before `SetConfig`, so storage
  is canonical and validation sees the canonical form.

This is a deliberate, in-scope reshaping of the catalog contract — justified
because both fields are genuine requirements of #64 and are reusable by future
parameters. We are early in development; reshaping contracts when required is
acceptable, but only the minimal change needed is made (the other four entries
are not retrofitted).

## The `SYSTEM_ID` catalog entry

```go
const KeySystemID = "SYSTEM_ID"

{
    Key:       KeySystemID,
    Label:     "System ID:",
    Default:   "PROXY",
    Length:    7,
    Normalize: func(v string) string { return strings.ToUpper(strings.TrimSpace(v)) },
    Validate:  validateSystemID,
}
```

`validateSystemID` runs on the already-normalized value and returns an uppercase
`errMsg` (or `""`), matching the style of the existing entries:

| Condition                         | Message                              |
|-----------------------------------|--------------------------------------|
| empty                             | `SYSTEM ID REQUIRED`                 |
| length > 7                        | `SYSTEM ID TOO LONG (MAX 7)`         |
| first char is `-`                 | `SYSTEM ID MUST NOT START WITH A DASH` |
| any char outside `A-Z 0-9 -`      | `SYSTEM ID: USE A-Z 0-9 AND DASH`    |

## Admin form wiring (`admin_system.go`)

Two edits in `systemParams`, both generic (benefit every catalog entry):

- **Field width:** use `e.Length`, falling back to 64 when 0. Only `SYSTEM_ID`
  sets 7 today; the other four stay at 64.
- **Submit path:** normalize first, then validate / compare / store the normalized
  value:

  ```go
  nv := vals[e.Key]
  if e.Normalize != nil { nv = e.Normalize(nv) }
  if msg := e.Validate(nv); msg != "" { return msg, nil }
  // compare nv vs oldVal; SetConfig(nv); audit oldVal -> nv; fields[i].Value = nv
  ```

  Storing the normalized value means the re-rendered form (`StayOnSave`) shows the
  canonical `PROXY`, and the audit line records the canonical value. Validation
  stays all-or-nothing (validate every entry before writing any), as today.

## Menu wiring (`session.go`)

- Delete `const systemIDPlaceholder`.
- Add a `systemID(ctx)` helper mirroring `mfaIssuer()`: `GetConfig(KeySystemID)`,
  and on error **or** empty value fall back to `"PROXY"`.
- In the menu loop, set `status.SystemID = s.systemID(ctx)`.

## Data flow

```
migrate() seeds SYSTEM_ID="PROXY"
  → admin edits via System Parameters form (Normalize → Validate → SetConfig)
  → next menu render reads via GetConfig
  → MenuStatus.SystemID
  → truncateRunes(value, 7) in internal/screens/menu.go
```

Consistent with the existing "admin changes apply at the next menu render/login"
convention — live sessions are not re-evaluated.

## Error handling

- **Read error / empty value at menu paint** → silent fallback to `"PROXY"`. The
  menu must never fail to render over a config read.
- **Save error** → existing `storeErr` path, unchanged.
- **Invalid input** → uppercase `errMsg` on the form; all-or-nothing, so a single
  bad field blocks the whole save (existing Submit behavior).

## Testing plan

**`internal/sysconfig` (catalog_test.go)** — table-driven `validateSystemID` +
`Normalize`:
- valid: `PROXY`, `SYSA`, `A1`, `SYS-01`, `A-B-C-D`, max-length `ABCDEFG` (7)
- normalize: `" proxy "` → `PROXY`, `sysa` → `SYSA` (assert normalize-then-validate passes)
- reject: `""` (required), `ABCDEFGH` (8, too long), `-SYS` (leading dash),
  `SYS_01` / `SYS.1` / `SYS@` (charset) — each returns the exact uppercase message.

**`internal/store`** — seed/get/set round-trip: fresh DB →
`GetConfig("SYSTEM_ID")` == `"PROXY"` (migrate seeded it); `SetConfig` →
`GetConfig` returns the new value. Extend the existing catalog-driven seed test to
assert the new key rather than duplicate the harness.

**`internal/server` (session_test.go)** — the existing status-block test asserts
`status.SystemID == "PROXY"`; keep that as the default-path case and add: with
`SetConfig("SYSTEM_ID","SYSA")`, the menu loop populates `status.SystemID ==
"SYSA"`; plus an empty/unset case → `"PROXY"` fallback.

**`internal/server` (admin_system_test.go)** — form round-trip: load (shows
`PROXY`) → submit `"sysa"` → assert the store holds `"SYSA"` (normalized) and an
audit line was recorded; submit invalid (`"-X"`) → assert errMsg and no write.

**Smoke (s3270-smoke-testing skill)** — the value shows in the right-hand status
block; admin edit works end to end (edit → save → return to menu → new value
visible).

## Out of scope (YAGNI)

- Retrofitting `Length`/`Normalize` onto the other four catalog entries.
- Live-session re-evaluation (admin convention is next-render).
- `ToUpper` at render time — storage is already canonical, so it's redundant.
