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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

func openSeedStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestApplySeedsTrustedNetworks(t *testing.T) {
	ctx := context.Background()
	st := openSeedStore(t)

	data := SeedData{TrustedNetworks: []SeedTrustedNetwork{
		{CIDR: "127.0.0.1", Comment: "local OEC controller"},
		{CIDR: "10.1.2.3/24", Comment: "lab LAN"},
		{CIDR: "::1", Comment: "loopback v6"},
	}}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	nets, err := st.ListTrustedNetworks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, n := range nets {
		got[n.CIDR] = n.Comment
	}
	want := map[string]string{
		"127.0.0.1/32": "local OEC controller", // bare IP → host route
		"10.1.2.0/24":  "lab LAN",              // host bits masked
		"::1/128":      "loopback v6",
	}
	if len(got) != len(want) {
		t.Fatalf("trusted networks = %+v, want %v", nets, want)
	}
	for cidr, comment := range want {
		if got[cidr] != comment {
			t.Errorf("trusted network %s: comment = %q, want %q (all: %+v)", cidr, got[cidr], comment, nets)
		}
	}
}

// assertNothingSeeded checks that a rejected Apply left no user, service,
// non-reserved group, or trusted network behind.
func assertNothingSeeded(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := st.GetUserByUsername(ctx, "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("alice must not be created when the seed is rejected: %v", err)
	}
	if _, err := st.GetServiceByName(ctx, "PROD"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("PROD must not be created when the seed is rejected: %v", err)
	}
	groups, err := st.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.Name == "OPS" {
			t.Errorf("group OPS must not be created when the seed is rejected")
		}
	}
	nets, err := st.ListTrustedNetworks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 0 {
		t.Errorf("no trusted network may be created when the seed is rejected, got %+v", nets)
	}
}

// fullSeed is a seed with one of everything plus the given trusted networks,
// so rejection tests can prove nothing at all was written.
func fullSeed(nets ...SeedTrustedNetwork) SeedData {
	return SeedData{
		Groups:          []string{"ops"},
		Users:           []SeedUser{{Username: "alice", Password: "pw", Groups: []string{"ops"}}},
		Services:        []SeedService{{Name: "PROD", Description: "Production", Host: "h", Port: 23, Groups: []string{"ops"}}},
		TrustedNetworks: nets,
	}
}

func TestApplyRejectsInvalidTrustedCIDRWithoutPartialWrite(t *testing.T) {
	st := openSeedStore(t)
	err := Apply(context.Background(), st, fullSeed(
		SeedTrustedNetwork{CIDR: "127.0.0.1", Comment: "good entry first"},
		SeedTrustedNetwork{CIDR: "10.0.0.0/99", Comment: "bad prefix length"},
	))
	if err == nil {
		t.Fatal("Apply with an invalid CIDR should fail, got nil")
	}
	if !strings.Contains(err.Error(), "10.0.0.0/99") {
		t.Errorf("error should name the bad CIDR, got: %v", err)
	}
	assertNothingSeeded(t, st)
}

func TestApplyRejectsBadTrustedCommentWithoutPartialWrite(t *testing.T) {
	for name, comment := range map[string]string{
		"missing":  "",
		"too long": strings.Repeat("x", store.MaxCommentLen+1),
	} {
		t.Run(name, func(t *testing.T) {
			st := openSeedStore(t)
			err := Apply(context.Background(), st, fullSeed(
				SeedTrustedNetwork{CIDR: "192.168.1.5", Comment: comment},
			))
			if err == nil {
				t.Fatal("Apply with a bad comment should fail, got nil")
			}
			if !strings.Contains(err.Error(), "192.168.1.5") {
				t.Errorf("error should name the offending entry, got: %v", err)
			}
			assertNothingSeeded(t, st)
		})
	}
}

func TestApplyRejectsDuplicateTrustedNetworkInSeed(t *testing.T) {
	// "127.0.0.1" and "127.0.0.1/32" normalize to the same row; accepting both
	// would silently drop the second comment.
	st := openSeedStore(t)
	err := Apply(context.Background(), st, fullSeed(
		SeedTrustedNetwork{CIDR: "127.0.0.1", Comment: "first"},
		SeedTrustedNetwork{CIDR: "127.0.0.1/32", Comment: "second"},
	))
	if err == nil {
		t.Fatal("Apply with a duplicate trusted network should fail, got nil")
	}
	if !strings.Contains(err.Error(), "127.0.0.1/32") {
		t.Errorf("error should name the duplicated network, got: %v", err)
	}
	assertNothingSeeded(t, st)
}

func TestApplyBlocksReSeedTrustedNetwork(t *testing.T) {
	ctx := context.Background()
	st := openSeedStore(t)
	if _, err := st.CreateTrustedNetwork(ctx, "127.0.0.1", "added in the admin UI"); err != nil {
		t.Fatal(err)
	}

	// The bare IP matches the existing /32 after normalization.
	err := Apply(ctx, st, fullSeed(SeedTrustedNetwork{CIDR: "127.0.0.1", Comment: "from seed"}))
	if err == nil {
		t.Fatal("re-seed with an existing trusted network should fail, got nil")
	}
	if !strings.Contains(err.Error(), "127.0.0.1/32") {
		t.Errorf("error should name the colliding network, got: %v", err)
	}
	if _, err := st.GetUserByUsername(ctx, "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("alice must not be created when a trusted network conflicts: %v", err)
	}
	nets, _ := st.ListTrustedNetworks(ctx)
	if len(nets) != 1 || nets[0].Comment != "added in the admin UI" {
		t.Errorf("existing trusted network must be left untouched, got %+v", nets)
	}
}

func TestSeedExampleFileAppliesWithTrustedNetworks(t *testing.T) {
	// The shipped example is the format reference; it must pass the same
	// strict Parse the `seed` subcommand uses and apply cleanly.
	raw, err := os.ReadFile(filepath.Join("..", "..", "seed.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse(seed.example.json): %v", err)
	}
	if len(data.TrustedNetworks) == 0 {
		t.Fatal("seed.example.json should demonstrate the trusted_networks key")
	}
	st := openSeedStore(t)
	if err := Apply(context.Background(), st, data); err != nil {
		t.Fatalf("Apply(seed.example.json): %v", err)
	}
	nets, err := st.ListTrustedNetworks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != len(data.TrustedNetworks) {
		t.Errorf("trusted networks = %+v, want %d rows", nets, len(data.TrustedNetworks))
	}
}
