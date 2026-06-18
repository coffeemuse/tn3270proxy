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
	"fmt"
	"net"
	"strings"

	"github.com/coffeemuse/tn3270proxy/internal/screens"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
)

// isReservedGroup reports whether name is in the app-dictated ZZ* namespace
// (case-insensitive): the admin UI can neither create nor delete such groups.
func isReservedGroup(name string) bool {
	return strings.HasPrefix(strings.ToUpper(name), store.ReservedGroupPrefix)
}

// groups drives the group list and the add-group form.
func (f *adminFlow) groups(ctx context.Context, conn net.Conn) error {
	r := f.renderer(conn)
	return ui3270.RunList(ctx, r, ui3270.ListConfig[store.Group]{
		Title:  "TN3270 GATEWAY ADMIN: GROUPS",
		Header: "CMD GROUP                MEMBERS  SERVICES",
		Legend: "M = members   D = delete   PF4 = add group",
		PFHelp: "Enter = process   PF7/PF8 = page   PF3 = admin menu",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.Row[store.Group], string) {
			groups, err := f.store.ListGroups(ctx)
			if err != nil {
				return nil, f.storeErr("list groups", err)
			}
			rows := make([]ui3270.Row[store.Group], len(groups))
			for i, g := range groups {
				members, merr := f.store.CountGroupMembers(ctx, g.ID)
				services, serr := f.store.CountGroupServices(ctx, g.ID)
				if merr != nil || serr != nil {
					members, services = 0, 0
				}
				rows[i] = ui3270.Row[store.Group]{
					Display: fmt.Sprintf("%-20s %7d  %8d", g.Name, members, services), Item: g,
				}
			}
			return rows, ""
		},
		Add: func(ctx context.Context, r ui3270.Renderer) (string, error) { return "", f.groupAdd(ctx, r) },
		Cmds: []ui3270.Command[store.Group]{
			{Key: 'M', Commit: func(ctx context.Context, r ui3270.Renderer, g store.Group) (string, error) {
				return "", f.groupMembers(ctx, r, g)
			}},
			{Key: 'D',
				Confirm: func(g store.Group) (string, string) {
					if isReservedGroup(g.Name) {
						return "", "ZZ* GROUP NAMES ARE RESERVED"
					}
					return fmt.Sprintf("ENTER = CONFIRM DELETE OF '%s', PF3 = CANCEL", g.Name), ""
				},
				Commit: func(ctx context.Context, _ ui3270.Renderer, g store.Group) (string, error) {
					if err := f.store.DeleteGroup(ctx, g.ID); err != nil {
						return f.storeErr("delete group", err), nil
					}
					f.recordAdmin(ctx, "group delete "+g.Name)
					return "", nil
				}},
		},
	})
}

// groupMembers shows every user with an X membership marker for g; line
// command A adds the user to the group, R removes (guarded for the last
// ZZADMIN member). Membership is manageable from either side: this is the
// group-side mirror of userGroups.
func (f *adminFlow) groupMembers(ctx context.Context, r ui3270.Renderer, g store.Group) error {
	return ui3270.RunList(ctx, r, ui3270.ListConfig[store.User]{
		Title:  "TN3270 GATEWAY ADMIN: MEMBERS OF " + g.Name,
		Header: "CMD USERNAME         MEMBER",
		Legend: "A = add to group   R = remove from group",
		PFHelp: "PF3=Back    PF7=PgUp    PF8=PgDn",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.Row[store.User], string) {
			users, err := f.store.ListUsers(ctx)
			if err != nil {
				return nil, f.storeErr("list users", err)
			}
			members, merr := f.store.ListUsersInGroup(ctx, g.ID)
			errMsg := ""
			if merr != nil {
				errMsg = f.storeErr("list group members", merr)
			}
			memberSet := make(map[int64]bool, len(members))
			for _, m := range members {
				memberSet[m.ID] = true
			}
			rows := make([]ui3270.Row[store.User], len(users))
			for i, u := range users {
				marker := ""
				if memberSet[u.ID] {
					marker = "X"
				}
				rows[i] = ui3270.Row[store.User]{Display: fmt.Sprintf("%-16s %s", u.Username, marker), Item: u}
			}
			return rows, errMsg
		},
		Cmds: []ui3270.Command[store.User]{
			{Key: 'A', Commit: func(ctx context.Context, _ ui3270.Renderer, u store.User) (string, error) {
				if err := f.store.AddUserToGroup(ctx, u.ID, g.ID); err != nil {
					return f.storeErr("add membership", err), nil
				}
				f.recordAdmin(ctx, "group "+g.Name+" add-member "+u.Username)
				return "", nil
			}},
			{Key: 'R', Commit: func(ctx context.Context, _ ui3270.Renderer, u store.User) (string, error) {
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
				f.recordAdmin(ctx, "group "+g.Name+" remove-member "+u.Username)
				return "", nil
			}},
		},
	})
}

func (f *adminFlow) groupAdd(ctx context.Context, r ui3270.Renderer) error {
	// fields is declared as a local variable so a rejected submit re-seeds the
	// typed group name on the next render (RunForm re-sends the same slice each loop).
	fields := []ui3270.FormField{{Name: screens.FieldName, Label: "Group name .", Length: 32}}
	return ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title:  "TN3270 GATEWAY ADMIN: ADD GROUP",
		Fields: fields,
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			name := vals[screens.FieldName]
			fields[0].Value = name // preserve typed input on re-render
			if name == "" {
				return "GROUP NAME IS REQUIRED", nil
			}
			if isReservedGroup(name) {
				return "ZZ* GROUP NAMES ARE RESERVED", nil
			}
			if _, exists, err := f.groupIDByName(ctx, name); err != nil {
				return f.storeErr("check group", err), nil
			} else if exists {
				return "'" + name + "' ALREADY EXISTS", nil
			}
			if _, err := f.store.CreateGroup(ctx, name); err != nil {
				return f.storeErr("create group", err), nil
			}
			f.recordAdmin(ctx, "group create "+name)
			return "", nil
		},
	})
}
