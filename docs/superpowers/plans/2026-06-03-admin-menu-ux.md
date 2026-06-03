# Admin / Menu UX Refinements (#3.5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the three UX refinements from `docs/superpowers/specs/2026-06-03-admin-menu-ux-design.md`: a group→members toggle view (`M` line command), PA3 removed from the admin screens (PF3 walks up one level), and layered PF3 logout (menu PF3 → login screen; login PF3 → disconnect).

**Architecture:** All changes live in `internal/store` (one new query), `internal/server` (adminFlow + session state machine), and `internal/screens` (help-line texts). No schema migration. The `bail bool` plumbing threaded through every adminFlow method is deleted; `Session.Run` gains an outer login↔menu loop.

**Tech Stack:** Go, go3270, modernc SQLite. Tests: stdlib `testing` with the existing fakes (`fakePresenter`, `fakeAdminPresenter`, real temp-file store). Always verify with `go test ./... -race` (bridge is concurrent).

**Task order matters:** Task 2 (PA3 removal) simplifies every adminFlow signature from `(bool, error)` to `error`; Task 3 (members view) is written against the new signatures. Don't reorder.

---

### Task 1: `store.ListUsersInGroup`

**Files:**
- Modify: `internal/store/admin.go` (add `ListUsersInGroup`, extract `queryUsers` helper)
- Test: `internal/store/admin_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/admin_test.go`:

```go
func TestListUsersInGroup(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	uid, gid, _ := seedTriangle(t, st) // alice in ops
	zoe, _ := st.CreateUser(ctx, "zoe", "h")
	st.CreateUser(ctx, "bob", "h") // NOT in ops — must not appear
	st.AddUserToGroup(ctx, zoe, gid)

	users, err := st.ListUsersInGroup(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Username != "alice" || users[1].Username != "zoe" {
		t.Fatalf("users = %+v, want [alice zoe] (ordered by username)", users)
	}
	if users[0].ID != uid {
		t.Errorf("alice ID = %d, want %d", users[0].ID, uid)
	}
	// missing/empty group → empty result, no error
	if empty, err := st.ListUsersInGroup(ctx, 99999); err != nil || len(empty) != 0 {
		t.Errorf("missing group = %+v, %v; want empty, nil", empty, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestListUsersInGroup -v`
Expected: FAIL to compile — `st.ListUsersInGroup undefined`

- [ ] **Step 3: Implement**

In `internal/store/admin.go`, replace the existing `ListUsers` (lines 21–38) with a `queryUsers`-based version plus the new method (mirrors the existing `queryGroups` pattern):

```go
// ListUsers returns all users ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	return s.queryUsers(ctx,
		"SELECT id, username, password_hash FROM users ORDER BY username")
}

// ListUsersInGroup returns the group's members ordered by username.
func (s *Store) ListUsersInGroup(ctx context.Context, groupID int64) ([]User, error) {
	return s.queryUsers(ctx,
		`SELECT u.id, u.username, u.password_hash FROM users u
		 JOIN user_groups ug ON ug.user_id = u.id
		 WHERE ug.group_id = ? ORDER BY u.username`, groupID)
}

func (s *Store) queryUsers(ctx context.Context, query string, args ...any) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -race`
Expected: PASS (all store tests, including the pre-existing `TestListUsersOrdered`)

- [ ] **Step 5: Commit**

```bash
git add internal/store/admin.go internal/store/admin_test.go
git commit -m "feat(store): ListUsersInGroup query for the group members view"
```

---

### Task 2: Drop PA3 from the admin screens (delete `bail` plumbing)

PA3's product meaning is "escape the remote host"; inside admin it becomes an inactive key (go3270 re-presents the screen when an AID isn't in the handled set). This is a behavior **removal**, so tests and code change in lockstep: every adminFlow method goes `(bool, error)` → `error`, `AdminListAction`/`AdminFormAction` lose their `PA3` field, and `AdminMenu` loses `exit`.

**Files:**
- Modify: `internal/server/presenter_admin.go`
- Modify: `internal/server/admin.go`
- Modify: `internal/server/admin_users.go`
- Modify: `internal/server/admin_groups.go`
- Modify: `internal/server/admin_services.go`
- Modify: `internal/screens/admin.go` (help texts)
- Test: `internal/server/presenter_admin_test.go`, `internal/server/admin_test.go`

