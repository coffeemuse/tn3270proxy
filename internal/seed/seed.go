// Package seed populates the store with users, groups, and services from a
// declarative description (used by the `seed` subcommand and tests).
package seed

import (
	"context"
	"fmt"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// SeedUser describes one user to create.
type SeedUser struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	Groups   []string `json:"groups"`
}

// SeedService describes one service to create.
type SeedService struct {
	Name   string   `json:"name"`
	Host   string   `json:"host"`
	Port   int      `json:"port"`
	TLS    bool     `json:"tls"`
	Verify *bool    `json:"verify"` // omitted → verify ON (secure default)
	Groups []string `json:"groups"`
}

// SeedData is the full set of records to apply.
type SeedData struct {
	Groups   []string      `json:"groups"`
	Users    []SeedUser    `json:"users"`
	Services []SeedService `json:"services"`
}

// Apply creates the groups, users, and services described by data, linking
// memberships and access. It is idempotent: re-applying the same data does not
// create duplicates.
func Apply(ctx context.Context, st *store.Store, data SeedData) error {
	// Pre-validate every password before any writes: Apply is non-transactional,
	// so a password rejected mid-run (e.g. bcrypt's 72-byte limit) would leave
	// earlier users already committed. Failing up front keeps the store clean.
	for _, u := range data.Users {
		if err := auth.ValidatePassword(u.Password); err != nil {
			return fmt.Errorf("user %q: %w", u.Username, err)
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
	}

	for _, svc := range data.Services {
		// CreateService is INSERT OR IGNORE: re-seeding an existing service does
		// not update tls/tls_verify. Change these via a manual UPDATE for now.
		verify := true // secure default when "verify" is omitted
		if svc.Verify != nil {
			verify = *svc.Verify
		}
		sid, err := st.CreateService(ctx, svc.Name, svc.Host, svc.Port, svc.TLS, verify)
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
