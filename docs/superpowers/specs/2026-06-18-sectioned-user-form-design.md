# Sectioned form layout + Edit/Add User redesign

**Date:** 2026-06-18
**Issue:** #130 (CUA/ISPF UI refinement)
**Status:** Design — approved, pending spec review

## Problem

The admin **Edit User** / **Add User** screens are rendered by the shared
`internal/ui3270` form engine, which lays fields out as a flat, double-spaced
list (`row = 3 + 2*i`) with truncated labels (`Full name .`, `Lock self Y.`)
and no visual grouping. The result is hard to scan and the abbreviated labels
read poorly. The MFA/policy/action controls are visually indistinguishable from
the identity and password fields.

We want a sectioned, single-spaced layout with full dot-leader labels, inline
hints, and grouped controls — matching the approved mockup below — without
regressing the other forms (Group, Service, sysparms) that share the engine.

## Approved layout (Edit User, MOD2, edit mode)

Brackets denote unprotected-field boundaries only; the engine renders inputs as
underscored runs (attribute byte + `go3270.Underscore`), never literal brackets.

```
 0                  TN3270 GATEWAY ADMIN: EDIT USER ADMIN
 2   User ID . . . . . . : ADMIN            (standalone read-only — the key)
 4  --- Identity ----------------------------------------------------------
 5   Full name . . . . .  [____________________________________]
 6   Email . . . . . . .  [____________________________________]
 8  --- Authentication ----------------------------------------------------
 9   New password  . . .  [________________]  (blank = no change)
10   Confirm password  .  [________________]
11   MFA required  . . .  [_] Y/N      MFA status . : ENROLLED   (same row)
13  --- Account policy ----------------------------------------------------
14   Block self-service   [_] Y/N  (blocks user-initiated changes)
16  --- Account actions ---------------------------------------------------
17   Clear MFA . . . . .  [_] Y/N
18     Confirm: type CLEAR [_______]
22   <red error / message line>
23  Enter=Save    PF3=Cancel
```

**Add User** uses the same engine. User ID becomes an editable standalone top
field; only the sections whose fields exist render — in create mode that is
**Identity** (User ID, Full name, Email) and **Authentication** (password,
confirm). MFA / Account policy / Account actions are edit-only, matching current
behavior.

## Decisions (locked)

- **Footer:** `Enter=Save    PF3=Cancel` only. No F1=Help, no F12=Cancel — the
  app-wide "PF3 = step back one level" contract is preserved.
- **MFA status vocabulary:** keep `NONE / PENDING / ENROLLED` (preserves the
  "required but not yet enrolled" = PENDING distinction).
- **Clear-MFA guardrail:** keep BOTH the `Y/N` toggle and the typed-`CLEAR`
  confirm. Both must agree to wipe.
- **User ID placement:** standalone, above the Identity banner, in both modes
  (it is the primary key).
- **Compact error line:** relocated to the bottom (row `helpRow-1`, i.e. 22 on
  MOD2), since row 2 now holds the User ID.

## Engine additions (`internal/ui3270`)

All additions are opt-in; zero-valued fields/flags reproduce today's rendering
exactly, so the Group/Service/sysparms forms are unaffected until they migrate.

### `FormField` (types.go) gains:

- `Section string` — when non-empty, emit a section banner
  (`--- <Section> ----…----` to the right margin) preceded by a blank gutter row
  (suppressed when the banner would be the first content row), immediately above
  this field. Marks a section's first field.
- `Suffix string` — static dim text rendered after the input's stop field
  (e.g. `Y/N`, `(blank = no change)`, `(blocks user-initiated changes)`).
- `SameRow bool` — render this field on the same row as the previous field, at a
  fixed second column that clears the previous field's input + suffix. Used for
  `MFA status` beside `MFA required`.

### `FormConfig` / `FormView` gains:

