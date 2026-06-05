# Edit User Details Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `full_name` + `email` to the user record and replace the `S = set password` line command with one **Edit User Details** form reused for both create and edit.

**Architecture:** Two new nullable-defaulting columns on `users`; new store validators/normalizer and an `UpdateUserDetails` method; a new `FormField.ReadOnly` capability in the `ui3270` form driver (for the display-only username on edit); and a single `userEdit` server flow replacing the separate `userAdd`/`setPassword` flows.

**Tech Stack:** Go, `modernc.org/sqlite` (pure-Go), `github.com/racingmars/go3270`, bcrypt via `internal/auth`.

Spec: `docs/superpowers/specs/2026-06-05-edit-user-details-design.md`

---

## File Structure

- **Modify** `internal/store/store.go` — `users` schema (+`full_name`,+`email`), `migrate()` `ensureColumn` calls, `User` struct, `GetUserByUsername` SELECT+Scan, `ValidateFullName`, `NormalizeEmail`, `ValidateEmail`.
- **Modify** `internal/store/admin.go` — `queryUsers` SELECT+Scan, new `UpdateUserDetails`.
- **Modify** `internal/store/users_test.go` / `internal/store/admin_test.go` — new store tests.
- **Modify** `internal/ui3270/types.go` — `FormField.ReadOnly` field.
- **Modify** `internal/ui3270/screen.go` — `buildFormScreen` renders ReadOnly fields as static content; cursor skips to first editable field.
- **Modify** `internal/ui3270/screen_test.go` — ReadOnly rendering + cursor tests.
- **Modify** `internal/screens/admin.go` — `FieldFullName`, `FieldEmail` constants.
- **Modify** `internal/server/admin_users.go` — collapse `userAdd`+`setPassword` into `userEdit`; relabel `S`; password helper gains `required` mode.
- **Modify** `internal/server/admin_test.go` — update existing add/set-password tests; add edit-details tests.
- **Modify** `.claude/skills/s3270-smoke-testing/smoke.sh` — cover the Edit User Details screen.

---

## Task 1: Store — add `full_name`/`email` columns and read them back

**Files:**
- Modify: `internal/store/store.go` (schema ~63-67, `migrate()` ~127-130, `User` ~182-187, `GetUserByUsername` ~218-231)
- Modify: `internal/store/admin.go` (`queryUsers` ~42-69)
- Test: `internal/store/users_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/store/users_test.go`:

```go
func TestUserDetailsDefaultEmpty(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if _, err := st.CreateUser(ctx, "alice", "hash-a"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.FullName != "" || u.Email != "" {
		t.Errorf("new user details = %q/%q, want empty/empty", u.FullName, u.Email)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestUserDetailsDefaultEmpty`
Expected: FAIL — `u.FullName`/`u.Email` undefined (compile error).

- [ ] **Step 3: Implement the schema + struct + scans**

In `internal/store/store.go`, change the `users` table in `const schema` to:

```go
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY,
	username      TEXT UNIQUE COLLATE NOCASE NOT NULL,
	password_hash TEXT NOT NULL,
	full_name     TEXT NOT NULL DEFAULT '',
	email         TEXT NOT NULL DEFAULT ''
);
```

In `migrate()`, after the existing `services` `ensureColumn` calls and before the `AdminGroup` insert, add (existing DBs predating these columns):

```go
	if err := s.ensureColumn("users", "full_name",
		"ALTER TABLE users ADD COLUMN full_name TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "email",
		"ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
```

Change the `User` struct to:

```go
// User is an account record.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	FullName     string
	Email        string
}
```

Update `GetUserByUsername` SELECT + Scan:

```go
	err := s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash, full_name, email FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Email)
```

In `internal/store/admin.go`, update both `ListUsers` and `ListUsersInGroup` SELECTs and the `queryUsers` Scan:

