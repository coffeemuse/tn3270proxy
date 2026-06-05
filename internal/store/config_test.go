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

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
)

func TestMigrateSeedsCatalogDefaults(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for _, e := range sysconfig.Catalog {
		val, err := st.GetConfig(ctx, e.Key)
		if err != nil {
			t.Errorf("GetConfig(%q) after migrate: %v", e.Key, err)
			continue
		}
		if val != e.Default {
			t.Errorf("GetConfig(%q) = %q, want default %q", e.Key, val, e.Default)
		}
	}
}

func TestMigrateSeedingIsIdempotent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// Set MOTD_FILE to a non-default value.
	if err := st.SetConfig(ctx, "MOTD_FILE", "/etc/motd"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	// Re-running migrate must not reset the value.
	if err := st.migrate(); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	val, err := st.GetConfig(ctx, "MOTD_FILE")
	if err != nil {
		t.Fatalf("GetConfig after re-migrate: %v", err)
	}
	if val != "/etc/motd" {
		t.Errorf("re-migrate reset value: got %q, want %q", val, "/etc/motd")
	}
}

func TestGetConfigNotFound(t *testing.T) {
	st := newTestStore(t)
	_, err := st.GetConfig(context.Background(), "NO_SUCH_KEY")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetConfig missing key = %v, want ErrNotFound", err)
	}
}

func TestSetConfigHappyPath(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.SetConfig(ctx, "MOTD_FILE", "/var/motd.txt"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	val, err := st.GetConfig(ctx, "MOTD_FILE")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if val != "/var/motd.txt" {
		t.Errorf("GetConfig = %q, want %q", val, "/var/motd.txt")
	}
}

func TestSetConfigNotFound(t *testing.T) {
	st := newTestStore(t)
	err := st.SetConfig(context.Background(), "NO_SUCH_KEY", "val")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SetConfig missing key = %v, want ErrNotFound", err)
	}
}

func TestGetConfigCaseInsensitiveKey(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// MOTD_FILE was seeded uppercase; querying in lower case must still work
	// because the column is COLLATE NOCASE.
	val, err := st.GetConfig(ctx, "motd_file")
	if err != nil {
		t.Fatalf("GetConfig lowercase key: %v", err)
	}
	if val != "" {
		t.Errorf("default value = %q, want empty", val)
	}
}