- `Compact bool` — switches the row engine from legacy double-spacing
  (`3 + 2*i`, no sections, row-2 message line) to single-spacing with section
  banners, gutter rows, suffixes, same-row pairing, and the bottom message line.
  **Default false = byte-identical to today.**

### `buildFormScreen` (screen.go) — compact path

When `Compact` is true:

1. Title at row 0 (unchanged).
2. Walk fields maintaining a running content row starting at row 2.
   - If `Section` is set: emit a blank gutter row (unless this is the first
     content) then a banner row, advancing the running row.
   - Place the field's label (dot-leader, indented under the banner) + input
     (or static value when `ReadOnly`) on the next row.
   - If `SameRow`: place this field on the SAME row as the previous field, label
     + value/input starting at the fixed second column.
   - If `Suffix`: render it as static dim text two columns past the input's stop
     field.
3. Message line (red, `fieldError`) at `helpRow(rows) - 1`.
4. Help line `Enter=Save    PF3=Cancel` at `helpRow(rows)`.

Cursor lands on the first writable field (existing "first writable" logic — in
edit mode that is Full name, since User ID is read-only).

## Form field set (admin_users.go `userEdit`)

The field slice gains `Section` / `Suffix` / `SameRow` annotations and a new
typed-CLEAR confirm field. Full labels replace the truncated ones:

| Field (Name)              | Label                | Section          | Suffix                          | Notes |
|---------------------------|----------------------|------------------|---------------------------------|-------|
| FieldUsername             | User ID              | — (standalone)   | —                               | ReadOnly in edit, writable in create |
| FieldFullName             | Full name            | Identity         | —                               | |
| FieldEmail                | Email                | (Identity)       | —                               | |
| FieldPassword             | New password         | Authentication   | `(blank = no change)`           | Hidden |
| FieldRetype               | Confirm password     | (Authentication) | —                               | Hidden |
| FieldMFARequired          | MFA required         | (Authentication) | `Y/N`                           | edit-only |
| FieldMFAStatus            | MFA status           | —                | —                               | ReadOnly, `SameRow` beside MFA required |
| FieldUserSettingsLocked   | Block self-service   | Account policy   | `(blocks user-initiated changes)`| edit-only; renamed label |
| FieldMFAClear             | Clear MFA            | Account actions  | `Y/N`                           | edit-only |
| FieldMFAClearConfirm (new)| Confirm: type CLEAR  | (Account actions)| —                               | edit-only; new field |

The existing edit-only `lockhint` note ("MFA REQ INERT WHILE LOCKED W/O SECRET")
is retained as a read-only field when its condition holds.

## Clear-MFA confirm wiring (admin_users.go `applyMFAEdit`)

Add a guard before the existing clear branch: if `FieldMFAClear` is `Y` but the
trimmed `FieldMFAClearConfirm` value ≠ `CLEAR` (case-insensitive), return
`TYPE CLEAR TO CONFIRM MFA WIPE` and do not clear. Both controls must agree.
Behavior is otherwise unchanged (wipe secret + audit `AuditMFACleared`).

## Testing

TDD, per repo convention (assert field names/content/color, not raw row
numbers):

- `buildFormScreen` compact-path unit tests: section banner emission + gutter
  suppression for the first section; suffix placement; `SameRow` second-column
  placement; bottom message-row relocation; cursor on first writable field.
- Backward-compat test: a non-compact form renders byte-identically to today.
- `applyMFAEdit` test: CLEAR toggle without `CLEAR` typed → rejected, secret
  retained; toggle + `CLEAR` → wiped + audited.
- s3270 smoke test: cursor lands on Full name; live field geometry of the
  sectioned layout; Clear-MFA happy/blocked paths.

## Out of scope

- Migrating Group / Service / sysparms forms to compact/sectioned layout (they
  keep current rendering; can adopt later).
- Per-panel help (`HELP-EDITUSER`) and F1 on this panel.
- F12=Cancel / dirty-discard semantics.
