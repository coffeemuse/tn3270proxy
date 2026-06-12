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
	"os"
	"strings"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/screens"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
)

// Documents list orders by name: BRANDING row 0, HELP-MENU row 1, MOTD row 2.
const motdRow = 2

// docStore unwraps the fixture's real *store.Store for document seeding.
func docStore(t *testing.T, f *adminFlow) *store.Store {
	t.Helper()
	st, ok := f.store.(*store.Store)
	if !ok {
		t.Fatalf("fixture store is %T, want *store.Store", f.store)
	}
	return st
}

func TestAdminDocumentsListShowsMembers(t *testing.T) {
	p := &fakeAdminPresenter{lists: []ui3270.ListAction{{PF: 3}}}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := docStore(t, f).SetDocument(ctx, store.DocMOTD, "A\nB", "ADMIN"); err != nil {
		t.Fatal(err)
	}
	if err := f.documents(ctx, nil); err != nil {
		t.Fatal(err)
	}
	last := lastList(t, p)
	if !strings.Contains(last.Title, "DOCUMENTS") {
		t.Errorf("title = %q, want it to contain DOCUMENTS", last.Title)
	}
	var motd string
	for _, row := range last.Rows {
		if strings.Contains(row, "MOTD") {
			motd = row
		}
	}
	if motd == "" {
		t.Fatalf("no MOTD row rendered: %q", last.Rows)
	}
	if !strings.Contains(motd, "2") {
		t.Errorf("MOTD row missing line count 2: %q", motd)
	}
	if !strings.Contains(motd, "ADMIN") {
		t.Errorf("MOTD row missing updater ADMIN: %q", motd)
	}
}