No test in the existing suite scripts a `{PA3: true}` action, so the flow tests survive the field removal; only the signature/struct ripples below are needed.

- [ ] **Step 1: Update `presenter_admin_test.go`**

In `TestListActionFromResponse`, delete the case row:

```go
		{"pa3", go3270.AIDPA3, nil, 2, AdminListAction{PA3: true}},
```

In `TestFormActionFromResponse`, delete these four lines:

```go
	got = formActionFromResponse(go3270.Response{AID: go3270.AIDPA3}, fields)
	if !got.PA3 {
		t.Errorf("PA3 should bail: %+v", got)
	}
```

- [ ] **Step 2: Update `presenter_admin.go`**

Replace the action structs, interface, and exit keys (top of file) with:

```go
// AdminListAction is what the user did on an admin list screen.
type AdminListAction struct {
	Cmd byte // upper-cased line command ('S', 'D', ...), 0 if none
	Row int  // index into the rendered page's rows (valid when Cmd != 0)
	PF  int  // 3 (back), 4 (add), 7/8 (page); 0 for plain Enter
}

// AdminFormAction is what the user did on an admin form screen.
type AdminFormAction struct {
	Values map[string]string // by field name; visible fields trimmed
	Cancel bool              // PF3
}

// AdminPresenter renders the admin screens. The real implementation wraps
// go3270; adminFlow tests use a fake.
type AdminPresenter interface {
	// AdminMenu returns choice 1/2/3 (users/groups/services) or back (PF3,
	// to the service menu). It loops internally on invalid input.
	AdminMenu(conn net.Conn, errMsg string) (choice int, back bool, err error)
	AdminList(conn net.Conn, v screens.AdminListView) (AdminListAction, error)
	AdminForm(conn net.Conn, v screens.AdminFormView) (AdminFormAction, error)
}

var adminListExitKeys = []go3270.AID{
	go3270.AIDPF3, go3270.AIDPF4, go3270.AIDPF7, go3270.AIDPF8,
}
```

Replace `AdminMenu` with:

```go
func (go3270Presenter) AdminMenu(conn net.Conn, errMsg string) (int, bool, error) {
	for {
		screen := screens.AdminMenuScreen(errMsg)
		resp, err := go3270.HandleScreen(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			[]go3270.AID{go3270.AIDPF3},
			screens.FieldError, 19, 8, conn,
		)
		if err != nil {
			return 0, false, err
		}
		if resp.AID == go3270.AIDPF3 {
			return 0, true, nil
		}
		switch strings.TrimSpace(resp.Values[screens.FieldOption]) {
		case "1":
			return 1, false, nil
		case "2":
			return 2, false, nil
		case "3":
			return 3, false, nil
		}
		errMsg = "Invalid option"
	}
}
```

In `AdminForm`, change the exit-keys argument from
`[]go3270.AID{go3270.AIDPF3, go3270.AIDPA3}` to `[]go3270.AID{go3270.AIDPF3}`.

In `listActionFromResponse`, delete the case:

```go
	case go3270.AIDPA3:
		return AdminListAction{PA3: true}
```

In `formActionFromResponse`, replace the AID switch with:

```go
	if resp.AID == go3270.AIDPF3 {
		return AdminFormAction{Cancel: true}
	}
```

- [ ] **Step 3: Update `admin.go` `Run`**

Replace `Run` (including its doc comment) with:

```go
// Run loops on the admin menu until the user leaves via PF3 (back to the
// service menu). A non-nil error means the client connection is unusable and
// the session should end.
func (f *adminFlow) Run(ctx context.Context, conn net.Conn) error {
	errMsg := ""
	for {
		choice, back, err := f.presenter.AdminMenu(conn, errMsg)
		if err != nil {
			return err
		}
		if back {
			return nil
		}
		errMsg = ""
		switch choice {
		case 1:
			err = f.users(ctx, conn)
		case 2:
			err = f.groups(ctx, conn)
		case 3:
			err = f.services(ctx, conn)
		}
		if err != nil {
			return err
		}
	}
}
```

- [ ] **Step 4: Update `admin_users.go`**

