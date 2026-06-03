package screens

import (
	"fmt"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)

// FieldSelection is the name of the menu's numeric input field.
const FieldSelection = "selection"

// MenuScreen renders the service menu and returns a mapping from the user's
// typed selection (e.g. "1") to the chosen service. errMsg, if non-empty, is
// shown on the error line. The caller drives it with go3270.HandleScreen using
// AIDEnter to select and AIDPF3 to disconnect, with errorField = FieldError.
func MenuScreen(services []store.Service, errMsg string) (go3270.Screen, map[string]store.Service) {
	screen := go3270.Screen{
		{Row: 1, Col: 27, Intense: true, Content: "TN3270 GATEWAY MENU"},
		{Row: 3, Col: 2, Content: "Select a service and press ENTER:"},
	}
	mapping := make(map[string]store.Service, len(services))

	row := 5
	for i, svc := range services {
		key := fmt.Sprintf("%d", i+1)
		mapping[key] = svc
		label := fmt.Sprintf("%2s.  %-20s (%s:%d)", key, svc.Name, svc.Host, svc.Port)
		screen = append(screen, go3270.Field{Row: row, Col: 4, Content: label})
		row++
	}
	if len(services) == 0 {
		screen = append(screen, go3270.Field{Row: 5, Col: 4, Content: "(no services available for your account)"})
	}

	screen = append(screen,
		go3270.Field{Row: 20, Col: 2, Content: "===>"},
		go3270.Field{Row: 20, Col: 7, Name: FieldSelection, Write: true, NumericOnly: true, Highlighting: go3270.Underscore},
		go3270.Field{Row: 20, Col: 15}, // stop field
		go3270.Field{Row: 22, Col: 2, Content: "Enter = connect    PF3 = disconnect    (PA3 returns here from a session)"},
		go3270.Field{Row: 23, Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
	)
	return screen, mapping
}
