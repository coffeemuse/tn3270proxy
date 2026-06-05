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
	FieldOption      = "option"      // admin menu option input
	FieldRetype      = "retype"      // password confirmation input
	FieldName        = "name"        // entity name input (group/service forms)
	FieldDescription = "description" // service form: human label
	FieldHost        = "host"        // service form inputs
	FieldPort        = "port"        // service form inputs
	FieldTLS         = "tls"         // service form inputs
	FieldVerify      = "verify"      // service form inputs
	FieldCIDR        = "cidr"        // trusted networks form: network CIDR or bare IP
	FieldComment     = "comment"     // trusted networks form: operator annotation
)

// AdminMenuScreen renders the top-level admin menu sized for geom. The caller
// drives it with HandleScreenAlt: AIDEnter submits, PF3 returns to the
// service menu.
func AdminMenuScreen(geom Geometry, errMsg string) (go3270.Screen, Cursor) {
	option := go3270.Field{Row: geom.InputRow(), Col: 7, Name: FieldOption, Write: true, Highlighting: go3270.Underscore}
	return go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY ADMIN"},
		{Row: 3, Col: 4, Content: "1.  Users"},
		{Row: 4, Col: 4, Content: "2.  Groups"},
		{Row: 5, Col: 4, Content: "3.  Services"},
		{Row: 6, Col: 4, Content: "4.  System Parameters"},
		{Row: 7, Col: 4, Content: "5.  Trusted Networks"},
		{Row: geom.InputRow(), Col: 2, Content: "===>"},
		option,
		{Row: geom.InputRow(), Col: 11}, // stop field
		{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Content: "Enter = select    PF3 = main menu"},
	}, cursorAt(option)
}
