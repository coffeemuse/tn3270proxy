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
	"bytes"
	"testing"
)

func testKey() []byte {
	k := make([]byte, KeyLen)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	c, err := NewCipher(testKey())
	if err != nil {
		t.Fatal(err)
	}
	ct, err := c.Seal([]byte("JBSWY3DPEHPK3PXP"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := c.Open(ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, []byte("JBSWY3DPEHPK3PXP")) {
		t.Fatalf("round trip mismatch: %q", pt)
	}
}

func TestSealIsNondeterministic(t *testing.T) {
	c, _ := NewCipher(testKey())
	a, _ := c.Seal([]byte("secret"))
	b, _ := c.Seal([]byte("secret"))
	if a == b {
		t.Fatal("two seals of the same plaintext must differ (random nonce)")
	}
}

func TestOpenWrongKeyFails(t *testing.T) {
	c1, _ := NewCipher(testKey())
	ct, _ := c1.Seal([]byte("secret"))
	other := testKey()
	other[0] ^= 0xFF
	c2, _ := NewCipher(other)
	if _, err := c2.Open(ct); err != ErrDecrypt {
		t.Fatalf("want ErrDecrypt, got %v", err)
	}
}

func TestNewCipherRejectsBadKeyLen(t *testing.T) {
	if _, err := NewCipher([]byte("short")); err == nil {
		t.Fatal("want error for 5-byte key")
	}
}

func TestOpenGarbageFails(t *testing.T) {
	c, _ := NewCipher(testKey())
	if _, err := c.Open("not-base64-!!!"); err != ErrDecrypt {
		t.Fatalf("want ErrDecrypt for bad base64, got %v", err)
	}
}