```go
// ListUsers returns all users ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	return s.queryUsers(ctx,
		"SELECT id, username, password_hash, full_name, email FROM users ORDER BY username")
}

// ListUsersInGroup returns the group's members ordered by username.
func (s *Store) ListUsersInGroup(ctx context.Context, groupID int64) ([]User, error) {
	return s.queryUsers(ctx,
		`SELECT u.id, u.username, u.password_hash, u.full_name, u.email FROM users u
		 JOIN user_groups ug ON ug.user_id = u.id
		 WHERE ug.group_id = ? ORDER BY u.username`, groupID)
}
```

```go
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Email); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestUserDetailsDefaultEmpty`
Expected: PASS.

- [ ] **Step 5: Verify nothing else broke**

Run: `go build ./... && go test ./internal/store/ ./internal/auth/`
Expected: PASS (auth reads only `PasswordHash`; extra columns are ignored).

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/admin.go internal/store/users_test.go
git commit -m "feat(store): add full_name/email columns to user record (GH #46)"
```

---

## Task 2: Store — `ValidateFullName`

**Files:**
- Modify: `internal/store/store.go` (near `ValidateDescription` ~307-316)
- Test: `internal/store/users_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestValidateFullName(t *testing.T) {
	if err := ValidateFullName(""); err != nil {
		t.Errorf("empty full name should be allowed: %v", err)
	}
	if err := ValidateFullName("Robert Lawrence"); err != nil {
		t.Errorf("normal full name rejected: %v", err)
	}
	long := strings.Repeat("a", MaxDescriptionLen+1)
	if err := ValidateFullName(long); err == nil {
		t.Errorf("over-length full name should be rejected")
	}
}
```

Add `"strings"` to the test file's imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestValidateFullName`
Expected: FAIL — `ValidateFullName` undefined.

- [ ] **Step 3: Implement**

In `internal/store/store.go`, after `ValidateDescription`:

```go
// ValidateFullName checks the optional display name: empty is allowed; a
// non-empty value reuses the description length cap (MaxDescriptionLen bytes).
func ValidateFullName(name string) error {
	if len(name) > MaxDescriptionLen {
		return errors.New("full name must be 40 characters or fewer")
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestValidateFullName`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/users_test.go
git commit -m "feat(store): ValidateFullName (optional, 40-char cap) (GH #46)"
```

---

## Task 3: Store — `NormalizeEmail` + `ValidateEmail`

**Files:**
- Modify: `internal/store/store.go` (after `ValidateFullName`)
- Test: `internal/store/users_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Bob@Example.COM "); got != "bob@example.com" {
		t.Errorf("NormalizeEmail = %q, want bob@example.com", got)
	}
	if got := NormalizeEmail(""); got != "" {
		t.Errorf("NormalizeEmail(empty) = %q, want empty", got)
	}
}

