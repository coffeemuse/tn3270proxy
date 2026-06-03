package screens

import (
	"fmt"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)

// FieldSelection is the name of the menu's numeric input field.
const FieldSelection = "selection"

// MenuScreen renders the service menu sized for geom and returns a mapping
// from the user's typed selection (e.g. "1") to the chosen service. When
// admin is true an "A.  Administration" entry is shown (handled by the
// presenter, not the mapping) and the selection field accepts letters.
// errMsg, if non-empty, is shown on the error line. Services beyond the
// screen's capacity are truncated (no pagination) so the list can never
// collide with the input/error/help rows.
func MenuScreen(geom Geometry, services []store.Service, admin bool, errMsg string) (go3270.Screen, map[string]store.Service) {
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
		label := fmt.Sprintf("%2s.  %-20s (%s:%d)", key, svc.Name, svc.Host, svc.Port)
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

	screen = append(screen,
		go3270.Field{Row: geom.InputRow(), Col: 2, Content: "===>"},
		go3270.Field{Row: geom.InputRow(), Col: 7, Name: FieldSelection, Write: true, NumericOnly: !admin, Highlighting: go3270.Underscore},
		go3270.Field{Row: geom.InputRow(), Col: 15}, // stop field
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: "Enter = connect    PF3 = logoff    (PA3 returns here from a session)"},
	)
	return screen, mapping
}
