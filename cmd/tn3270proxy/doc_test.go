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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

func TestDocImportExportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	src := filepath.Join(dir, "art.txt")
	if err := os.WriteFile(src, []byte("ART\nLINES\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"doc", "import", "-db", db, "-name", "branding", "-file", src}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDocument(context.Background(), store.DocBranding)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "ART\nLINES\n" || d.UpdatedBy != "cli" {
		t.Errorf("imported doc: %+v", d)
	}
	st.Close()

	out := filepath.Join(dir, "out.txt")
	if err := run([]string{"doc", "export", "-db", db, "-name", "BRANDING", "-file", out}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if string(got) != "ART\nLINES\n" {
		t.Errorf("exported %q", got)
	}
	// Existing file refused without -force.
	if err := run([]string{"doc", "export", "-db", db, "-name", "BRANDING", "-file", out}); err == nil ||
		!strings.Contains(err.Error(), "-force") {
		t.Errorf("overwrite without -force: err=%v", err)
	}
	if err := run([]string{"doc", "export", "-db", db, "-name", "BRANDING", "-file", out, "-force"}); err != nil {
		t.Errorf("export -force: %v", err)
	}
}

func TestDocImportRejectsBadArgs(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	if err := run([]string{"doc", "import", "-db", db, "-name", "BOGUS", "-file", "/tmp/x"}); err == nil {
		t.Error("unknown doc name: want error")
	}
	if err := run([]string{"doc", "import", "-db", db}); err == nil {
		t.Error("missing flags: want error")
	}
	if err := run([]string{"doc"}); err == nil {
		t.Error("missing verb: want error")
	}
}
