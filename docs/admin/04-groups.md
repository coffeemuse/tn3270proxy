# Managing groups

Admin menu option **2**. The list shows each group with its member and
service counts.

Line commands: **M** = manage members, **D** = delete (confirm-gated).
**PF4** adds a group.

## Adding a group

One field: the group name (stored uppercase, must be unique). Names starting
with `ZZ` are rejected — `ZZ* GROUP NAMES ARE RESERVED` (see
[Concepts](01-concepts.md)).

## Members (`M`)

Shows every user with an `X` next to current members. **A** = add the user,
**R** = remove. This is the same membership data you can edit from the user
side ([Users](03-users.md), `G` command) — use whichever direction is more
convenient.

ZZADMIN guardrails: you cannot remove yourself from ZZADMIN, and you cannot
remove its last member.

## Deleting a group

`D`, then Enter to confirm. `ZZ*` groups cannot be deleted. Deleting a group
removes its memberships and its service grants; users who relied on it lose
those menu entries at their next menu render or login.

## Granting services to a group

Service access is managed from the **service** side: open
[Services](05-services.md), line command `G` on a service, and grant or
revoke groups there.
