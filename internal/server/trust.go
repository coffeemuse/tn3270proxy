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
)

// trustList holds the operator-configured trusted client networks (GH #18).
// A trusted client is exempt from the pre-auth idle/ceiling timers and the
// per-IP connection cap (it is still counted toward the global cap). An empty
// list trusts nobody. It is a plain []netip.Prefix so config's already-parsed
// prefixes convert in for free.
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
