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
	"context"
	"net"
	"net/netip"
)

// trustList holds trusted client network prefixes. An empty list trusts
// nobody. It is a plain []netip.Prefix for easy construction in tests.
type trustList []netip.Prefix

// Contains reports whether addr's host IP falls in any trusted network.
func (t trustList) Contains(addr net.Addr) bool {
	if len(t) == 0 {
		return false
	}
	host := ipKey(addr) // reuses limiter.go: host portion, or "" if unparseable
	if host == "" {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap() // normalize 4-in-6 so v4 prefixes match
	for _, p := range t {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// TrustChecker decides whether a network address is a trusted client.
// A trusted client is exempt from the pre-auth idle/ceiling timers and the
// per-IP connection cap (it is still counted toward the global cap).
// nil is a valid implementation that trusts nobody.
type TrustChecker interface {
	IsTrusted(addr net.Addr) bool
}

// TrustStore is the minimal interface StoreTrustChecker needs from the store.
type TrustStore interface {
	LoadTrustedPrefixes(ctx context.Context) ([]netip.Prefix, error)
}

// StoreTrustChecker implements TrustChecker by loading prefixes from the DB
// on each check. A per-accept DB read is fine: the data is cold and
// human-cadence, and it means admin edits apply to new connections immediately.
// A DB error is treated as untrusted (fail-closed).
type StoreTrustChecker struct {
	store TrustStore
}

// NewStoreTrustChecker returns a TrustChecker backed by the given store.
func NewStoreTrustChecker(st TrustStore) *StoreTrustChecker {
	return &StoreTrustChecker{store: st}
}

// IsTrusted reports whether addr's host IP falls in any trusted network
// currently stored in the DB.
func (c *StoreTrustChecker) IsTrusted(addr net.Addr) bool {
	prefixes, err := c.store.LoadTrustedPrefixes(context.Background())
	if err != nil {
		return false
	}
	return trustList(prefixes).Contains(addr)
}

// StaticTrustChecker returns a TrustChecker backed by a fixed set of
// prefixes. Primarily useful in tests.
func StaticTrustChecker(prefixes ...netip.Prefix) TrustChecker {
	return staticTrust(prefixes)
}

type staticTrust []netip.Prefix

func (s staticTrust) IsTrusted(addr net.Addr) bool {
	return trustList(s).Contains(addr)
}
