package server

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// isReservedGroup reports whether name is in the app-dictated ZZ* namespace
// (case-insensitive): the admin UI can neither create nor delete such groups.
func isReservedGroup(name string) bool {
	return strings.HasPrefix(strings.ToUpper(name), store.ReservedGroupPrefix)
}

// groups drives the group list and the add-group form.
func (f *adminFlow) groups(ctx context.Context, conn net.Conn) error {
	page, errMsg := 0, ""
	var pendingDelete *store.Group
	for {
		groups, err := f.store.ListGroups(ctx)
		if err != nil {
			errMsg = logStoreErr("list groups", err)
			groups = nil
			pendingDelete = nil // confirm lost; user must re-initiate D
		}
		var start, end int
		var rowInfo string
		page, start, end, rowInfo = pageBounds(page, len(groups))
		pageGroups := groups[start:end]
		rows := make([]string, len(pageGroups))
		for i, g := range pageGroups {
			members, merr := f.store.CountGroupMembers(ctx, g.ID)
			services, serr := f.store.CountGroupServices(ctx, g.ID)
			if merr != nil || serr != nil {
				members, services = 0, 0
			}
			rows[i] = fmt.Sprintf("%-20s %7d  %8d", g.Name, members, services)
		}
		act, err := f.presenter.AdminList(conn, screens.AdminListView{
			Title:   "TN3270 GATEWAY ADMIN: GROUPS",
			RowInfo: rowInfo,
			Header:  "CMD  GROUP                MEMBERS  SERVICES",
			Rows:    rows,
			Legend:  "D = delete   PF4 = add group",
			ErrMsg:  errMsg,
			PFHelp:  "Enter = process   PF7/PF8 = page   PF3 = admin menu",
		})
		if err != nil {
			return err
		}
		errMsg = ""

		if pendingDelete != nil {
			target := *pendingDelete
			pendingDelete = nil
			switch {
			case act.Cmd == 0 && act.PF == 0:
				if err := f.store.DeleteGroup(ctx, target.ID); err != nil {
					errMsg = logStoreErr("delete group", err)
				}
				continue
			case act.PF == 3:
				continue
			}
		}

		switch {
		case act.PF == 3:
			return nil
		case act.PF == 4:
			if err := f.groupAdd(ctx, conn); err != nil {
				return err
			}
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
			case 'D':
				if isReservedGroup(g.Name) {
					errMsg = "ZZ* GROUP NAMES ARE RESERVED"
					continue
				}
				pendingDelete = &g
				errMsg = fmt.Sprintf("ENTER = CONFIRM DELETE OF '%s', PF3 = CANCEL", g.Name)
			default:
				errMsg = "INVALID COMMAND: " + string(act.Cmd)
			}
		}
	}
}

func (f *adminFlow) groupAdd(ctx context.Context, conn net.Conn) error {
	name, errMsg := "", ""
	for {
		act, err := f.presenter.AdminForm(conn, screens.AdminFormView{
			Title: "TN3270 GATEWAY ADMIN: ADD GROUP",
			Fields: []screens.AdminFormField{
				{Name: screens.FieldName, Label: "Group name .", Value: name, Length: 32},
			},
			ErrMsg: errMsg,
		})
		if err != nil {
			return err
		}
		if act.Cancel {
			return nil
		}
		name = act.Values[screens.FieldName]
		if name == "" {
			errMsg = "GROUP NAME IS REQUIRED"
			continue
		}
		if isReservedGroup(name) {
			errMsg = "ZZ* GROUP NAMES ARE RESERVED"
			continue
		}
		if _, exists, err := f.groupIDByName(ctx, name); err != nil {
			errMsg = logStoreErr("check group", err)
			continue
		} else if exists {
			errMsg = "'" + name + "' ALREADY EXISTS"
			continue
		}
		if _, err := f.store.CreateGroup(ctx, name); err != nil {
			errMsg = logStoreErr("create group", err)
			continue
		}
		return nil
	}
}
