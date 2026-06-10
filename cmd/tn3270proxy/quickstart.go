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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/coffeemuse/tn3270proxy/internal/quickstart"
)

// runQuickstart implements the `quickstart` subcommand: idempotently provision a
// fresh data dir with opinionated defaults, or no-op on an already-provisioned
// dir. It is wired into the Docker image before `serve`; users never run it by hand.
func runQuickstart(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("quickstart", flag.ContinueOnError)
	dataDir := fs.String("data", "/data", "path to the data directory to provision")
	if err := fs.Parse(args); err != nil {
		return err
	}

	res, err := quickstart.Provision(context.Background(), *dataDir)
	if errors.Is(err, quickstart.ErrAlreadyProvisioned) {
		fmt.Fprintf(w, "Existing installation detected at %s — leaving it untouched.\n", *dataDir)
		return nil
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "============================================================\n")
	fmt.Fprintf(w, " FRESH INSTALL detected — quick-start defaults provisioned.\n")
	fmt.Fprintf(w, "============================================================\n")
	fmt.Fprintf(w, "Admin user: %s\n", res.Admin.Username)
	fmt.Fprintf(w, "Credentials written to: %s/SETUP-DEFAULTS.TXT\n", res.DataDir)
	fmt.Fprintf(w, "Listening (after serve starts): plaintext %s, TLS %s\n", res.PlainAddr, res.TLSAddr)
	fmt.Fprintf(w, "CHANGE THE GENERATED PASSWORDS AFTER FIRST LOGIN.\n")
	return nil
}
