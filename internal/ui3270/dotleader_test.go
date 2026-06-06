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

package ui3270

import "testing"

func TestDotLeaderLabel(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"MOTD File:", 23, "MOTD File . . . . . . :"},
		{"MFA Issuer:", 23, "MFA Issuer  . . . . . :"},
		{"System ID:", 23, "System ID . . . . . . :"},
		{"Auth Delay Base (sec):", 23, "Auth Delay Base (sec) :"},
		{"Max Auth Tries:", 23, "Max Auth Tries  . . . :"},
		{"Auth Fail Window (min):", 23, "Auth Fail Window (min):"},
		{"System ID", 23, "System ID . . . . . . :"}, // no trailing colon in the input
	}
	for _, c := range cases {
		if got := dotLeaderLabel(c.in, c.w); got != c.want {
			t.Errorf("dotLeaderLabel(%q,%d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

func TestDotLeaderLabelInvariants(t *testing.T) {
	// For labels short enough to leave colon room, the result is exactly width
	// runes and ends with a colon (so colons align across rows).
	for _, in := range []string{"A:", "Hello:"} {
		for _, w := range []int{8, 16, 23} {
			got := []rune(dotLeaderLabel(in, w))
			if len(got) != w {
				t.Errorf("dotLeaderLabel(%q,%d) len = %d, want %d", in, w, len(got), w)
			}
			if got[w-1] != ':' {
				t.Errorf("dotLeaderLabel(%q,%d) last = %q, want ':'", in, w, string(got[w-1]))
			}
		}
	}
}

func TestDotLeaderLabelEdgeCases(t *testing.T) {
	if got := dotLeaderLabel("X:", 0); got != "" {
		t.Errorf("width 0 = %q, want \"\"", got)
	}
	// base longer than width: clamp to width, no colon room.
	if got := dotLeaderLabel("Toolongbase:", 4); got != "Tool" {
		t.Errorf("clamp = %q, want \"Tool\"", got)
	}
	// width=1: a non-empty base clamps (same rule as any too-narrow width),
	// while an empty base leaves room for just the colon.
	if got := dotLeaderLabel("Anything:", 1); got != "A" {
		t.Errorf("width 1 non-empty base = %q, want \"A\"", got)
	}
	if got := dotLeaderLabel(":", 1); got != ":" {
		t.Errorf("width 1 colon-only = %q, want \":\"", got)
	}
}
