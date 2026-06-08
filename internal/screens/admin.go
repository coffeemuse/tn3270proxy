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

package screens

import (
	"github.com/racingmars/go3270"
)

// Field-name constants for the admin screens.
const (
	FieldOption             = "option"          // admin menu option input
	FieldRetype             = "retype"          // password confirmation input
	FieldName               = "name"            // entity name input (group/service forms)
	FieldDescription        = "description"     // service form: human label
	FieldHost               = "host"            // service form inputs
	FieldPort               = "port"            // service form inputs
	FieldTLS                = "tls"             // service form inputs
	FieldVerify             = "verify"          // service form inputs
	FieldCIDR               = "cidr"            // trusted networks form: network CIDR or bare IP
	FieldComment            = "comment"         // trusted networks form: operator annotation
	FieldFullName           = "fullname"        // edit-user form: display name
	FieldEmail              = "email"           // edit-user form: email address
	FieldMFARequired        = "mfarequired"     // edit-user form: MFA enforce toggle (Y/N)
	FieldMFAStatus          = "mfastatus"       // edit-user form: display-only NONE/PENDING/ENROLLED
	FieldMFAClear           = "mfaclear"        // edit-user form: Y wipes the secret
	FieldUserSettingsLocked = "usettingslocked" // edit-user form: Y locks self-service
)

// AdminMenuScreen renders the top-level admin menu sized for geom. The caller
// drives it with HandleScreenAlt: AIDEnter submits, PF3 returns to the
// service menu.
func AdminMenuScreen(geom Geometry, errMsg string) (go3270.Screen, Cursor) {
	title := "TN3270 GATEWAY ADMIN"
	opts := []struct{ key, name, desc string }{
		{"1", "Users", "User accounts and group membership"},
		{"2", "Groups", "Group definitions"},
		{"3", "Services", "Backend TN3270 services"},
		{"4", "Sysparms", "Runtime system parameters"},
		{"5", "Networks", "Trusted networks (DoS allow-list)"},
		{"6", "Audit", "Browse the audit trail"},
	}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
	}
	for i, o := range opts {
		row := geom.BodyTopRow() + i
		screen = append(screen,
			go3270.Field{Row: row, Col: 0, Color: go3270.White, Intense: true, Content: "  " + o.key},
			go3270.Field{Row: row, Col: 6, Color: go3270.Turquoise, Content: o.name},
			go3270.Field{Row: row, Col: 17, Color: go3270.Green, Content: o.desc},
		)
	}
	promptF, option, stopF := commandLine(geom, "Option ===>", FieldOption)
	screen = append(screen,
		promptF,
		option,
		stopF,
		go3270.Field{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Main Menu"},
	)
	return screen, cursorAt(option)
}