Replace `users` with (changes: signature, doc comment, PFHelp, no PA3 cases, sub-calls):

```go
// users drives the user list and its sub-screens.
func (f *adminFlow) users(ctx context.Context, conn net.Conn) error {
	page, errMsg := 0, ""
	var pendingDelete *store.User
	for {
		users, err := f.store.ListUsers(ctx)
		if err != nil {
			errMsg = logStoreErr("list users", err)
			users = nil
			pendingDelete = nil // confirm lost; user must re-initiate D
		}
		var start, end int
		var rowInfo string
		page, start, end, rowInfo = pageBounds(page, len(users))
		pageUsers := users[start:end]
		rows := make([]string, len(pageUsers))
		for i, u := range pageUsers {
			groups, gerr := f.store.GetUserGroups(ctx, u.ID)
			if gerr != nil {
				groups = nil
			}
			rows[i] = fmt.Sprintf("%-16s %s", u.Username, strings.Join(groups, ","))
		}
		act, err := f.presenter.AdminList(conn, screens.AdminListView{
			Title:   "TN3270 GATEWAY ADMIN: USERS",
			RowInfo: rowInfo,
			Header:  "CMD  USERNAME         GROUPS",
			Rows:    rows,
			Legend:  "S = set password   G = groups   D = delete   PF4 = add user",
			ErrMsg:  errMsg,
			PFHelp:  "Enter = process   PF7/PF8 = page   PF3 = admin menu",
		})
		if err != nil {
			return err
		}
		errMsg = ""

		// A pending delete is resolved by the very next action: plain Enter
		// confirms, PF3 cancels (stays on the list), anything else cancels and
		// is processed normally.
		if pendingDelete != nil {
			target := *pendingDelete
			pendingDelete = nil
			switch {
			case act.Cmd == 0 && act.PF == 0:
				errMsg = f.deleteUser(ctx, target)
				continue
			case act.PF == 3:
				continue
			}
		}

		switch {
		case act.PF == 3:
			return nil
		case act.PF == 4:
			if err := f.userAdd(ctx, conn); err != nil {
				return err
			}
		case act.PF == 7:
			page--
		case act.PF == 8:
			if end < len(users) {
				page++
			}
		case act.Cmd != 0:
			if act.Row >= len(pageUsers) {
				continue
			}
			u := pageUsers[act.Row]
			switch act.Cmd {
			case 'S':
				if err := f.setPassword(ctx, conn, u); err != nil {
					return err
				}
			case 'G':
				if err := f.userGroups(ctx, conn, u); err != nil {
					return err
				}
			case 'D':
				pendingDelete = &u
				errMsg = fmt.Sprintf("ENTER = CONFIRM DELETE OF '%s', PF3 = CANCEL", u.Username)
			default:
				errMsg = "INVALID COMMAND: " + string(act.Cmd)
			}
		}
	}
}
```

In `userAdd`: signature `func (f *adminFlow) userAdd(ctx context.Context, conn net.Conn) error`; delete the block

```go
		if act.PA3 {
			return true, nil
		}
```

and change `return false, err` → `return err`, `return false, nil` → `return nil` (three sites: the `err != nil` return, the `act.Cancel` return, the success return).

In `setPassword`: same treatment — signature `func (f *adminFlow) setPassword(ctx context.Context, conn net.Conn, u store.User) error`, delete the `act.PA3` block, `return false, err` → `return err`, `return false, nil` → `return nil`.

In `userGroups`: signature `func (f *adminFlow) userGroups(ctx context.Context, conn net.Conn, u store.User) error`; PFHelp becomes `"Enter = process   PF7/PF8 = page   PF3 = back"`; delete the case

```go
		case act.PA3:
			return true, nil
```

and change `return false, err` → `return err`, `case act.PF == 3: return false, nil` → `case act.PF == 3: return nil`.

- [ ] **Step 5: Update `admin_groups.go`**

In `groups`: signature `func (f *adminFlow) groups(ctx context.Context, conn net.Conn) error`; PFHelp becomes `"Enter = process   PF7/PF8 = page   PF3 = admin menu"`; in the pending-delete switch delete the `case act.PA3: return true, nil`; in the main switch delete `case act.PA3: return true, nil`, change `case act.PF == 3: return false, nil` → `return nil`, and replace the PF4 case with:

