package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// users drives the user list and its sub-screens. Returns bail=true when the
// user pressed PA3 (straight back to the service menu).
func (f *adminFlow) users(ctx context.Context, conn net.Conn) (bool, error) {
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
			PFHelp:  "Enter = process   PF7/PF8 = page   PF3 = admin menu   PA3 = main menu",
		})
		if err != nil {
			return false, err
		}
		errMsg = ""

		// A pending delete is resolved by the very next action: plain Enter
		// confirms, PF3 cancels (stays on the list), anything else cancels and
		// is processed normally.
		if pendingDelete != nil {
			target := *pendingDelete
			pendingDelete = nil
			switch {
			case act.PA3:
				return true, nil
			case act.Cmd == 0 && act.PF == 0:
				errMsg = f.deleteUser(ctx, target)
				continue
			case act.PF == 3:
				continue
			}
		}

		switch {
		case act.PA3:
			return true, nil
		case act.PF == 3:
			return false, nil
		case act.PF == 4:
			bail, err := f.userAdd(ctx, conn)
			if err != nil || bail {
				return bail, err
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
				bail, err := f.setPassword(ctx, conn, u)
				if err != nil || bail {
					return bail, err
				}
			case 'G':
				bail, err := f.userGroups(ctx, conn, u)
				if err != nil || bail {
					return bail, err
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

// deleteUser applies the lockout guardrails, then deletes. Returns the
// error-line message ("" on success).
func (f *adminFlow) deleteUser(ctx context.Context, u store.User) string {
	if u.ID == f.identity.UserID {
		return "CANNOT DELETE YOUR OWN ACCOUNT"
	}
	groups, err := f.store.GetUserGroups(ctx, u.ID)
	if err != nil {
		return logStoreErr("get user groups", err)
	}
	if slices.Contains(groups, store.AdminGroup) {
		if msg := f.guardLastAdmin(ctx); msg != "" {
			return msg
		}
	}
	if err := f.store.DeleteUser(ctx, u.ID); err != nil {
		return logStoreErr("delete user", err)
	}
	return ""
}

// guardLastAdmin returns a blocking message when ZZADMIN has a single member
// (removing or deleting that member would lock every admin out).
func (f *adminFlow) guardLastAdmin(ctx context.Context) string {
	gid, ok, err := f.groupIDByName(ctx, store.AdminGroup)
	if err != nil {
		return logStoreErr("find admin group", err)
	}
	if !ok {
		return "" // unreachable: migrate() creates ZZADMIN
	}
	n, err := f.store.CountGroupMembers(ctx, gid)
	if err != nil {
		return logStoreErr("count admin members", err)
	}
	if n <= 1 {
		return "CANNOT REMOVE LAST " + store.AdminGroup + " MEMBER"
	}
	return ""
}

// passwordFromForm validates the password/retype pair from form values,
// returning the password or an error-line message. Passwords are never
// trimmed, logged, or echoed.
func passwordFromForm(values map[string]string) (string, string) {
	pass, retype := values[screens.FieldPassword], values[screens.FieldRetype]
	if pass == "" {
		return "", "PASSWORD IS REQUIRED"
	}
	if pass != retype {
		return "", "PASSWORDS DO NOT MATCH"
	}
	return pass, ""
}

func (f *adminFlow) userAdd(ctx context.Context, conn net.Conn) (bool, error) {
	username, errMsg := "", ""
	for {
		act, err := f.presenter.AdminForm(conn, screens.AdminFormView{
			Title: "TN3270 GATEWAY ADMIN: ADD USER",
			Fields: []screens.AdminFormField{
				{Name: screens.FieldUsername, Label: "Userid . . .", Value: username, Length: 32},
				{Name: screens.FieldPassword, Label: "Password . .", Hidden: true, Length: 32},
				{Name: screens.FieldRetype, Label: "Retype . . .", Hidden: true, Length: 32},
			},
			ErrMsg: errMsg,
		})
		if err != nil {
			return false, err
		}
		if act.PA3 {
			return true, nil
		}
		if act.Cancel {
			return false, nil
		}
		username = act.Values[screens.FieldUsername]
		if username == "" {
			errMsg = "USERID IS REQUIRED"
			continue
		}
		pass, msg := passwordFromForm(act.Values)
		if msg != "" {
			errMsg = msg
			continue
		}
		// Pre-check: CreateUser is INSERT OR IGNORE and would silently no-op.
		if _, err := f.store.GetUserByUsername(ctx, username); err == nil {
			errMsg = "'" + username + "' ALREADY EXISTS"
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			errMsg = logStoreErr("check user", err)
			continue
		}
		hash, err := auth.HashPassword(pass)
		if err != nil {
			errMsg = logStoreErr("hash password", err)
			continue
		}
		if _, err := f.store.CreateUser(ctx, username, hash); err != nil {
			errMsg = logStoreErr("create user", err)
			continue
		}
		return false, nil
	}
}

func (f *adminFlow) setPassword(ctx context.Context, conn net.Conn, u store.User) (bool, error) {
	errMsg := ""
	for {
		act, err := f.presenter.AdminForm(conn, screens.AdminFormView{
			Title: "TN3270 GATEWAY ADMIN: SET PASSWORD FOR " + u.Username,
			Fields: []screens.AdminFormField{
				{Name: screens.FieldPassword, Label: "Password . .", Hidden: true, Length: 32},
				{Name: screens.FieldRetype, Label: "Retype . . .", Hidden: true, Length: 32},
			},
			ErrMsg: errMsg,
		})
		if err != nil {
			return false, err
		}
		if act.PA3 {
			return true, nil
		}
		if act.Cancel {
			return false, nil
		}
		pass, msg := passwordFromForm(act.Values)
		if msg != "" {
			errMsg = msg
			continue
		}
		hash, err := auth.HashPassword(pass)
		if err != nil {
			errMsg = logStoreErr("hash password", err)
			continue
		}
		if err := f.store.SetPassword(ctx, u.ID, hash); err != nil {
			errMsg = logStoreErr("set password", err)
			continue
		}
		return false, nil
	}
}

// userGroups shows every group with an X membership marker; line command A
// adds the user, R removes (guarded for the last ZZADMIN member).
func (f *adminFlow) userGroups(ctx context.Context, conn net.Conn, u store.User) (bool, error) {
	page, errMsg := 0, ""
	for {
		groups, err := f.store.ListGroups(ctx)
		if err != nil {
			errMsg = logStoreErr("list groups", err)
			groups = nil
		}
		memberOf, err := f.store.GetUserGroups(ctx, u.ID)
		if err != nil {
			errMsg = logStoreErr("get user groups", err)
		}
		member := make(map[string]bool, len(memberOf))
		for _, name := range memberOf {
			member[name] = true
		}
		var start, end int
		var rowInfo string
		page, start, end, rowInfo = pageBounds(page, len(groups))
		pageGroups := groups[start:end]
		rows := make([]string, len(pageGroups))
		for i, g := range pageGroups {
			marker := ""
			if member[g.Name] {
				marker = "X"
			}
			rows[i] = fmt.Sprintf("%-20s %s", g.Name, marker)
		}
		act, err := f.presenter.AdminList(conn, screens.AdminListView{
			Title:   "TN3270 GATEWAY ADMIN: GROUPS FOR " + u.Username,
			RowInfo: rowInfo,
			Header:  "CMD  GROUP                MEMBER",
			Rows:    rows,
			Legend:  "A = add to group   R = remove from group",
			ErrMsg:  errMsg,
			PFHelp:  "Enter = process   PF7/PF8 = page   PF3 = back   PA3 = main menu",
		})
		if err != nil {
			return false, err
		}
		errMsg = ""
		switch {
		case act.PA3:
			return true, nil
		case act.PF == 3:
			return false, nil
		case act.PF == 7:
			page--
		case act.PF == 8:
			if end < len(groups) {
				page++
			}
		case act.Cmd != 0:
			if act.Row >= len(pageGroups) {
				continue
			}
			g := pageGroups[act.Row]
			switch act.Cmd {
			case 'A':
				if err := f.store.AddUserToGroup(ctx, u.ID, g.ID); err != nil {
					errMsg = logStoreErr("add membership", err)
				}
			case 'R':
				if g.Name == store.AdminGroup && member[g.Name] {
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
