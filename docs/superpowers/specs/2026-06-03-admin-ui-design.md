# 3270 Admin management UI — Design

**Status:** Approved (brainstorm complete)
**Date:** 2026-06-03
**Roadmap item:** #3 (`docs/superpowers/ROADMAP.md`)
**Spec for:** managing users, groups, services, memberships, and group↔service links
from a 3270 admin screen set inside the proxy itself — replacing hand-edit-JSON + re-seed
as the only management path.

## Goal

An admin is just a user in a reserved **ZZADMIN** group. After a normal login, members of
that group see one extra selection (`A`) on the service menu that enters an admin screen
set offering **full CRUD**: users (add / set password / delete / group membership), groups
(add / delete), services (add / edit incl. both TLS fields / delete / group access). All
writes go through `internal/store` — the single data path — so the seed command and the
admin UI stay interchangeable.

## Decisions (from brainstorm)

- **Surface:** 3270 screens (not CLI, not HTTP). On-brand, no new network surface,
  exercises the product itself.
- **Scope:** full CRUD including deletes.
- **Reserved group namespace:** group names with the (case-insensitive) prefix **`ZZ`**
  are app-dictated. The admin UI can neither create nor delete them; it can manage their
  *membership*. `ZZADMIN` is the first such group.
- **ZZADMIN always exists:** the store migration creates it idempotently (like the
  `tls_verify` column migration). Seed JSON assigns the first member; that is the
  bootstrap path.
- **Lockout guardrails:** an admin cannot delete their own account, cannot remove the
  last ZZADMIN member, and cannot delete ZZADMIN (covered by the ZZ rule).
- **Entry point:** menu selection `A`, rendered only for ZZADMIN members.
- **List UX:** ISPF-style line commands (a 1-char `CMD` field per row), PF7/PF8 paging.
- **Architecture:** admin sub-flow behind its own seams (`AdminStore`/`AdminPresenter`),
  not inline growth of `Session.Run`/`Presenter`.

## Architecture

```
screens   + admin_*.go — pure builders (no DB/network), fixed 24×80 (MOD 3+ is roadmap #7)
server    + admin.go — adminFlow{store AdminStore, presenter AdminPresenter,
            identity auth.Identity} with Run(ctx, conn); session menu branch;
            AdminGroup / ReservedGroupPrefix constants
store     + list/update/delete methods; migrate() ensures ZZADMIN exists
auth      + HashPassword(password) (hash, error) — single bcrypt path, shared with seed
seed      Apply switches to auth.HashPassword; seed.example.json gains ZZADMIN + member
```

Dependency direction is unchanged: screens know nothing of the store; the flow logic in
`server` talks to the store through an interface and to the terminal through a presenter,
so it unit-tests with fakes exactly like `Session.Run`.

### 1. Constants and menu entry (`internal/server`)

```go
const AdminGroup = "ZZADMIN"          // reserved admin group (auto-created by migrate)
const ReservedGroupPrefix = "ZZ"      // app-dictated group namespace (case-insensitive)
```

`Presenter.Menu` carries the admin entry:

```go
Menu(conn net.Conn, services []store.Service, admin bool, errMsg string) (selected *store.Service, adminSel bool, quit bool, err error)
```

- `admin` (in): render the `A` selection and its help text.
- `adminSel` (out): the user picked `A` (only honored when `admin` was true).
- `Session.Run` computes `admin` from `identity.Groups`, and on `adminSel` runs the
  admin flow, then loops back to re-render the menu (fresh service list — edits show up
  immediately). `fakePresenter` in `session_test.go` updates to match.

### 2. The admin flow (`internal/server/admin.go`)

A self-contained state machine driving the screens below. Its seams:

```go
// AdminStore is the slice of *store.Store the admin flow needs (satisfied by *store.Store).
type AdminStore interface {
    ListUsers(ctx) ([]store.User, error)
    GetUserByUsername(ctx, username) (store.User, error)
    GetUserGroups(ctx, userID) ([]string, error)
    CreateUser(ctx, username, passwordHash) (int64, error)
    SetPassword(ctx, userID, passwordHash) error
    DeleteUser(ctx, userID) error
    ListGroups(ctx) ([]store.Group, error)
    CreateGroup(ctx, name) (int64, error)
    DeleteGroup(ctx, groupID) error
    CountGroupMembers(ctx, groupID) (int, error)
    AddUserToGroup(ctx, userID, groupID) error
    RemoveUserFromGroup(ctx, userID, groupID) error
    ListAllServices(ctx) ([]store.Service, error)
    GetService(ctx, serviceID) (store.Service, error)
    CreateService(ctx, name, host, port, tls, verify) (int64, error)
    UpdateService(ctx, id, name, host, port, tls, verify) error
    DeleteService(ctx, serviceID) error
    ListGroupsForService(ctx, serviceID) ([]store.Group, error)
    LinkGroupService(ctx, groupID, serviceID) error
    UnlinkGroupService(ctx, groupID, serviceID) error
}

// AdminPresenter renders the admin screens: one method per screen, taking view
// data and returning a small action value (verb + target + form fields). The
// real impl wraps go3270 like go3270Presenter; tests use a fake. Exact method
// signatures are pinned in the implementation plan.
```

