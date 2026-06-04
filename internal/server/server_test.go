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
	"sync"
	"testing"
	"time"
)

type handlerFunc func(net.Conn)

func (h handlerFunc) Handle(c net.Conn) { h(c) }

func TestServerAcceptsAndDispatches(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	handled := make(chan struct{}, 1)
	srv := &Server{
		Listener: ln,
		Handler: handlerFunc(func(c net.Conn) {
			defer c.Close()
			handled <- struct{}{}
		}),
	}
	go func() {
		defer wg.Done()
		srv.Serve()
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked")
	}

	ln.Close()
	wg.Wait()
}
