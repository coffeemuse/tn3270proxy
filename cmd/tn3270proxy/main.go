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

// Command tn3270proxy is the TN3270 gateway: it presents a TN3270 server to
// clients, authenticates users against a local SQLite database, shows a
// group-filtered menu of backend services, and bridges the chosen session.
//
// The first argument selects a subcommand:
//
//	serve      run the gateway (the default when omitted)
//	bootstrap  create the first ADMIN account on a fresh database
//	seed       bulk-load users/groups/services from a JSON file
//	quickstart provision a fresh Docker data dir with opinionated defaults
//	audit      query (list) or trim (prune) the audit trail
//	mfa        break-glass MFA operations (reset-all)
//	version    print the resolved build version
//
// Each subcommand only wires internal packages together; no business logic
// lives in this package. Run any subcommand with -h for its flags.
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
	"github.com/CoffeeMuse/tn3270proxy/internal/logging"
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

// run dispatches on the first argument. An unrecognized first argument falls
// through to serve (the default subcommand), whose flag parsing rejects it.
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
	if len(args) > 0 && args[0] == "quickstart" {
		return runQuickstart(args[1:], os.Stdout)
	}
	if len(args) > 0 && args[0] == "mfa" {
		return runMFA(args[1:], os.Stdout)
	}
	if len(args) > 0 && args[0] == "version" {
		runVersion()
		return nil
	}
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	return runServe(args)
}

// runServe builds and runs the gateway: config → logger → store (runs schema
// migrations) → fail-closed MFA key check → listeners → one shared session
// handler served on every listener. It blocks until the accept loops exit.
func runServe(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return err
	}

	level, _ := logging.ParseLevel(cfg.Log.Level) // already validated by config.Load
	logger, closer, err := logging.New(level, cfg.Log.File)
	if err != nil {
		return err
	}
	defer closer.Close()

	rv := resolvedVersion()
	logger.Info("starting", "version", rv)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	mfaCipher, err := mfaStartup(context.Background(), st, cfg.MFA.Key)
	if err != nil {
		return err
	}

	warnIfNoAdmin(context.Background(), st, os.Stderr)

	listeners, err := listen.Build(cfg)
	if err != nil {
		return err
	}

	if cfg.Plain.Enabled {
		logger.Info("listening", "proto", "plain", "addr", cfg.Plain.Addr, "db", cfg.DBPath)
	}
	if cfg.TLS.Enabled {
		logger.Info("listening", "proto", "tls", "addr", cfg.TLS.Addr, "db", cfg.DBPath)
	}

	limits := server.Limits{
		PreAuthIdle:      cfg.Limits.PreAuthIdle,
		Idle:             cfg.Limits.Idle,
		MaxConns:         cfg.Limits.MaxConns,
		MaxPerIP:         cfg.Limits.MaxPerIP,
		PreAuthMax:       cfg.Limits.PreAuthMax,
		BridgeIdleExempt: cfg.Limits.BridgeIdleExempt,
		Trust:            server.NewStoreTrustChecker(st),
	}
	handler := server.NewSessionHandler(st, bridge.EscapeAIDPA3, limits, logger, rv, mfaCipher)
	return server.ServeAll(listeners, handler, limits)
}

// runSeed implements the `seed` subcommand: parse a JSON seed file and apply
// it idempotently (see internal/seed), then warn if the DB still has no admin.
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
