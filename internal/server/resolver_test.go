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
	"errors"
	"testing"
)

type fakeResolver struct {
	names []string
	err   error
}

func (f fakeResolver) LookupAddr(context.Context, string) ([]string, error) {
	return f.names, f.err
}

func TestPTR(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		addr string
		res  Resolver
		want string
	}{
		{"present strips trailing dot", "203.0.113.9:54221", fakeResolver{names: []string{"host.example.de."}}, "host.example.de"},
		{"no record", "203.0.113.9:54221", fakeResolver{names: nil}, "(none)"},
		{"lookup error", "203.0.113.9:54221", fakeResolver{err: errors.New("nx")}, "(unavailable)"},
		{"bare ip no port", "203.0.113.9", fakeResolver{names: []string{"a.example."}}, "a.example"},
		{"empty addr", "", fakeResolver{names: []string{"x."}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ptr(ctx, c.res, c.addr); got != c.want {
				t.Errorf("ptr(%q) = %q, want %q", c.addr, got, c.want)
			}
		})
	}
}
