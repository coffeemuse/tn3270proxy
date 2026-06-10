package quickstart

import "github.com/coffeemuse/tn3270proxy/internal/seed"

// buildSeedData turns generated credentials into a seed.SeedData: all users
// with their group memberships, the DEMO group, and the DEMO service pointing
// at the dummy3270 sibling container (plaintext), visible to the DEMO group.
func buildSeedData(r Result) seed.SeedData {
	users := []seed.SeedUser{{Username: r.Admin.Username, Password: r.Admin.Password, Groups: r.Admin.Groups}}
	for _, s := range r.Samples {
		users = append(users, seed.SeedUser{Username: s.Username, Password: s.Password, Groups: s.Groups})
	}
	return seed.SeedData{
		Groups: []string{DemoGroup},
		Users:  users,
		Services: []seed.SeedService{{
			Name:        DemoServiceName,
			Description: "Demo service (dummy3270)",
			Host:        DemoBackendHost,
			Port:        DemoBackendPort,
			TLS:         false,
			Groups:      []string{DemoGroup},
		}},
	}
}
