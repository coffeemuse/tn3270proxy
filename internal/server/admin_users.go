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
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
)

// users drives the user list and its sub-screens.
func (f *adminFlow) users(ctx context.Context, conn net.Conn) error {
	r := f.renderer(conn)
	return ui3270.RunList(ctx, r, ui3270.ListConfig[store.User]{
		Title:  "TN3270 GATEWAY ADMIN: USERS",
		Header: "CMD  USERNAME         GROUPS",
		Legend: "S = edit user   G = groups   D = delete",
		PFHelp: "PF3=Admin Menu    PF4=Add User    PF7=PgUp    PF8=PgDn",
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
	})
}

// fetchUsers maps store users → display rows (username + comma-joined groups).
func (f *adminFlow) fetchUsers(ctx context.Context) ([]ui3270.Row[store.User], string) {
	users, err := f.store.ListUsers(ctx)
	if err != nil {
		return nil, f.storeErr("list users", err)
	}
	rows := make([]ui3270.Row[store.User], len(users))
	for i, u := range users {
		groups, gerr := f.store.GetUserGroups(ctx, u.ID)
		if gerr != nil {
			groups = nil
		}
		rows[i] = ui3270.Row[store.User]{
			Display: fmt.Sprintf("%-16s %s", u.Username, strings.Join(groups, ",")),
			Item:    u,
		}
	}
	return rows, ""
}

// deleteUser applies the lockout guardrails, then deletes. Returns the
// error-line message ("" on success).
func (f *adminFlow) deleteUser(ctx context.Context, u store.User) string {
	if u.ID == f.identity.UserID {
		return "CANNOT DELETE YOUR OWN ACCOUNT"
	}
	groups, err := f.store.GetUserGroups(ctx, u.ID)
	if err != nil {
		return f.storeErr("get user groups", err)
	}
	if slices.Contains(groups, store.AdminGroup) {
		if msg := f.guardLastAdmin(ctx); msg != "" {
			return msg
		}
	}
	if err := f.store.DeleteUser(ctx, u.ID); err != nil {
		return f.storeErr("delete user", err)
	}
	f.recordAdmin(ctx, "user delete "+u.Username)
	return ""
}

// guardLastAdmin returns a blocking message when ZZADMIN has a single member
// (removing or deleting that member would lock every admin out).
func (f *adminFlow) guardLastAdmin(ctx context.Context) string {
	gid, ok, err := f.groupIDByName(ctx, store.AdminGroup)
	if err != nil {
		return f.storeErr("find admin group", err)
	}
	if !ok {
		return "" // unreachable: migrate() creates ZZADMIN
	}
	n, err := f.store.CountGroupMembers(ctx, gid)
	if err != nil {
		return f.storeErr("count admin members", err)
	}
	if n <= 1 {
		return "CANNOT REMOVE LAST " + store.AdminGroup + " MEMBER"
	}
	return ""
}

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
			if create {
				fields[0].Value = vals[screens.FieldUsername] // preserve typed username on re-render
			}
			if err := store.ValidateFullName(fullNameVal); err != nil {
				return "FULL NAME TOO LONG (MAX 40)", nil
			}
			if err := store.ValidateEmail(emailVal); err != nil {
				return "INVALID EMAIL ADDRESS", nil
			}
			if create {
				return f.userCreate(ctx, vals, fullNameVal, emailVal)
			}
			return f.userSaveEdit(ctx, *u, vals, fullNameVal, emailVal)
		},
	})
}

// userCreate handles the create-mode submit: requires + confirms password,
// rejects duplicates, then creates the user and writes the optional details.
func (f *adminFlow) userCreate(ctx context.Context, vals map[string]string, fullName, email string) (string, error) {
	username := vals[screens.FieldUsername]
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
		return f.storeErr("check user", err), nil
	}
	hash, err := auth.HashPassword(pass)
	if err != nil {
		return f.storeErr("hash password", err), nil
	}
	uid, err := f.store.CreateUser(ctx, username, hash)
	if err != nil {
		return f.storeErr("create user", err), nil
	}
	if err := f.store.UpdateUserDetails(ctx, uid, fullName, email); err != nil {
		return f.storeErr("set user details", err), nil
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
			return f.storeErr("hash password", err), nil
		}
		if err := f.store.SetPassword(ctx, u.ID, hash); err != nil {
			return f.storeErr("set password", err), nil
		}
	}
	if err := f.store.UpdateUserDetails(ctx, u.ID, fullName, email); err != nil {
		return f.storeErr("set user details", err), nil
	}
	f.recordAdmin(ctx, "user edit "+u.Username)
	return "", nil
}

// userGroups shows every group with an X membership marker; line command A
// adds the user, R removes (guarded for the last ZZADMIN member).
func (f *adminFlow) userGroups(ctx context.Context, r ui3270.Renderer, u store.User) error {
	return ui3270.RunList(ctx, r, ui3270.ListConfig[store.Group]{
		Title:  "TN3270 GATEWAY ADMIN: GROUPS FOR " + u.Username,
		Header: "CMD  GROUP                MEMBER",
		Legend: "A = add to group   R = remove from group",
		PFHelp: "Enter = process   PF7/PF8 = page   PF3 = back",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.Row[store.Group], string) {
			groups, err := f.store.ListGroups(ctx)
			if err != nil {
				return nil, f.storeErr("list groups", err)
			}
			memberOf, gerr := f.store.GetUserGroups(ctx, u.ID)
			errMsg := ""
			if gerr != nil {
				errMsg = f.storeErr("get user groups", gerr)
			}
			member := make(map[string]bool, len(memberOf))
			for _, name := range memberOf {
				member[name] = true
			}
			rows := make([]ui3270.Row[store.Group], len(groups))
			for i, g := range groups {
				marker := ""
				if member[g.Name] {
					marker = "X"
				}
				rows[i] = ui3270.Row[store.Group]{Display: fmt.Sprintf("%-20s %s", g.Name, marker), Item: g}
			}
			return rows, errMsg
		},
		Cmds: []ui3270.Command[store.Group]{
			{Key: 'A', Commit: func(ctx context.Context, _ ui3270.Renderer, g store.Group) (string, error) {
				if err := f.store.AddUserToGroup(ctx, u.ID, g.ID); err != nil {
					return f.storeErr("add membership", err), nil
				}
				f.recordAdmin(ctx, "user "+u.Username+" add-group "+g.Name)
				return "", nil
			}},
			{Key: 'R', Commit: func(ctx context.Context, _ ui3270.Renderer, g store.Group) (string, error) {
				if g.Name == store.AdminGroup {
					// Last-admin guard first: a sole admin self-removing gets the
					// more informative "last admin" message; the self-demotion
					// guard then catches the ≥2-admins fat-finger case.
					if msg := f.guardLastAdmin(ctx); msg != "" {
						return msg, nil
					}
					if u.ID == f.identity.UserID {
						return "CANNOT REMOVE YOUR OWN ADMIN MEMBERSHIP", nil
					}
				}
				if err := f.store.RemoveUserFromGroup(ctx, u.ID, g.ID); err != nil {
					return f.storeErr("remove membership", err), nil
				}
				f.recordAdmin(ctx, "user "+u.Username+" remove-group "+g.Name)
				return "", nil
			}},
		},
	})
}
