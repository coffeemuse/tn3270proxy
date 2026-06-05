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
	"strings"
	"testing"

	"github.com/racingmars/go3270"
)

func fieldContent(screen go3270.Screen, substr string) bool {
	for _, f := range screen {
		if strings.Contains(f.Content, substr) {
			return true
		}
	}
	return false
}

func hasNamedField(screen go3270.Screen, name string) bool {
	for _, f := range screen {
		if f.Name == name {
			return true
		}
	}
	return false
}

func TestEnrollMFAScreen(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, rules, cur := EnrollMFAScreen(g, "TN3270PROXY", "ALICE", "ABCD EFGH IJKL MNOP", "")
	if !fieldContent(screen, "ABCD EFGH IJKL MNOP") {
		t.Fatal("chunked key not rendered")
	}
	if !fieldContent(screen, "TN3270PROXY") || !fieldContent(screen, "ALICE") {
		t.Fatal("issuer/account not rendered")
	}
	if fieldContent(screen, "otpauth://") {
		t.Fatal("URI must NOT be shown on the enrollment screen")
	}
	if !hasNamedField(screen, FieldMFACode) {
		t.Fatal("code input field missing")
	}
	if _, ok := rules[FieldMFACode]; !ok {
		t.Fatal("code field should have a validation rule")
	}
	if cur.Row == 0 && cur.Col == 0 {
		t.Fatal("cursor should land on the code field, not home")
	}
}

func TestEnrollMFAScreenShowsError(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := EnrollMFAScreen(g, "I", "A", "K", "Code incorrect")
	if !fieldContent(screen, "Code incorrect") {
		t.Fatal("error message not rendered")
	}
}

func TestVerifyMFAScreen(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, rules, cur := VerifyMFAScreen(g, "")
	if !hasNamedField(screen, FieldMFACode) {
		t.Fatal("code input field missing")
	}
	if _, ok := rules[FieldMFACode]; !ok {
		t.Fatal("code field should have a validation rule")
	}
	if cur.Row == 0 && cur.Col == 0 {
		t.Fatal("cursor should land on the code field")
	}
}
