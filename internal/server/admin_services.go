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
func (f *adminFlow) services(ctx context.Context, conn net.Conn) (bool, error) {
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
		page, start, end, rowInfo = pageBounds(page, len(svcs))
		pageSvcs := svcs[start:end]
		rows := make([]string, len(pageSvcs))
		for i, s := range pageSvcs {
			rows[i] = fmt.Sprintf("%-16s %-28s %-4s %s",
				s.Name, fmt.Sprintf("%s:%d", s.Host, s.Port), yn(s.TLS), yn(s.TLSVerify))
		}
		act, err := f.presenter.AdminList(conn, screens.AdminListView{
			Title:   "TN3270 GATEWAY ADMIN: SERVICES",
			RowInfo: rowInfo,
			Header:  "CMD  NAME             HOST:PORT                    TLS  VERIFY",
			Rows:    rows,
			Legend:  "S = edit   G = group access   D = delete   PF4 = add service",
			ErrMsg:  errMsg,
			PFHelp:  "Enter = process   PF7/PF8 = page   PF3 = admin menu   PA3 = main menu",
		})
		if err != nil {
			return false, err
		}
		errMsg = ""

		if pendingDelete != nil {
			target := *pendingDelete
			pendingDelete = nil
			switch {
			case act.PA3:
				return true, nil
			case act.Cmd == 0 && act.PF == 0:
				if err := f.store.DeleteService(ctx, target.ID); err != nil {
					errMsg = logStoreErr("delete service", err)
				}
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
			bail, err := f.serviceForm(ctx, conn, nil)
			if err != nil || bail {
				return bail, err
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
				bail, err := f.serviceForm(ctx, conn, &s)
				if err != nil || bail {
					return bail, err
				}
			case 'G':
				bail, err := f.serviceGroups(ctx, conn, s)
				if err != nil || bail {
					return bail, err
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
func (f *adminFlow) serviceForm(ctx context.Context, conn net.Conn, existing *store.Service) (bool, error) {
	title := "TN3270 GATEWAY ADMIN: ADD SERVICE"
	name, host, port, tlsYN, verifyYN := "", "", "", "N", "Y" // verify defaults on (secure default)
	if existing != nil {
		title = "TN3270 GATEWAY ADMIN: EDIT SERVICE"
		name, host, port = existing.Name, existing.Host, strconv.Itoa(existing.Port)
		tlsYN, verifyYN = yn(existing.TLS), yn(existing.TLSVerify)
	}
	errMsg := ""
	for {
		act, err := f.presenter.AdminForm(conn, screens.AdminFormView{
			Title: title,
			Fields: []screens.AdminFormField{
				{Name: screens.FieldName, Label: "Name . . . .", Value: name, Length: 32},
				{Name: screens.FieldHost, Label: "Host . . . .", Value: host, Length: 48},
				{Name: screens.FieldPort, Label: "Port . . . .", Value: port, Length: 5},
				{Name: screens.FieldTLS, Label: "TLS (Y/N) .", Value: tlsYN, Length: 1},
				{Name: screens.FieldVerify, Label: "Verify (Y/N)", Value: verifyYN, Length: 1},
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
		name = act.Values[screens.FieldName]
		host = act.Values[screens.FieldHost]
		port = act.Values[screens.FieldPort]
		tlsYN = strings.ToUpper(act.Values[screens.FieldTLS])
		verifyYN = strings.ToUpper(act.Values[screens.FieldVerify])

		p, perr := strconv.Atoi(port)
		tlsB, tlsOK := ynBool(tlsYN)
		verifyB, verifyOK := ynBool(verifyYN)
		switch {
		case name == "":
			errMsg = "NAME IS REQUIRED"
		case host == "":
			errMsg = "HOST IS REQUIRED"
		case perr != nil || p < 1 || p > 65535:
			errMsg = "PORT MUST BE 1-65535"
		case !tlsOK || !verifyOK:
			errMsg = "TLS AND VERIFY MUST BE Y OR N"
		default:
			if msg := f.checkServiceNameFree(ctx, name, existing); msg != "" {
				errMsg = msg
				continue
			}
			if existing == nil {
				if _, err := f.store.CreateService(ctx, name, host, p, tlsB, verifyB); err != nil {
					errMsg = logStoreErr("create service", err)
					continue
				}
			} else if err := f.store.UpdateService(ctx, existing.ID, name, host, p, tlsB, verifyB); err != nil {
				errMsg = logStoreErr("update service", err)
				continue
			}
			return false, nil
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

// serviceGroups is implemented in Task 15; temporary stub.
func (f *adminFlow) serviceGroups(ctx context.Context, conn net.Conn, svc store.Service) (bool, error) {
	return false, nil
}
