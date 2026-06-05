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
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
)

// TestAdminSystemParamsMenuEntry verifies choice 4 routes to systemParams and
// returns cleanly on form cancel (PF3).
func TestAdminSystemParamsMenuEntry(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 4}, {back: true}},
		forms: []ui3270.FormAction{{Cancel: true}},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(p.gotForms) != 1 {
		t.Errorf("form renders = %d, want 1", len(p.gotForms))
	}
}

// TestAdminSystemParamsFormShowsCatalogLabels checks that the form shows one
// field per catalog entry with the correct label.
func TestAdminSystemParamsFormShowsCatalogLabels(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 4}, {back: true}},
		forms: []ui3270.FormAction{{Cancel: true}},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)

	if len(p.gotForms) == 0 {
		t.Fatal("no form rendered")
	}
	form := p.gotForms[0]
	if len(form.Fields) != len(sysconfig.Catalog) {
		t.Fatalf("fields count = %d, want %d", len(form.Fields), len(sysconfig.Catalog))
	}
	for i, e := range sysconfig.Catalog {
		if form.Fields[i].Name != e.Key {
			t.Errorf("field %d name = %q, want %q", i, form.Fields[i].Name, e.Key)
		}
		if form.Fields[i].Label != e.Label {
			t.Errorf("field %d label = %q, want %q", i, form.Fields[i].Label, e.Label)
		}
	}
}

// TestAdminSystemParamsPrePopulatesCurrentValue checks that the form is
// pre-populated with the current store value.
func TestAdminSystemParamsPrePopulatesCurrentValue(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 4}, {back: true}},
		forms: []ui3270.FormAction{{Cancel: true}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()

	// Set a non-default value before opening the form.
	if err := f.store.SetConfig(ctx, "MOTD_FILE", "/var/motd"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	f.Run(ctx, nil)

	form := p.gotForms[0]
	var motdField *ui3270.FormField
	for i := range form.Fields {
		if form.Fields[i].Name == "MOTD_FILE" {
			motdField = &form.Fields[i]
			break
		}
	}
	if motdField == nil {
		t.Fatal("MOTD_FILE field not found in form")
	}
	if motdField.Value != "/var/motd" {
		t.Errorf("MOTD_FILE pre-populated = %q, want %q", motdField.Value, "/var/motd")
	}
}

// TestAdminSystemParamsSaveHappyPath submits a new value and verifies it is
// persisted to the store and audited.
func TestAdminSystemParamsSaveHappyPath(t *testing.T) {
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 4}, {back: true}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{"MOTD_FILE": "/etc/motd.txt"}}, // Enter: save, stay
			{Cancel: true}, // PF3: leave to admin menu
		},
	}
	f, _ := newAdminFixture(t, p)
	var audited []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { audited = append(audited, ev) }

	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}

	// Enter saves in place: the form re-renders (stays) rather than returning,
	// so there are two renders before PF3 leaves.
	if len(p.gotForms) != 2 {
		t.Errorf("form renders = %d, want 2 (save stays, then PF3 leaves)", len(p.gotForms))
	}

	val, err := f.store.GetConfig(ctx, "MOTD_FILE")
	if err != nil {
		t.Fatalf("GetConfig after save: %v", err)
	}
	if val != "/etc/motd.txt" {
		t.Errorf("MOTD_FILE = %q, want %q", val, "/etc/motd.txt")
	}
	if len(audited) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audited))
	}
	if audited[0].Kind != store.AuditAdmin {
		t.Errorf("audit kind = %q, want AuditAdmin", audited[0].Kind)
	}
	if audited[0].Detail != "sysconfig set MOTD_FILE:  -> /etc/motd.txt" {
		t.Errorf("audit detail = %q", audited[0].Detail)
	}
}

// TestAdminSystemParamsNoAuditWhenUnchanged verifies that submitting the form
// with the same value already in the store produces no audit event.
func TestAdminSystemParamsNoAuditWhenUnchanged(t *testing.T) {
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 4}, {back: true}},
		forms: []ui3270.FormAction{
			// MOTD_FILE default is "" — submit the same value
			{Values: map[string]string{"MOTD_FILE": ""}}, // Enter: no change, stay
			{Cancel: true}, // PF3: leave
		},
	}
	f, _ := newAdminFixture(t, p)
	var audited []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { audited = append(audited, ev) }

	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(audited) != 0 {
		t.Errorf("audit events = %d, want 0 for unchanged value", len(audited))
	}
}

// TestAdminSystemParamsEmptyValueIsValid verifies that an empty MOTD_FILE
// (feature disabled) is accepted and saved.
func TestAdminSystemParamsEmptyValueIsValid(t *testing.T) {
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 4}, {back: true}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{"MOTD_FILE": ""}}, // Enter: clear (disable), stay
			{Cancel: true}, // PF3: leave
		},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()

	// Pre-set a non-default value.
	if err := f.store.SetConfig(ctx, "MOTD_FILE", "/tmp/motd"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// Empty path is valid, so the save succeeds and the form stays (re-renders)
	// with no error line — two renders before PF3 leaves.
	if len(p.gotForms) != 2 {
		t.Fatalf("form renders = %d; expected 2 (valid save stays, then PF3)", len(p.gotForms))
	}
	for i, fv := range p.gotForms {
		if fv.ErrMsg != "" {
			t.Errorf("render %d errMsg = %q, want empty (empty path is valid)", i, fv.ErrMsg)
		}
	}
	if val, _ := f.store.GetConfig(ctx, "MOTD_FILE"); val != "" {
		t.Errorf("MOTD_FILE = %q, want empty after clear", val)
	}
}
