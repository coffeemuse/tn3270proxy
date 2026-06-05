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

package server

import (
	"net"
	"net/netip"
	"testing"
)

func tcp(addr string) net.Addr { return &net.TCPAddr{IP: net.ParseIP(addr), Port: 23} }

func TestTrustListContains(t *testing.T) {
	tl := trustList{
		netip.MustParsePrefix("10.0.0.0/24"),
		netip.MustParsePrefix("192.168.1.5/32"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	cases := []struct {
		addr net.Addr
		want bool
	}{
		{tcp("10.0.0.7"), true},
		{tcp("10.0.1.7"), false},
		{tcp("192.168.1.5"), true},
		{tcp("192.168.1.6"), false},
		{tcp("2001:db8::1"), true},
		{tcp("2001:dead::1"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := tl.Contains(c.addr); got != c.want {
			t.Errorf("Contains(%v) = %v, want %v", c.addr, got, c.want)
		}
	}
}

func TestTrustListEmptyIsAlwaysFalse(t *testing.T) {
	var tl trustList
	if tl.Contains(tcp("10.0.0.1")) {
		t.Error("empty trust list must trust nobody")
	}
}
