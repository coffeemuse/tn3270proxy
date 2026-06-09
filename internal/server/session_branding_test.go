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

package server

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
)

func TestLoginBranding(t *testing.T) {
	ctx := context.Background()
	mk := func(t *testing.T) *Session {
		st, err := store.Open(t.TempDir() + "/s.db")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		return &Session{Store: st}
	}

	t.Run("disabled when empty", func(t *testing.T) {
		s := mk(t)
		if got := s.loginBranding(ctx); got != nil {
			t.Errorf("empty path = %v, want nil", got)
		}
	})

	t.Run("relative path skipped", func(t *testing.T) {
		s := mk(t)
		s.Store.SetConfig(ctx, sysconfig.KeyBrandingFile, "branding.txt")
		s.BrandingRead = func(string) ([]byte, error) { t.Fatal("should not read a relative path"); return nil, nil }
		if got := s.loginBranding(ctx); got != nil {
			t.Errorf("relative path = %v, want nil", got)
		}
	})

	t.Run("unreadable skipped", func(t *testing.T) {
		s := mk(t)
		s.Store.SetConfig(ctx, sysconfig.KeyBrandingFile, "/abs/missing.txt")
		s.BrandingRead = func(string) ([]byte, error) { return nil, errors.New("nope") }
		if got := s.loginBranding(ctx); got != nil {
			t.Errorf("unreadable = %v, want nil", got)
		}
	})

	t.Run("valid splits lines", func(t *testing.T) {
		s := mk(t)
		s.Store.SetConfig(ctx, sysconfig.KeyBrandingFile, "/abs/branding.txt")
		s.BrandingRead = func(string) ([]byte, error) { return []byte("ALPHA\nBETA\n"), nil }
		got := s.loginBranding(ctx)
		if len(got) != 2 || got[0] != "ALPHA" || got[1] != "BETA" {
			t.Errorf("got %v, want [ALPHA BETA]", got)
		}
	})
}

// TestReadBrandingCapped exercises the real default reader (not the injected
// BrandingRead seam, which bypasses the cap): an over-cap file is truncated to
// brandingReadCap bytes, not rejected.
func TestReadBrandingCapped(t *testing.T) {
	path := t.TempDir() + "/big.txt"
	if err := os.WriteFile(path, bytes.Repeat([]byte("X"), brandingReadCap+4096), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := readBrandingCapped(path)
	if err != nil {
		t.Fatalf("readBrandingCapped: %v", err)
	}
	if len(data) != brandingReadCap {
		t.Errorf("read %d bytes, want cap %d", len(data), brandingReadCap)
	}
}
