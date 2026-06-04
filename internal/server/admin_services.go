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
	"strconv"
	"strings"

	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
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
	r := f.renderer(conn)
	return ui3270.RunList(ctx, r, ui3270.ListConfig[store.Service]{
		Title:  "TN3270 GATEWAY ADMIN: SERVICES",
		Header: "CMD  NAME     DESCRIPTION          HOST:PORT          TLS VERIFY",
		Legend: "S = edit   G = group access   D = delete   PF4 = add service",
		PFHelp: "Enter = process   PF7/PF8 = page   PF3 = admin menu",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.Row[store.Service], string) {
			svcs, err := f.store.ListAllServices(ctx)
			if err != nil {
				return nil, logStoreErr("list services", err)
			}
			rows := make([]ui3270.Row[store.Service], len(svcs))
			for i, s := range svcs {
				rows[i] = ui3270.Row[store.Service]{
					Display: fmt.Sprintf("%-8s %-20.20s %-18.18s %-3s %s",
						s.Name, s.Description, fmt.Sprintf("%s:%d", s.Host, s.Port), yn(s.TLS), yn(s.TLSVerify)),
					Item: s,
				}
			}
			return rows, ""
		},
		Add: func(ctx context.Context, r ui3270.Renderer) (string, error) { return "", f.serviceForm(ctx, r, nil) },
		Cmds: []ui3270.Command[store.Service]{
			{Key: 'S', Commit: func(ctx context.Context, r ui3270.Renderer, s store.Service) (string, error) {
				return "", f.serviceForm(ctx, r, &s)
			}},
			{Key: 'G', Commit: func(ctx context.Context, r ui3270.Renderer, s store.Service) (string, error) {
				return "", f.serviceGroups(ctx, r, s)
			}},
			{Key: 'D',
				Confirm: func(s store.Service) (string, string) {
					return fmt.Sprintf("ENTER = CONFIRM DELETE OF '%s', PF3 = CANCEL", s.Name), ""
				},
				Commit: func(ctx context.Context, _ ui3270.Renderer, s store.Service) (string, error) {
					if err := f.store.DeleteService(ctx, s.ID); err != nil {
						return logStoreErr("delete service", err), nil
					}
					f.recordAdmin(ctx, "service delete "+s.Name)
					return "", nil
				}},
		},
	})
}

