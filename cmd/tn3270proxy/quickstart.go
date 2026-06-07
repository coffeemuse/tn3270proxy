package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/CoffeeMuse/tn3270proxy/internal/quickstart"
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
