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

package dummy

import (
	"net"
	"testing"
	"time"
)

func TestListenRejectsBadAddr(t *testing.T) {
	if _, err := Listen("not-an-addr"); err == nil {
		t.Fatal("expected error binding a bad address")
	}
}

func TestServeAcceptsConnections(t *testing.T) {
	s, err := Listen("127.0.0.1:0") // port 0 -> OS picks a free port
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer s.Close()
	go s.Serve()

	conn, err := net.DialTimeout("tcp", s.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("dial accepted listener: %v", err)
	}
	conn.Close()
}
