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

	"github.com/CoffeeMuse/tn3270proxy/internal/config"
	"github.com/CoffeeMuse/tn3270proxy/internal/mfa"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// mfaSentinelPlaintext is the fixed value sealed under the master key to detect
// a wrong/rotated key at startup. Its content is irrelevant; only the ability
// to decrypt it matters.
const mfaSentinelPlaintext = "tn3270proxy-mfa-key-check-v1"

// mfaStartup enforces the fail-closed key contract and returns the cipher (nil
// when no key is configured and no users are enrolled). It:
//   - errors if users are enrolled but no key is configured;
//   - errors if a sentinel exists but the key can't decrypt it (wrong/rotated);
//   - writes the sentinel on first run with a key.
func mfaStartup(ctx context.Context, st *store.Store, key []byte) (*mfa.Cipher, error) {
	enrolled, err := st.CountEnrolledUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("mfa: count enrolled users: %w", err)
	}
	if len(key) == 0 {
		if enrolled > 0 {
			return nil, fmt.Errorf("mfa: %d user(s) are enrolled but no master key is configured (set %s or mfa.key); refusing to start", enrolled, "TN3270PROXY_MFA_KEY")
		}
		return nil, nil // MFA disabled
	}
	c, err := mfa.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("mfa: %w", err)
	}
	sentinel, err := st.GetMFASentinel(ctx)
	if err != nil {
		return nil, fmt.Errorf("mfa: read sentinel: %w", err)
	}
	if sentinel == "" {
		sealed, err := c.Seal([]byte(mfaSentinelPlaintext))
		if err != nil {
			return nil, fmt.Errorf("mfa: seal sentinel: %w", err)
		}
		if err := st.SetMFASentinel(ctx, sealed); err != nil {
			return nil, fmt.Errorf("mfa: write sentinel: %w", err)
		}
		return c, nil
	}
	if _, err := c.Open(sentinel); err != nil {
		if errors.Is(err, mfa.ErrDecrypt) {
			return nil, errors.New("mfa: master key does not match the key that encrypted existing secrets (wrong or rotated key); refusing to start. Restore the correct key, or run `tn3270proxy mfa reset-all` to wipe all enrollments")
		}
		return nil, fmt.Errorf("mfa: verify sentinel: %w", err)
	}
	return c, nil
}

// runMFA dispatches the `mfa` subcommand group. out receives human-readable
// status (os.Stdout in production, a buffer in tests).
func runMFA(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("mfa: expected a subcommand (reset-all)")
	}
	switch args[0] {
	case "reset-all":
		return runMFAResetAll(args[1:], out)
	default:
		return fmt.Errorf("mfa: unknown subcommand %q", args[0])
	}
}

func runMFAResetAll(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("mfa reset-all", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	key, err := resolveMFAKeyForCLI()
	if err != nil {
		return err
	}
	if len(key) == 0 {
		return fmt.Errorf("mfa reset-all: no master key configured (set TN3270PROXY_MFA_KEY); a key is required to rewrite the sentinel")
	}
	c, err := mfa.NewCipher(key)
	if err != nil {
		return fmt.Errorf("mfa reset-all: %w", err)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	n, err := st.ResetAllMFA(ctx)
	if err != nil {
		return fmt.Errorf("mfa reset-all: %w", err)
	}
	sealed, err := c.Seal([]byte(mfaSentinelPlaintext))
	if err != nil {
		return fmt.Errorf("mfa reset-all: %w", err)
	}
	if err := st.SetMFASentinel(ctx, sealed); err != nil {
		return fmt.Errorf("mfa reset-all: rewrite sentinel: %w", err)
	}
	fmt.Fprintf(out, "reset MFA for %d user(s); sentinel rewritten for the current key\n", n)
	return nil
}

// resolveMFAKeyForCLI reads the master key from the same sources as serve
// (env wins over config file).
func resolveMFAKeyForCLI() ([]byte, error) {
	cfg, err := config.Load([]string{})
	if err != nil {
		return nil, err
	}
	return cfg.MFA.Key, nil
}
