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

	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
)

// networks drives the trusted-network list and its sub-screens.
func (f *adminFlow) networks(ctx context.Context, conn net.Conn) error {
	r := f.renderer(conn)
	return ui3270.RunList(ctx, r, ui3270.ListConfig[store.TrustedNetwork]{
		Title:  "TN3270 GATEWAY ADMIN: TRUSTED NETWORKS",
		Header: "CMD  NETWORK          COMMENT",
		Legend: "S = edit   D = delete",
		PFHelp: "PF3=Admin Menu    PF4=Add Network    PF7=PgUp    PF8=PgDn",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.Row[store.TrustedNetwork], string) {
			nets, err := f.store.ListTrustedNetworks(ctx)
			if err != nil {
				return nil, logStoreErr("list trusted networks", err)
			}
			rows := make([]ui3270.Row[store.TrustedNetwork], len(nets))
			for i, n := range nets {
				rows[i] = ui3270.Row[store.TrustedNetwork]{
					Display: fmt.Sprintf("%-18s %s", n.CIDR, n.Comment),
					Item:    n,
				}
			}
			return rows, ""
		},
		Add: func(ctx context.Context, r ui3270.Renderer) (string, error) {
			return "", f.networkForm(ctx, r, nil)
		},
		Cmds: []ui3270.Command[store.TrustedNetwork]{
			{Key: 'S', Commit: func(ctx context.Context, r ui3270.Renderer, n store.TrustedNetwork) (string, error) {
				return "", f.networkForm(ctx, r, &n)
			}},
			{Key: 'D',
				Confirm: func(n store.TrustedNetwork) (string, string) {
					return fmt.Sprintf("ENTER = CONFIRM DELETE OF '%s', PF3 = CANCEL", n.CIDR), ""
				},
				Commit: func(ctx context.Context, _ ui3270.Renderer, n store.TrustedNetwork) (string, error) {
					if err := f.store.DeleteTrustedNetwork(ctx, n.ID); err != nil {
						return logStoreErr("delete trusted network", err), nil
					}
					f.recordAdmin(ctx, "trust delete "+n.CIDR)
					return "", nil
				}},
		},
	})
}

// networkForm adds (existing == nil) or edits a trusted network entry.
func (f *adminFlow) networkForm(ctx context.Context, r ui3270.Renderer, existing *store.TrustedNetwork) error {
	title := "TN3270 GATEWAY ADMIN: ADD TRUSTED NETWORK"
	cidr, comment := "", ""
	if existing != nil {
		title = "TN3270 GATEWAY ADMIN: EDIT TRUSTED NETWORK"
		cidr, comment = existing.CIDR, existing.Comment
	}
	fields := []ui3270.FormField{
		{Name: screens.FieldCIDR, Label: "Network. . .", Value: cidr, Length: 48},
		{Name: screens.FieldComment, Label: "Comment. . .", Value: comment, Length: 40},
	}
	return ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title:  title,
		Fields: fields,
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			cidrVal := strings.TrimSpace(vals[screens.FieldCIDR])
			commentVal := vals[screens.FieldComment]
			fields[0].Value = cidrVal
			fields[1].Value = commentVal

			if _, err := store.ParseTrustedCIDR(cidrVal); err != nil {
				return strings.ToUpper(err.Error()), nil
			}
			if err := store.ValidateComment(commentVal); err != nil {
				return strings.ToUpper(err.Error()), nil
			}

			if existing == nil {
				if _, err := f.store.CreateTrustedNetwork(ctx, cidrVal, commentVal); err != nil {
					return logStoreErr("create trusted network", err), nil
				}
				f.recordAdmin(ctx, "trust create "+cidrVal)
			} else {
				if err := f.store.UpdateTrustedNetwork(ctx, existing.ID, cidrVal, commentVal); err != nil {
					return logStoreErr("update trusted network", err), nil
				}
				f.recordAdmin(ctx, "trust update "+cidrVal)
			}
			return "", nil
		},
	})
}
