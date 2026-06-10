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

func TestPageBounds(t *testing.T) {
	cases := []struct {
		page, total, rows      int
		wantPage, wantS, wantE int
		wantInfo               string
	}{
		{0, 0, 24, 0, 0, 0, "ROW 0 OF 0"},
		{0, 3, 24, 0, 0, 3, "ROW 1 TO 3 OF 3"},
		{-1, 3, 24, 0, 0, 3, "ROW 1 TO 3 OF 3"},
		{0, 20, 24, 0, 0, 14, "ROW 1 TO 14 OF 20"},
		{1, 20, 24, 1, 14, 20, "ROW 15 TO 20 OF 20"},
		{5, 20, 24, 1, 14, 20, "ROW 15 TO 20 OF 20"},
		{0, 30, 32, 0, 0, 22, "ROW 1 TO 22 OF 30"},
		{1, 30, 32, 1, 22, 30, "ROW 23 TO 30 OF 30"},
	}
	for _, c := range cases {
		page, s, e, info := pageBounds(c.page, c.total, c.rows)
		if page != c.wantPage || s != c.wantS || e != c.wantE || info != c.wantInfo {
			t.Errorf("pageBounds(%d,%d,%d) = %d,%d,%d,%q want %d,%d,%d,%q",
				c.page, c.total, c.rows, page, s, e, info, c.wantPage, c.wantS, c.wantE, c.wantInfo)
		}
	}
}

func TestLayoutFormulas(t *testing.T) {
	if got := listPageSize(24); got != 14 {
		t.Errorf("listPageSize(24) = %d, want 14", got)
	}
	if got := formMaxFields(24); got != 9 {
		t.Errorf("formMaxFields(24) = %d, want 9", got)
	}
	if got := listPageSize(10); got != 14 {
		t.Errorf("listPageSize(10) = %d, want 14", got)
	}
}

func TestFormInputCol(t *testing.T) {
	// max(16, 2+1+maxLabel+1), clamped to the ceiling (79-16 = 63).
	cases := []struct{ maxLabel, want int }{
		{0, 16},  // empty label → floor
		{11, 16}, // short → floor
		{12, 16}, // dot-leader convention → exactly the historical col 16
		{13, 17}, // first width that pushes right
		{15, 19}, // "Max Auth Tries:"
		{22, 26}, // "Auth Delay Base (sec):"
		{23, 27}, // "Auth Fail Window (min):" — the worst real label
		{80, 63}, // pathological → clamped to ceiling
	}
	for _, c := range cases {
		if got := formInputCol(c.maxLabel); got != c.want {
			t.Errorf("formInputCol(%d) = %d, want %d", c.maxLabel, got, c.want)
		}
	}
}

func TestFormLabelMax(t *testing.T) {
	// inputCol - labelGutter - labelAttrCol - 1.
	cases := []struct{ inputCol, want int }{
		{16, 12}, // historical 12-char dot-leader cap falls out naturally
		{27, 23}, // worst real label fits exactly
		{63, 59}, // at the ceiling
	}
	for _, c := range cases {
		if got := formLabelMax(c.inputCol); got != c.want {
			t.Errorf("formLabelMax(%d) = %d, want %d", c.inputCol, got, c.want)
		}
	}
}

func TestBandHelpers(t *testing.T) {
	if messageRow() != 2 {
		t.Errorf("messageRow() = %d, want 2", messageRow())
	}
	if bodyTopRow() != 3 {
		t.Errorf("bodyTopRow() = %d, want 3", bodyTopRow())
	}
	// "RECENT ACTIVITY" is 15 runes → (80-15)/2 = 32.
	if got := centerCol(15); got != 32 {
		t.Errorf("centerCol(15) = %d, want 32", got)
	}
	if got := centerCol(200); got != 0 {
		t.Errorf("centerCol(200) = %d, want 0", got)
	}
}

func TestTruncRunes(t *testing.T) {
	if got := truncRunes("abcdef", 3); got != "abc" {
		t.Errorf("truncRunes(\"abcdef\",3) = %q, want \"abc\"", got)
	}
	if got := truncRunes("abc", 10); got != "abc" {
		t.Errorf("truncRunes(\"abc\",10) = %q, want \"abc\"", got)
	}
	if got := truncRunes("abc", -1); got != "" {
		t.Errorf("truncRunes(\"abc\",-1) = %q, want \"\"", got)
	}
	if got := truncRunes("日本語", 2); got != "日本" {
		t.Errorf("truncRunes(\"日本語\",2) = %q, want \"日本\"", got)
	}
}
