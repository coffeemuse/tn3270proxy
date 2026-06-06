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

import "github.com/racingmars/go3270"

// Field names for the self-service User Settings screens.
const (
	FieldUSOption        = "usoption"        // user-settings menu option input
	FieldCurrentPassword = "currentpassword" // self change-password / MFA step-up
)

// UserSettingsRow is one selectable row on the User Settings menu, rendered on
// the shared tri-color option grid: Key (white numeral), Name (turquoise short
// keyword), Description (green). The caller builds the ordered slice adaptively
// from the user's MFA state.
type UserSettingsRow struct {
	Key, Name, Description string
}

// UserSettingsScreen renders the self-service settings menu sized for geom.
// The caller drives it with HandleScreenAlt: AIDEnter submits, PF3 returns to
// the service menu. rows are rendered in order from BodyTopRow+2 down.
func UserSettingsScreen(geom Geometry, username string, rows []UserSettingsRow, errMsg string) (go3270.Screen, Cursor) {
	title := "TN3270 GATEWAY USER SETTINGS"
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: geom.BodyTopRow(), Col: 2, Color: go3270.Turquoise, Content: "User: " + username},
	}
	row := geom.BodyTopRow() + 2
	for _, r := range rows {
		screen = append(screen,
			go3270.Field{Row: row, Col: 0, Color: go3270.White, Intense: true, Content: "  " + r.Key},
			go3270.Field{Row: row, Col: 6, Color: go3270.Turquoise, Content: r.Name},
			go3270.Field{Row: row, Col: 17, Color: go3270.Green, Content: r.Description},
		)
		row++
	}
	promptF, option, stopF := commandLine(geom, "Option ===>", FieldUSOption)
	screen = append(screen,
		promptF,
		option,
		stopF,
		go3270.Field{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Service Menu"},
	)
	return screen, cursorAt(option)
}
