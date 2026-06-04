# Service NAME/Description Split + Case Normalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make user/group names canonical uppercase, split the service `name` into an uppercase `NAME` identifier plus a required mixed-case `Description`, and render an ISPF-style end-user menu that hides host/port — closing GH issue #9.

**Architecture:** All normalization and validation live in the `store` layer (the single choke point per the "store owns all SQL" convention). The schema is edited directly and dev DBs are reseeded — this is pre-production, so **no data migration is written**. Screens/admin/seed consume the new store API; the admin form surfaces friendly validation errors by reusing the store's `NormalizeServiceName`.

**Tech Stack:** Go, `modernc.org/sqlite` (pure-Go), `go3270`, stdlib `testing`.

---

## Background for the implementer (read once)

- `internal/store/store.go` owns the schema (`const schema`), `CreateUser`, `CreateGroup`, `CreateService`, `GetUserByUsername`, and the `Service` struct.
- `internal/store/admin.go` owns `UpdateService`.
- `internal/server/admin.go` declares the `AdminStore` interface that `*store.Store` must satisfy (`var _ AdminStore = (*store.Store)(nil)`); changing a store method signature **requires** updating this interface too or the build breaks.
- `internal/server/admin_services.go` is the admin add/edit/list flow.
- `internal/screens/menu.go` builds the end-user menu; `internal/screens/admin.go` holds the `Field*` name constants.
- `internal/seed/seed.go` + `seed.example.json` seed declaratively.
- Go is strictly compiled: a signature change and **all** its call sites (including test files) must land in the same commit to keep the build green. Use `go build ./...` to find every caller.
- Run tests with the race detector: `go test ./... -race`.
- SQLite `COLLATE NOCASE` on a `UNIQUE` column means `PROD` and `prod` collide; combined with uppercase folding it is belt-and-suspenders.

## File structure (what changes and why)

- `internal/store/store.go` — schema (`COLLATE NOCASE` on the three name columns, new `services.description`), fold user/group names, `Service.Description`, `CreateService` gains `description`, new `NormalizeServiceName` + length consts.
- `internal/store/admin.go` — `UpdateService` gains `description` and normalizes.
- `internal/store/*_test.go` — new fold/validation tests; update existing `CreateService`/`UpdateService` call sites.
- `internal/server/admin.go` — `AdminStore` interface: `CreateService`/`UpdateService` signatures gain `description`.
- `internal/server/admin_services.go` — service form gains a Description field + NAME validation; list shows Description; pass normalized name + description to store.
- `internal/server/admin.go`/`screens/admin.go` — `FieldDescription` constant.
- `internal/server/*_test.go` — update `CreateService` call sites.
- `internal/screens/menu.go` — ISPF-style row, drop host/port.
- `internal/screens/menu_test.go` — assert new row format.
- `internal/seed/seed.go` — `SeedService.Description`; pass through.
- `seed.example.json` — valid uppercase NAMEs + descriptions.

---

## Task 1: Case-fold usernames and group names (+ NOCASE schema)

Closes the user/group half of #9: `alice` logs in as `ALICE`; a `zzadmin` group resolves to the admin group.

**Files:**
- Modify: `internal/store/store.go` (schema `users`/`groups`; `CreateUser`, `CreateGroup`, `GetUserByUsername`)
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/store/store_test.go`:

```go
func TestUsernameFoldsToUppercase(t *testing.T) {
	st := newTestStore(t) // existing helper; opens an in-memory/temp store
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "alice", "hash"); err != nil {
		t.Fatalf("create: %v", err)
	}
	u, err := st.GetUserByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("lookup ALICE: %v", err)
	}
	if u.Username != "ALICE" {
		t.Errorf("stored username = %q, want ALICE", u.Username)
	}
	// Mixed-case lookup of a mixed-case create resolves to the same row.
	if u2, err := st.GetUserByUsername(ctx, "aLiCe"); err != nil || u2.ID != u.ID {
		t.Errorf("aLiCe lookup = (%+v, %v), want same id %d", u2, err, u.ID)
	}
}

