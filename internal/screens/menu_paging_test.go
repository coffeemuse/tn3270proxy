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
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

func TestMenuPageBounds(t *testing.T) {
	g := DefaultGeometry // MOD 2: capacity 18 non-admin, 17 admin
	cases := []struct {
		name                string
		total, page         int
		admin               bool
		wantPage, wantStart int
		wantEnd             int
		wantIndicator       string
	}{
		{"empty", 0, 0, false, 0, 0, 0, "ITEMS 0 OF 0"},
		{"empty ignores page", 0, 3, false, 0, 0, 0, "ITEMS 0 OF 0"},
		{"single page", 5, 0, false, 0, 0, 5, "ITEMS 1 TO 5 OF 5"},
		{"single page over-clamps", 5, 9, false, 0, 0, 5, "ITEMS 1 TO 5 OF 5"},
		{"two pages first", 22, 0, false, 0, 0, 18, "ITEMS 1 TO 18 OF 22"},
		{"two pages second", 22, 1, false, 1, 18, 22, "ITEMS 19 TO 22 OF 22"},
		{"over-clamp to last", 22, 9, false, 1, 18, 22, "ITEMS 19 TO 22 OF 22"},
		{"under-clamp to first", 22, -3, false, 0, 0, 18, "ITEMS 1 TO 18 OF 22"},
		{"admin smaller page", 22, 1, true, 1, 17, 22, "ITEMS 18 TO 22 OF 22"},
	}
	for _, c := range cases {
		gotPage, gotStart, gotEnd, gotInd := MenuPageBounds(g, c.total, c.admin, c.page)
		if gotPage != c.wantPage || gotStart != c.wantStart || gotEnd != c.wantEnd || gotInd != c.wantIndicator {
			t.Errorf("%s: MenuPageBounds(total=%d, admin=%v, page=%d) = (%d,%d,%d,%q), want (%d,%d,%d,%q)",
				c.name, c.total, c.admin, c.page,
				gotPage, gotStart, gotEnd, gotInd,
				c.wantPage, c.wantStart, c.wantEnd, c.wantIndicator)
		}
	}
}

// TestMenuScreenIndicatorClearsTitle guards against the page indicator's field
// attribute byte landing inside the centered title on the title row. The title
// centers on the FULL negotiated width (CenterCol uses geom.Cols), but the
// indicator is right-anchored to the 80-column margin. On a wide (132-col)
// geometry the title therefore reaches past the indicator's start column, and
// the indicator's attribute byte lands inside the title text and corrupts it.
// The indicator must start strictly right of the title's last content column at
// every supported geometry.
func TestMenuScreenIndicatorClearsTitle(t *testing.T) {
	const title = "TN3270 GATEWAY MENU"
	// Enough services to span multiple pages so the indicator renders in its
	// widest "ITEMS x TO y OF z" form (the most collision-prone case).
	svcs := make([]store.Service, 60)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	geoms := []Geometry{
		{Rows: 24, Cols: 80},  // MOD 2
		{Rows: 32, Cols: 80},  // MOD 3
		{Rows: 43, Cols: 80},  // MOD 4
		{Rows: 27, Cols: 132}, // MOD 5
	}
	for _, g := range geoms {
		screen, _, _ := MenuScreen(g, svcs, false, false, MenuStatus{}, "", 0)

		titleF, ok := fieldByContent(screen, title)
		if !ok {
			t.Fatalf("%dx%d: title field %q not found", g.Rows, g.Cols, title)
		}
		titleEndCol := titleF.Col + len(title) - 1

		indCol, indContent := -1, ""
		for _, f := range screen {
			if strings.HasPrefix(f.Content, "ITEMS ") {
				indCol, indContent = f.Col, f.Content
				break
			}
		}
		if indCol < 0 {
			t.Fatalf("%dx%d: page indicator field (ITEMS ...) not found", g.Rows, g.Cols)
		}
		if indCol <= titleEndCol {
			t.Errorf("%dx%d: indicator %q starts at col %d, but title %q ends at col %d; indicator must start strictly right of the title",
				g.Rows, g.Cols, indContent, indCol, title, titleEndCol)
		}
	}
}
