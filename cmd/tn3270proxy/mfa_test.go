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

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/mfa"
	"github.com/coffeemuse/tn3270proxy/internal/store"
)

func key32() []byte { return bytes.Repeat([]byte{0x11}, 32) }

func TestMFAStartupFailsClosedWhenEnrolledButNoKey(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/s.db")
	defer st.Close()
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "h")
	st.StoreMFAEnrollment(ctx, uid, "ct", "t", 1)

	if _, err := mfaStartup(ctx, st, nil); err == nil {
		t.Fatal("expected fail-closed error when enrolled users exist but no key")
	}
}

func TestMFAStartupWritesSentinelOnFirstRun(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/s.db")
	defer st.Close()
	ctx := context.Background()
	c, _ := mfa.NewCipher(key32())
	cipher, err := mfaStartup(ctx, st, key32())
	if err != nil {
		t.Fatal(err)
	}
	if cipher == nil {
		t.Fatal("expected a cipher back")
	}
	sent, _ := st.GetMFASentinel(ctx)
	if sent == "" {
		t.Fatal("first run should write the sentinel")
	}
	if _, err := c.Open(sent); err != nil {
		t.Fatalf("sentinel should decrypt under the same key: %v", err)
	}
}

func TestMFAStartupFailsClosedOnWrongKey(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/s.db")
	defer st.Close()
	ctx := context.Background()
	if _, err := mfaStartup(ctx, st, key32()); err != nil {
		t.Fatal(err)
	}
	wrong := bytes.Repeat([]byte{0x22}, 32)
	if _, err := mfaStartup(ctx, st, wrong); err == nil {
		t.Fatal("expected fail-closed error on key mismatch")
	}
}

func TestMFAStartupNoKeyNoEnrolledIsFine(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/s.db")
	defer st.Close()
	cipher, err := mfaStartup(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cipher != nil {
		t.Fatal("no key → nil cipher (MFA disabled)")
	}
}

func TestRunMFAResetAll(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/s.db"
	st, _ := store.Open(dbPath)
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "h")
	c, _ := mfa.NewCipher(key32())
	sealed, _ := c.Seal([]byte("JBSWY3DPEHPK3PXP"))
	st.StoreMFAEnrollment(ctx, uid, sealed, "t", 5)
	// First run also needs a sentinel so reset-all's startup check passes.
	st.SetMFASentinel(ctx, mustSeal(t, c))
	st.Close()

	t.Setenv("TN3270PROXY_MFA_KEY", b64Key(key32()))
	var out bytes.Buffer
	if err := runMFA([]string{"reset-all", "-db", dbPath}, &out); err != nil {
		t.Fatalf("reset-all: %v", err)
	}

	st2, _ := store.Open(dbPath)
	defer st2.Close()
	if n, _ := st2.CountEnrolledUsers(ctx); n != 0 {
		t.Fatalf("want 0 enrolled after reset, got %d", n)
	}
}

func mustSeal(t *testing.T, c *mfa.Cipher) string {
	t.Helper()
	s, err := c.Seal([]byte(mfaSentinelPlaintext))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func b64Key(k []byte) string {
	return base64.StdEncoding.EncodeToString(k)
}