func TestAdminDocumentEditSaves(t *testing.T) {
	p := &fakeAdminPresenter{
		lists: []ui3270.ListAction{{Cmd: 'E', Row: motdRow}, {PF: 3}},
		edits: []ui3270.EditorAction{{Text: map[int]string{0: "NEW"}, PF: 3}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	st := docStore(t, f)
	if err := st.SetDocument(ctx, store.DocMOTD, "A\nB", "ADMIN"); err != nil {
		t.Fatal(err)
	}
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }

	if err := f.documents(ctx, nil); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDocument(ctx, store.DocMOTD)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "NEW\nB" {
		t.Errorf("content = %q, want %q", d.Content, "NEW\nB")
	}
	if d.UpdatedBy != f.identity.Username {
		t.Errorf("updated_by = %q, want %q", d.UpdatedBy, f.identity.Username)
	}
	if len(got) != 1 || got[0].Kind != store.AuditDocUpdate || !strings.Contains(got[0].Detail, "MOTD") {
		t.Errorf("audit = %+v, want one doc_update event mentioning MOTD", got)
	}
}

func TestAdminDocumentEditCancelDoesNotSave(t *testing.T) {
	p := &fakeAdminPresenter{
		lists: []ui3270.ListAction{{Cmd: 'E', Row: motdRow}, {PF: 3}},
		edits: []ui3270.EditorAction{{Text: map[int]string{0: "NEW"}, PF: 12}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	st := docStore(t, f)
	if err := st.SetDocument(ctx, store.DocMOTD, "A\nB", "ADMIN"); err != nil {
		t.Fatal(err)
	}
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }

	if err := f.documents(ctx, nil); err != nil {
		t.Fatal(err)
	}
	d, _ := st.GetDocument(ctx, store.DocMOTD)
	if d.Content != "A\nB" {
		t.Errorf("content = %q, want unchanged %q", d.Content, "A\nB")
	}
	if len(got) != 0 {
		t.Errorf("audit = %+v, want no events on cancel", got)
	}
}

func TestAdminDocumentImport(t *testing.T) {
	path := t.TempDir() + "/motd.txt"
	if err := os.WriteFile(path, []byte("ART LINE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &fakeAdminPresenter{
		lists: []ui3270.ListAction{{Cmd: 'I', Row: motdRow}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{screens.FieldPath: path}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	st := docStore(t, f)
	if err := st.SetConfig(ctx, sysconfig.KeyMOTDFile, path); err != nil {
		t.Fatal(err)
	}
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }

	if err := f.documents(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// The form pre-fills from the MOTD_FILE import-path param.
	if v := p.gotForms[0].Fields[0].Value; v != path {
		t.Errorf("path pre-fill = %q, want %q", v, path)
	}
	d, _ := st.GetDocument(ctx, store.DocMOTD)
	if d.Content != "ART LINE\n" {
		t.Errorf("content = %q, want %q", d.Content, "ART LINE\n")
	}
	if len(got) != 1 || got[0].Kind != store.AuditDocImport || !strings.Contains(got[0].Detail, path) {
		t.Errorf("audit = %+v, want one doc_import event carrying the path", got)
	}
	// The list re-render after a successful import shows the IMPORTED status.
	if msg := lastList(t, p).ErrMsg; !strings.Contains(msg, "IMPORTED") {
		t.Errorf("list status = %q, want it to contain IMPORTED", msg)
	}
}

func TestAdminDocumentImportBadPath(t *testing.T) {
	p := &fakeAdminPresenter{
		lists: []ui3270.ListAction{{Cmd: 'I', Row: motdRow}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldPath: "relative.txt"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	st := docStore(t, f)
	if err := st.SetDocument(ctx, store.DocMOTD, "KEEP", "ADMIN"); err != nil {
		t.Fatal(err)
	}
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }

	if err := f.documents(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(p.gotForms) < 2 {
		t.Fatalf("expected 2 form renders, got %d", len(p.gotForms))
	}
	if msg := p.gotForms[1].ErrMsg; msg != "PATH MUST BE ABSOLUTE" {
		t.Errorf("errMsg = %q, want PATH MUST BE ABSOLUTE", msg)
	}
	d, _ := st.GetDocument(ctx, store.DocMOTD)
	if d.Content != "KEEP" {
		t.Errorf("content = %q, want unchanged KEEP", d.Content)
	}
	if len(got) != 0 {
		t.Errorf("audit = %+v, want no events for a rejected import", got)
	}
}

func TestDocImportPathKey(t *testing.T) {
	if k := docImportPathKey(store.DocMOTD); k != sysconfig.KeyMOTDFile {
		t.Errorf("MOTD key = %q, want %q", k, sysconfig.KeyMOTDFile)
	}
	if k := docImportPathKey(store.DocBranding); k != sysconfig.KeyBrandingFile {
		t.Errorf("BRANDING key = %q, want %q", k, sysconfig.KeyBrandingFile)
	}
	if k := docImportPathKey(store.DocHelpMenu); k != "" {
		t.Errorf("HELP-MENU key = %q, want \"\" (blank pre-fill, no sysconfig param)", k)
	}
}

func TestAdminDocumentOversizeSaveRejected(t *testing.T) {
	// Seed MOTD with one 5000-byte line; an R (repeat) prefix doubles it to
	// 10001 bytes (> 8 KiB cap), so the PF3 save must be rejected by the store
	// and the editor must stay up showing the error. PF12 then abandons.
	big := strings.Repeat("x", 5000)
	p := &fakeAdminPresenter{
		lists: []ui3270.ListAction{{Cmd: 'E', Row: motdRow}, {PF: 3}},
		edits: []ui3270.EditorAction{
			{Prefix: map[int]byte{0: 'R'}, PF: 3},
			{PF: 12},
		},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	st := docStore(t, f)
	if err := st.SetDocument(ctx, store.DocMOTD, big, "ADMIN"); err != nil {
		t.Fatal(err)
	}
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }

	if err := f.documents(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(p.gotEdits) != 2 {
		t.Fatalf("editor renders = %d, want 2", len(p.gotEdits))
	}
	if msg := p.gotEdits[1].ErrMsg; msg != "DOCUMENT TOO LARGE (MAX 8 KIB)" {
		t.Errorf("editor errMsg = %q, want DOCUMENT TOO LARGE (MAX 8 KIB)", msg)
	}
	d, _ := st.GetDocument(ctx, store.DocMOTD)
	if d.Content != big {
		t.Errorf("content changed (len %d), want unchanged len %d", len(d.Content), len(big))
	}
	if len(got) != 0 {
		t.Errorf("audit = %+v, want no events for a rejected save", got)
	}
}
