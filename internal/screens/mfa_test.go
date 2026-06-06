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

func TestMFAScreensPalette(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}

	enroll, _, _ := EnrollMFAScreen(g, "TN3270", "ROBERT", "ABCD EFGH", "")
	et, ok := fieldByContent(enroll, "MFA ENROLLMENT - SECURITY KEY SETUP")
	if !ok || et.Row != 0 || et.Color != go3270.White || !et.Intense {
		t.Errorf("enroll title = %+v ok=%v, want row 0 white intense", et, ok)
	}
	caution, ok := fieldByContent(enroll, "Multi-factor authentication is now required for your account.")
	if !ok || caution.Color != go3270.Yellow || !caution.Intense {
		t.Errorf("caution = %+v ok=%v, want yellow intense", caution, ok)
	}
	ec, ok := fieldByName(enroll, FieldMFACode)
	if !ok || ec.Color != go3270.Green {
		t.Errorf("enroll code = %+v ok=%v, want green", ec, ok)
	}
	em, _ := fieldByName(enroll, FieldError)
	if em.Row != 2 {
		t.Errorf("enroll message row = %d, want 2", em.Row)
	}

	verify, _, _ := VerifyMFAScreen(g, "")
	vt, ok := fieldByContent(verify, "MFA VERIFICATION")
	if !ok || vt.Row != 0 || vt.Color != go3270.White || !vt.Intense {
		t.Errorf("verify title = %+v ok=%v, want row 0 white intense", vt, ok)
	}
	vm, _ := fieldByName(verify, FieldError)
	if vm.Row != 2 {
		t.Errorf("verify message row = %d, want 2", vm.Row)
	}
}