func TestValidateEmail(t *testing.T) {
	ok := []string{"", "a@b.co", "robert@example.com"}
	for _, e := range ok {
		if err := ValidateEmail(e); err != nil {
			t.Errorf("ValidateEmail(%q) rejected: %v", e, err)
		}
	}
	bad := []string{"no-at", "a@b", "a b@c.com", "@b.com", "a@", "a@@b.com"}
	for _, e := range bad {
		if err := ValidateEmail(e); err == nil {
			t.Errorf("ValidateEmail(%q) should be rejected", e)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestNormalizeEmail|TestValidateEmail'`
Expected: FAIL — functions undefined.

- [ ] **Step 3: Implement**

In `internal/store/store.go`, after `ValidateFullName`:

```go
// NormalizeEmail trims surrounding space and lower-cases the address, so the
// column is a clean key for future email lookups. Empty in, empty out.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail does a deliberately permissive "looks like an address" check:
// empty is allowed (the field is optional); otherwise exactly one "@", a
// non-empty local part, a domain that is non-empty and contains a ".", and no
// spaces. This is intentionally loose and is expected to loosen further later
// for legacy / pre-SMTP address forms — keep the rule in this one function.
func ValidateEmail(email string) error {
	if email == "" {
		return nil
	}
	if strings.ContainsAny(email, " \t") {
		return errors.New("email must not contain spaces")
	}
	local, domain, found := strings.Cut(email, "@")
	if !found || strings.Contains(domain, "@") {
		return errors.New("email must contain exactly one @")
	}
	if local == "" || domain == "" {
		return errors.New("email must have text before and after the @")
	}
	if !strings.Contains(domain, ".") {
		return errors.New("email domain must contain a .")
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestNormalizeEmail|TestValidateEmail'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/users_test.go
git commit -m "feat(store): NormalizeEmail + light ValidateEmail (GH #46)"
```

---

## Task 4: Store — `UpdateUserDetails`

**Files:**
- Modify: `internal/store/admin.go` (near `SetPassword` ~160-163)
- Test: `internal/store/admin_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/store/admin_test.go`:

```go
func TestUpdateUserDetails(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	uid, err := st.CreateUser(ctx, "alice", "h")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.UpdateUserDetails(ctx, uid, "Alice Doe", "  Alice@Example.COM "); err != nil {
		t.Fatalf("UpdateUserDetails: %v", err)
	}
	u, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.FullName != "Alice Doe" || u.Email != "alice@example.com" {
		t.Errorf("details = %q/%q, want 'Alice Doe'/'alice@example.com'", u.FullName, u.Email)
	}
	if err := st.UpdateUserDetails(ctx, 99999, "X", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing id: err = %v, want ErrNotFound", err)
	}
	if err := st.UpdateUserDetails(ctx, uid, "X", "bogus"); err == nil {
		t.Errorf("invalid email should be rejected")
	}
}
```

Ensure `internal/store/admin_test.go` imports `"errors"` (add if missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestUpdateUserDetails`
Expected: FAIL — `UpdateUserDetails` undefined.

- [ ] **Step 3: Implement**

In `internal/store/admin.go`, after `SetPassword`:

```go
// UpdateUserDetails replaces the user's optional display name and email.
// Email is normalized (trim + lower-case). Returns ErrNotFound for an unknown
// user id. The password is updated separately via SetPassword.
func (s *Store) UpdateUserDetails(ctx context.Context, userID int64, fullName, email string) error {
	if err := ValidateFullName(fullName); err != nil {
		return err
	}
	// Normalize BEFORE validating: ValidateEmail rejects surrounding spaces, but
	// callers (and the test) may pass an untrimmed value that is valid once
	// trimmed/lower-cased.
	email = NormalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return err
	}
	return s.execExpectingRow(ctx,
		"UPDATE users SET full_name = ?, email = ? WHERE id = ?",
		fullName, email, userID)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestUpdateUserDetails`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/admin.go internal/store/admin_test.go
git commit -m "feat(store): UpdateUserDetails(id, fullName, email) (GH #46)"
```

---

## Task 5: ui3270 — `FormField.ReadOnly` (display-only fields)

**Files:**
- Modify: `internal/ui3270/types.go` (`FormField` ~35-39)
- Modify: `internal/ui3270/screen.go` (`buildFormScreen` ~69-101)
- Test: `internal/ui3270/screen_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/ui3270/screen_test.go`:

```go
func TestBuildFormScreenReadOnlyField(t *testing.T) {
	screen, cur := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "username", Label: "User", Value: "RJLAWREN", ReadOnly: true},
		{Name: "fullname", Label: "Name", Length: 40},
	}})
	// Cursor skips the read-only field and homes to the first editable input.
	// Editable field is the 2nd row: attribute byte at (5,16), input one col right.
	if cur != (Cursor{Row: 5, Col: 17}) {
		t.Errorf("cursor = %+v, want {5,17}", cur)
	}
	// The read-only value renders as static content with no writable input field.
	var sawStatic, sawWritableUsername bool
	for _, f := range screen {
		if f.Content == "RJLAWREN" && !f.Write {
			sawStatic = true
		}
		if f.Name == "username" && f.Write {
			sawWritableUsername = true
		}
	}
	if !sawStatic {
		t.Errorf("read-only value not rendered as static content")
	}
	if sawWritableUsername {
		t.Errorf("read-only field must not be a writable input")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui3270/ -run TestBuildFormScreenReadOnlyField`
Expected: FAIL — `ReadOnly` undefined (compile error).

- [ ] **Step 3: Add the field to `FormField`**

In `internal/ui3270/types.go`:

```go
// FormField is one labeled input on a form.
type FormField struct {
	Name, Label, Value string
	Hidden             bool // non-display (passwords)
	ReadOnly           bool // display-only: rendered as static content, never an input
	Length             int  // input columns; effective max 62 (stop field clamps at col 79)
}
```

- [ ] **Step 4: Render ReadOnly as static + skip cursor**

In `internal/ui3270/screen.go`, replace the field loop in `buildFormScreen` (the `for i, f := range fields { ... }` block) with:

```go
	cur := Cursor{Row: 0, Col: 0}
	for i, f := range fields {
		row := 3 + 2*i
		if f.ReadOnly {
			// Display-only: label + static value, no writable input, never the
			// cursor target.
			screen = append(screen,
				go3270.Field{Row: row, Col: 2, Content: f.Label},
				go3270.Field{Row: row, Col: 16, Content: f.Value},
			)
			continue
		}
		stopCol := 17 + f.Length
		if stopCol > 79 {
			stopCol = 79
		}
		input := go3270.Field{Row: row, Col: 16, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Highlighting: go3270.Underscore}
		if cur == (Cursor{Row: 0, Col: 0}) {
			cur = Cursor{Row: input.Row, Col: input.Col + 1}
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Content: f.Label},
			input,
			go3270.Field{Row: row, Col: stopCol}, // stop field
		)
	}
```

Note: the cursor guard changes from `if i == 0` to `if cur == (Cursor{Row: 0, Col: 0})` so the first *editable* field wins even when row 0 is read-only.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui3270/`
Expected: PASS (new test + existing `TestBuildFormScreenCursor` still `{3,17}` since its single field is editable).

- [ ] **Step 6: Commit**

```bash
git add internal/ui3270/types.go internal/ui3270/screen.go internal/ui3270/screen_test.go
git commit -m "feat(ui3270): FormField.ReadOnly display-only fields (GH #46)"
```

---

## Task 6: screens — `FieldFullName`/`FieldEmail` constants

**Files:**
- Modify: `internal/screens/admin.go` (const block ~27-39)

- [ ] **Step 1: Add the constants**

In `internal/screens/admin.go`, add to the field-name `const` block:

```go
	FieldFullName    = "fullname"    // edit-user form: display name
	FieldEmail       = "email"       // edit-user form: email address
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./...`
Expected: PASS (constants unused yet is fine — they're exported).

- [ ] **Step 3: Commit**

```bash
git add internal/screens/admin.go
git commit -m "feat(screens): FieldFullName/FieldEmail constants (GH #46)"
```

---

## Task 7: server — unify create/edit into `userEdit`

This task replaces `userAdd` + `setPassword` with one `userEdit` and rewires the user list. The existing tests `TestAdminUserAddHappyPath`, `TestAdminUserAddDuplicatePreservesInput`, `TestAdminUserAddPasswordMismatch`, `TestAdminUserAddPasswordTooLong`, and `TestAdminSetPassword` exercise the old flow and are updated here.

**Files:**
- Modify: `internal/server/admin_users.go` (`users` ~37-63, `passwordFromForm` ~130-142, `userAdd` ~144-182, `setPassword` ~184-207)
- Modify: `internal/server/admin_test.go` (the five tests named above; add new edit tests)

- [ ] **Step 1: Write the failing edit test**

Add to `internal/server/admin_test.go`:

```go
func TestAdminEditUserDetails(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}}, // S on alice (row 0)
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldFullName: "Alice Doe",
			screens.FieldEmail:    "Alice@Example.COM",
			// password + retype blank → keep current
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	u, err := f.store.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	if u.FullName != "Alice Doe" || u.Email != "alice@example.com" {
		t.Errorf("details = %q/%q", u.FullName, u.Email)
	}
	if u.PasswordHash != "h" {
		t.Errorf("blank password should keep current hash, got %q", u.PasswordHash)
	}
}

func TestAdminEditUserChangesPassword(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldPassword: "newpw", screens.FieldRetype: "newpw",
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, f.store, "alice", "newpw"); err != nil {
		t.Errorf("authenticate with new password: %v", err)
	}
}

func TestAdminEditUserUsernameReadOnly(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		forms: []ui3270.FormAction{{Cancel: true}},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	form := p.gotForms[len(p.gotForms)-1]
	var uf *ui3270.FormField
	for i := range form.Fields {
		if form.Fields[i].Name == screens.FieldUsername {
			uf = &form.Fields[i]
		}
	}
	if uf == nil {
		t.Fatal("username field missing from edit form")
	}
	if !uf.ReadOnly {
		t.Errorf("username should be read-only on edit")
	}
	if uf.Value != "ALICE" {
		t.Errorf("username value = %q, want ALICE", uf.Value)
	}
}

func TestAdminEditUserInvalidEmail(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldEmail: "not-an-email"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if msg := p.gotForms[len(p.gotForms)-1].ErrMsg; msg == "" {
		t.Errorf("expected a validation error message, got empty")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run TestAdminEditUser`
Expected: FAIL — fields/flow not implemented (compile error or assertion failure).

- [ ] **Step 3: Rewrite `passwordFromForm` with a `required` mode**

In `internal/server/admin_users.go`, replace `passwordFromForm` with:

```go
// passwordFromForm validates the password/retype pair. When required is false
// (edit mode) a blank password means "keep current": it returns change=false
// and no error. A non-blank password must match its retype. Passwords are never
// trimmed, logged, or echoed.
func passwordFromForm(values map[string]string, required bool) (pass string, change bool, errMsg string) {
	pass, retype := values[screens.FieldPassword], values[screens.FieldRetype]
	if pass == "" {
		if required {
			return "", false, "PASSWORD IS REQUIRED"
		}
		return "", false, "" // keep current
	}
	if pass != retype {
		return "", false, "PASSWORDS DO NOT MATCH"
	}
	if errors.Is(auth.ValidatePassword(pass), auth.ErrPasswordTooLong) {
		return "", false, fmt.Sprintf("PASSWORD TOO LONG (MAX %d BYTES)", auth.MaxPasswordLen)
	}
	return pass, true, ""
}
```

- [ ] **Step 4: Replace `userAdd` and `setPassword` with `userEdit`**

In `internal/server/admin_users.go`, delete `userAdd` and `setPassword`, and add:

```go
// userEdit drives the unified Edit User Details form. u == nil → create mode
// (username editable + required, password required); u != nil → edit mode
// (username display-only, blank password keeps the current hash). Full name and
// email are optional in both modes.
func (f *adminFlow) userEdit(ctx context.Context, r ui3270.Renderer, u *store.User) error {
	create := u == nil
	title := "TN3270 GATEWAY ADMIN: ADD USER"
	username, fullName, email := "", "", ""
	if !create {
		title = "TN3270 GATEWAY ADMIN: EDIT USER " + u.Username
		username, fullName, email = u.Username, u.FullName, u.Email
	}
	// Rebuilt-by-reference so a rejected submit re-seeds typed input on the next
	// render (RunForm re-sends the same slice each loop).
	fields := []ui3270.FormField{
		{Name: screens.FieldUsername, Label: "Userid . . .", Length: 32, Value: username, ReadOnly: !create},
		{Name: screens.FieldFullName, Label: "Full name .", Length: 40, Value: fullName},
		{Name: screens.FieldEmail, Label: "Email  . . .", Length: 40, Value: email},
		{Name: screens.FieldPassword, Label: "Password . .", Hidden: true, Length: 32},
		{Name: screens.FieldRetype, Label: "Retype . . .", Hidden: true, Length: 32},
	}
	return ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title:  title,
		Fields: fields,
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			fullNameVal := vals[screens.FieldFullName]
			emailVal := vals[screens.FieldEmail]
			fields[1].Value = fullNameVal // preserve typed input on re-render
			fields[2].Value = emailVal
			if err := store.ValidateFullName(fullNameVal); err != nil {
				return "FULL NAME TOO LONG (MAX 40)", nil
			}
			if err := store.ValidateEmail(emailVal); err != nil {
				return "INVALID EMAIL ADDRESS", nil
			}
			if create {
				return f.userCreate(ctx, vals, fields, fullNameVal, emailVal)
			}
			return f.userSaveEdit(ctx, *u, vals, fullNameVal, emailVal)
		},
	})
}

