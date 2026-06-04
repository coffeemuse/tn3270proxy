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

func usersCfg(rows []Row[string], log *[]string) ListConfig[string] {
	return ListConfig[string]{
		Rows:  24,
		Fetch: func(context.Context) ([]Row[string], string) { return rows, "" },
		Cmds: []Command[string]{
			{Key: 'A', Commit: func(_ context.Context, _ Renderer, it string) (string, error) {
				*log = append(*log, "A:"+it)
				return "", nil
			}},
			{Key: 'D',
				Confirm: func(it string) (string, string) {
					if it == "locked" {
						return "", "BLOCKED"
					}
					return "ENTER=CONFIRM " + it, ""
				},
				Commit: func(_ context.Context, _ Renderer, it string) (string, error) {
					*log = append(*log, "D:"+it)
					return "", nil
				}},
		},
	}
}

func rowsOf(items ...string) []Row[string] {
	out := make([]Row[string], len(items))
	for i, it := range items {
		out[i] = Row[string]{Display: it, Item: it}
	}
	return out
}

func TestRunListImmediateCommand(t *testing.T) {
	var log []string
	r := &fakeRenderer{lists: []ListAction{{Cmd: 'A', Row: 1}, {PF: 3}}}
	if err := RunList(context.Background(), r, usersCfg(rowsOf("x", "y"), &log)); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0] != "A:y" {
		t.Errorf("log = %v, want [A:y]", log)
	}
}

func TestRunListConfirmThenCommit(t *testing.T) {
	var log []string
	r := &fakeRenderer{lists: []ListAction{{Cmd: 'D', Row: 0}, {}, {PF: 3}}}
	if err := RunList(context.Background(), r, usersCfg(rowsOf("alice"), &log)); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0] != "D:alice" {
		t.Errorf("log = %v, want [D:alice]", log)
	}
	if r.gotLists[1].ErrMsg != "ENTER=CONFIRM alice" {
		t.Errorf("confirm prompt = %q", r.gotLists[1].ErrMsg)
	}
}

func TestRunListConfirmPF3Cancels(t *testing.T) {
	var log []string
	r := &fakeRenderer{lists: []ListAction{{Cmd: 'D', Row: 0}, {PF: 3}, {PF: 3}}}
	if err := RunList(context.Background(), r, usersCfg(rowsOf("alice"), &log)); err != nil {
		t.Fatal(err)
	}
	if len(log) != 0 {
		t.Errorf("PF3 after D should cancel; log = %v", log)
	}
}

func TestRunListPressTimeVeto(t *testing.T) {
	var log []string
	r := &fakeRenderer{lists: []ListAction{{Cmd: 'D', Row: 0}, {PF: 3}}}
	if err := RunList(context.Background(), r, usersCfg(rowsOf("locked"), &log)); err != nil {
		t.Fatal(err)
	}
	if len(log) != 0 || r.gotLists[1].ErrMsg != "BLOCKED" {
		t.Errorf("veto failed; log=%v err=%q", log, r.gotLists[1].ErrMsg)
	}
}

func TestRunListPaging(t *testing.T) {
	var log []string
	r := &fakeRenderer{lists: []ListAction{{PF: 8}, {PF: 7}, {PF: 3}}}
	rows := make([]Row[string], 20)
	for i := range rows {
		rows[i] = Row[string]{Display: "u", Item: "u"}
	}
	cfg := usersCfg(rows, &log)
	if err := RunList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if r.gotLists[0].RowInfo != "ROW 1 TO 14 OF 20" || r.gotLists[1].RowInfo != "ROW 15 TO 20 OF 20" {
		t.Errorf("paging RowInfo wrong: %q then %q", r.gotLists[0].RowInfo, r.gotLists[1].RowInfo)
	}
}

func TestRunListFetchErrorClearsPending(t *testing.T) {
	var log []string
	calls := 0
	cfg := usersCfg(rowsOf("alice"), &log)
	cfg.Fetch = func(context.Context) ([]Row[string], string) {
		calls++
		if calls == 2 {
			return nil, "TEMP ERROR"
		}
		return rowsOf("alice"), ""
	}
	r := &fakeRenderer{lists: []ListAction{{Cmd: 'D', Row: 0}, {}, {PF: 3}}}
	if err := RunList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if len(log) != 0 {
		t.Errorf("fetch error should clear pending; log = %v", log)
	}
}

func TestRunListAddNilInert(t *testing.T) {
	var log []string
	r := &fakeRenderer{lists: []ListAction{{PF: 4}, {PF: 3}}}
	if err := RunList(context.Background(), r, usersCfg(rowsOf("x"), &log)); err != nil {
		t.Fatal(err)
	}
	if len(r.gotLists) != 2 {
		t.Errorf("PF4 should be inert and re-render; renders = %d", len(r.gotLists))
	}
}
