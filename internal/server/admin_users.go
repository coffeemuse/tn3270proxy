/*
 * Copyright 2026 by CoffeeMuse.
 *
 * This file is part of tn3270proxy.
 *
 * tn3270proxy is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * tn3270proxy is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with tn3270proxy. If not, see <https://www.gnu.org/licenses/>.
 */

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
		page, start, end, rowInfo = f.pageBounds(page, len(users))
		pageUsers := users[start:end]
		rows := make([]string, len(pageUsers))
		for i, u := range pageUsers {
			groups, gerr := f.store.GetUserGroups(ctx, u.ID)
			if gerr != nil {
				groups = nil
			}
			rows[i] = fmt.Sprintf("%-16s %s", u.Username, strings.Join(groups, ","))
		}
		act, err := f.presenter.AdminList(conn, f.term, screens.AdminListView{
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
	f.recordAdmin(ctx, "user delete "+u.Username)
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

func (f *adminFlow) userAdd(ctx context.Context, conn net.Conn) error {
	username, errMsg := "", ""
	for {
		act, err := f.presenter.AdminForm(conn, f.term, screens.AdminFormView{
			Title: "TN3270 GATEWAY ADMIN: ADD USER",
			Fields: []screens.AdminFormField{
				{Name: screens.FieldUsername, Label: "Userid . . .", Value: username, Length: 32},
				{Name: screens.FieldPassword, Label: "Password . .", Hidden: true, Length: 32},
				{Name: screens.FieldRetype, Label: "Retype . . .", Hidden: true, Length: 32},
			},
			ErrMsg: errMsg,
		})
		if err != nil {
			return err
		}
		if act.Cancel {
			return nil
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
		f.recordAdmin(ctx, "user create "+username)
		return nil
	}
}

func (f *adminFlow) setPassword(ctx context.Context, conn net.Conn, u store.User) error {
	errMsg := ""
	for {
		act, err := f.presenter.AdminForm(conn, f.term, screens.AdminFormView{
			Title: "TN3270 GATEWAY ADMIN: SET PASSWORD FOR " + u.Username,
			Fields: []screens.AdminFormField{
				{Name: screens.FieldPassword, Label: "Password . .", Hidden: true, Length: 32},
				{Name: screens.FieldRetype, Label: "Retype . . .", Hidden: true, Length: 32},
			},
			ErrMsg: errMsg,
		})
		if err != nil {
			return err
		}
		if act.Cancel {
			return nil
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
		f.recordAdmin(ctx, "user set-password "+u.Username)
		return nil
	}
}

// userGroups shows every group with an X membership marker; line command A
// adds the user, R removes (guarded for the last ZZADMIN member).
func (f *adminFlow) userGroups(ctx context.Context, conn net.Conn, u store.User) error {
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
		page, start, end, rowInfo = f.pageBounds(page, len(groups))
		pageGroups := groups[start:end]
		rows := make([]string, len(pageGroups))
		for i, g := range pageGroups {
			marker := ""
			if member[g.Name] {
				marker = "X"
			}
			rows[i] = fmt.Sprintf("%-20s %s", g.Name, marker)
		}
		act, err := f.presenter.AdminList(conn, f.term, screens.AdminListView{
			Title:   "TN3270 GATEWAY ADMIN: GROUPS FOR " + u.Username,
			RowInfo: rowInfo,
			Header:  "CMD  GROUP                MEMBER",
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
				} else {
					f.recordAdmin(ctx, "user "+u.Username+" add-group "+g.Name)
				}
			case 'R':
				if g.Name == store.AdminGroup && member[g.Name] {
					// Last-admin guard first: a sole admin self-removing gets the
					// more informative "last admin" message; the self-demotion
					// guard then catches the ≥2-admins fat-finger case.
					if msg := f.guardLastAdmin(ctx); msg != "" {
						errMsg = msg
						continue
					}
					if u.ID == f.identity.UserID {
						errMsg = "CANNOT REMOVE YOUR OWN ADMIN MEMBERSHIP"
						continue
					}
				}
				if err := f.store.RemoveUserFromGroup(ctx, u.ID, g.ID); err != nil {
					errMsg = logStoreErr("remove membership", err)
				} else {
					f.recordAdmin(ctx, "user "+u.Username+" remove-group "+g.Name)
				}
			default:
				errMsg = "INVALID COMMAND: " + string(act.Cmd)
			}
		}
	}
}
