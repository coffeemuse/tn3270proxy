package seed

import (
	"context"
	"path/filepath"
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
			{Name: "PROD CICS", Host: "prod", Port: 23, Groups: []string{"ops"}},
			{Name: "TEST CICS", Host: "test", Port: 992, TLS: true, Groups: []string{"dev"}},
		},
	}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	id, err := auth.Authenticate(ctx, st, "alice", "s3cret")
	if err != nil {
		t.Fatalf("authenticate seeded user: %v", err)
	}
	if len(id.Groups) != 1 || id.Groups[0] != "ops" {
		t.Errorf("groups = %v", id.Groups)
	}

	svcs, err := st.ListServicesForGroups(ctx, id.Groups)
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || svcs[0].Name != "PROD CICS" {
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
		Services: []SeedService{{Name: "PROD", Host: "h", Port: 23, Groups: []string{"ops"}}},
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
