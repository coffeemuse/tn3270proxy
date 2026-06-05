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
	"fmt"
	"strings"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)

// MenuStatus carries the values shown in the menu's right-hand status block.
// Now is the paint-time clock; the presenter stamps it on every render so the
// builder stays deterministic (no hidden time source). Username/SystemID/
// Release are supplied by the session; TermType is filled by the presenter
// from the negotiated terminal.
type MenuStatus struct {
	Username string
	TermType string
	SystemID string
	Release  string
	Now      time.Time
}

// julianDate formats t as YY.DDD (two-digit year, three-digit day-of-year),
// e.g. 2026-06-05 -> "26.156". Six runes, so it fits the 7-rune value column.
func julianDate(t time.Time) string {
	return fmt.Sprintf("%02d.%03d", t.Year()%100, t.YearDay())
}

// clockHM formats t as 24-hour HH:MM.
func clockHM(t time.Time) string { return t.Format("15:04") }

// termDisplay prepares a terminal type for the status block: strip a leading
// "IBM-", hard-cut to 7 runes, then trim a trailing "-" so "IBM-3278-2-E"
// becomes "3278-2".
func termDisplay(t string) string {
	t = strings.TrimPrefix(t, "IBM-")
	t = truncateRunes(t, 7)
	return strings.TrimRight(t, "-")
}

// truncateRunes hard-cuts s to at most n runes. Status values and descriptions
// are ASCII in this EBCDIC display context, but rune-safe to match news.go.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// FieldSelection is the name of the menu's numeric input field.
const FieldSelection = "selection"

// MenuScreen renders the service menu sized for geom and returns a mapping
// from the user's typed selection (e.g. "1") to the chosen service. When
// admin is true an "A.  Administration" entry is shown (handled by the
// presenter, not the mapping) and the selection field accepts letters.
// errMsg, if non-empty, is shown on the error line. Services beyond the
// screen's capacity are truncated (no pagination) so the list can never
// collide with the input/error/help rows.
func MenuScreen(geom Geometry, services []store.Service, admin bool, errMsg string) (go3270.Screen, map[string]store.Service, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY MENU"},
		{Row: 2, Col: 2, Content: "Select a service and press ENTER:"},
	}

	shown := services
	if capacity := geom.MenuCapacity(admin); len(shown) > capacity {
		shown = shown[:capacity]
	}
	mapping := make(map[string]store.Service, len(shown))

	row := 4
	for i, svc := range shown {
		key := fmt.Sprintf("%d", i+1)
		mapping[key] = svc
		label := fmt.Sprintf("%2s  %-8s  %s", key, svc.Name, svc.Description)
		screen = append(screen, go3270.Field{Row: row, Col: 4, Content: label})
		row++
	}
	if len(shown) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: 4, Content: "(no services available for your account)"})
		row = 5
	}
	if admin {
		adminRow := row + 1
		if last := geom.InputRow() - 2; adminRow > last {
			adminRow = last // MenuCapacity reserved this row when the list is full
		}
		screen = append(screen, go3270.Field{Row: adminRow, Col: 4, Content: " A.  Administration"})
	}

	selection := go3270.Field{Row: geom.InputRow(), Col: 7, Name: FieldSelection, Write: true, NumericOnly: !admin, Highlighting: go3270.Underscore}
	screen = append(screen,
		go3270.Field{Row: geom.InputRow(), Col: 2, Content: "===>"},
		selection,
		go3270.Field{Row: geom.InputRow(), Col: 15}, // stop field
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: "PF3=Logoff    (PA3 returns here from a session)"},
	)
	return screen, mapping, cursorAt(selection)
}
