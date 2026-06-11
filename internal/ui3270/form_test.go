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

package ui3270

import (
	"context"
	"testing"
)

// fakeRenderer pops scripted actions and captures the views it is asked to
// paint. Shared by the form and list driver tests.
type fakeRenderer struct {
	lists []ListAction
	forms []FormAction

	gotLists []ListView
	gotForms []FormView

	editorViews []EditorView
	editorActs  []EditorAction
}

func (f *fakeRenderer) List(v ListView) (ListAction, error) {
	f.gotLists = append(f.gotLists, v)
	a := f.lists[0]
	f.lists = f.lists[1:]
	return a, nil
}

func (f *fakeRenderer) Form(v FormView) (FormAction, error) {
	f.gotForms = append(f.gotForms, v)
	a := f.forms[0]
	f.forms = f.forms[1:]
	return a, nil
}

func (f *fakeRenderer) Snapshot(SnapshotView) (ListAction, error) {
	return ListAction{PF: 3}, nil
}

func (f *fakeRenderer) Detail(DetailView) error                       { return nil }
func (f *fakeRenderer) DetailAct(DetailView, int) (ListAction, error) { return ListAction{}, nil }

func (f *fakeRenderer) Editor(v EditorView) (EditorAction, error) {
	f.editorViews = append(f.editorViews, v)
	if len(f.editorActs) == 0 {
		return EditorAction{PF: 12}, nil
	}
	a := f.editorActs[0]
	f.editorActs = f.editorActs[1:]
	return a, nil
}

func TestRunFormCancel(t *testing.T) {
	r := &fakeRenderer{forms: []FormAction{{Cancel: true}}}
	submitted := false
	err := RunForm(context.Background(), r, FormConfig{
		Submit: func(context.Context, map[string]string) (string, error) { submitted = true; return "", nil },
	})
	if err != nil || submitted {
		t.Errorf("cancel should return nil without submitting; err=%v submitted=%v", err, submitted)
	}
}

func TestRunFormErrorThenSuccess(t *testing.T) {
	r := &fakeRenderer{forms: []FormAction{
		{Values: map[string]string{"x": "bad"}},
		{Values: map[string]string{"x": "ok"}},
	}}
	calls := 0
	err := RunForm(context.Background(), r, FormConfig{
		Fields: []FormField{{Name: "x"}},
		Submit: func(_ context.Context, v map[string]string) (string, error) {
			calls++
			if v["x"] != "ok" {
				return "TRY AGAIN", nil
			}
			return "", nil
		},
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d, want nil/2", err, calls)
	}
	if r.gotForms[1].ErrMsg != "TRY AGAIN" {
		t.Errorf("second render ErrMsg = %q, want \"TRY AGAIN\"", r.gotForms[1].ErrMsg)
	}
}

func TestRunFormStayOnSave(t *testing.T) {
	// With StayOnSave, each successful submit re-renders the form (saves in
	// place); only Cancel (PF3) leaves.
	r := &fakeRenderer{forms: []FormAction{
		{Values: map[string]string{"x": "v1"}}, // Enter: save, stay
		{Values: map[string]string{"x": "v2"}}, // Enter: save, stay
		{Cancel: true},                         // PF3: exit
	}}
	calls := 0
	err := RunForm(context.Background(), r, FormConfig{
		Fields:     []FormField{{Name: "x"}},
		StayOnSave: true,
		Submit: func(_ context.Context, _ map[string]string) (string, error) {
			calls++
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
	if calls != 2 {
		t.Errorf("Submit calls = %d, want 2 (each Enter saves and stays)", calls)
	}
	// Three renders: initial, after first save, after second save (then Cancel).
	if len(r.gotForms) != 3 {
		t.Errorf("form renders = %d, want 3 (initial + one per save)", len(r.gotForms))
	}
	// A successful save clears any prior error line on the re-render.
	if last := r.gotForms[len(r.gotForms)-1]; last.ErrMsg != "" {
		t.Errorf("re-render after save ErrMsg = %q, want empty", last.ErrMsg)
	}
}
