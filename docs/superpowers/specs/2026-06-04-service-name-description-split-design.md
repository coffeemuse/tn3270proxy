# Service NAME/Description Split + Case Normalization Design

**Date:** 2026-06-04
**Status:** Approved for spec; pending user review of this document
**Closes:** GH issue #9 (no case normalization for usernames or entity names)
**Scope:** Canonical-uppercase identifiers for users and groups; a split
service model (uppercase `NAME` identifier + mixed-case `Description` label);
an ISPF-style end-user service menu.

> **Amended 2026-06-08:** the original "pre-production, no data migration" details
> in this document have been removed. Schema is now versioned via the forward-only
> migration framework ([#88](https://github.com/coffeemuse/TN3270Proxy/issues/88));
> see `docs/superpowers/specs/2026-06-08-schema-migration-framework-design.md`. The
> superseded details are omitted here — git history and issues #9/#88 preserve the
> record.

## 1. Purpose

Two related problems, addressed together because they touch the same files
(`internal/store`, `internal/screens`, `internal/server`, `internal/seed`):

1. **No case normalization (#9).** Nothing folds case for usernames, group
   names, or service names. Lookups and `UNIQUE` constraints are exact-match, so
   a user seeded as `ALICE` cannot log in by typing `alice`, and a service
   `PROD` can coexist with a confusingly distinct `prod`. A latent variant: the
   reserved-prefix check folds case (`admin_groups.go`) but the admin gate
   `slices.Contains(identity.Groups, store.AdminGroup)` is case-sensitive, so a
   seeded `zzadmin` group is reserved yet grants nobody admin.

2. **Service name conflates identifier and label.** The single `services.name`
   column is both the dedup key and the string shown to users. Making it a clean
   uppercase identifier would produce ugly menus; keeping it human-friendly
   leaves the duplicate-row bug open.

## 2. Decisions

- **Users and groups:** names are **canonical uppercase**. Fold to uppercase at
  the store layer; add `COLLATE NOCASE` to the `UNIQUE` columns as a safety net.
  Passwords are never normalized (bcrypt path untouched).
- **Services:** split into two columns.
  - `name` — the canonical **identifier**: auto-uppercased, restricted to
    `A–Z`/`0–9` (no spaces or other characters), `UNIQUE` (`COLLATE NOCASE`),
    max **8 characters**. This is the dedup/lookup key.
  - `description` — the mixed-case **human label**, **required**, max **40
    characters**, shown to end users. Stored exactly as typed.
  - `host`/`port` remain admin-only detail.
- **End-user menu** mimics the ISPF primary-option menu: `NN  NAME  Description`.
  Host and port are **not** shown to end users. The proxy exists to mediate
  access for security and, secondarily, to shield users from backend
  implementation details (host/port); exposing addresses in the user menu would
  undercut that. Host/port remain visible on the admin screens.

## 3. Store layer (`internal/store`)

The store is the single choke point — no normalization or validation leaks into
other packages (per the "store owns all SQL" convention).

### Schema

```sql
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    username TEXT UNIQUE COLLATE NOCASE NOT NULL,
    password_hash TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS groups (
    id INTEGER PRIMARY KEY,
    name TEXT UNIQUE COLLATE NOCASE NOT NULL
);
CREATE TABLE IF NOT EXISTS services (
    id INTEGER PRIMARY KEY,
    name TEXT UNIQUE COLLATE NOCASE NOT NULL,   -- uppercase A-Z/0-9, <=8
    description TEXT NOT NULL,                    -- mixed case, required, <=40
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    tls INTEGER NOT NULL DEFAULT 0,
    tls_verify INTEGER NOT NULL DEFAULT 1
);
```

### API changes

- `Service` struct gains `Description string`.
- `CreateService(ctx, name, description, host string, port int, tls, verify bool)`
  and `UpdateService(...)` gain the `description` parameter.
- `CreateUser`, `CreateGroup`, `CreateService`, `UpdateService`,
  `GetUserByUsername` fold the name/username to uppercase before use.
- A small unexported `normalizeServiceName(string) (string, error)` folds to
  uppercase, trims, validates `^[A-Z0-9]{1,8}$`, and returns a clear error
  (e.g. `service name must be 1-8 chars, A-Z and 0-9 only`). `CreateService` /
  `UpdateService` reject empty `description` and `len > 40` with clear errors.

## 4. Menu rendering (`internal/screens/menu.go`)

`MenuScreen` renders each service as an ISPF-style row:

```
 1  PRODCICS  Production CICS Region
 2  TSO       Time Sharing Option
```

Format: `%2s  %-8s  %s` (selection number, `NAME` left-justified to 8 columns,
`Description`). The selection number remains the menu key in the returned
mapping. `Host`/`Port` are no longer referenced in this builder. Content stays
within columns 0–79 for the configured `Geometry` (description is bounded to 40).

## 5. Admin UI (`internal/server/admin_services.go`, `internal/screens/admin.go`)

- New field-name constant `FieldDescription`.
- The service form gains a **Description** field (mixed-case, required)
  alongside Name/Host/Port/TLS/Verify; Name input is auto-uppercased and
  validated on submit (errors surfaced on the form's error line).
- The admin service **list** shows `NAME  Description` so admins see both the
  identifier and the label; the existing `checkServiceNameFree` pre-check keys
  on the normalized NAME.

## 6. Seed (`internal/seed`)

- `SeedService` gains `Description string `json:"description"`` (required).
- `Apply()` passes `description` through to `CreateService`.
- `seed.example.json` updated with descriptions; service names made valid
  uppercase identifiers.

## 7. Admin-gate bonus (no extra code)

With group names stored uppercase, `identity.Groups` is always uppercase, so the
exact compares at `server/session.go:125` and `server/admin_users.go:115`
against `store.AdminGroup` (`ZZADMIN`) become correct as-is. The
`zzadmin`-grants-nobody-admin latent bug closes. `ListServicesForGroups`
(`WHERE g.name IN (...)`) also matches correctly since both sides are uppercase.

## 8. Testing (TDD, `go test ./... -race`)

- **store:** `name` folds (`prod` → `PROD`); invalid charset and spaces
  rejected; over-length name/description rejected; empty description rejected;
  `PROD` then `prod` dedups to one row; `CreateUser("alice")` then
  `GetUserByUsername("ALICE")` succeeds; password case-sensitivity unchanged;
  seeded `zzadmin` group resolves to admin.
- **screens:** `MenuScreen` renders `NN  NAME  Description` and maps the number
  to the right `Service`; host/port absent from output.
- **server/admin:** Description round-trips through create/edit; invalid NAME
  surfaces a form error.
- **seed:** apply with descriptions; missing description is an error.

## 9. Out of scope (YAGNI)

- No audit-username folding (best-effort audit strings left as-is).
- No per-service description uniqueness.
- No NAME rename history or aliasing.