// userCreate handles the create-mode submit: requires + confirms password,
// rejects duplicates, then creates the user and writes the optional details.
func (f *adminFlow) userCreate(ctx context.Context, vals map[string]string, fields []ui3270.FormField, fullName, email string) (string, error) {
	username := vals[screens.FieldUsername]
	fields[0].Value = username // preserve typed input on re-render
	if username == "" {
		return "USERID IS REQUIRED", nil
	}
	pass, _, msg := passwordFromForm(vals, true)
	if msg != "" {
		return msg, nil
	}
	if _, err := f.store.GetUserByUsername(ctx, username); err == nil {
		return "'" + username + "' ALREADY EXISTS", nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return logStoreErr("check user", err), nil
	}
	hash, err := auth.HashPassword(pass)
	if err != nil {
		return logStoreErr("hash password", err), nil
	}
	uid, err := f.store.CreateUser(ctx, username, hash)
	if err != nil {
		return logStoreErr("create user", err), nil
	}
	if err := f.store.UpdateUserDetails(ctx, uid, fullName, email); err != nil {
		return logStoreErr("set user details", err), nil
	}
	f.recordAdmin(ctx, "user create "+username)
	return "", nil
}

// userSaveEdit handles the edit-mode submit: optionally changes the password
// (blank = keep), always writes the details, and audits a single edit record.
func (f *adminFlow) userSaveEdit(ctx context.Context, u store.User, vals map[string]string, fullName, email string) (string, error) {
	pass, change, msg := passwordFromForm(vals, false)
	if msg != "" {
		return msg, nil
	}
	if change {
		hash, err := auth.HashPassword(pass)
		if err != nil {
			return logStoreErr("hash password", err), nil
		}
		if err := f.store.SetPassword(ctx, u.ID, hash); err != nil {
			return logStoreErr("set password", err), nil
		}
	}
	if err := f.store.UpdateUserDetails(ctx, u.ID, fullName, email); err != nil {
		return logStoreErr("set user details", err), nil
	}
	f.recordAdmin(ctx, "user edit "+u.Username)
	return "", nil
}
```

- [ ] **Step 5: Rewire the user list (`S` + `PF4`)**

In `internal/server/admin_users.go`, update the `users` `ListConfig` `Legend`, the `Add` hook, and the `S` command:

```go
		Legend: "S = edit user   G = groups   D = delete   PF4 = add user",
		PFHelp: "Enter = process   PF7/PF8 = page   PF3 = admin menu",
		Rows:   f.term.Rows,
		Fetch:  f.fetchUsers,
		Add:    func(ctx context.Context, r ui3270.Renderer) (string, error) { return "", f.userEdit(ctx, r, nil) },
		Cmds: []ui3270.Command[store.User]{
			{Key: 'S', Commit: func(ctx context.Context, r ui3270.Renderer, u store.User) (string, error) {
				return "", f.userEdit(ctx, r, &u)
			}},
			{Key: 'G', Commit: func(ctx context.Context, r ui3270.Renderer, u store.User) (string, error) {
				return "", f.userGroups(ctx, r, u)
			}},
			{Key: 'D',
				Confirm: func(u store.User) (string, string) {
					return fmt.Sprintf("ENTER = CONFIRM DELETE OF '%s', PF3 = CANCEL", u.Username), ""
				},
				Commit: func(ctx context.Context, _ ui3270.Renderer, u store.User) (string, error) {
					return f.deleteUser(ctx, u), nil
				}},
		},
