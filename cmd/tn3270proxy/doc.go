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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

// runDoc dispatches the document verbs (DB-resident MOTD/branding text).
func runDoc(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("doc: usage: doc import|export [flags]")
	}
	switch args[0] {
	case "import":
		return runDocImport(args[1:])
	case "export":
		return runDocExport(args[1:])
	default:
		return fmt.Errorf("doc: unknown subcommand %q (want import or export)", args[0])
	}
}

// runDocImport replaces a document's content with a local file's contents —
// the CLI sibling of the admin Documents import screen (handy when nobody is
// at a 3270: provisioning, docker, CI).
func runDocImport(args []string) error {
	fs := flag.NewFlagSet("doc import", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	name := fs.String("name", "", "document name: MOTD or BRANDING (required)")
	file := fs.String("file", "", "path to the text file to import (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *file == "" {
		return fmt.Errorf("doc import: -name and -file are required")
	}
	docName, err := store.NormalizeDocName(*name)
	if err != nil {
		return fmt.Errorf("doc import: %w", err)
	}
	abs, err := filepath.Abs(*file)
	if err != nil {
		return fmt.Errorf("doc import: %w", err)
	}
	content, err := store.ReadDocumentFile(abs)
	if err != nil {
		return fmt.Errorf("doc import: %w", err)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SetDocument(ctx, docName, content, "cli"); err != nil {
		return err
	}
	lines := store.Document{Content: content}.LineCount()
	// Best-effort audit: the import succeeded either way.
	_ = st.RecordAudit(ctx, store.AuditEvent{
		At: time.Now(), SessionID: "cli", Kind: store.AuditDocImport, Actor: "cli",
		Detail: fmt.Sprintf("%s from %s (%d lines)", docName, abs, lines),
	})
	fmt.Printf("imported %s (%d lines)\n", docName, lines)
	return nil
}

// runDocExport writes a document's content to a file (round-trips offline art
// editing). Refuses to overwrite an existing file without -force.
func runDocExport(args []string) error {
	fs := flag.NewFlagSet("doc export", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	name := fs.String("name", "", "document name: MOTD or BRANDING (required)")
	file := fs.String("file", "", "destination file path (required)")
	force := fs.Bool("force", false, "overwrite the destination if it exists")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *file == "" {
		return fmt.Errorf("doc export: -name and -file are required")
	}
	docName, err := store.NormalizeDocName(*name)
	if err != nil {
		return fmt.Errorf("doc export: %w", err)
	}
	if !*force {
		if _, err := os.Stat(*file); err == nil {
			return fmt.Errorf("doc export: %s exists (use -force to overwrite)", *file)
		}
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	d, err := st.GetDocument(context.Background(), docName)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*file, []byte(d.Content), 0o644); err != nil {
		return err
	}
	fmt.Printf("exported %s to %s (%d lines)\n", docName, *file, d.LineCount())
	return nil
}
