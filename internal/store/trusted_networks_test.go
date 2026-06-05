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
	"net/netip"
	"strings"
	"testing"
)

// --- ParseTrustedCIDR ---

func TestParseTrustedCIDRAcceptsCIDR(t *testing.T) {
	p, err := ParseTrustedCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.String() != "10.0.0.0/24" {
		t.Errorf("got %q, want 10.0.0.0/24", p.String())
	}
}

func TestParseTrustedCIDRExpandsBareIPv4(t *testing.T) {
	p, err := ParseTrustedCIDR("192.168.1.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.String() != "192.168.1.5/32" {
		t.Errorf("got %q, want 192.168.1.5/32", p.String())
	}
}

func TestParseTrustedCIDRExpandsBareIPv6(t *testing.T) {
	p, err := ParseTrustedCIDR("2001:db8::1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.String() != "2001:db8::1/128" {
		t.Errorf("got %q, want 2001:db8::1/128", p.String())
	}
}

func TestParseTrustedCIDRMasksHostBits(t *testing.T) {
	// "10.0.0.1/24" has host bits set; Masked() must zero them → "10.0.0.0/24".
	p, err := ParseTrustedCIDR("10.0.0.1/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.String() != "10.0.0.0/24" {
		t.Errorf("got %q, want 10.0.0.0/24 (host bits zeroed)", p.String())
	}
}

func TestParseTrustedCIDRRejectsGarbage(t *testing.T) {
	cases := []string{"", "not-an-ip", "300.0.0.1", "10.0.0.0/33"}
	for _, c := range cases {
		if _, err := ParseTrustedCIDR(c); err == nil {
			t.Errorf("ParseTrustedCIDR(%q): want error, got nil", c)
		}
	}
}

// --- ValidateComment ---

