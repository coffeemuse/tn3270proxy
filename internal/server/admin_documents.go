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

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/coffeemuse/tn3270proxy/internal/screens"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
)

// documents drives the Documents member list (an ISPF PDS-member-list feel):
// the two DB documents with E=edit (line editor) and I=import (server file).
func (f *adminFlow) documents(ctx context.Context, conn net.Conn) error {
	r := f.renderer(conn)
	return ui3270.RunList(ctx, r, ui3270.ListConfig[store.Document]{
		Title:  "TN3270 GATEWAY ADMIN: DOCUMENTS",
		Header: "CMD  NAME       LINES  CHANGED (UTC)     ID",
		Legend: "E = edit   I = import from server file",
		PFHelp: "PF3=Admin Menu    PF7=PgUp    PF8=PgDn",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.Row[store.Document], string) {
			docs, err := f.store.ListDocuments(ctx)
			if err != nil {
				return nil, f.storeErr("list documents", err)
			}
			rows := make([]ui3270.Row[store.Document], len(docs))
			for i, d := range docs {
				rows[i] = ui3270.Row[store.Document]{
					Display: fmt.Sprintf("%-10s %5d  %-16s  %s",
						d.Name, d.LineCount(), docStamp(d.UpdatedAt), d.UpdatedBy),
					Item: d,
				}
			}
			return rows, ""
		},
		Cmds: []ui3270.Command[store.Document]{
			{Key: 'E', Commit: func(ctx context.Context, r ui3270.Renderer, d store.Document) (string, error) {
				return f.documentEditor(ctx, r, d)
			}},
			{Key: 'I', Commit: func(ctx context.Context, r ui3270.Renderer, d store.Document) (string, error) {
				return f.documentImport(ctx, r, d)
			}},
		},
	})
}

// docStamp renders an RFC3339 stamp as the member list's "2026-06-11 14:02"
// Changed column ("" until first save).
func docStamp(rfc3339 string) string {
	s := strings.Replace(rfc3339, "T", " ", 1)
	if len(s) > 16 {
		s = s[:16]
	}
	return s
}

// documentEditor runs the line editor on d. PF3 saves (audited doc_update);
// PF12 discards. The store enforces the size cap — the editor surfaces it.
func (f *adminFlow) documentEditor(ctx context.Context, r ui3270.Renderer, d store.Document) (string, error) {
	saved := ""
	err := ui3270.RunEditor(ctx, r, ui3270.EditorConfig{
		Title: "EDIT " + d.Name,
		Rows:  f.term.Rows,
		Lines: d.Lines(),
		Save: func(ctx context.Context, lines []string) (string, error) {
			content := strings.Join(lines, "\n")
			if err := f.store.SetDocument(ctx, d.Name, content, f.identity.Username); err != nil {
				if errors.Is(err, store.ErrDocumentTooLarge) {
					return "DOCUMENT TOO LARGE (MAX 8 KIB)", nil
				}
				return f.storeErr("save document", err), nil
			}
			f.record(ctx, store.AuditDocUpdate, fmt.Sprintf("%s %d lines", d.Name, len(lines)))
			saved = d.Name + " SAVED"
			return "", nil
		},
	})
	return saved, err
}

// documentImport prompts for a server-side absolute path (pre-filled from the
// document's import-path sysconfig param) and replaces d's content with the
// file's contents. Audited as doc_import with the path in the detail.
func (f *adminFlow) documentImport(ctx context.Context, r ui3270.Renderer, d store.Document) (string, error) {
	defaultPath, _ := f.store.GetConfig(ctx, docImportPathKey(d.Name)) // "": blank pre-fill
	imported := ""
	fields := []ui3270.FormField{
		{Name: screens.FieldPath, Label: "Server file path. .", Value: defaultPath, Length: 56},
	}
	err := ui3270.RunForm(ctx, r, ui3270.FormConfig{
		Title:  "IMPORT " + d.Name + " FROM SERVER FILE",
		Fields: fields,
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			path := strings.TrimSpace(vals[screens.FieldPath])
			fields[0].Value = path
			content, err := store.ReadDocumentFile(path)
			if err != nil {
				return strings.ToUpper(err.Error()), nil
			}
			if err := f.store.SetDocument(ctx, d.Name, content, f.identity.Username); err != nil {
				return f.storeErr("import document", err), nil
			}
			n := store.Document{Content: content}.LineCount()
			f.record(ctx, store.AuditDocImport, fmt.Sprintf("%s from %s (%d lines)", d.Name, path, n))
			imported = fmt.Sprintf("%s IMPORTED (%d LINES)", d.Name, n)
			return "", nil
		},
	})
	return imported, err
}

// docImportPathKey maps a document name to its import-path sysconfig key.
func docImportPathKey(doc string) string {
	if doc == store.DocBranding {
		return sysconfig.KeyBrandingFile
	}
	return sysconfig.KeyMOTDFile
}
