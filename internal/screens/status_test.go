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
	"testing"
	"time"
)

func TestJulianDate(t *testing.T) {
	got := julianDate(time.Date(2026, 6, 5, 21, 14, 0, 0, time.UTC))
	if got != "26.156" {
		t.Errorf("julianDate = %q, want %q", got, "26.156")
	}
}

func TestClockHM(t *testing.T) {
	got := clockHM(time.Date(2026, 6, 5, 21, 14, 0, 0, time.UTC))
	if got != "21:14" {
		t.Errorf("clockHM = %q, want %q", got, "21:14")
	}
}

func TestTermDisplay(t *testing.T) {
	cases := map[string]string{
		"IBM-3278-2-E": "3278-2",
		"IBM-3279-2":   "3279-2",
		"IBM-DYNAMIC":  "DYNAMIC", // 7 runes exactly, no trailing dash
		"":             "",
	}
	for in, want := range cases {
		if got := termDisplay(in); got != want {
			t.Errorf("termDisplay(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("ABCDEFGHIJ", 7); got != "ABCDEFG" {
		t.Errorf("truncateRunes cut = %q, want ABCDEFG", got)
	}
	if got := truncateRunes("ABC", 7); got != "ABC" {
		t.Errorf("truncateRunes short = %q, want ABC", got)
	}
}
