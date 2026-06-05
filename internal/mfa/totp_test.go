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

package mfa

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
)

func TestGenerateSecretIs16Base32Chars(t *testing.T) {
	s, err := GenerateSecret("TN3270PROXY", "ALICE")
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 16 {
		t.Fatalf("want 16-char (80-bit) base32 secret, got %d: %q", len(s), s)
	}
}

func TestChunk(t *testing.T) {
	if got := Chunk("ABCDEFGHIJKLMNOP"); got != "ABCD EFGH IJKL MNOP" {
		t.Fatalf("Chunk = %q", got)
	}
}

// codeFor computes the valid code for a secret at a given step (test helper).
func codeFor(t *testing.T, secret string, step uint64) string {
	t.Helper()
	code, err := hotp.GenerateCodeCustom(secret, step, hotp.ValidateOpts{
		Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestValidateAcceptsCurrentStep(t *testing.T) {
	secret, _ := GenerateSecret("X", "Y")
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	ok, got, err := Validate(secret, codeFor(t, secret, step), 0, now)
	if err != nil || !ok {
		t.Fatalf("want valid, got ok=%v err=%v", ok, err)
	}
	if got != step {
		t.Fatalf("want step %d, got %d", step, got)
	}
}

func TestValidateAcceptsSkewWindow(t *testing.T) {
	secret, _ := GenerateSecret("X", "Y")
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	for _, s := range []uint64{step - 1, step + 1} {
		if ok, _, _ := Validate(secret, codeFor(t, secret, s), 0, now); !ok {
			t.Fatalf("step %d within ±1 window should be accepted", s)
		}
	}
}

func TestValidateRejectsOutOfWindow(t *testing.T) {
	secret, _ := GenerateSecret("X", "Y")
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	if ok, _, _ := Validate(secret, codeFor(t, secret, step+2), 0, now); ok {
		t.Fatal("step+2 should be rejected")
	}
}

func TestValidateRejectsReplay(t *testing.T) {
	secret, _ := GenerateSecret("X", "Y")
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	// lastStep == current step: the current code was already used.
	if ok, _, _ := Validate(secret, codeFor(t, secret, step), step, now); ok {
		t.Fatal("code at/below lastStep must be rejected as replay")
	}
}

func TestValidateRejectsWrongCode(t *testing.T) {
	secret, _ := GenerateSecret("X", "Y")
	now := time.Unix(1_700_000_000, 0)
	if ok, _, _ := Validate(secret, "000000", 0, now); ok {
		t.Fatal("000000 should not validate (overwhelmingly likely)")
	}
}
