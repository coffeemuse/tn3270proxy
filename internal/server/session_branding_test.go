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
	"context"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/store"
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
		// The BRANDING document is seeded empty by reconcileDefaults.
		s := mk(t)
		if got := s.loginBranding(ctx); got != nil {
			t.Errorf("empty document = %v, want nil", got)
		}
	})

	t.Run("valid splits lines", func(t *testing.T) {
		s := mk(t)
		if err := s.Store.SetDocument(ctx, store.DocBranding, "ALPHA\nBETA\n", "TEST"); err != nil {
			t.Fatalf("SetDocument BRANDING: %v", err)
		}
		got := s.loginBranding(ctx)
		if len(got) != 2 || got[0] != "ALPHA" || got[1] != "BETA" {
			t.Errorf("got %v, want [ALPHA BETA]", got)
		}
	})
}