```go
		case act.PF == 4:
			if err := f.groupAdd(ctx, conn); err != nil {
				return err
			}
```

All `return false, err` → `return err`.

In `groupAdd`: signature `func (f *adminFlow) groupAdd(ctx context.Context, conn net.Conn) error`; delete the `act.PA3` block; `return false, err` → `return err`, `return false, nil` → `return nil` (cancel + success).

- [ ] **Step 6: Update `admin_services.go`**

In `services`: signature `func (f *adminFlow) services(ctx context.Context, conn net.Conn) error`; PFHelp becomes `"Enter = process   PF7/PF8 = page   PF3 = admin menu"`; delete both `case act.PA3: return true, nil` (pending-delete switch and main switch); `case act.PF == 3: return false, nil` → `return nil`; replace the PF4 / `'S'` / `'G'` cases with:

```go
		case act.PF == 4:
			if err := f.serviceForm(ctx, conn, nil); err != nil {
				return err
			}
```

```go
			case 'S':
				if err := f.serviceForm(ctx, conn, &s); err != nil {
					return err
				}
			case 'G':
				if err := f.serviceGroups(ctx, conn, s); err != nil {
					return err
				}
```

All `return false, err` → `return err`.

In `serviceForm`: signature `func (f *adminFlow) serviceForm(ctx context.Context, conn net.Conn, existing *store.Service) error`; delete the `act.PA3` block; `return false, err` → `return err`, `return false, nil` → `return nil` (cancel + success).

In `serviceGroups`: signature `func (f *adminFlow) serviceGroups(ctx context.Context, conn net.Conn, svc store.Service) error`; PFHelp becomes `"Enter = process   PF7/PF8 = page   PF3 = back"`; delete `case act.PA3: return true, nil`; `case act.PF == 3: return false, nil` → `return nil`; `return false, err` → `return err`.

- [ ] **Step 7: Update `internal/screens/admin.go` help texts**

In `AdminFormScreen`, change the help line to:

```go
		go3270.Field{Row: 23, Col: 2, Content: "Enter = save    PF3 = cancel"},
```

In `AdminMenuScreen`, change the doc comment and help line:

```go
// AdminMenuScreen renders the top-level admin menu. The caller drives it with
// HandleScreen: AIDEnter submits, PF3 returns to the service menu.
```

```go
		{Row: 23, Col: 2, Content: "Enter = select    PF3 = main menu"},
```

- [ ] **Step 8: Update `admin_test.go` fakes**

Replace `adminMenuStep` and the fake's `AdminMenu`:

```go
type adminMenuStep struct {
	choice int
	back   bool
}
```

```go
func (f *fakeAdminPresenter) AdminMenu(_ net.Conn, errMsg string) (int, bool, error) {
	f.gotMenuErrs = append(f.gotMenuErrs, errMsg)
	if len(f.menu) == 0 {
		panic("unexpected AdminMenu call")
	}
	s := f.menu[0]
	f.menu = f.menu[1:]
	return s.choice, s.back, nil
}
```

Replace `TestAdminFlowMenuBackAndExit` with:

```go
func TestAdminFlowMenuBack(t *testing.T) {
	p := &fakeAdminPresenter{menu: []adminMenuStep{{back: true}}}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}
```

(No other test scripts `exit: true` or a `PA3` action — verify with `grep -n "exit\|PA3" internal/server/admin_test.go`; expect no remaining hits.)

- [ ] **Step 9: Build and run the full suite**

Run: `go build ./... && go test ./... -race`
Expected: PASS everywhere. If `internal/server` fails to compile, a `(bool, error)` site was missed — `grep -n "bail\|PA3" internal/server/*.go` must return no hits outside `_test.go` files referencing the bridge escape (there are none in `server`; PA3 handling lives in `internal/bridge` only).

- [ ] **Step 10: Commit**

```bash
git add internal/server internal/screens
git commit -m "feat(admin): PF3-only navigation — PA3 no longer acts inside admin screens"
```

---

### Task 3: Group → members view (`M` line command)

**Files:**
- Modify: `internal/server/admin.go` (AdminStore interface)
- Modify: `internal/server/admin_groups.go` (legend, `M` dispatch, new `groupMembers`)
- Test: `internal/server/admin_test.go`

