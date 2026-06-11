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

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeDocName(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"motd", DocMOTD, true},
		{" Branding ", DocBranding, true},
		{"MOTD", DocMOTD, true},
		{"bogus", "", false},
		{"", "", false},
	} {
		got, err := NormalizeDocName(tc.in)
		if tc.ok && (err != nil || got != tc.want) {
			t.Errorf("NormalizeDocName(%q) = %q, %v; want %q, nil", tc.in, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Errorf("NormalizeDocName(%q): want error", tc.in)
		}
	}
}

func TestSetGetDocument(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.SetDocument(ctx, "motd", "HELLO\nWORLD", "ADMIN"); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDocument(ctx, DocMOTD)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "HELLO\nWORLD" || d.UpdatedBy != "ADMIN" {
		t.Errorf("got %+v", d)
	}
	if _, err := time.Parse(time.RFC3339, d.UpdatedAt); err != nil {
		t.Errorf("UpdatedAt %q not RFC3339: %v", d.UpdatedAt, err)
	}
	if d.LineCount() != 2 {
		t.Errorf("LineCount = %d, want 2", d.LineCount())
	}
}

func TestSetDocumentRejectsUnknownAndOversize(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.SetDocument(ctx, "NOPE", "x", "A"); err == nil {
		t.Error("unknown name: want error")
	}
	big := strings.Repeat("x", MaxDocumentBytes+1)
	if err := st.SetDocument(ctx, DocMOTD, big, "A"); !errors.Is(err, ErrDocumentTooLarge) {
		t.Errorf("oversize: got %v, want ErrDocumentTooLarge", err)
	}
}

func TestListDocumentsAlwaysShowsKnownDocs(t *testing.T) {
	st := newTestStore(t)
	docs, err := st.ListDocuments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Name != DocBranding || docs[1].Name != DocMOTD {
		t.Errorf("got %+v, want [BRANDING, MOTD] (alphabetical)", docs)
	}
	if docs[0].Content != "" || docs[0].LineCount() != 0 {
		t.Errorf("fresh doc should be empty: %+v", docs[0])
	}
}

func TestReadDocumentFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "art.txt")
	if err := os.WriteFile(p, []byte("LINE1\nLINE2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDocumentFile(p)
	if err != nil || got != "LINE1\nLINE2\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := ReadDocumentFile("relative/path.txt"); err == nil {
		t.Error("relative path: want error")
	}
	if _, err := ReadDocumentFile(filepath.Join(dir, "absent.txt")); err == nil {
		t.Error("missing file: want error")
	}
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", MaxDocumentBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDocumentFile(big); !errors.Is(err, ErrDocumentTooLarge) {
		t.Errorf("oversize file: got %v, want ErrDocumentTooLarge", err)
	}
}

// TestDocumentReopenSurvival guards the invariant that reconcileDefaults'
// INSERT OR IGNORE never clobbers saved content on re-open.
func TestDocumentReopenSurvival(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reopen.db")
	ctx := context.Background()

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := st.SetDocument(ctx, DocMOTD, "PERSISTED", "ADMIN"); err != nil {
		t.Fatalf("SetDocument: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer st2.Close()

	d, err := st2.GetDocument(ctx, DocMOTD)
	if err != nil {
		t.Fatalf("GetDocument after reopen: %v", err)
	}
	if d.Content != "PERSISTED" {
		t.Errorf("reopen clobbered content: got %q, want %q", d.Content, "PERSISTED")
	}
}

// TestSetDocumentExactMaxSize verifies that content of exactly MaxDocumentBytes
// is accepted (boundary: one byte under the rejection threshold).
func TestSetDocumentExactMaxSize(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	exact := strings.Repeat("x", MaxDocumentBytes)
	if err := st.SetDocument(ctx, DocMOTD, exact, "A"); err != nil {
		t.Errorf("exact MaxDocumentBytes: got %v, want nil", err)
	}
}

// TestReadDocumentFileExactMaxSize verifies that a file of exactly
// MaxDocumentBytes is accepted.
func TestReadDocumentFileExactMaxSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "exact.txt")
	if err := os.WriteFile(p, []byte(strings.Repeat("y", MaxDocumentBytes)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDocumentFile(p)
	if err != nil {
		t.Errorf("exact MaxDocumentBytes file: got %v, want nil", err)
	}
	if len(got) != MaxDocumentBytes {
		t.Errorf("got len %d, want %d", len(got), MaxDocumentBytes)
	}
}

// TestGetDocumentUnknownName verifies that GetDocument returns an error for an
// unrecognised document name (NormalizeDocName rejects it before any DB hit).
func TestGetDocumentUnknownName(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.GetDocument(context.Background(), "BOGUS"); err == nil {
		t.Error("GetDocument(BOGUS): want error, got nil")
	}
}
