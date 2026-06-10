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

// Package seed populates the store with users, groups, and services from a
// declarative description (used by the `seed` subcommand and tests).
package seed

import (
	"context"
	"errors"
	"fmt"

	"github.com/coffeemuse/tn3270proxy/internal/auth"
	"github.com/coffeemuse/tn3270proxy/internal/store"
)

// SeedUser describes one user to create.
type SeedUser struct {
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	Groups             []string `json:"groups"`
	UserSettingsLocked bool     `json:"user_settings_locked"` // omitted → unlocked
}

// SeedService describes one service to create.
type SeedService struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	TLS         bool     `json:"tls"`
	Verify      *bool    `json:"verify"` // omitted → verify ON (secure default)
	Groups      []string `json:"groups"`
}

// SeedData is the full set of records to apply.
type SeedData struct {
	Groups   []string      `json:"groups"`
	Users    []SeedUser    `json:"users"`
	Services []SeedService `json:"services"`
}

// Apply creates the groups, users, and services described by data, linking
// memberships and access. It is intended for first-run population of a fresh
// database only; re-running against a populated database returns an error naming
// the first conflicting user or service.
func Apply(ctx context.Context, st *store.Store, data SeedData) error {
	// Pre-validate every password before any writes: Apply is non-transactional,
	// so a password rejected mid-run (e.g. bcrypt's 72-byte limit) would leave
	// earlier users already committed. Failing up front keeps the store clean.
	for _, u := range data.Users {
		if err := auth.ValidatePassword(u.Password); err != nil {
			return fmt.Errorf("user %q: %w", u.Username, err)
		}
	}

	// Pre-flight conflict check: seed is a one-time tool. Detect any colliding
	// user or service before the first write so a re-seed fails loudly and
	// atomically (no partial state).
	for _, u := range data.Users {
		if _, err := st.GetUserByUsername(ctx, u.Username); err == nil {
			return fmt.Errorf("user %q already exists; seed is a one-time tool for new installations — use the admin UI to change passwords", u.Username)
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("check user %q: %w", u.Username, err)
		}
	}
	for _, svc := range data.Services {
		if _, err := st.GetServiceByName(ctx, svc.Name); err == nil {
			return fmt.Errorf("service %q already exists; seed is a one-time tool for new installations — use the admin UI to manage services", svc.Name)
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("check service %q: %w", svc.Name, err)
		}
	}

	groupID := make(map[string]int64)
	ensureGroup := func(name string) (int64, error) {
		if id, ok := groupID[name]; ok {
			return id, nil
		}
		id, err := st.CreateGroup(ctx, name)
		if err != nil {
			return 0, fmt.Errorf("create group %q: %w", name, err)
		}
		groupID[name] = id
		return id, nil
	}

	for _, g := range data.Groups {
		if _, err := ensureGroup(g); err != nil {
			return err
		}
	}

	for _, u := range data.Users {
		hash, err := auth.HashPassword(u.Password)
		if err != nil {
			return fmt.Errorf("hash password for %q: %w", u.Username, err)
		}
		uid, err := st.CreateUser(ctx, u.Username, hash)
		if err != nil {
			return fmt.Errorf("create user %q: %w", u.Username, err)
		}
		for _, g := range u.Groups {
			gid, err := ensureGroup(g)
			if err != nil {
				return err
			}
			if err := st.AddUserToGroup(ctx, uid, gid); err != nil {
				return fmt.Errorf("add %q to %q: %w", u.Username, g, err)
			}
		}
		if u.UserSettingsLocked {
			if err := st.SetUserSettingsLocked(ctx, uid, true); err != nil {
				return fmt.Errorf("lock %q: %w", u.Username, err)
			}
		}
	}

	for _, svc := range data.Services {
		verify := true // secure default when "verify" is omitted
		if svc.Verify != nil {
			verify = *svc.Verify
		}
		sid, err := st.CreateService(ctx, svc.Name, svc.Description, svc.Host, svc.Port, svc.TLS, verify)
		if err != nil {
			return fmt.Errorf("create service %q: %w", svc.Name, err)
		}
		for _, g := range svc.Groups {
			gid, err := ensureGroup(g)
			if err != nil {
				return err
			}
			if err := st.LinkGroupService(ctx, gid, sid); err != nil {
				return fmt.Errorf("link %q to %q: %w", svc.Name, g, err)
			}
		}
	}
	return nil
}
