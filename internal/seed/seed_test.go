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

package seed

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func TestApplySeedsUsersGroupsServices(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	data := SeedData{
		Groups: []string{"ops", "dev"},
		Users: []SeedUser{
			{Username: "alice", Password: "s3cret", Groups: []string{"ops"}},
		},
		Services: []SeedService{
			{Name: "PRODCICS", Description: "Production CICS", Host: "prod", Port: 23, Groups: []string{"ops"}},
			{Name: "TESTCICS", Description: "Test CICS", Host: "test", Port: 992, TLS: true, Groups: []string{"dev"}},
		},
	}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	id, err := auth.Authenticate(ctx, st, "alice", "s3cret")
	if err != nil {
		t.Fatalf("authenticate seeded user: %v", err)
	}
	if len(id.Groups) != 1 || id.Groups[0] != "OPS" {
		t.Errorf("groups = %v", id.Groups)
	}

	svcs, err := st.ListServicesForGroups(ctx, id.Groups)
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || svcs[0].Name != "PRODCICS" {
		t.Errorf("services = %+v", svcs)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	st, _ := store.Open(filepath.Join(t.TempDir(), "seed.db"))
	defer st.Close()
	data := SeedData{
		Groups:   []string{"ops"},
		Users:    []SeedUser{{Username: "alice", Password: "pw", Groups: []string{"ops"}}},
		Services: []SeedService{{Name: "PROD", Description: "Production", Host: "h", Port: 23, Groups: []string{"ops"}}},
	}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	svcs, _ := st.ListServicesForGroups(ctx, []string{"ops"})
	if len(svcs) != 1 {
		t.Errorf("expected 1 service after double seed, got %d", len(svcs))
	}
}

func TestApplySeedsServiceDescription(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	data := SeedData{Services: []SeedService{
		{Name: "prodcics", Description: "Production CICS", Host: "h", Port: 23},
	}}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatalf("apply: %v", err)
	}
	svcs, _ := st.ListAllServices(ctx)
	if len(svcs) != 1 || svcs[0].Name != "PRODCICS" || svcs[0].Description != "Production CICS" {
		t.Fatalf("services = %+v, want PRODCICS/Production CICS", svcs)
	}
}

func TestApplyRejectsTooLongPasswordWithoutPartialWrite(t *testing.T) {
	ctx := context.Background()
	st, _ := store.Open(filepath.Join(t.TempDir(), "seed.db"))
	defer st.Close()
	data := SeedData{
		Users: []SeedUser{
			{Username: "alice", Password: "ok"},
			{Username: "bob", Password: strings.Repeat("a", 73)}, // > 72-byte bcrypt limit
		},
	}
	err := Apply(ctx, st, data)
	if !errors.Is(err, auth.ErrPasswordTooLong) {
		t.Fatalf("Apply err = %v, want ErrPasswordTooLong", err)
	}
	// Pre-validation must run before any writes: the earlier valid user must
	// NOT have been committed (no partial state).
	if _, err := st.GetUserByUsername(ctx, "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("alice should not be created when a later user is invalid: %v", err)
	}
}

func TestApplyVerifyDefaultsOnWhenOmitted(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	verifyOff := false
	verifyOn := true
	data := SeedData{
		Groups: []string{"ops"},
		Services: []SeedService{
			// Verify omitted (nil) → must default to ON.
			{Name: "DEFON", Description: "Default On", Host: "a", Port: 992, TLS: true, Groups: []string{"ops"}},
			// Verify explicitly false → must stay OFF.
			{Name: "OFF", Description: "Verify Off", Host: "b", Port: 992, TLS: true, Verify: &verifyOff, Groups: []string{"ops"}},
			{Name: "ON", Description: "Verify On", Host: "c", Port: 992, TLS: true, Verify: &verifyOn, Groups: []string{"ops"}},
		},
	}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	svcs, err := st.ListServicesForGroups(ctx, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]store.Service{}
	for _, s := range svcs {
		byName[s.Name] = s
	}
	if !byName["DEFON"].TLSVerify {
		t.Errorf("omitted verify should default ON, got %+v", byName["DEFON"])
	}
	if byName["OFF"].TLSVerify {
		t.Errorf("explicit verify=false should stay OFF, got %+v", byName["OFF"])
	}
	if !byName["ON"].TLSVerify {
		t.Errorf("explicit verify=true should stay ON, got %+v", byName["ON"])
	}
}
