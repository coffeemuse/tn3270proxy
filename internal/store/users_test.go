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
	"strings"
	"testing"
)

func TestValidateFullName(t *testing.T) {
	if err := ValidateFullName(""); err != nil {
		t.Errorf("empty full name should be allowed: %v", err)
	}
	if err := ValidateFullName("Robert Lawrence"); err != nil {
		t.Errorf("normal full name rejected: %v", err)
	}
	long := strings.Repeat("a", MaxDescriptionLen+1)
	if err := ValidateFullName(long); err == nil {
		t.Errorf("over-length full name should be rejected")
	}
}

func TestUserAndGroupRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	uid, err := st.CreateUser(ctx, "alice", "hash-a")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	gid, err := st.CreateGroup(ctx, "ops")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := st.AddUserToGroup(ctx, uid, gid); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}

	u, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.ID != uid || u.PasswordHash != "hash-a" {
		t.Errorf("got %+v", u)
	}

	groups, err := st.GetUserGroups(ctx, uid)
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 1 || groups[0] != "OPS" {
		t.Errorf("groups = %v, want [OPS]", groups)
	}
}

func TestUserDetailsDefaultEmpty(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if _, err := st.CreateUser(ctx, "alice", "hash-a"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.FullName != "" || u.Email != "" {
		t.Errorf("new user details = %q/%q, want empty/empty", u.FullName, u.Email)
	}
}

func TestGetUserByUsernameNotFound(t *testing.T) {
	st := newTestStore(t)
	_, err := st.GetUserByUsername(context.Background(), "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateGroupIdempotent(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	id1, err := st.CreateGroup(ctx, "ops")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := st.CreateGroup(ctx, "ops")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("CreateGroup not idempotent: %d != %d", id1, id2)
	}
}
