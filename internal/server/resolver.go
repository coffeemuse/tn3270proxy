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
	"strings"
	"time"
)

// Resolver is the slice of *net.Resolver the audit detail screen needs for the
// reverse-DNS (PTR) lookup. *net.Resolver satisfies it directly; tests inject a
// fake. The lookup is advisory only — PTR records are controlled by the owner
// of the IP's reverse zone and are not authenticated (no FCrDNS).
type Resolver interface {
	LookupAddr(ctx context.Context, addr string) (names []string, err error)
}

// ptrTimeout bounds the reverse-DNS lookup so a slow or hostile resolver cannot
// hang the admin's session.
const ptrTimeout = 2 * time.Second

// ptr resolves the first PTR name for the host portion of remoteAddr
// ("host:port" or bare host). Returns "" for an empty address, "(none)" when no
// record exists, and "(unavailable)" on timeout/error. The trailing dot of an
// FQDN is stripped.
func ptr(ctx context.Context, res Resolver, remoteAddr string) string {
	if strings.TrimSpace(remoteAddr) == "" {
		return ""
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ctx, cancel := context.WithTimeout(ctx, ptrTimeout)
	defer cancel()
	names, err := res.LookupAddr(ctx, host)
	if err != nil {
		return "(unavailable)"
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.TrimSuffix(names[0], ".")
}
