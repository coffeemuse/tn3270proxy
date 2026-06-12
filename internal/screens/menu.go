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

	"github.com/coffeemuse/tn3270proxy/internal/store"
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

// MenuPageBounds clamps page against the service total and the per-page menu
// capacity, returning the clamped page, the [start,end) slice bounds for that
// page, and the "ITEMS x TO y OF z" indicator string. It is the single source
// of menu paging math: MenuScreen uses it to render the page window, and the
// presenter uses it to clamp its stored page so PF7/PF8 are no-ops at the ends.
func MenuPageBounds(geom Geometry, total int, admin bool, page int) (clamped, start, end int, indicator string) {
	if total == 0 {
		return 0, 0, 0, "ITEMS 0 OF 0"
	}
	size := geom.MenuCapacity(admin)
	maxPage := (total - 1) / size
	if page > maxPage {
		page = maxPage
	}
	if page < 0 {
		page = 0
	}
	start = page * size
	end = min(start+size, total)
	return page, start, end, fmt.Sprintf("ITEMS %d TO %d OF %d", start+1, end, total)
}

// MenuScreen renders one page of the service menu sized for geom and returns a
// mapping from the user's typed selection (e.g. "1") to the chosen service,
// plus the initial cursor. Numbering is global and stable: the mapping covers
// ALL services keyed by global index, while only page's window (sized by
// MenuCapacity) is rendered, each row showing its global number. Services render
// on a fixed grid (number col 0, name col 6, description col 17, hard-cut 40) so
// they never collide with the right-hand status block (StatusBlockCol). status
// supplies the block's values; an empty MenuStatus renders blank values. The
// "0 User Settings" meta row (omitted when settingsLocked) and, when admin,
// "A Administration" flow directly below the page's service rows on every page,
// after one blank separator row (no separator below the empty-list placeholder).
// An "ITEMS x TO y OF z" indicator sits on the title row. errMsg, if non-empty, shows on the message
// line. PF7/PF8 page; out-of-range pages clamp (see MenuPageBounds).
func MenuScreen(geom Geometry, services []store.Service, admin bool, settingsLocked bool, status MenuStatus, errMsg string, page int) (go3270.Screen, map[string]store.Service, Cursor) {
	_, start, end, indicator := MenuPageBounds(geom, len(services), admin, page)

	// Right-align the page indicator so its content ends at the screen's right
	// margin (col 79); this guarantees the full "ITEMS x TO y OF z" never clips
	// (a Field's Col is the attribute byte, so content starts at Col+1). Kept
	// within the 80-column logical width per the rows-only adaptation model.
	indicatorCol := max(79-len(indicator), 0)

	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len("TN3270 GATEWAY MENU")), Color: go3270.White, Intense: true, Content: "TN3270 GATEWAY MENU"},
		{Row: geom.TitleRow(), Col: indicatorCol, Color: go3270.Turquoise, Content: indicator},
		{Row: geom.BodyTopRow(), Col: 2, Color: go3270.Turquoise, Content: "Select a service and press ENTER:"},
	}

	// Global mapping: every service is selectable by its global number, even one
	// that lives on another page.
	mapping := make(map[string]store.Service, len(services))
	for i, svc := range services {
		mapping[fmt.Sprintf("%d", i+1)] = svc
	}

	// Render only the current page's window, with global (stable) numbers.
	row := geom.BodyTopRow() + 1
	for i := start; i < end; i++ {
		svc := services[i]
		screen = append(screen,
			go3270.Field{Row: row, Col: 0, Intense: true, Content: fmt.Sprintf("%3d", i+1)},
			go3270.Field{Row: row, Col: 6, Color: go3270.Turquoise, Content: truncateRunes(svc.Name, 8)},
			go3270.Field{Row: row, Col: 17, Color: go3270.Green, Content: truncateRunes(svc.Description, 40)},
		)
		row++
	}
	if len(services) == 0 {
		screen = append(screen, go3270.Field{Row: geom.BodyTopRow() + 1, Col: 6, Content: "(no services available for your account)"})
	}

	// Meta band: flows directly below the page's service rows (GH #133) — one
	// blank separator row after the last service row, none after the
	// empty-list placeholder (a message, not a service row: `row` still sits
	// on it, so row+1 lands directly beneath). "0 User Settings" renders first
	// unless settingsLocked; "A Administration" (admins) takes the next row,
	// or the anchor row itself when "0" is hidden. MenuCapacity reserves the
	// separator + band rows, so a full page's band ends exactly on
	// BodyBottomRow and never collides with the page window or PF legend.
	metaRow := row + 1
	if !settingsLocked {
		screen = append(screen,
			go3270.Field{Row: metaRow, Col: 0, Intense: true, Content: "  0"},
			go3270.Field{Row: metaRow, Col: 17, Color: go3270.Green, Content: "User Settings"},
		)
		metaRow++
	}
	if admin {
		screen = append(screen,
			go3270.Field{Row: metaRow, Col: 0, Intense: true, Content: "  A"},
			go3270.Field{Row: metaRow, Col: 17, Color: go3270.Green, Content: "Administration"},
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
		go3270.Field{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF1=Help  PF3=Logoff  PF7=PgUp  PF8=PgDn  (PA3 returns here from a session)"},
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
