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
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/config"
	"github.com/CoffeeMuse/tn3270proxy/internal/listen"
	"github.com/CoffeeMuse/tn3270proxy/internal/seed"
	"github.com/CoffeeMuse/tn3270proxy/internal/server"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tn3270proxy:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "seed" {
		return runSeed(args[1:])
	}
	if len(args) > 0 && args[0] == "audit" {
		return runAudit(args[1:])
	}
	if len(args) > 0 && args[0] == "bootstrap" {
		return runBootstrap(args[1:], os.Stdout)
	}
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	return runServe(args)
}

func runServe(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	warnIfNoAdmin(context.Background(), st, os.Stderr)

	listeners, err := listen.Build(cfg)
	if err != nil {
		return err
	}

	if cfg.Plain.Enabled {
		fmt.Printf("tn3270proxy listening (plain) on %s (db=%s)\n", cfg.Plain.Addr, cfg.DBPath)
	}
	if cfg.TLS.Enabled {
		fmt.Printf("tn3270proxy listening (tls) on %s (db=%s)\n", cfg.TLS.Addr, cfg.DBPath)
	}

	limits := server.Limits{
		PreAuthIdle:      cfg.Limits.PreAuthIdle,
		Idle:             cfg.Limits.Idle,
		MaxConns:         cfg.Limits.MaxConns,
		MaxPerIP:         cfg.Limits.MaxPerIP,
		PreAuthMax:       cfg.Limits.PreAuthMax,
		TrustedCIDRs:     cfg.Limits.TrustedCIDRs,
		BridgeIdleExempt: cfg.Limits.BridgeIdleExempt,
	}
	handler := server.NewSessionHandler(st, bridge.EscapeAIDPA3, limits)
	return server.ServeAll(listeners, handler, limits)
}

func runSeed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	file := fs.String("file", "", "path to JSON seed file (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("seed: -file is required")
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	var data seed.SeedData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("parse seed file: %w", err)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := seed.Apply(context.Background(), st, data); err != nil {
		return err
	}
	fmt.Println("seed applied")
	warnIfNoAdmin(context.Background(), st, os.Stderr)
	return nil
}
