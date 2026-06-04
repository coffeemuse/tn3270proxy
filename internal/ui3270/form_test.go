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
