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

// Package store: documents.go owns the documents table — the DB-resident text
// documents (MOTD, login branding) edited via the admin Documents screen.
// Content is LF-joined lines; the 8 KiB cap is enforced at WRITE time so reads
// never need a cap.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaxDocumentBytes caps a document's content (matches the old file read cap).
const MaxDocumentBytes = 8 << 10 // 8 KiB

// ErrDocumentTooLarge is returned when content (or an imported file) exceeds
// MaxDocumentBytes.
var ErrDocumentTooLarge = errors.New("document too large (max 8 KiB)")

// Known document names. The set is code-defined: reconcileDefaults seeds one
// empty row per name so the admin member list always shows them all.
const (
	DocMOTD     = "MOTD"
	DocBranding = "BRANDING"
)

// KnownDocuments lists every valid document name.
var KnownDocuments = []string{DocMOTD, DocBranding}

// Document is one DB-resident text document.
type Document struct {
	Name      string
	Content   string // LF-joined lines; "" = empty (feature disabled)
	UpdatedAt string // UTC RFC3339; "" until first save
	UpdatedBy string // actor username; "" until first save
}

// Lines splits Content for the editor; nil for an empty document.
func (d Document) Lines() []string {
	if d.Content == "" {
		return nil
	}
	return strings.Split(d.Content, "\n")
}

// LineCount is the member-list Lines column.
func (d Document) LineCount() int { return len(d.Lines()) }

// NormalizeDocName folds name to canonical uppercase and rejects names outside
// KnownDocuments (the single choke point, mirroring usernames/service names).
func NormalizeDocName(name string) (string, error) {
	n := strings.ToUpper(strings.TrimSpace(name))
	for _, k := range KnownDocuments {
		if n == k {
			return n, nil
		}
	}
	return "", fmt.Errorf("unknown document %q (want %s)", name, strings.Join(KnownDocuments, " or "))
}

// GetDocument returns the named document, or ErrNotFound for a known name whose
// row is missing (cannot happen after reconcileDefaults; defensive).
func (s *Store) GetDocument(ctx context.Context, name string) (Document, error) {
	n, err := NormalizeDocName(name)
	if err != nil {
		return Document{}, err
	}
	var d Document
	err = s.db.QueryRowContext(ctx,
		"SELECT name, content, updated_at, updated_by FROM documents WHERE name = ?", n).
		Scan(&d.Name, &d.Content, &d.UpdatedAt, &d.UpdatedBy)
	if err == sql.ErrNoRows {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	return d, nil
}

// SetDocument replaces the named document's content, stamping who and when.
func (s *Store) SetDocument(ctx context.Context, name, content, actor string) error {
	n, err := NormalizeDocName(name)
	if err != nil {
		return err
	}
	if len(content) > MaxDocumentBytes {
		return ErrDocumentTooLarge
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO documents (name, content, updated_at, updated_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET content=excluded.content,
		   updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		n, content, time.Now().UTC().Format(time.RFC3339), actor)
	return err
}

// ListDocuments returns all documents ordered by name.
func (s *Store) ListDocuments(ctx context.Context) ([]Document, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, content, updated_at, updated_by FROM documents ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.Name, &d.Content, &d.UpdatedAt, &d.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ReadDocumentFile reads a server-side file for import: absolute path required,
// rejected (not truncated) over MaxDocumentBytes. Used by the admin import
// screen and the CLI; the v3 migration has its own truncating reader to match
// the legacy render behavior.
func ReadDocumentFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxDocumentBytes {
		return "", ErrDocumentTooLarge
	}
	return string(data), nil
}
