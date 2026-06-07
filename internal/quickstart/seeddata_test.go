package quickstart

import (
	"slices"
	"testing"
)

func TestBuildSeedData(t *testing.T) {
	r := Result{
		Admin:   Cred{Username: "ADMIN", Password: "p1", Groups: []string{"ZZADMIN", "DEMO"}},
		Samples: []Cred{{Username: "OPERATOR", Password: "p2", Groups: []string{"DEMO"}}, {Username: "GUEST", Password: "p3", Groups: []string{"DEMO"}}},
	}
	d := buildSeedData(r)
	if len(d.Users) != 3 {
		t.Fatalf("want 3 users, got %d", len(d.Users))
	}
	if len(d.Services) != 1 || d.Services[0].Name != "DEMO" || d.Services[0].Host != "dummy3270" || d.Services[0].Port != 3300 {
		t.Fatalf("want one DEMO service to dummy3270:3300, got %+v", d.Services)
	}
	if d.Services[0].TLS {
		t.Errorf("demo service must be plaintext")
	}
	if !slices.Contains(d.Services[0].Groups, "DEMO") {
		t.Errorf("demo service must be visible to DEMO group, got %v", d.Services[0].Groups)
	}
	found := false
	for _, u := range d.Users {
		if u.Username == "ADMIN" {
			found = true
			if !slices.Contains(u.Groups, "ZZADMIN") || !slices.Contains(u.Groups, "DEMO") {
				t.Errorf("ADMIN must be in ZZADMIN and DEMO, got %v", u.Groups)
			}
		}
	}
	if !found {
		t.Fatalf("ADMIN user not present in seed data")
	}
}
