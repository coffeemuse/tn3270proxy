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

// Command dummy3270 is a tiny standalone TN3270 server used as a demo and test
// bridge target for the gateway (point a service's host:port at its -listen
// address). Deliberately throwaway: no TLS, no auth, no database, no
// per-connection logging. See internal/dummy for the implementation.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/CoffeeMuse/tn3270proxy/internal/dummy"
)

func main() {
	listen := flag.String("listen", ":3300", "address to listen on (plaintext TN3270, no TLS)")
	flag.Parse()

	// One startup line only; the server logs nothing per-connection by design.
	fmt.Fprintf(os.Stderr, "dummy3270 listening on %s\n", *listen)
	if err := dummy.ListenAndServe(*listen); err != nil {
		fmt.Fprintln(os.Stderr, "dummy3270:", err)
		os.Exit(1)
	}
}
