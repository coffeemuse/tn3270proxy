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
	"crypto/subtle"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"
)

const (
	secretBytes = 10 // 80 bits → 16 base32 chars
	periodSecs  = 30 // RFC 6238 default time step
)

// GenerateSecret returns a new random base32 TOTP secret (80-bit, 16 chars).
// issuer/account only label the otpauth provisioning data; manual key entry is
// the 3270 path, so only the secret is returned.
func GenerateSecret(issuer, account string) (string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      periodSecs,
		SecretSize:  secretBytes,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", err
	}
	return key.Secret(), nil
}

// Chunk groups a base32 secret into space-separated 4-char blocks for display:
// "ABCDEFGHIJKLMNOP" → "ABCD EFGH IJKL MNOP".
func Chunk(secret string) string {
	var b strings.Builder
	for i, r := range secret {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Validate checks code against secret at time now, accepting the current step
// ±1 (clock skew). It rejects any code whose step counter is <= lastStep
// (replay). On success it returns the accepted step, which the caller persists
// as the new replay floor.
func Validate(secret, code string, lastStep uint64, now time.Time) (ok bool, step uint64, err error) {
	code = strings.TrimSpace(code)
	current := uint64(now.Unix() / periodSecs)
	// Newest first so the returned step is the highest match.
	for _, d := range []int{1, 0, -1} {
		if d < 0 && current == 0 {
			continue
		}
		cand := current
		switch {
		case d > 0:
			cand = current + uint64(d)
		case d < 0:
			cand = current - uint64(-d)
		}
		want, gerr := hotp.GenerateCodeCustom(secret, cand, hotp.ValidateOpts{
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if gerr != nil {
			return false, 0, gerr
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			if cand <= lastStep {
				return false, 0, nil // replay
			}
			return true, cand, nil
		}
	}
	return false, 0, nil
}