func TestGroupNameFoldsToUppercase(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.CreateGroup(ctx, "zzadmin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Re-creating with different case is the same row (idempotent + NOCASE).
	id2, err := st.CreateGroup(ctx, "ZZADMIN")
	if err != nil || id2 != id {
		t.Fatalf("re-create ZZADMIN = (%d, %v), want same id %d", id2, err, id)
	}
}
```

If `newTestStore` does not exist, use the helper the existing tests already use to construct a `*store.Store` (check the top of `store_test.go`) and match its name.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'FoldsToUppercase' -v`
Expected: FAIL — `GetUserByUsername("ALICE")` returns `ErrNotFound` (names not yet folded).

- [ ] **Step 3: Add `COLLATE NOCASE` to the schema**

In `internal/store/store.go`, in `const schema`, change the two columns:

```sql
	username      TEXT UNIQUE COLLATE NOCASE NOT NULL,
```
```sql
	name TEXT UNIQUE COLLATE NOCASE NOT NULL
```
(the `users.username` line and the `groups.name` line).

- [ ] **Step 4: Fold names in the user/group store methods**

Ensure `strings` is imported. In `CreateUser`, fold first:

```go
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (int64, error) {
	username = strings.ToUpper(username)
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO users (username, password_hash) VALUES (?, ?)",
		[]any{username, passwordHash},
		"SELECT id FROM users WHERE username = ?",
		[]any{username})
}
```

In `CreateGroup`:

```go
func (s *Store) CreateGroup(ctx context.Context, name string) (int64, error) {
	name = strings.ToUpper(name)
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO groups (name) VALUES (?)",
		[]any{name},
		"SELECT id FROM groups WHERE name = ?",
		[]any{name})
}
```

In `GetUserByUsername`, fold the argument before the query:

```go
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	username = strings.ToUpper(username)
	var u User
	err := s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.PasswordHash)
	// ... unchanged
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'FoldsToUppercase' -v`
Expected: PASS.

- [ ] **Step 6: Run the full store package + race**

Run: `go test ./internal/store/ -race`
Expected: PASS. (The `migrate()` step that inserts `AdminGroup` = `ZZADMIN` is already uppercase, so it is unaffected.)

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): fold usernames and group names to canonical uppercase (#9)"
```

---

## Task 2: Service NAME normalization + Description column + store API

**Files:**
- Modify: `internal/store/store.go` (schema `services`, `Service` struct, `CreateService`, new `NormalizeServiceName` + consts)
- Modify: `internal/store/admin.go` (`UpdateService`)
- Test: `internal/store/store_test.go`, `internal/store/admin_test.go`

- [ ] **Step 1: Write failing tests for `NormalizeServiceName` + description**

Add to `internal/store/store_test.go`:

```go
func TestNormalizeServiceName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"prod", "PROD", false},
		{" Tso ", "TSO", false},
		{"PRODCICS", "PRODCICS", false},
		{"prod cics", "", true},  // space rejected
		{"PROD_CICS", "", true},  // underscore rejected
		{"TOOLONGNM", "", true},  // 9 chars
		{"", "", true},           // empty
	}
	for _, c := range cases {
		got, err := store.NormalizeServiceName(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeServiceName(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("NormalizeServiceName(%q) = (%q, %v), want (%q, nil)", c.in, got, err, c.want)
		}
	}
}

