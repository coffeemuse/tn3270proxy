package store

import (
	"context"
	"errors"
	"testing"
)

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
	if len(groups) != 1 || groups[0] != "ops" {
		t.Errorf("groups = %v, want [ops]", groups)
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
