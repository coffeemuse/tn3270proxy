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

import (
	"slices"
	"testing"
)

func TestApplyPrefix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      []string
		cmds    map[int]byte
		want    []string
		wantErr bool
	}{
		{"insert", []string{"a", "b"}, map[int]byte{0: 'I'}, []string{"a", "", "b"}, false},
		{"delete", []string{"a", "b", "c"}, map[int]byte{1: 'D'}, []string{"a", "c"}, false},
		{"repeat", []string{"a", "b"}, map[int]byte{0: 'R'}, []string{"a", "a", "b"}, false},
		{"multiple same transmission", []string{"a", "b", "c"}, map[int]byte{0: 'D', 2: 'I'},
			[]string{"b", "c", ""}, false},
		{"delete last line leaves one empty", []string{"a"}, map[int]byte{0: 'D'}, []string{""}, false},
		{"invalid command vetoes all", []string{"a", "b"}, map[int]byte{0: 'D', 1: 'X'},
			[]string{"a", "b"}, true},
		{"out of range ignored", []string{"a"}, map[int]byte{5: 'D'}, []string{"a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, errMsg := applyPrefix(slices.Clone(tc.in), tc.cmds)
			if (errMsg != "") != tc.wantErr {
				t.Fatalf("errMsg = %q, wantErr=%v", errMsg, tc.wantErr)
			}
			if !tc.wantErr && !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