- [ ] **Step 1: Add `ListUsersInGroup` to the `AdminStore` interface**

In `internal/server/admin.go`, in the groups block of the interface (after `CountGroupServices`), add:

```go
	ListUsersInGroup(ctx context.Context, groupID int64) ([]store.User, error)
```

(`var _ AdminStore = (*store.Store)(nil)` already compile-checks this against Task 1's method.)

- [ ] **Step 2: Write the failing tests**

Append to `internal/server/admin_test.go`:

```go
func TestAdminGroupMembersToggle(t *testing.T) {
	// Group rows sort ZZADMIN(0), ops(1); user rows sort alice(0), root(1).
	// M on ops, add root, remove alice.
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{
			{Cmd: 'M', Row: 1}, // groups list: M on ops
			{Cmd: 'A', Row: 1}, // add root
			{Cmd: 'R', Row: 0}, // remove alice
			{PF: 3},            // back to groups list
			{PF: 3},            // back to admin menu
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	members, _ := f.store.ListUsersInGroup(ctx, ids["ops"])
	if len(members) != 1 || members[0].Username != "root" {
		t.Errorf("ops members = %+v, want [root]", members)
	}
	// groups list advertises the new command
	if legend := p.gotLists[0].Legend; !strings.Contains(legend, "M = members") {
		t.Errorf("groups legend = %q", legend)
	}
	// first members render: alice carries the X marker, root does not
	if rows := p.gotLists[1].Rows; len(rows) != 2 ||
		!strings.Contains(rows[0], "X") || strings.Contains(rows[1], "X") {
		t.Errorf("member markers wrong: %q", rows)
	}
	if title := p.gotLists[1].Title; !strings.Contains(title, "MEMBERS OF ops") {
		t.Errorf("title = %q", title)
	}
}

func TestAdminGroupMembersLastAdminGuard(t *testing.T) {
	// root is ZZADMIN's only member; R from the members side must be blocked.
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{
			{Cmd: 'M', Row: 0}, // groups list: M on ZZADMIN
			{Cmd: 'R', Row: 1}, // user rows alice(0), root(1): remove root — blocked
			{PF: 3}, {PF: 3},
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	f.Run(ctx, nil)
	if msg := p.gotLists[2].ErrMsg; !strings.Contains(msg, "CANNOT REMOVE LAST") {
		t.Errorf("errMsg = %q", msg)
	}
	if members, _ := f.store.ListUsersInGroup(ctx, ids["zzadmin"]); len(members) != 1 {
		t.Errorf("ZZADMIN members = %+v, want just root", members)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/server/ -run TestAdminGroupMembers -v`
Expected: FAIL — the `M` command hits the `default:` branch (`INVALID COMMAND: M`), so membership never changes and the legend assertion fails.

- [ ] **Step 4: Implement**

In `internal/server/admin_groups.go`, in `groups()`:

Change the legend line to:

```go
			Legend:  "M = members   D = delete   PF4 = add group",
```

In the line-command switch, add the `M` case before `case 'D':`:

```go
			switch act.Cmd {
			case 'M':
				if err := f.groupMembers(ctx, conn, g); err != nil {
					return err
				}
			case 'D':
```

Append the new method to the file (mirrors `userGroups`, membership keyed by user ID):

```go
// groupMembers shows every user with an X membership marker for g; line
// command A adds the user to the group, R removes (guarded for the last
// ZZADMIN member). Membership is manageable from either side: this is the
// group-side mirror of userGroups.
func (f *adminFlow) groupMembers(ctx context.Context, conn net.Conn, g store.Group) error {
	page, errMsg := 0, ""
	for {
		users, err := f.store.ListUsers(ctx)
		if err != nil {
			errMsg = logStoreErr("list users", err)
			users = nil
		}
		members, err := f.store.ListUsersInGroup(ctx, g.ID)
		if err != nil {
			errMsg = logStoreErr("list group members", err)
		}
		memberSet := make(map[int64]bool, len(members))
		for _, m := range members {
			memberSet[m.ID] = true
		}
		var start, end int
		var rowInfo string
		page, start, end, rowInfo = pageBounds(page, len(users))
		pageUsers := users[start:end]
		rows := make([]string, len(pageUsers))
		for i, u := range pageUsers {
			marker := ""
			if memberSet[u.ID] {
				marker = "X"
			}
			rows[i] = fmt.Sprintf("%-16s %s", u.Username, marker)
		}
		act, err := f.presenter.AdminList(conn, screens.AdminListView{
			Title:   "TN3270 GATEWAY ADMIN: MEMBERS OF " + g.Name,
			RowInfo: rowInfo,
			Header:  "CMD  USERNAME         MEMBER",
			Rows:    rows,
			Legend:  "A = add to group   R = remove from group",
			ErrMsg:  errMsg,
			PFHelp:  "Enter = process   PF7/PF8 = page   PF3 = back",
		})
		if err != nil {
			return err
		}
		errMsg = ""
		switch {
		case act.PF == 3:
			return nil
		case act.PF == 7:
			page--
		case act.PF == 8:
			if end < len(users) {
				page++
			}
		case act.Cmd != 0:
			if act.Row >= len(pageUsers) {
				continue
			}
			u := pageUsers[act.Row]
			switch act.Cmd {
			case 'A':
				if err := f.store.AddUserToGroup(ctx, u.ID, g.ID); err != nil {
					errMsg = logStoreErr("add membership", err)
				}
			case 'R':
				if g.Name == store.AdminGroup && memberSet[u.ID] {
					if msg := f.guardLastAdmin(ctx); msg != "" {
						errMsg = msg
						continue
					}
				}
				if err := f.store.RemoveUserFromGroup(ctx, u.ID, g.ID); err != nil {
					errMsg = logStoreErr("remove membership", err)
				}
			default:
				errMsg = "INVALID COMMAND: " + string(act.Cmd)
			}
		}
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/ -race`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/admin.go internal/server/admin_groups.go internal/server/admin_test.go
git commit -m "feat(admin): M line command opens a group members toggle view"
```

---

### Task 4: Layered PF3 logout

Menu PF3 now logs off (back to the login screen) instead of disconnecting; login PF3 still disconnects. **Every existing session test that ends with a menu `{quit: true}` now re-renders the login screen**, so each needs a terminating `{quit: true}` login step — otherwise the fake panics popping an empty `logins` slice.

**Files:**
- Modify: `internal/server/session.go` (`Run` outer loop)
- Modify: `internal/screens/menu.go` (help text)
- Test: `internal/server/session_test.go`, `internal/screens/menu_test.go`

- [ ] **Step 1: Write the new failing tests**

Append to `internal/server/session_test.go`:

```go
func TestSessionMenuQuitLogsOffToLogin(t *testing.T) {
	// PF3 at the menu logs off (back to login); PF3 at login disconnects.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render: user disconnects
		},
		menuPicks: []menuResult{{quit: true}},
	}
	b := &fakeBridger{}
	s := newTestSession(t, p, b)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.logins) != 0 {
		t.Errorf("menu quit should re-render login; %d logins left", len(p.logins))
	}
	// both login renders are pristine (no logoff notice — spec decision)
	if len(p.loginErrors) != 2 || p.loginErrors[0] != "" || p.loginErrors[1] != "" {
		t.Errorf("login renders = %q, want two empty messages", p.loginErrors)
	}
	if b.calls != 0 {
		t.Errorf("bridge calls = %d, want 0", b.calls)
	}
}

