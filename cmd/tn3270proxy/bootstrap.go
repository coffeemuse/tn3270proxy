package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// bootstrapCharset is the 3270-typeable unambiguous alphanumeric set used for
// one-time passwords: uppercase letters excluding I, L, O; digits excluding 0
// and 1. Eliminates common transcription errors on physical 3270 keyboards.
const bootstrapCharset = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

// generateBootstrapPassword returns a random password in XXXX-XXXX-XXXX form
// using bootstrapCharset. The format groups 12 characters for readability.
func generateBootstrapPassword() (string, error) {
	b := make([]byte, 12)
	n := big.NewInt(int64(len(bootstrapCharset)))
	for i := range b {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		b[i] = bootstrapCharset[idx.Int64()]
	}
	return fmt.Sprintf("%s-%s-%s", string(b[0:4]), string(b[4:8]), string(b[8:12])), nil
}

// adminCounter is the store subset the warning helper needs.
type adminCounter interface {
	CountAdminMembers(ctx context.Context) (int, error)
}

// warnIfNoAdmin writes a prominent warning to w when ZZADMIN has no members.
// Best-effort: a query error is logged and control returns without writing.
func warnIfNoAdmin(ctx context.Context, st adminCounter, w io.Writer) {
	n, err := st.CountAdminMembers(ctx)
	if err != nil {
		log.Printf("tn3270proxy: could not check admin group: %v", err)
		return
	}
	if n == 0 {
		fmt.Fprintln(w, "WARNING: no admin user exists — the admin UI (menu entry 'A') is unreachable.")
		fmt.Fprintln(w, "         Run:  tn3270proxy bootstrap -db <db-path>")
	}
}

// runBootstrap implements the `bootstrap` subcommand. It creates the initial
// ADMIN account in ZZADMIN with a one-time crypto/rand password and prints
// that password once to w (stdout in production). It refuses when any admin
// already exists. The writer is injectable so tests can capture the printed
// password and confirm it matches the stored bcrypt hash.
func runBootstrap(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	n, err := st.CountAdminMembers(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("bootstrap: an admin already exists; use the admin UI to manage accounts")
	}

	password, err := generateBootstrapPassword()
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("bootstrap: hash password: %w", err)
	}

	uid, err := st.CreateUser(ctx, "ADMIN", hash)
	if err != nil {
		return fmt.Errorf("bootstrap: create user: %w", err)
	}
	// If ADMIN existed but was not in ZZADMIN (count==0), refresh its password
	// so the printed password matches the stored hash.
	if err := st.SetPassword(ctx, uid, hash); err != nil {
		return fmt.Errorf("bootstrap: set password: %w", err)
	}
	adminGID, err := st.CreateGroup(ctx, store.AdminGroup) // idempotent; returns existing id
	if err != nil {
		return fmt.Errorf("bootstrap: get admin group: %w", err)
	}
	if err := st.AddUserToGroup(ctx, uid, adminGID); err != nil {
		return fmt.Errorf("bootstrap: add to admin group: %w", err)
	}

	fmt.Fprintf(w, "Bootstrap complete. One-time password for user ADMIN:\n\n  %s\n\n", password)
	fmt.Fprintln(w, "Log in as ADMIN, create your real admin account, then delete ADMIN.")
	return nil
}
