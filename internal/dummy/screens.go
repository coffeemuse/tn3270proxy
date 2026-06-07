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

// Package dummy is a throwaway TN3270 server used as a bridge target for demos
// and tests. It authenticates nothing, stores nothing, and logs nothing; on
// each connection it paints one of a few obviously-fake mockup welcome screens.
package dummy

import "github.com/racingmars/go3270"

const (
	markerText = "DUMMY3270"
	footerText = "Press PA3 to disconnect."
)

// builders is the set of welcome screens; one is chosen at random per paint.
var builders = []func() go3270.Screen{screenA, screenB, screenC}

// screenFor returns the i-th welcome screen, wrapping i into range so any int
// (e.g. a rand result) is safe to pass.
func screenFor(i int) go3270.Screen {
	n := len(builders)
	return builders[((i%n)+n)%n]()
}

// titleField centers text on row 0 in intense white. Col is the attribute
// byte, so placing it one column left of the centered start puts the content
// itself (which begins at Col+1) at the true center.
func titleField(text string) go3270.Field {
	return go3270.Field{Row: 0, Col: (80-len(text))/2 - 1, Content: text, Color: go3270.White, Intense: true}
}

// footerField is the bottom-anchored PA3 hint shared by every screen.
func footerField() go3270.Field {
	return go3270.Field{Row: 22, Col: 2, Content: footerText, Color: go3270.Turquoise}
}

// markerFields returns the blinking red DUMMY3270 marker (row 0, columns 71-79)
// plus a stop field at (1,0) so the red/blink attribute does not bleed into the
// body. A Field's Col is the attribute byte; content begins at Col+1, so Col=70
// places the 9-char marker in columns 71-79.
func markerFields() go3270.Screen {
	return go3270.Screen{
		{Row: 0, Col: 70, Content: markerText, Color: go3270.Red, Highlighting: go3270.Blink},
		{Row: 1, Col: 0}, // stop field: resets attributes after the marker
	}
}

// compose assembles a full screen: title first (field 0), then body, then the
// shared footer and the blinking marker.
func compose(title go3270.Field, body go3270.Screen) go3270.Screen {
	s := go3270.Screen{title}
	s = append(s, body...)
	s = append(s, footerField())
	s = append(s, markerFields()...)
	return s
}

func screenA() go3270.Screen {
	body := go3270.Screen{
		{Row: 4, Col: 10, Content: "WELCOME TO CICSDEMO", Color: go3270.Green, Intense: true},
		{Row: 6, Col: 10, Content: "CICS/TS region CICSDEMO is now available.", Color: go3270.Turquoise},
		{Row: 8, Col: 10, Content: "This is a non-working demonstration host.", Color: go3270.Turquoise},
	}
	return compose(titleField("CICSDEMO"), body)
}

func screenB() go3270.Screen {
	body := go3270.Screen{
		{Row: 4, Col: 10, Content: "0  SETTINGS    Terminal and user parameters", Color: go3270.Turquoise},
		{Row: 5, Col: 10, Content: "1  VIEW        Display source data or listings", Color: go3270.Turquoise},
		{Row: 6, Col: 10, Content: "2  EDIT        Create or change source data", Color: go3270.Turquoise},
		{Row: 7, Col: 10, Content: "3  UTILITIES   Perform utility functions", Color: go3270.Turquoise},
		{Row: 9, Col: 10, Content: "These options are inert - this is a demo host.", Color: go3270.Green},
	}
	return compose(titleField("ISPF PRIMARY OPTION MENU"), body)
}

func screenC() go3270.Screen {
	body := go3270.Screen{
		{Row: 5, Col: 10, Content: "*** SYSTEM AVAILABLE ***", Color: go3270.Green, Intense: true},
		{Row: 7, Col: 10, Content: "LPAR: DEMO1     SYSID: DUM1", Color: go3270.Turquoise},
		{Row: 9, Col: 10, Content: "No application is running here - demonstration only.", Color: go3270.Turquoise},
	}
	return compose(titleField("SYSTEM STATUS"), body)
}