func TestSessionReloginRecomputesAdmin(t *testing.T) {
	// root (ZZADMIN) logs off; alice (ops) logs in: the A entry must vanish.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "root", pass: "good"},
			{user: "alice", pass: "good"},
			{quit: true},
		},
		menuPicks: []menuResult{{quit: true}, {quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.AdminPresenter = &fakeAdminPresenter{}
	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)
	if len(p.gotAdminFlag) != 2 || !p.gotAdminFlag[0] || p.gotAdminFlag[1] {
		t.Errorf("admin flags = %v, want [true false]", p.gotAdminFlag)
	}
}
```

Append to `internal/screens/menu_test.go`:

```go
func TestMenuScreenHelpSaysLogoff(t *testing.T) {
	screen, _ := MenuScreen(nil, false, "")
	if !screenContains(screen, "PF3 = logoff") {
		t.Errorf("menu help should say PF3 = logoff")
	}
}
```

- [ ] **Step 2: Update the existing session tests for the new layering**

In each of these tests, append `{quit: true}` to the `logins` slice (the menu logoff now renders login one more time; the user then quits to end the session):

- `TestSessionLoginRetryThenQuit` — `logins` becomes `{alice/bad}, {alice/good}, {quit: true}`
- `TestSessionEscapeReturnsToMenu`
- `TestSessionBackendErrorShownOnMenu`
- `TestSessionPassesTLSIntentToBridger`
- `TestSessionAdminFlagFollowsGroup`
- `TestSessionAdminSelectionRunsFlowAndReturnsToMenu`
- `TestSessionNonAdminAdminSelIgnored`

`TestSessionClientClosedEndsSession` is unchanged (the session ends from the bridge cause, never reaching a menu quit).

- [ ] **Step 3: Run tests to verify the new ones fail**

Run: `go test ./internal/server/ -run TestSession -v && go test ./internal/screens/ -run TestMenuScreenHelp -v`
Expected: `TestSessionMenuQuitLogsOffToLogin`, `TestSessionReloginRecomputesAdmin`, and `TestMenuScreenHelpSaysLogoff` FAIL (today menu quit returns immediately, leaving the extra login unconsumed; help text still says "disconnect"). `TestSessionLoginRetryThenQuit` also fails its `len(p.logins) != 0` assertion — expected at this point.

- [ ] **Step 4: Implement — `Session.Run` outer loop**

In `internal/server/session.go`, replace `Run` with:

```go
// Run executes the session state machine for one connection. It returns when
// the user disconnects or an unrecoverable error occurs. It does not close conn
// (the caller owns it).
func (s *Session) Run(conn net.Conn) {
	ctx := context.Background()

	termType, err := s.Presenter.Negotiate(conn)
	if err != nil {
		log.Printf("telnet negotiation failed: %v", err)
		return
	}

	// Each outer iteration is one login → menu lifetime: PF3 at the menu logs
	// off (back to the login screen); PF3 at the login screen disconnects.
	// Re-login re-evaluates groups, so a demoted admin loses the A entry at
	// logoff.
	for {
		identity, ok := s.doLogin(ctx, conn)
		if !ok {
			return
		}

		isAdmin := s.AdminPresenter != nil && slices.Contains(identity.Groups, store.AdminGroup)
		errMsg := ""
	menu:
		for {
			services, err := s.Store.ListServicesForGroups(ctx, identity.Groups)
			if err != nil {
				log.Printf("listing services for user %s failed: %v", identity.Username, err)
				services = nil
				errMsg = "Temporary error retrieving services; try again"
			}
			selected, adminSel, quit, err := s.Presenter.Menu(conn, services, isAdmin, errMsg)
			if err != nil {
				return
			}
			if quit {
				break menu // logoff: back to the login screen
			}
			errMsg = ""
			if adminSel && isAdmin {
				flow := &adminFlow{store: s.Store, presenter: s.AdminPresenter, identity: identity}
				if aerr := flow.Run(ctx, conn); aerr != nil {
					log.Printf("admin flow for %s ended: %v", identity.Username, aerr)
					return
				}
				continue // re-render the menu: fresh service list shows admin edits
			}
			if selected == nil {
				continue
			}

			addr := net.JoinHostPort(selected.Host, strconv.Itoa(selected.Port))
			btls := BackendTLS{Enabled: selected.TLS, Verify: selected.TLSVerify}
			cause, berr := s.Bridger.Bridge(conn, addr, termType, s.EscapeAID, btls)
			switch cause {
			case bridge.CauseClientClosed:
				return
			case bridge.CauseError:
				log.Printf("bridge error to %s (%s): %v", selected.Name, addr, berr)
				errMsg = "Could not connect to " + selected.Name
				if berr == nil {
					errMsg = "Session error on " + selected.Name
				}
			default:
				// CauseBackendClosed or CauseUserEscaped → back to the menu.
			}
		}
	}
}
```

(`doLogin` is unchanged.)

- [ ] **Step 5: Implement — menu help text**

In `internal/screens/menu.go`, change the help line to:

```go
		go3270.Field{Row: 23, Col: 2, Content: "Enter = connect    PF3 = logoff    (PA3 returns here from a session)"},