// serviceForm adds (existing == nil) or edits a service. Both TLS fields are
// exposed: tls (encrypt) and verify (authenticate the backend cert).
func (f *adminFlow) serviceForm(ctx context.Context, r ui3270.Renderer, existing *store.Service) error {
	title := "TN3270 GATEWAY ADMIN: ADD SERVICE"
	name, description, host, port, tlsYN, verifyYN := "", "", "", "", "N", "Y" // verify defaults on (secure default)
	if existing != nil {
		title = "TN3270 GATEWAY ADMIN: EDIT SERVICE"
		name, description, host, port = existing.Name, existing.Description, existing.Host, strconv.Itoa(existing.Port)
		tlsYN, verifyYN = yn(existing.TLS), yn(existing.TLSVerify)
	}
	// fields is rebuilt-by-reference so a rejected submit re-seeds the typed
	// (and canonicalized) values on the next render.
	fields := []ui3270.FormField{
		{Name: screens.FieldName, Label: "Name . . . .", Value: name, Length: 8},
		{Name: screens.FieldDescription, Label: "Descr. . . .", Value: description, Length: 40},
		{Name: screens.FieldHost, Label: "Host . . . .", Value: host, Length: 48},
		{Name: screens.FieldPort, Label: "Port . . . .", Value: port, Length: 5},
		{Name: screens.FieldTLS, Label: "TLS (Y/N) .", Value: tlsYN, Length: 1},
		{Name: screens.FieldVerify, Label: "Verify (Y/N)", Value: verifyYN, Length: 1},
	}
	return ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title:  title,
		Fields: fields,
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			name := vals[screens.FieldName]
			description := vals[screens.FieldDescription]
			host := vals[screens.FieldHost]
			port := vals[screens.FieldPort]
			tlsYN := strings.ToUpper(vals[screens.FieldTLS])
			verifyYN := strings.ToUpper(vals[screens.FieldVerify])
			// Preserve typed (Y/N canonicalized) input on re-render.
			fields[0].Value, fields[1].Value, fields[2].Value = name, description, host
			fields[3].Value, fields[4].Value, fields[5].Value = port, tlsYN, verifyYN

			p, perr := strconv.Atoi(port)
			tlsB, tlsOK := ynBool(tlsYN)
			verifyB, verifyOK := ynBool(verifyYN)
			normName, nameErr := store.NormalizeServiceName(name)
			descErr := store.ValidateDescription(description)
			switch {
			case nameErr != nil:
				return strings.ToUpper(nameErr.Error()), nil
			case descErr != nil:
				return strings.ToUpper(descErr.Error()), nil
			case host == "":
				return "HOST IS REQUIRED", nil
			case perr != nil || p < 1 || p > 65535:
				return "PORT MUST BE 1-65535", nil
			case !tlsOK || !verifyOK:
				return "TLS AND VERIFY MUST BE Y OR N", nil
			}
			if msg := f.checkServiceNameFree(ctx, normName, existing); msg != "" {
				return msg, nil
			}
			if existing == nil {
				if _, err := f.store.CreateService(ctx, normName, description, host, p, tlsB, verifyB); err != nil {
					return logStoreErr("create service", err), nil
				}
				f.recordAdmin(ctx, "service create "+normName)
			} else if err := f.store.UpdateService(ctx, existing.ID, normName, description, host, p, tlsB, verifyB); err != nil {
				return logStoreErr("update service", err), nil
			} else {
				f.recordAdmin(ctx, "service update "+normName)
			}
			return "", nil
		},
	})
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
func (f *adminFlow) serviceGroups(ctx context.Context, r ui3270.Renderer, svc store.Service) error {
	return ui3270.RunList(ctx, r, ui3270.ListConfig[store.Group]{
		Title:  "TN3270 GATEWAY ADMIN: ACCESS TO " + svc.Name,
		Header: "CMD  GROUP                ACCESS",
		Legend: "A = grant access   R = revoke access",
		PFHelp: "Enter = process   PF7/PF8 = page   PF3 = back",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.Row[store.Group], string) {
			groups, err := f.store.ListGroups(ctx)
			if err != nil {
				return nil, logStoreErr("list groups", err)
			}
			linked, lerr := f.store.ListGroupsForService(ctx, svc.ID)
			if lerr != nil {
				return nil, logStoreErr("list service groups", lerr)
			}
			linkSet := make(map[int64]bool, len(linked))
			for _, g := range linked {
				linkSet[g.ID] = true
			}
			rows := make([]ui3270.Row[store.Group], len(groups))
			for i, g := range groups {
				marker := ""
				if linkSet[g.ID] {
					marker = "X"
				}
				rows[i] = ui3270.Row[store.Group]{Display: fmt.Sprintf("%-20s %s", g.Name, marker), Item: g}
			}
			return rows, ""
		},
		Cmds: []ui3270.Command[store.Group]{
			{Key: 'A', Commit: func(ctx context.Context, _ ui3270.Renderer, g store.Group) (string, error) {
				if err := f.store.LinkGroupService(ctx, g.ID, svc.ID); err != nil {
					return logStoreErr("grant access", err), nil
				}
				f.recordAdmin(ctx, "service "+svc.Name+" grant "+g.Name)
				return "", nil
			}},
			{Key: 'R', Commit: func(ctx context.Context, _ ui3270.Renderer, g store.Group) (string, error) {
				if err := f.store.UnlinkGroupService(ctx, g.ID, svc.ID); err != nil {
					return logStoreErr("revoke access", err), nil
				}
				f.recordAdmin(ctx, "service "+svc.Name+" revoke "+g.Name)
				return "", nil
			}},
		},
	})
}
