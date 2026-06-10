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
)

// MaxCommentLen is the byte cap on a trusted network's operator annotation
// (matches the 40-char admin form field).
const MaxCommentLen = 40

// TrustedNetwork is a trusted client network record.
type TrustedNetwork struct {
	ID      int64
	CIDR    string
	Comment string
}

// ParseTrustedCIDR validates and normalizes a trusted network string.
// A bare IP is expanded to a host route (/32 for IPv4, /128 for IPv6).
// The returned prefix is in canonical masked form (host bits zeroed).
func ParseTrustedCIDR(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Prefix{}, errors.New("CIDR is required")
	}
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked(), nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, errors.New("must be a valid IP address or CIDR (e.g. 10.0.0.0/24 or 192.168.1.5)")
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// ValidateComment enforces a required, length-bounded operator annotation.
func ValidateComment(comment string) error {
	if comment == "" {
		return errors.New("comment is required")
	}
	if len(comment) > MaxCommentLen {
		return errors.New("comment must be 40 characters or fewer")
	}
	return nil
}

// CreateTrustedNetwork inserts a trusted network, or returns the existing
// record's id if the CIDR already exists (idempotent, mirrors CreateService).
func (s *Store) CreateTrustedNetwork(ctx context.Context, cidr, comment string) (int64, error) {
	p, err := ParseTrustedCIDR(cidr)
	if err != nil {
		return 0, err
	}
	if err := ValidateComment(comment); err != nil {
		return 0, err
	}
	canonical := p.String()
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO trusted_networks (cidr, comment) VALUES (?, ?)",
		[]any{canonical, comment},
		"SELECT id FROM trusted_networks WHERE cidr = ?",
		[]any{canonical})
}

// ListTrustedNetworks returns all trusted networks ordered by CIDR.
func (s *Store) ListTrustedNetworks(ctx context.Context) ([]TrustedNetwork, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, cidr, comment FROM trusted_networks ORDER BY cidr")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrustedNetwork
	for rows.Next() {
		var n TrustedNetwork
		if err := rows.Scan(&n.ID, &n.CIDR, &n.Comment); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpdateTrustedNetwork replaces the CIDR and comment of an existing entry.
// Returns ErrNotFound for an unknown id.
func (s *Store) UpdateTrustedNetwork(ctx context.Context, id int64, cidr, comment string) error {
	p, err := ParseTrustedCIDR(cidr)
	if err != nil {
		return err
	}
	if err := ValidateComment(comment); err != nil {
		return err
	}
	return s.execExpectingRow(ctx,
		"UPDATE trusted_networks SET cidr = ?, comment = ? WHERE id = ?",
		p.String(), comment, id)
}

// DeleteTrustedNetwork removes a trusted network by id. Deleting an absent id
// is a silent no-op (the admin always deletes from a just-fetched list).
func (s *Store) DeleteTrustedNetwork(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM trusted_networks WHERE id = ?", id)
	return err
}

// LoadTrustedPrefixes returns all trusted network prefixes for the live trust
// check. A per-accept DB read is fine: the data is human-cadence and tiny,
// and it means admin edits apply to new connections immediately.
func (s *Store) LoadTrustedPrefixes(ctx context.Context) ([]netip.Prefix, error) {
	networks, err := s.ListTrustedNetworks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Prefix, 0, len(networks))
	for _, n := range networks {
		p, err := netip.ParsePrefix(n.CIDR)
		if err != nil {
			continue // skip malformed rows; store validates on write so this is a safety net
		}
		out = append(out, p)
	}
	return out, nil
}