```

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go test ./... -race`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/server/session.go internal/server/session_test.go internal/screens/menu.go internal/screens/menu_test.go
git commit -m "feat(server): layered PF3 — menu logs off to login, login disconnects"
```

---

### Task 5: Docs, final verification, manual smoke test

**Files:**
- Modify: `CLAUDE.md` (PF3/PA3 behavior description)
- Modify: `docs/superpowers/ROADMAP.md` (#3.5 status)

- [ ] **Step 1: Update `CLAUDE.md`**

In the "What this is" section, replace the sentence

> During a bridged session, **PA3** returns the user to the menu; **PF3** at the menu disconnects.

with:

> During a bridged session, **PA3** returns the user to the menu (PA3 does nothing
> on the proxy's own screens). **PF3** uniformly steps back one level: admin
> sub-screen → admin menu → service menu → login screen → disconnect; PF3 at the
> service menu is a logoff, and re-login re-evaluates groups.

- [ ] **Step 2: Update `docs/superpowers/ROADMAP.md`**

Change the `## 3.5 Admin / menu UX refinements` heading to:

```markdown
## 3.5 Admin / menu UX refinements  ✅ **DONE** *(merged; live smoke test pending)*

> **Completed.** Spec: `docs/superpowers/specs/2026-06-03-admin-menu-ux-design.md`;
> plan: `docs/superpowers/plans/2026-06-03-admin-menu-ux.md`. Delivered: `M` line
> command on the GROUPS list opening a members toggle view (`store.ListUsersInGroup`,
> last-ZZADMIN guard applies from both sides); PA3 removed from all admin screens
> (`bail` plumbing deleted — PF3 walks up one level, PA3 stays bridge-only); layered
> PF3 (menu PF3 = logoff to login screen, login PF3 = disconnect; re-login
> re-evaluates groups). Run the manual smoke checklist below in a real emulator,
> then drop the "smoke test pending" qualifier. The historical notes below are
> retained for reference.
```

