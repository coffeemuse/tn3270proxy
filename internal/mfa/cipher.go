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

// Package mfa provides TOTP (RFC 6238) generation/validation and AES-256-GCM
// encryption of TOTP secrets at rest. It is pure: no DB, no network.
package mfa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// KeyLen is the required master-key length (AES-256).
const KeyLen = 32

// ErrDecrypt is returned by Open when ciphertext cannot be authenticated with
// the configured key (wrong key, truncated, or tampered data).
var ErrDecrypt = errors.New("mfa: decryption failed")

// Cipher seals and opens TOTP secrets with AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a 32-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeyLen {
		return nil, errors.New("mfa: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Seal encrypts plaintext and returns base64(nonce || ciphertext).
func (c *Cipher) Seal(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open reverses Seal. It returns ErrDecrypt on any authentication failure.
func (c *Cipher) Open(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrDecrypt
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return nil, ErrDecrypt
	}
	pt, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}