The flow owns **policy**: guardrails, duplicate pre-checks, input validation, and
password hashing (via `auth.HashPassword`). The store stays mechanical.

### 3. Store additions (`internal/store`)

New type `Group{ID int64; Name string}`. New methods (all SQL in `store`, as ever):

```
ListUsers(ctx) []User                ListGroups(ctx) []Group
ListAllServices(ctx) []Service      GetService(ctx, id) Service
SetPassword(ctx, userID, hash)      UpdateService(ctx, id, name, host, port, tls, verify)
DeleteUser(ctx, id)                 DeleteGroup(ctx, id)     DeleteService(ctx, id)
RemoveUserFromGroup(ctx, uid, gid)  UnlinkGroupService(ctx, gid, sid)
CountGroupMembers(ctx, gid) int     ListGroupsForService(ctx, sid) []Group
```

- **Deletes cascade explicitly** in one transaction: `DeleteUser` removes its
  `user_groups` rows; `DeleteGroup` removes its memberships and `group_services` links;
  `DeleteService` removes its links. No reliance on FK pragmas.
- **Migration:** `migrate()` gains an idempotent `INSERT OR IGNORE` of the `ZZADMIN`
  group (the existing `CreateGroup` SQL shape). Repeated `migrate()` stays a no-op.
- `Update*`/`SetPassword` are plain UPDATEs by ID (they exist precisely because the
  idempotent `Create*` never modify existing rows).

### 4. Screens (`internal/screens/admin_*.go`)

All fixed 24×80, following the house layout: title row 0, error line just above the
PF-key help, help on row 23, cursor at `(field.Row, field.Col+1)` of the primary input.
Field-name constants exported like `FieldUsername` today. Page size for lists is derived
from the fixed layout (≈14 data rows); tests assert field names/content, not row numbers.

| Screen | Content / fields | Actions |
|---|---|---|
| Admin menu | options 1=Users 2=Groups 3=Services, `OPTION ===>` | Enter, PF3/PA3 |
| User list | rows: CMD, username, groups | `S`=set password, `G`=groups, `D`=delete; PF4=add |
| User add | USERNAME, PASSWORD, RETYPE (both non-display) | Enter=create, PF3=cancel |
| Set password | (title shows user) PASSWORD, RETYPE (non-display) | Enter=save, PF3=cancel |
| User groups | rows: CMD, group name, `X` membership marker | `A`=add, `R`=remove |
| Group list | rows: CMD, group name, member/service counts | `D`=delete; PF4=add |
| Group add | NAME | Enter=create, PF3=cancel |
| Service list | rows: CMD, name, host:port, TLS/VERIFY flags | `S`=edit, `G`=group access, `D`=delete; PF4=add |
| Service form | NAME, HOST, PORT, TLS (Y/N), VERIFY (Y/N) — shared by add and edit (edit pre-fills) | Enter=save, PF3=cancel |
| Service groups | rows: CMD, group name, `X` access marker | `A`=add, `R`=remove |

**Navigation conventions:**

- **PF3 = up one level** (form → list → admin menu → service menu). This is the ISPF
  idiom; the *service menu's* PF3=disconnect behavior is unchanged.
- **PA3 = straight back to the service menu** from anywhere in admin (symmetric with
  PA3-during-bridge).
- **PF7/PF8** page lists; paging state lives in the flow, not the screen builders.
- **Deletes confirm with one extra round-trip:** the first `D` re-renders the list with
  `ENTER=CONFIRM DELETE OF '<name>', PF3=CANCEL` on the message line; the delete executes
  only when re-submitted. Cheap insurance on a destructive line command.

### 5. Validation, guardrails, semantics

**Validation** (in the flow; errors on the convention error line, prior input preserved
except passwords):