func TestValidateCommentAccepts(t *testing.T) {
	if err := ValidateComment("internal OEC"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateCommentRejectsEmpty(t *testing.T) {
	if err := ValidateComment(""); !strings.Contains(err.Error(), "required") {
		t.Errorf("got %v, want required error", err)
	}
}

func TestValidateCommentRejectsTooLong(t *testing.T) {
	long := strings.Repeat("x", MaxCommentLen+1)
	if err := ValidateComment(long); err == nil {
		t.Error("want error for overlong comment, got nil")
	}
}

func TestValidateCommentAcceptsExactlyMaxLen(t *testing.T) {
	ok := strings.Repeat("x", MaxCommentLen)
	if err := ValidateComment(ok); err != nil {
		t.Errorf("unexpected error at max len: %v", err)
	}
}

// --- CRUD ---

func TestCreateTrustedNetworkHappyPath(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	id, err := st.CreateTrustedNetwork(ctx, "10.0.0.0/24", "internal OEC")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Error("id should be non-zero")
	}
	networks, err := st.ListTrustedNetworks(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(networks) != 1 || networks[0].CIDR != "10.0.0.0/24" || networks[0].Comment != "internal OEC" {
		t.Errorf("networks = %+v", networks)
	}
}

func TestCreateTrustedNetworkExpandsBareIP(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if _, err := st.CreateTrustedNetwork(ctx, "192.168.1.5", "dev workstation"); err != nil {
		t.Fatalf("create: %v", err)
	}
	nets, _ := st.ListTrustedNetworks(ctx)
	if len(nets) != 1 || nets[0].CIDR != "192.168.1.5/32" {
		t.Errorf("stored CIDR = %q, want 192.168.1.5/32", nets[0].CIDR)
	}
}

func TestCreateTrustedNetworkIsIdempotent(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	id1, err := st.CreateTrustedNetwork(ctx, "10.0.0.0/24", "first comment")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	id2, err := st.CreateTrustedNetwork(ctx, "10.0.0.0/24", "second comment")
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if id1 != id2 {
		t.Errorf("dedup: id1=%d id2=%d, want same", id1, id2)
	}
	nets, _ := st.ListTrustedNetworks(ctx)
	if len(nets) != 1 {
		t.Errorf("dedup: got %d rows, want 1", len(nets))
	}
}

func TestCreateTrustedNetworkValidationErrors(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if _, err := st.CreateTrustedNetwork(ctx, "bad", "comment"); err == nil {
		t.Error("bad CIDR: want error, got nil")
	}
	if _, err := st.CreateTrustedNetwork(ctx, "10.0.0.0/24", ""); err == nil {
		t.Error("empty comment: want error, got nil")
	}
	if _, err := st.CreateTrustedNetwork(ctx, "10.0.0.0/24", strings.Repeat("x", MaxCommentLen+1)); err == nil {
		t.Error("overlong comment: want error, got nil")
	}
}

func TestListTrustedNetworksOrdered(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	st.CreateTrustedNetwork(ctx, "192.168.0.0/16", "z last")
	st.CreateTrustedNetwork(ctx, "10.0.0.0/8", "a first")
	nets, err := st.ListTrustedNetworks(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(nets) != 2 || nets[0].CIDR != "10.0.0.0/8" || nets[1].CIDR != "192.168.0.0/16" {
		t.Errorf("order wrong: %+v", nets)
	}
}

func TestUpdateTrustedNetworkHappyPath(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	id, _ := st.CreateTrustedNetwork(ctx, "10.0.0.0/24", "original")
	if err := st.UpdateTrustedNetwork(ctx, id, "10.0.1.0/24", "updated"); err != nil {
		t.Fatalf("update: %v", err)
	}
	nets, _ := st.ListTrustedNetworks(ctx)
	if len(nets) != 1 || nets[0].CIDR != "10.0.1.0/24" || nets[0].Comment != "updated" {
		t.Errorf("after update: %+v", nets)
	}
}

func TestUpdateTrustedNetworkNotFound(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if err := st.UpdateTrustedNetwork(ctx, 99999, "10.0.0.0/24", "comment"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing id: got %v, want ErrNotFound", err)
	}
}

func TestUpdateTrustedNetworkValidationErrors(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	id, _ := st.CreateTrustedNetwork(ctx, "10.0.0.0/24", "comment")
	if err := st.UpdateTrustedNetwork(ctx, id, "bad", "comment"); err == nil {
		t.Error("bad CIDR on update: want error")
	}
	if err := st.UpdateTrustedNetwork(ctx, id, "10.0.0.0/24", ""); err == nil {
		t.Error("empty comment on update: want error")
	}
}

func TestDeleteTrustedNetwork(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	id, _ := st.CreateTrustedNetwork(ctx, "10.0.0.0/24", "to delete")
	if err := st.DeleteTrustedNetwork(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	nets, _ := st.ListTrustedNetworks(ctx)
	if len(nets) != 0 {
		t.Errorf("after delete: %+v", nets)
	}
	// Deleting an absent id is a silent no-op.
	if err := st.DeleteTrustedNetwork(ctx, id); err != nil {
		t.Errorf("re-delete: want nil, got %v", err)
	}
}

func TestLoadTrustedPrefixes(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	st.CreateTrustedNetwork(ctx, "10.0.0.0/24", "net A")
	st.CreateTrustedNetwork(ctx, "192.168.1.5", "host B")
	prefixes, err := st.LoadTrustedPrefixes(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
		netip.MustParsePrefix("192.168.1.5/32"),
	}
	if len(prefixes) != len(want) {
		t.Fatalf("got %d prefixes, want %d: %v", len(prefixes), len(want), prefixes)
	}
	for i, w := range want {
		if prefixes[i] != w {
			t.Errorf("prefixes[%d] = %v, want %v", i, prefixes[i], w)
		}
	}
}

func TestLoadTrustedPrefixesEmpty(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	prefixes, err := st.LoadTrustedPrefixes(ctx)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if len(prefixes) != 0 {
		t.Errorf("want empty, got %v", prefixes)
	}
}