```

- [ ] **Step 6: Update the existing add/set-password tests**

In `internal/server/admin_test.go`:

- `TestAdminUserAddHappyPath`, `TestAdminUserAddDuplicatePreservesInput`, `TestAdminUserAddPasswordMismatch`, `TestAdminUserAddPasswordTooLong`: no behavior change needed for the add path (PF4 still routes to create mode, username editable + required). They keep passing as written. Run them to confirm.
- `TestAdminSetPassword`: this exercised the old `S = set password`. The `S` command now opens the edit form; the test's form values (`FieldPassword`/`FieldRetype` only) still change the password in edit mode, so the assertion (authenticate with new password) still holds. Run to confirm.

No edits expected; this step is to **run and confirm** the migrated behavior:

Run: `go test ./internal/server/ -run 'TestAdminUserAdd|TestAdminSetPassword'`
Expected: PASS. If `TestAdminUserAddDuplicatePreservesInput` asserts `last.Fields[0].Value` — field 0 is still the username on the create form, so it holds.

- [ ] **Step 7: Run the full server + build**

Run: `go build ./... && go test ./internal/server/ -race`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/server/admin_users.go internal/server/admin_test.go
git commit -m "feat(server): unified Edit User Details form (create+edit) (GH #46)"
```

---

## Task 8: Smoke test — cover the Edit User Details screen

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`

- [ ] **Step 1: Locate the admin-users coverage in the smoke script**

Run: `grep -n "USERS\|set password\|edit user\|PF4\|admin" .claude/skills/s3270-smoke-testing/smoke.sh`
Expected: find where the script navigates the admin user list (if present). If the script does not yet cover admin user screens, add a new section after the admin-menu navigation.

- [ ] **Step 2: Add an Edit User Details assertion block**

In the admin-users portion of `.claude/skills/s3270-smoke-testing/smoke.sh`, after navigating into the user list, press `S` on a known seeded user and assert the screen:

```sh
# --- Edit User Details (GH #46) ---
# Enter 'S' line command on row 0, Enter to open the edit form.
expect_send "S\n"        # line command in the CMD column of the first user row
expect_send "Enter\n"
# The form shows the read-only username and the full-name / email labels.
assert_screen_contains "EDIT USER"
assert_screen_contains "Full name"
assert_screen_contains "Email"
# Cursor must land on the first EDITABLE field (Full name), not the read-only
# username. Full name is the 2nd form row (row 5, 0-based), input one col right
# of its attribute byte at col 16 → cursor col 17.
assert_cursor 5 17
# Leave via PF3 (no change).
expect_send "PF(3)\n"
```

Match the helper names already used in the script (`assert_screen_contains`, `assert_cursor`, `expect_send` — adjust to the script's actual helpers found in Step 1).

- [ ] **Step 3: Run the smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all assertions pass, including the new cursor `5 17` check.

> If the script's helper names or navigation differ from the snippet, adapt to the script's conventions (invoke the `s3270-smoke-testing` skill for its exact API). The required assertions are: the form renders with username display-only + Full name + Email, and the cursor lands on the first editable field.

- [ ] **Step 4: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "test(smoke): cover Edit User Details screen (GH #46)"
```

---

## Final verification

- [ ] **Run the whole suite with the race detector**

Run: `go build ./... && go test ./... -race`
Expected: PASS.

- [ ] **Manual emulator pass (final word on visual polish)**

`./bin/tn3270proxy serve -config <throwaway-no-tls>` then `c3270 127.0.0.1:2323`: log in as an admin, open `A → Users → S` on a user, confirm the username is display-only, edit name/email, leave the password blank (verify login still works), then set a new password and confirm it takes. Add a new user via PF4 with name/email populated.