func TestCreateServiceFoldsNameAndRequiresDescription(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.CreateService(ctx, "prod", "Production CICS", "h", 23, false, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Case-variant create dedups to the same row (NOCASE + fold).
	id2, err := st.CreateService(ctx, "PROD", "Production CICS", "h", 23, false, true)
	if err != nil || id2 != id {
		t.Fatalf("dedup create = (%d, %v), want same id %d", id2, err, id)
	}
	svcs, _ := st.ListAllServices(ctx)
	if len(svcs) != 1 || svcs[0].Name != "PROD" || svcs[0].Description != "Production CICS" {
		t.Fatalf("services = %+v, want one PROD/Production CICS", svcs)
	}
	// Empty description rejected.
	if _, err := st.CreateService(ctx, "TSO", "", "h", 23, false, true); err == nil {
		t.Error("empty description: want error, got nil")
	}
	// Invalid name rejected.
	if _, err := st.CreateService(ctx, "bad name", "desc", "h", 23, false, true); err == nil {
		t.Error("invalid name: want error, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'NormalizeServiceName|CreateServiceFolds' -v`
Expected: FAIL to compile — `NormalizeServiceName` undefined and `CreateService` takes 6 args, not 7.

- [ ] **Step 3: Add the `services` schema column + NOCASE**

In `const schema`, replace the `services` table with:

```sql
CREATE TABLE IF NOT EXISTS services (
	id          INTEGER PRIMARY KEY,
	name        TEXT UNIQUE COLLATE NOCASE NOT NULL,
	description TEXT NOT NULL,
	host        TEXT NOT NULL,
	port        INTEGER NOT NULL,
	tls         INTEGER NOT NULL DEFAULT 0,
	tls_verify  INTEGER NOT NULL DEFAULT 1
);
```

- [ ] **Step 4: Add the `Description` field + normalization helper + consts**

Add the field to the `Service` struct:

```go
type Service struct {
	ID          int64
	Name        string
	Description string
	Host        string
	Port        int
	TLS         bool
	TLSVerify   bool
}
```

Add near the `Service` type (ensure `errors` and `strings` are imported):

```go
// MaxServiceNameLen and MaxDescriptionLen bound the service identifier and its
// human label.
const (
	MaxServiceNameLen = 8
	MaxDescriptionLen = 40
)

// NormalizeServiceName folds name to uppercase and validates it as a service
// identifier: 1-8 characters, A-Z and 0-9 only. It returns the normalized name
// or an error naming the rule violated.
func NormalizeServiceName(name string) (string, error) {
	n := strings.ToUpper(strings.TrimSpace(name))
	if n == "" {
		return "", errors.New("service name is required")
	}
	if len(n) > MaxServiceNameLen {
		return "", errors.New("service name must be 8 characters or fewer")
	}
	for _, r := range n {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return "", errors.New("service name may contain only letters A-Z and digits 0-9")
		}
	}
	return n, nil
}

// validateDescription enforces a required, length-bounded label.
func validateDescription(desc string) error {
	if desc == "" {
		return errors.New("description is required")
	}
	if len(desc) > MaxDescriptionLen {
		return errors.New("description must be 40 characters or fewer")
	}
	return nil
}
```

- [ ] **Step 5: Update `CreateService` to take + validate `description`**

```go
func (s *Store) CreateService(ctx context.Context, name, description, host string, port int, tls, verify bool) (int64, error) {
	name, err := NormalizeServiceName(name)
	if err != nil {
		return 0, err
	}
	if err := validateDescription(description); err != nil {
		return 0, err
	}
	tlsInt := 0
	if tls {
		tlsInt = 1
	}
	verifyInt := 0
	if verify {
		verifyInt = 1
	}
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO services (name, description, host, port, tls, tls_verify) VALUES (?, ?, ?, ?, ?, ?)",
		[]any{name, description, host, port, tlsInt, verifyInt},
		"SELECT id FROM services WHERE name = ?",
		[]any{name})
}
```

- [ ] **Step 6: Update `UpdateService` (in `internal/store/admin.go`)**

```go
func (s *Store) UpdateService(ctx context.Context, id int64, name, description, host string, port int, tls, verify bool) error {
	name, err := NormalizeServiceName(name)
	if err != nil {
		return err
	}
	if err := validateDescription(description); err != nil {
		return err
	}
	tlsInt, verifyInt := 0, 0
	if tls {
		tlsInt = 1
	}
	if verify {
		verifyInt = 1
	}
	return s.execExpectingRow(ctx,
		"UPDATE services SET name = ?, description = ?, host = ?, port = ?, tls = ?, tls_verify = ? WHERE id = ?",
		name, description, host, port, tlsInt, verifyInt, id)
}
```

Ensure any `SELECT` that reads service columns also reads `description`. Find them:

Run: `grep -n "SELECT .*FROM services\|Scan(&.*\.Host" internal/store/*.go`
Update each `SELECT ... FROM services` to include `description` and each matching `rows.Scan`/`row.Scan` to scan into `&svc.Description` in column order (it sits between `name` and `host`). The functions to check: `GetService`, `ListAllServices`, `ListServicesForGroups`.

- [ ] **Step 7: Fix store-package test call sites**

Run: `go build ./internal/store/ 2>&1 | head` and `go vet ./internal/store/` to find broken callers. Update every `CreateService(ctx, name, host, ...)` to `CreateService(ctx, name, "Desc", host, ...)` and every `UpdateService(ctx, id, name, host, ...)` to `UpdateService(ctx, id, name, "Desc", host, ...)` in `internal/store/admin_test.go` (e.g. line ~141/152) and any other store test. Use a real description string, not empty.

- [ ] **Step 8: Run the store tests**

Run: `go test ./internal/store/ -race`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/store/
git commit -m "feat(store): split service into NAME identifier + required Description (#9)"
```

---

## Task 3: Admin interface, service form, and list

**Files:**
- Modify: `internal/server/admin.go` (`AdminStore` interface signatures)
- Modify: `internal/screens/admin.go` (`FieldDescription` constant)
- Modify: `internal/server/admin_services.go` (form fields + validation + list row + store calls)
- Test: `internal/server/admin_test.go` (+ fix call sites)

- [ ] **Step 1: Update the `AdminStore` interface**

In `internal/server/admin.go`, change the two lines to match the new store signatures:

```go
	CreateService(ctx context.Context, name, description, host string, port int, tls, verify bool) (int64, error)
	UpdateService(ctx context.Context, id int64, name, description, host string, port int, tls, verify bool) error
```

- [ ] **Step 2: Add the `FieldDescription` constant**

In `internal/screens/admin.go`, beside the other `Field*` constants:

```go
	FieldDescription = "description" // service form: human label
```

- [ ] **Step 3: Write a failing admin test for description round-trip**

In `internal/server/admin_test.go`, add a test that drives the service form to create a service with a description and asserts it persists. Match the existing admin-test harness style in that file (it constructs a real `*store.Store` and a fake presenter that scripts `AdminForm` responses). Skeleton — adapt field/return names to the existing fakes in this file:

```go
func TestServiceFormPersistsDescription(t *testing.T) {
	// Arrange: store + adminFlow with a presenter whose AdminForm returns
	// Values{FieldName:"prodcics", FieldDescription:"Production CICS",
	// FieldHost:"h", FieldPort:"23", FieldTLS:"N", FieldVerify:"Y"}.
	// Act: run the add-service path once.
	// Assert: ListAllServices has one row with Name "PRODCICS" and
	//         Description "Production CICS".
}
```

If wiring a full form drive is heavy, instead assert at the store seam: call the same validation the form uses (`store.NormalizeServiceName`) plus `CreateService`, and verify persisted `Description`. Prefer the form-level test if the existing fakes already support scripting `AdminForm`.

- [ ] **Step 4: Run it to verify failure**

Run: `go test ./internal/server/ -run ServiceFormPersistsDescription -v`
Expected: FAIL (description not yet wired through the form).

- [ ] **Step 5: Add the Description field to the form + validation**

In `internal/server/admin_services.go` `serviceForm`, add a `description` variable (seeded from `existing.Description` when editing), add the form field after Name, shorten Name length to 8, read the value back, validate, and pass it through:

```go
	name, description, host, port, tlsYN, verifyYN := "", "", "", "", "N", "Y"
	if existing != nil {
		title = "TN3270 GATEWAY ADMIN: EDIT SERVICE"
		name, description, host, port = existing.Name, existing.Description, existing.Host, strconv.Itoa(existing.Port)
		tlsYN, verifyYN = yn(existing.TLS), yn(existing.TLSVerify)
	}
```

Fields slice:

```go
			Fields: []screens.AdminFormField{
				{Name: screens.FieldName, Label: "Name . . . .", Value: name, Length: 8},
				{Name: screens.FieldDescription, Label: "Descr. . . .", Value: description, Length: 40},
				{Name: screens.FieldHost, Label: "Host . . . .", Value: host, Length: 48},
				{Name: screens.FieldPort, Label: "Port . . . .", Value: port, Length: 5},
				{Name: screens.FieldTLS, Label: "TLS (Y/N) .", Value: tlsYN, Length: 1},
				{Name: screens.FieldVerify, Label: "Verify (Y/N)", Value: verifyYN, Length: 1},
			},
```

Read-back + validation. Replace the value reads and the validation `switch`:

```go
		name = act.Values[screens.FieldName]
		description = act.Values[screens.FieldDescription]
		host = act.Values[screens.FieldHost]
		port = act.Values[screens.FieldPort]
		tlsYN = strings.ToUpper(act.Values[screens.FieldTLS])
		verifyYN = strings.ToUpper(act.Values[screens.FieldVerify])

		p, perr := strconv.Atoi(port)
		tlsB, tlsOK := ynBool(tlsYN)
		verifyB, verifyOK := ynBool(verifyYN)
		normName, nameErr := store.NormalizeServiceName(name)
		switch {
		case nameErr != nil:
			errMsg = strings.ToUpper(nameErr.Error())
		case description == "":
			errMsg = "DESCRIPTION IS REQUIRED"
		case len(description) > store.MaxDescriptionLen:
			errMsg = "DESCRIPTION MUST BE 40 CHARACTERS OR FEWER"
		case host == "":
			errMsg = "HOST IS REQUIRED"
		case perr != nil || p < 1 || p > 65535:
			errMsg = "PORT MUST BE 1-65535"
		case !tlsOK || !verifyOK:
			errMsg = "TLS AND VERIFY MUST BE Y OR N"
		default:
			if msg := f.checkServiceNameFree(ctx, normName, existing); msg != "" {
				errMsg = msg
				continue
			}
			if existing == nil {
				if _, err := f.store.CreateService(ctx, normName, description, host, p, tlsB, verifyB); err != nil {
					errMsg = logStoreErr("create service", err)
					continue
				}
				f.recordAdmin(ctx, "service create "+normName)
			} else if err := f.store.UpdateService(ctx, existing.ID, normName, description, host, p, tlsB, verifyB); err != nil {
				errMsg = logStoreErr("update service", err)
				continue
			} else {
				f.recordAdmin(ctx, "service update "+normName)
			}
			return nil
		}
```

(`store` is already imported in this file.) `checkServiceNameFree` is unchanged — it now receives the normalized name and compares against stored uppercase names.

- [ ] **Step 6: Update the admin list row to show Description**

Replace the list row + header (around line 48-56):

```go
		for i, s := range pageSvcs {
			rows[i] = fmt.Sprintf("%-8s %-20.20s %-18s %-3s %s",
				s.Name, s.Description, fmt.Sprintf("%s:%d", s.Host, s.Port), yn(s.TLS), yn(s.TLSVerify))
		}
```
```go
			Header:  "CMD  NAME     DESCRIPTION          HOST:PORT          TLS VERIFY",
```

(`%-20.20s` truncates long descriptions in the list; the full value is editable on the form.)

- [ ] **Step 7: Fix server-package test call sites**

Run: `go build ./internal/server/ 2>&1 | head`
Update `CreateService`/`UpdateService` calls in `internal/server/admin_test.go` (e.g. line ~84) and `internal/server/session_test.go` (e.g. line ~108) to include a description argument, e.g. `st.CreateService(ctx, "PROD", "Production", "h", 23, false, true)`.

- [ ] **Step 8: Run the server tests**

Run: `go test ./internal/server/ -race`
Expected: PASS (including `TestServiceFormPersistsDescription`).

- [ ] **Step 9: Commit**

```bash
git add internal/server/ internal/screens/admin.go
git commit -m "feat(admin): Description field + NAME validation in service form and list (#9)"
```

---

## Task 4: ISPF-style end-user menu (drop host/port)

**Files:**
- Modify: `internal/screens/menu.go`
- Test: `internal/screens/menu_test.go`

- [ ] **Step 1: Write a failing menu test**

Add to `internal/screens/menu_test.go` (match the existing test helpers/imports in that file):

```go
func TestMenuRendersISPFStyleAndHidesHostPort(t *testing.T) {
	svcs := []store.Service{
		{Name: "PRODCICS", Description: "Production CICS Region", Host: "secret.internal", Port: 992},
	}
	screen, mapping := MenuScreen(DefaultGeometry, svcs, false, "")
	if _, ok := mapping["1"]; !ok {
		t.Fatal("selection 1 not mapped")
	}
	var found bool
	for _, f := range screen {
		if strings.Contains(f.Content, "PRODCICS") && strings.Contains(f.Content, "Production CICS Region") {
			found = true
		}
		if strings.Contains(f.Content, "secret.internal") || strings.Contains(f.Content, "992") {
			t.Errorf("menu leaks host/port: %q", f.Content)
		}
	}
	if !found {
		t.Error("expected an ISPF-style row with NAME and Description")
	}
}
```

The other tests in this file pass the package var `DefaultGeometry` (there is no `NewGeometry`). Ensure `strings` and `store` are imported in the test.

- [ ] **Step 2: Run it to verify failure**

Run: `go test ./internal/screens/ -run ISPFStyle -v`
Expected: FAIL — current row uses `svc.Name`/host/port format and includes the port `992`.

- [ ] **Step 3: Update the menu row format**

In `internal/screens/menu.go`, replace the label line in the loop:

```go
		label := fmt.Sprintf("%2s  %-8s  %s", key, svc.Name, svc.Description)
```

(`svc.Host`/`svc.Port` are no longer referenced here.)

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/screens/ -run ISPFStyle -v`
Expected: PASS.

- [ ] **Step 5: Run the screens package**

Run: `go test ./internal/screens/ -race`
Expected: PASS. If an older menu test asserted the host/port format, update it to the new format.

- [ ] **Step 6: Commit**

```bash
git add internal/screens/menu.go internal/screens/menu_test.go
git commit -m "feat(menu): ISPF-style service rows; hide host/port from users (#9)"
```

---

## Task 5: Seed support for Description

**Files:**
- Modify: `internal/seed/seed.go`
- Modify: `seed.example.json`
- Test: `internal/seed/seed_test.go`

- [ ] **Step 1: Write a failing seed test**

Add to `internal/seed/seed_test.go` (match its existing store/helper setup):

```go
func TestApplySeedsServiceDescription(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "seed.db")) // matches existing seed_test setup
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	data := SeedData{Services: []SeedService{
		{Name: "prodcics", Description: "Production CICS", Host: "h", Port: 23},
	}}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatalf("apply: %v", err)
	}
	svcs, _ := st.ListAllServices(ctx)
	if len(svcs) != 1 || svcs[0].Name != "PRODCICS" || svcs[0].Description != "Production CICS" {
		t.Fatalf("services = %+v, want PRODCICS/Production CICS", svcs)
	}
}
```

- [ ] **Step 2: Run it to verify failure**

Run: `go test ./internal/seed/ -run ServiceDescription -v`
Expected: FAIL to compile — `SeedService` has no `Description` field; `CreateService` call arity differs.

- [ ] **Step 3: Add the field + pass it through**

In `internal/seed/seed.go`, add to `SeedService`:

```go
	Description string   `json:"description"`
```

Update the `CreateService` call in `Apply`:

```go
		sid, err := st.CreateService(ctx, svc.Name, svc.Description, svc.Host, svc.Port, svc.TLS, verify)
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/seed/ -run ServiceDescription -v`
Expected: PASS.

- [ ] **Step 5: Update `seed.example.json` to valid NAMEs + descriptions**

```json
{
  "groups": ["ops", "dev"],
  "users": [
    { "username": "admin", "password": "changeme", "groups": ["ZZADMIN"] },
    { "username": "alice", "password": "changeme", "groups": ["ops"] },
    { "username": "bob",   "password": "changeme", "groups": ["dev"] }
  ],
  "services": [
    { "name": "PUBTSO",  "description": "Public TSO",        "host": "localhost",         "port": 3270, "groups": ["ops"] },
    { "name": "SECCICS", "description": "Secure CICS Region", "host": "cics.corp.example", "port": 992, "tls": true, "groups": ["ops"] },
    { "name": "LABCICS", "description": "Lab CICS (self-signed)", "host": "10.0.0.5",     "port": 992, "tls": true, "verify": false, "groups": ["dev"] }
  ]
}
```

- [ ] **Step 6: Run the seed package + commit**

Run: `go test ./internal/seed/ -race`
Expected: PASS.

```bash
git add internal/seed/seed.go internal/seed/seed_test.go seed.example.json
git commit -m "feat(seed): service Description; uppercase NAMEs in example seed (#9)"
```

---

## Task 6: Whole-build verification + protocol smoke

**Files:** none (verification only)

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no output (success). If any call site is still broken (e.g. in `cmd/`), fix it to pass a description argument, then rebuild.

- [ ] **Step 2: Full test suite with race**

Run: `go test ./... -race`
Expected: PASS across all packages.

- [ ] **Step 3: Seed a fresh DB and run the protocol smoke test**

Because the schema changed without a migration, use a brand-new DB file:

```bash
rm -f smoke.db
go build -o bin/tn3270proxy ./cmd/tn3270proxy
./bin/tn3270proxy seed -db smoke.db -file seed.example.json
.claude/skills/s3270-smoke-testing/smoke.sh
```

Expected: smoke script passes — login works (try lowercase username to confirm folding), the menu shows ISPF-style `NN NAME Description` rows with **no** host/port, and bridging/PA3/PF3 behave. If `smoke.sh` needs a specific DB/flags, follow the s3270-smoke-testing skill.

- [ ] **Step 4: Final commit (if any cmd/ fixups were needed)**

```bash
git add -A
git commit -m "chore: fix remaining service call sites for description arg (#9)"
```

(Skip if the working tree is already clean.)

---

## Self-review notes

- **Spec coverage:** users/groups fold + NOCASE (Task 1); service NAME identifier + Description + NOCASE + validation (Task 2); admin form/list + interface (Task 3); ISPF menu hiding host/port (Task 4); seed + example JSON (Task 5); admin-gate bonus is covered for free by Task 1's group folding (no code change — `identity.Groups` is now uppercase and matches `store.AdminGroup`); no-migration/reseed handled in Task 6.
- **Signature consistency:** `CreateService(ctx, name, description, host, port, tls, verify)` and `UpdateService(ctx, id, name, description, host, port, tls, verify)` are used identically in store impl, `AdminStore` interface, admin form, seed, and all updated tests.
- **No placeholders:** every code step shows the code; test skeletons that depend on existing fakes (Task 3 Step 3) explicitly say to match the harness already in that file, with a store-seam fallback.
```
