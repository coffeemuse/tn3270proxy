# Admin / Menu UX Refinements — Design (Roadmap #3.5)

**Date:** 2026-06-03
**Status:** Approved
**Source:** UX issues surfaced by the #3 (admin UI) live smoke test, 2026-06-03.

Three small changes in the session / adminFlow / screens layer. One new store query;
no schema migration. Lands before #5 (audit logging) so audit captures the final
login/logoff/disconnect event shapes.

## 1. Group → members view (`M` line command)

From the admin GROUPS list, the line command `M` on a group opens a members screen for
that group. Membership becomes manageable from either side (user → groups, group →
members).

**Screen shape — toggle list**, an exact mirror of the existing `userGroups` screen:
all users listed with an `X` marker in a `MEMBER` column; line command `A` adds the
user to the group, `R` removes; PF7/PF8 page; PF3 returns to the GROUPS list. Built
with the existing `AdminListScreen` — no new screen builder.

```
TN3270 GATEWAY ADMIN: MEMBERS OF OPS
                                  ROW 1 TO 4 OF 4
CMD  USERNAME         MEMBER
_    ALICE            X
_    BOB
_    CAROL            X
_    DAVE

A = add to group   R = remove from group

Enter = process   PF7/PF8 = page   PF3 = back
```

- GROUPS list legend becomes `M = members   D = delete   PF4 = add group`.
- `R` on ZZADMIN goes through the existing `guardLastAdmin` (same rule as the
  `userGroups` screen). `A`/`R` are otherwise plain
  `AddUserToGroup`/`RemoveUserFromGroup`.
- **Store:** one new method, `ListUsersInGroup(ctx, groupID) ([]store.User, error)`
  (single JOIN query, ordered by username like `ListUsers`), added to the
  `AdminStore` interface. One query for the marker
  set instead of N per-row `GetUserGroups` calls; SQL stays in `store`.

**Files:** `internal/server/admin_groups.go` (new `groupMembers` method + `M`
dispatch + legend), `internal/server/admin.go` (`AdminStore` interface),
`internal/store` (query + test).

## 2. Drop PA3 from the admin screens

PA3's product meaning is "escape the remote host" (bridge escape). It no longer does
anything inside the admin screens; navigation is PF3 walking up one level at a time.
PA3 stays bridge-only.

- Remove `AIDPA3` from `adminListExitKeys`, the AdminMenu exit keys, and the
  AdminForm exit keys (`presenter_admin.go`). A PA3 press inside admin is simply not
  an active key — go3270 re-presents the screen.
- Delete the `PA3` field from `AdminListAction` and `AdminFormAction`; drop `exit`
  from `AdminMenu`'s returns.
- Remove the `bail bool` plumbing: every `adminFlow` method changes from
  `(bool, error)` to plain `error`. Pure deletion — no new behavior.
- Help-line texts lose the PA3 mention:
  - list screens: `Enter = process   PF7/PF8 = page   PF3 = admin menu` (or
    `PF3 = back` on sub-lists);
  - form screens (`screens/admin.go`): `Enter = save    PF3 = cancel`;
  - admin menu (`screens/admin.go`): `Enter = select    PF3 = main menu`.

**Files:** `internal/server/presenter_admin.go`, `internal/server/admin.go`,
`internal/server/admin_users.go`, `internal/server/admin_groups.go`,
`internal/server/admin_services.go`, `internal/screens/admin.go`, plus their tests.

## 3. Layered PF3 logout

PF3 is uniformly "back to the previous level" across the whole UI (decided
2026-06-03): admin sub-screen → admin menu → service menu → login screen →
disconnect.

- **Service menu PF3 → logoff** (back to the login screen), not disconnect.
  `Session.Run` gains an outer loop: menu-quit re-enters `doLogin` instead of
  returning. Disconnecting from the menu becomes two PF3 presses — standard
  mainframe layering.
- **Login screen PF3 → disconnect** (unchanged).
- **Admin menu PF3 → service menu** (unchanged from today).
- No logoff notice: the login screen re-renders pristine (decided during
  brainstorm).
- `isAdmin` and the service list are recomputed on each re-login, so a demoted
  admin loses the `A` entry at logoff — partially addresses the "admin changes
  apply at next login" caveat.
- Bridge causes unchanged: `CauseClientClosed` still ends the session immediately.
- Menu help text: `Enter = connect    PF3 = logoff    (PA3 returns here from a
  session)`. Login help text unchanged.

**Files:** `internal/server/session.go`, `internal/screens/menu.go`, plus tests.

## Error handling

Unchanged patterns: presenter I/O errors end the session; store errors log details
and show the generic `TEMPORARY ERROR; TRY AGAIN` on the error line.

## Testing (TDD, per convention)

- **store:** `ListUsersInGroup` round-trip test (members only, ordering, empty
  group).
- **adminFlow** (fake presenter): members list renders `X` markers; `A`/`R` mutate
  membership; `R` on the last ZZADMIN member is blocked; PF3 returns to the GROUPS
  list. Existing tests updated for the `(bool, error)` → `error` ripple; PA3-bail
  tests deleted.
- **Session** (fake presenter): menu-quit re-runs login instead of ending the
  session; login-quit still ends it; re-login recomputes admin status (a demoted
  admin loses `A`); the existing pin "adminSel from a non-admin never reaches the
  admin flow" still passes.
- **Manual smoke test** in a real emulator (c3270) before declaring done — keys,
  cursor positions, help-line texts are protocol surface (CLAUDE.md).
