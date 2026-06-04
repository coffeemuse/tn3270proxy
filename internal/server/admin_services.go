package server

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func yn(b bool) string {
	if b {
		return "Y"
	}
	return "N"
}

// ynBool parses an upper-cased Y/N flag; ok is false for anything else.
func ynBool(s string) (val, ok bool) {
	switch s {
	case "Y":
		return true, true
	case "N":
		return false, true
	}
	return false, false
}

// services drives the service list and its sub-screens.
func (f *adminFlow) services(ctx context.Context, conn net.Conn) error {
	page, errMsg := 0, ""
	var pendingDelete *store.Service
	for {
		svcs, err := f.store.ListAllServices(ctx)
		if err != nil {
			errMsg = logStoreErr("list services", err)
			svcs = nil
			pendingDelete = nil // confirm lost; user must re-initiate D
		}
		var start, end int
		var rowInfo string
		page, start, end, rowInfo = f.pageBounds(page, len(svcs))
		pageSvcs := svcs[start:end]
		rows := make([]string, len(pageSvcs))
		for i, s := range pageSvcs {
			rows[i] = fmt.Sprintf("%-8s %-20.20s %-18s %-3s %s",
				s.Name, s.Description, fmt.Sprintf("%s:%d", s.Host, s.Port), yn(s.TLS), yn(s.TLSVerify))
		}
		act, err := f.presenter.AdminList(conn, f.term, screens.AdminListView{
			Title:   "TN3270 GATEWAY ADMIN: SERVICES",
			RowInfo: rowInfo,
			Header:  "CMD  NAME     DESCRIPTION          HOST:PORT          TLS VERIFY",
			Rows:    rows,
			Legend:  "S = edit   G = group access   D = delete   PF4 = add service",
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
				if err := f.store.DeleteService(ctx, target.ID); err != nil {
					errMsg = logStoreErr("delete service", err)
				} else {
					f.recordAdmin(ctx, "service delete "+target.Name)
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
			if err := f.serviceForm(ctx, conn, nil); err != nil {
				return err
			}
		case act.PF == 7:
			page--
		case act.PF == 8:
			if end < len(svcs) {
				page++
			}
		case act.Cmd != 0:
			if act.Row >= len(pageSvcs) {
				continue
			}
			s := pageSvcs[act.Row]
			switch act.Cmd {
			case 'S':
				if err := f.serviceForm(ctx, conn, &s); err != nil {
					return err
				}
			case 'G':
				if err := f.serviceGroups(ctx, conn, s); err != nil {
					return err
				}
			case 'D':
				pendingDelete = &s
				errMsg = fmt.Sprintf("ENTER = CONFIRM DELETE OF '%s', PF3 = CANCEL", s.Name)
			default:
				errMsg = "INVALID COMMAND: " + string(act.Cmd)
			}
		}
	}
}

// serviceForm adds (existing == nil) or edits a service. Both TLS fields are
// exposed: tls (encrypt) and verify (authenticate the backend cert).
func (f *adminFlow) serviceForm(ctx context.Context, conn net.Conn, existing *store.Service) error {
	title := "TN3270 GATEWAY ADMIN: ADD SERVICE"
	name, description, host, port, tlsYN, verifyYN := "", "", "", "", "N", "Y" // verify defaults on (secure default)
	if existing != nil {
		title = "TN3270 GATEWAY ADMIN: EDIT SERVICE"
		name, description, host, port = existing.Name, existing.Description, existing.Host, strconv.Itoa(existing.Port)
		tlsYN, verifyYN = yn(existing.TLS), yn(existing.TLSVerify)
	}
	errMsg := ""
	for {
		act, err := f.presenter.AdminForm(conn, f.term, screens.AdminFormView{
			Title: title,
			Fields: []screens.AdminFormField{
				{Name: screens.FieldName, Label: "Name . . . .", Value: name, Length: 8},
				{Name: screens.FieldDescription, Label: "Descr. . . .", Value: description, Length: 40},
				{Name: screens.FieldHost, Label: "Host . . . .", Value: host, Length: 48},
				{Name: screens.FieldPort, Label: "Port . . . .", Value: port, Length: 5},
				{Name: screens.FieldTLS, Label: "TLS (Y/N) .", Value: tlsYN, Length: 1},
				{Name: screens.FieldVerify, Label: "Verify (Y/N)", Value: verifyYN, Length: 1},
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
		description = act.Values[screens.FieldDescription]
		host = act.Values[screens.FieldHost]
		port = act.Values[screens.FieldPort]
		tlsYN = strings.ToUpper(act.Values[screens.FieldTLS])
		verifyYN = strings.ToUpper(act.Values[screens.FieldVerify])

		p, perr := strconv.Atoi(port)
		tlsB, tlsOK := ynBool(tlsYN)
		verifyB, verifyOK := ynBool(verifyYN)
		normName, nameErr := store.NormalizeServiceName(name)
		descErr := store.ValidateDescription(description)
		switch {
		case nameErr != nil:
			errMsg = strings.ToUpper(nameErr.Error())
		case descErr != nil:
			errMsg = strings.ToUpper(descErr.Error())
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
	}
}

// checkServiceNameFree pre-checks the UNIQUE service name. CreateService is
// INSERT OR IGNORE (silent no-op on collision); UpdateService would fail
// loudly, but the screen message is friendlier. Renames skip the row itself.
func (f *adminFlow) checkServiceNameFree(ctx context.Context, name string, existing *store.Service) string {
	svcs, err := f.store.ListAllServices(ctx)
	if err != nil {
		return logStoreErr("list services", err)
	}
	for _, s := range svcs {
		if s.Name == name && (existing == nil || s.ID != existing.ID) {
			return "'" + name + "' ALREADY EXISTS"
		}
	}
	return ""
}

// serviceGroups shows every group with an X access marker for svc; line
// command A grants access, R revokes. No guardrails — revoking all access
// only hides the service from menus.
func (f *adminFlow) serviceGroups(ctx context.Context, conn net.Conn, svc store.Service) error {
	page, errMsg := 0, ""
	for {
		groups, err := f.store.ListGroups(ctx)
		if err != nil {
			errMsg = logStoreErr("list groups", err)
			groups = nil
		}
		linked, err := f.store.ListGroupsForService(ctx, svc.ID)
		if err != nil {
			errMsg = logStoreErr("list service groups", err)
		}
		linkSet := make(map[int64]bool, len(linked))
		for _, g := range linked {
			linkSet[g.ID] = true
		}
		var start, end int
		var rowInfo string
		page, start, end, rowInfo = f.pageBounds(page, len(groups))
		pageGroups := groups[start:end]
		rows := make([]string, len(pageGroups))
		for i, g := range pageGroups {
			marker := ""
			if linkSet[g.ID] {
				marker = "X"
			}
			rows[i] = fmt.Sprintf("%-20s %s", g.Name, marker)
		}
		act, err := f.presenter.AdminList(conn, f.term, screens.AdminListView{
			Title:   "TN3270 GATEWAY ADMIN: ACCESS TO " + svc.Name,
			RowInfo: rowInfo,
			Header:  "CMD  GROUP                ACCESS",
			Rows:    rows,
			Legend:  "A = grant access   R = revoke access",
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
				if err := f.store.LinkGroupService(ctx, g.ID, svc.ID); err != nil {
					errMsg = logStoreErr("grant access", err)
				} else {
					f.recordAdmin(ctx, "service "+svc.Name+" grant "+g.Name)
				}
			case 'R':
				if err := f.store.UnlinkGroupService(ctx, g.ID, svc.ID); err != nil {
					errMsg = logStoreErr("revoke access", err)
				} else {
					f.recordAdmin(ctx, "service "+svc.Name+" revoke "+g.Name)
				}
			default:
				errMsg = "INVALID COMMAND: " + string(act.Cmd)
			}
		}
	}
}