Also update the **Progress** paragraph near the top: change "**Next up: #3.5 (admin/menu UX refinements).**" to "**Next up: #5 (audit logging).**" and add 3.5 to the completed list ("Milestones **1, 2, 3, and 3.5 are complete**" — keep the existing #2-TLS-smoke-test carried-over-debt note as is).

- [ ] **Step 3: Full verification**

Run: `go build ./... && go test ./... -race && go vet ./...`
Expected: clean build, all tests PASS, no vet findings.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/superpowers/ROADMAP.md
git commit -m "docs: roadmap #3.5 implemented; CLAUDE.md reflects layered PF3 + admin PA3 removal"
```

- [ ] **Step 5: Manual smoke test (real emulator — protocol surface)**

Unit tests don't cover cursor placement, key handling, or real negotiation
(see CLAUDE.md gotchas). With a seeded DB (`seed.example.json` has an admin) and
`c3270 127.0.0.1:2323`:

1. **Members view:** login as admin → `A` → `2` (groups) → `M` on a group →
   markers correct → `A` a non-member, `R` a member → markers update →
   `R` the last ZZADMIN member → blocked with `CANNOT REMOVE LAST ZZADMIN MEMBER`.
2. **PA3 inert in admin:** on the admin menu, a list, and a form, press PA3 →
   screen re-presents, nothing navigates. Help lines nowhere mention PA3.
3. **PA3 still escapes the bridge:** connect to a service, press PA3 → back at
   the service menu.
4. **PF3 layering walk:** members view → PF3 → groups list → PF3 → admin menu →
   PF3 → service menu (help says `PF3 = logoff`) → PF3 → login screen (pristine,
   no message) → PF3 → disconnected.
5. **Demotion at logoff:** as a second admin, remove the first admin's ZZADMIN
   membership; the first admin presses PF3 to the login screen, re-logs-in →
   no `A` entry.
6. Cursor lands correctly on every screen's primary input (gotcha: col + 1).

If all six pass, remove the "live smoke test pending" qualifier from the ROADMAP
heading and commit (`docs: roadmap — #3.5 smoke test passed`).