- Names (user/group/service) non-empty, trimmed.
- Passwords: entered twice, must match, non-empty; fields are non-display; never logged
  (house rule), hashed via `auth.HashPassword` before the store sees anything.
- Port: numeric, 1–65535. TLS/VERIFY: `Y` or `N`.
- **Duplicates:** `Create*` are INSERT OR IGNORE and would silently no-op, so the flow
  pre-checks existence (e.g. `GetUserByUsername`) and shows `'NAME' ALREADY EXISTS`.
  Rename via `UpdateService` pre-checks the new name the same way. (Single-admin tool —
  TOCTOU is acceptable.)

**Guardrails** (in the flow):

| Attempt | Result |
|---|---|
| Delete own account | `CANNOT DELETE YOUR OWN ACCOUNT` |
| Remove last ZZADMIN member (`CountGroupMembers == 1`) | `CANNOT REMOVE LAST ZZADMIN MEMBER` |
| Create or delete a `ZZ*` group (case-insensitive prefix) | `ZZ* GROUP NAMES ARE RESERVED` |

Deleting a *user* who happens to be the last ZZADMIN member is blocked by the same
last-member check (the flow checks memberships before user deletion).

**Live-effect semantics (v1, documented, accepted):** changes apply at the next menu
render or login. An already-connected user keeps their session after being deleted or
demoted until they disconnect; deleting a service does not kill an in-flight bridge.

## Data flow

```
Session.Run menu state
  admin := identity in ZZADMIN
  Presenter.Menu(conn, services, admin, errMsg)
    → adminSel → adminFlow{AdminStore: s.Store, AdminPresenter, identity}.Run(ctx, conn)
        loop: AdminPresenter renders screen → action → flow validates/guards
              → AdminStore mutates/reads → re-render (or navigate)
        returns on PF3-from-admin-menu or PA3 anywhere → back to service menu loop
```

## Error handling

- Store/DB errors → generic `TEMPORARY ERROR; TRY AGAIN` on the error line, details to
  `log` (never credentials), screen re-rendered. The flow never crashes the session; the
  per-connection panic recovery in `Server.handle` remains the backstop.
- Presenter I/O errors (client gone) → flow returns; `Session.Run` ends as today.

## Testing (TDD, package by package)

- **store:** table tests per new method; cascade-delete assertions (memberships/links
  gone, other rows intact); `migrate()` creates ZZADMIN once (idempotent across calls);
  `-race` as always.
- **auth:** `HashPassword` round-trips through `Authenticate`.
- **seed:** behavior unchanged after switching to `auth.HashPassword`.
- **screens:** field names/content assertions per screen (not row numbers), mirroring
  existing screen tests.
- **server/adminFlow:** fakes for both seams. Cover: navigation (PF3 levels, PA3 bailout,
  PF7/PF8 paging incl. boundaries), every CRUD path, delete-confirm round-trip, all three
  guardrails, duplicate pre-checks, validation failures preserve input, DB-error rendering.
- **server/session:** menu shows `A` only for ZZADMIN members; `adminSel` dispatches to
  the flow (fake); non-admin selecting `A` is rejected.

### Manual smoke test (protocol surface)

Per `CLAUDE.md`, screens/cursor/PA3 are only truly verified live. With `c3270`: log in as
a ZZADMIN member → `A` appears → enter admin → walk every screen (cursor lands on the
primary field, error line and PF help sit on the conventional rows, non-display password
fields don't echo) → add/edit/delete each entity type → PF3 backs out level by level →
PA3 jumps to the service menu → a freshly added service appears on the menu without
re-login. Also verify a non-admin sees no `A` and cannot select it.

## Documentation updates

- `seed.example.json`: add ZZADMIN and an admin member (bootstrap path).
- `CLAUDE.md`: architecture map (admin flow), conventions (ZZ namespace, AdminGroup),
  update the "Admin via 3270" row in the future-hooks table.
- `docs/superpowers/ROADMAP.md`: mark #3 done when shipped.

## Out of scope (deferred)

- Renaming users or groups (only services are editable in place).
- Configurable admin group name / escape key (ZZADMIN and PA3 stay constants — same
  "reserved hook" treatment as `EscapeAID`).
- Forcing live sessions to re-evaluate permissions on change.
- Audit trail of admin actions (roadmap #5 will cover the session side; admin-action
  audit can ride on the same `Auditor` seam later).
- MOD 3/4/5 geometry for admin screens (roadmap #7 parameterizes all screens together).
- CLI or HTTP admin surfaces.
