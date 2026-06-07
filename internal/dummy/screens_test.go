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

package dummy

import (
	"testing"

	"github.com/racingmars/go3270"
)

// hasContent reports whether any field on the screen renders exactly text.
func hasContent(s go3270.Screen, text string) bool {
	for _, f := range s {
		if f.Content == text {
			return true
		}
	}
	return false
}

// markerField returns the DUMMY3270 marker field, if present.
func markerField(s go3270.Screen) (go3270.Field, bool) {
	for _, f := range s {
		if f.Content == markerText {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestEachScreenHasBlinkingRedMarker(t *testing.T) {
	for i, build := range builders {
		f, ok := markerField(build())
		if !ok {
			t.Fatalf("screen %d: no %q marker field", i, markerText)
		}
		if f.Color != go3270.Red {
			t.Errorf("screen %d: marker color = %v, want Red", i, f.Color)
		}
		if f.Highlighting != go3270.Blink {
			t.Errorf("screen %d: marker highlight = %v, want Blink", i, f.Highlighting)
		}
	}
}

func TestEachScreenHasPA3Footer(t *testing.T) {
	for i, build := range builders {
		if !hasContent(build(), footerText) {
			t.Errorf("screen %d: missing footer %q", i, footerText)
		}
	}
}

func TestScreensHaveNoInputFields(t *testing.T) {
	for i, build := range builders {
		for _, f := range build() {
			if f.Write {
				t.Errorf("screen %d: unexpected writable field at (%d,%d)", i, f.Row, f.Col)
			}
		}
	}
}

func TestScreenForWrapsAndCoversAll(t *testing.T) {
	titleOf := func(s go3270.Screen) string { return s[0].Content } // title is field 0
	a, b, c := titleOf(screenFor(0)), titleOf(screenFor(1)), titleOf(screenFor(2))
	if a == b || b == c || a == c {
		t.Errorf("screens not distinct: %q %q %q", a, b, c)
	}
	if titleOf(screenFor(3)) != a {
		t.Errorf("screenFor(3) should wrap to screenFor(0) (%q), got %q", a, titleOf(screenFor(3)))
	}
	if titleOf(screenFor(-1)) != c {
		t.Errorf("screenFor(-1) should wrap to screenFor(2) (%q), got %q", c, titleOf(screenFor(-1)))
	}
}
