package store

import (
	"context"
	"testing"
)

func TestListServicesForGroups(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	ops, _ := st.CreateGroup(ctx, "ops")
	dev, _ := st.CreateGroup(ctx, "dev")

	prod, _ := st.CreateService(ctx, "PROD CICS", "prod.example", 23, false, true)
	test, _ := st.CreateService(ctx, "TEST CICS", "test.example", 992, true, true)

	if err := st.LinkGroupService(ctx, ops, prod); err != nil {
		t.Fatal(err)
	}
	if err := st.LinkGroupService(ctx, dev, test); err != nil {
		t.Fatal(err)
	}
	// ops also gets test → ensures dedup when a user is in multiple groups.
	if err := st.LinkGroupService(ctx, ops, test); err != nil {
		t.Fatal(err)
	}

	svcs, err := st.ListServicesForGroups(ctx, []string{"ops", "dev"})
	if err != nil {
		t.Fatalf("ListServicesForGroups: %v", err)
	}
	if len(svcs) != 2 {
		t.Fatalf("got %d services, want 2 (deduped): %+v", len(svcs), svcs)
	}
	// Ordered by name: "PROD CICS" then "TEST CICS".
	if svcs[0].Name != "PROD CICS" || svcs[1].Name != "TEST CICS" {
		t.Errorf("order wrong: %+v", svcs)
	}
	if svcs[1].Port != 992 || !svcs[1].TLS {
		t.Errorf("TEST CICS fields wrong: %+v", svcs[1])
	}
}

func TestCreateServiceVerifyRoundTrips(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	ops, _ := st.CreateGroup(ctx, "ops")
	sid, _ := st.CreateService(ctx, "SEC", "sec.example", 992, true, true)
	st.LinkGroupService(ctx, ops, sid)

	svcs, err := st.ListServicesForGroups(ctx, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || !svcs[0].TLSVerify {
		t.Fatalf("want TLSVerify true by default, got %+v", svcs)
	}
}

func TestListServicesForGroupsEmpty(t *testing.T) {
	st := newTestStore(t)
	svcs, err := st.ListServicesForGroups(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 0 {
		t.Errorf("got %d, want 0", len(svcs))
	}
}
