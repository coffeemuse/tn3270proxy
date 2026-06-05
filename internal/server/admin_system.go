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
	"net"

	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
)

// systemParams drives the System Parameters form. Each catalog entry becomes
// one labeled field; Enter saves, PF3 cancels (back to admin menu).
func (f *adminFlow) systemParams(ctx context.Context, conn net.Conn) error {
	r := f.renderer(conn)

	// Build form fields from the catalog, pre-populated with current store values.
	fields := make([]ui3270.FormField, len(sysconfig.Catalog))
	for i, e := range sysconfig.Catalog {
		val, err := f.store.GetConfig(ctx, e.Key)
		if err != nil {
			val = e.Default // fall back to catalog default on unexpected error
		}
		fields[i] = ui3270.FormField{
			Name:   e.Key,
			Label:  e.Label,
			Value:  val,
			Length: 64,
		}
	}

	return ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title: "TN3270 GATEWAY ADMIN: SYSTEM PARAMETERS",
		// Enter saves in place and stays on the form; PF3 returns to the admin
		// menu. (Unlike the add/edit forms, there is no "done" terminal state.)
		StayOnSave: true,
		Fields:     fields,
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			// Validate all fields before touching the store (all-or-nothing).
			for _, e := range sysconfig.Catalog {
				if msg := e.Validate(vals[e.Key]); msg != "" {
					return msg, nil
				}
			}
			// Persist only changed values; audit each effective change.
			for i, e := range sysconfig.Catalog {
				newVal := vals[e.Key]
				oldVal := fields[i].Value
				if newVal == oldVal {
					continue
				}
				if err := f.store.SetConfig(ctx, e.Key, newVal); err != nil {
					return logStoreErr("set config "+e.Key, err), nil
				}
				f.recordAdmin(ctx, "sysconfig set "+e.Key+": "+oldVal+" -> "+newVal)
				fields[i].Value = newVal // keep in-slice value current for next render
			}
			return "", nil
		},
	})
}
