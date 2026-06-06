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
// from the user's typed selection (e.g. "1") to the chosen service, plus the
// initial cursor. Services render on a fixed grid (number col 0, name col 4,
// description col 13, hard-cut to 40) so they never collide with the right-hand
// status block (StatusBlockCol). status supplies the block's values; an empty
// MenuStatus simply renders blank values. When admin is true an
// "A  Administration" entry is shown (handled by the presenter, not the
// mapping) and the selection field accepts letters. errMsg, if non-empty, is
// shown on the error line. Services beyond the screen's capacity are truncated
// (no pagination) so the list can never collide with the input/error/help rows.
func MenuScreen(geom Geometry, services []store.Service, admin bool, status MenuStatus, errMsg string) (go3270.Screen, map[string]store.Service, Cursor) {
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len("TN3270 GATEWAY MENU")), Color: go3270.White, Intense: true, Content: "TN3270 GATEWAY MENU"},
		{Row: geom.BodyTopRow(), Col: 2, Color: go3270.Turquoise, Content: "Select a service and press ENTER:"},
	}

	shown := services
	if capacity := geom.MenuCapacity(admin); len(shown) > capacity {
		shown = shown[:capacity]
	}
	mapping := make(map[string]store.Service, len(shown))

	// Fixed grid: number col 0 (intense white), name col 4 (turquoise),
	// description col 13 (green, hard-cut 40). Three separate fields keep the
	// columns aligned and individually colored (ISPF style).
	row := geom.BodyTopRow() + 1
	for i, svc := range shown {
		key := fmt.Sprintf("%d", i+1)
		mapping[key] = svc
		screen = append(screen,
			go3270.Field{Row: row, Col: 0, Intense: true, Content: fmt.Sprintf("%3d", i+1)},
			go3270.Field{Row: row, Col: 6, Color: go3270.Turquoise, Content: truncateRunes(svc.Name, 8)},
			go3270.Field{Row: row, Col: 17, Color: go3270.Green, Content: truncateRunes(svc.Description, 40)},
		)
		row++
	}
	if len(shown) == 0 {
		screen = append(screen, go3270.Field{Row: geom.BodyTopRow() + 1, Col: 6, Content: "(no services available for your account)"})
		row = geom.BodyTopRow() + 2
	}
	// Bottom "meta" entries below the service list: User Settings (0) is shown
	// for every user; Administration (A) only for admins. Clamp so they never
	// overrun the input row (MenuCapacity reserved these rows when the list is
	// full).
	metaRow := row + 1
	lastMeta := geom.BodyBottomRow()
	if admin {
		lastMeta-- // leave a row below "0" for the "A" entry
	}
	if metaRow > lastMeta {
		metaRow = lastMeta
	}
	screen = append(screen,
		go3270.Field{Row: metaRow, Col: 0, Intense: true, Content: "  0"},
		go3270.Field{Row: metaRow, Col: 17, Color: go3270.Green, Content: "User Settings"},
	)
	if admin {
		adminRow := metaRow + 1
		screen = append(screen,
			go3270.Field{Row: adminRow, Col: 0, Intense: true, Content: "  A"},
			go3270.Field{Row: adminRow, Col: 17, Color: go3270.Green, Content: "Administration"},
		)
	}

	// Right-hand status block: 6 rows starting at the first service row.
	screen = append(screen, statusBlockFields(geom, status)...)

	promptF, selection, stopF := commandLine(geom, "Option ===>", FieldSelection)
	selection.NumericOnly = !admin
	screen = append(screen,
		promptF,
		selection,
		stopF,
		go3270.Field{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Logoff    (PA3 returns here from a session)"},
	)
	return screen, mapping, cursorAt(selection)
}

// statusRow is one label/value pair in an ISPF status block.
type statusRow struct{ label, value string }

// statusBlock renders a right-hand ISPF-style status block at
// geom.StatusBlockCol(): each row a 10-char colon-aligned turquoise label and a
// green value, stacked from startRow down. The menu and login screens share it
// so the two blocks line up column-for-column.
func statusBlock(geom Geometry, startRow int, rows []statusRow) go3270.Screen {
	const labelWidth = 10 // "System ID:" etc.; value field sits one space past
	labelCol := geom.StatusBlockCol()
	valueCol := labelCol + labelWidth + 1
	var fields go3270.Screen
	for i, r := range rows {
		fields = append(fields,
			go3270.Field{Row: startRow + i, Col: labelCol, Color: go3270.Turquoise, Content: r.label},
			go3270.Field{Row: startRow + i, Col: valueCol, Color: go3270.Green, Content: r.value},
		)
	}
	return fields
}

// statusBlockFields builds the menu's right-hand status block: six rows
// (User ID / Date / Time / Terminal / System ID / Release) starting at the
// first service row (row 4). Values are hard-cut to 7 runes.
func statusBlockFields(geom Geometry, status MenuStatus) go3270.Screen {
	return statusBlock(geom, 4, []statusRow{
		{"User ID. :", truncateRunes(strings.ToUpper(status.Username), 7)},
		{"Date . . :", julianDate(status.Now)},
		{"Time . . :", clockHM(status.Now)},
		{"Terminal :", termDisplay(status.TermType)},
		{"System ID:", truncateRunes(status.SystemID, 7)},
		{"Release. :", truncateRunes(status.Release, 7)},
	})
}
