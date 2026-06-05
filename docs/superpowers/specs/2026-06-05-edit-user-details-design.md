# Edit User Details — Design (GH #46)

**Status:** approved design, pending implementation
**Issue:** #46

## Goal

Grow the user record with two foundational fields — `full_name` (display) and
`email` (reserved for future password recovery / email-MFA codes) — and replace
the single-purpose `S = set password` line command with a full **Edit User
Details** form. The same form is reused for *create*, so an admin can populate
name/email at account creation. No behavior hangs off the new columns yet; they
are reserved.

This advances the north star (day-to-day admin functions get a 3270 UI) and is
the screen the later optional-MFA admin controls will attach to (separate issue).

## Decisions settled

- **Email**: stored **lowercased** (trim + fold). Validation is **deliberately
  light** ("looks like an address") and lives in a single function so it can be
  loosened later for legacy / pre-SMTP forms (e.g. bang-paths) without a hunt.
- **Create flow**: the Edit User Details form is **reused for create**. Name and
  email are *optional* there but available to populate at creation.
- **Username**: the canonical-uppercase identity key. Editable + required on
  create; **display-only** on edit. Rename = delete+recreate, out of scope.
- **Password retype**: **kept** as a confirmation. On create the password is
  required; on edit a blank password means "keep current" and retype is only
  checked when a new password is typed.
- **Line command**: stays **`S`**, relabeled "edit user".
- **Audit**: a **single** `user edit X` record on any successful edit save —
  password, name, and email changes all carry equal weight. Create emits
  `user create X` as today.

## Store layer (`internal/store`)

### Schema
Add to the `users` CREATE TABLE:
- `full_name TEXT NOT NULL DEFAULT ''`
- `email     TEXT NOT NULL DEFAULT ''`

Add two `ensureColumn` calls in `migrate()` for DBs predating these columns,
mirroring the existing `services.tls_verify` / `services.description` precedent
(`CREATE TABLE IF NOT EXISTS` won't add columns to an existing table).

### `User` struct
Add `FullName string` and `Email string`. Update the full-user read sites to
select and scan the new columns:
- `GetUserByUsername` (store.go) — SELECT + Scan
- `queryUsers` (admin.go), used by `ListUsers` and `ListUsersInGroup` — SELECT + Scan

`auth.Authenticate` only reads `PasswordHash`, so it is unaffected.

### Validation (new, single choke point in `store`)
- `ValidateFullName(s string) error` — empty OK; reject when `len(s) >
  MaxDescriptionLen` (40 bytes). New validator (not `ValidateDescription`)
  because empty must be allowed.
- `NormalizeEmail(s string) string` — `strings.ToLower(strings.TrimSpace(s))`.
- `ValidateEmail(s string) error` — empty OK; otherwise minimal shape check:
  exactly one `@`, non-empty local part, domain non-empty and containing a `.`,
  no spaces. **Commented as deliberately permissive**, expected to loosen for
  legacy/pre-SMTP addresses later.

### Update method
`UpdateUserDetails(ctx, id int64, fullName, email string) error`:
1. `ValidateFullName(fullName)`, then `ValidateEmail(email)`.
2. `email = NormalizeEmail(email)`.
3. `UPDATE users SET full_name = ?, email = ? WHERE id = ?` via
   `execExpectingRow` → `ErrNotFound` on a missing id.

Password remains on the existing `SetPassword` path. `CreateUser` keeps its
current signature; the create flow composes `CreateUser` + `UpdateUserDetails`
(the flow pre-checks existence, so `INSERT OR IGNORE` idempotency is safe).

## Form driver (`internal/ui3270`)

Add `FormField.ReadOnly bool`. In `buildFormScreen`:
- A ReadOnly field renders as label + **static content** (no `Write` field).
- The initial cursor lands on the **first editable** field (skips ReadOnly).

This is the new capability the display-only Username needs on the edit form. The
existing forms (all-editable) are unaffected — `ReadOnly` defaults false.

## Server flow (`internal/server/admin_users.go`)

Collapse `userAdd` and `setPassword` into one `userEdit(ctx, r, u *store.User)`:
- `u == nil` → **create** mode.
- `u != nil` → **edit** mode.

Fields (in order):
1. **Username** — ReadOnly + prefilled on edit; editable + required on create.
2. **Full name** — prefilled on edit; optional.
3. **Email** — prefilled on edit; optional.
4. **New password** — blank on render.
5. **Retype** — blank on render.

Password helper gains a `required` mode:
- Create: password required; must match retype.
- Edit: blank password ⇒ keep current (retype ignored); non-blank ⇒ must match
  retype, then `auth.HashPassword` + `SetPassword`.

Submit logic:
- **Create**: require username; pre-check it doesn't already exist; validate
  name/email (surfaces the store validator messages); require + confirm
  password; `HashPassword`; `CreateUser`; `UpdateUserDetails`; audit
  `user create X`.
- **Edit**: validate name/email; if a new password was typed, confirm + hash +
  `SetPassword`; always `UpdateUserDetails`; audit `user edit X`.

The user list relabels the `S` command to "edit user"; `PF4 = add user` and the
`S` command both route into `userEdit`. PA3 does nothing; PF3 returns to the
user list; Enter saves.

## Tests (TDD)

- **store**: full_name/email round-trip (write then read back via
  `GetUserByUsername` / `ListUsers`); `ValidateFullName` (empty OK, >40 reject);
  `ValidateEmail` (accept `a@b.c` and empty; reject no-`@`, spaces, missing
  domain dot); `NormalizeEmail` lowercases + trims; `UpdateUserDetails` happy
  path + `ErrNotFound`; `ensureColumn` adds the columns to a legacy DB.
- **ui3270**: a ReadOnly field renders static content (no input) and the cursor
  homes to the first editable field.
- **server**: create-with-details; edit with blank password keeps the hash;
  edit with a new password changes it; username is display-only on edit;
  validation error messages surface; audit emits `user create X` /
  `user edit X` (scripted fake renderer, per `admin_test.go`).
- **smoke**: extend the s3270 smoke script to cover the Edit User Details screen
  (fields present, cursor on the first editable field) — protocol-facing work is
  only truly verified against a real emulator.

## Out of scope

- MFA controls (a later issue adds them to this same form).
- Any actual email behavior (recovery, email codes) — the column is reserved.
- Username rename.
