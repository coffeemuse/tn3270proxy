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

// scriptRenderer returns scripted Snapshot actions and records each view; Detail
// calls are counted.
type scriptRenderer struct {
	acts      []ListAction
	views     []SnapshotView
	detailHit int
}

func (s *scriptRenderer) List(ListView) (ListAction, error) { panic("unused") }
func (s *scriptRenderer) Form(FormView) (FormAction, error) { panic("unused") }
func (s *scriptRenderer) Detail(DetailView) error           { s.detailHit++; return nil }
func (s *scriptRenderer) Snapshot(v SnapshotView) (ListAction, error) {
	s.views = append(s.views, v)
	if len(s.acts) == 0 {
		panic("unexpected Snapshot call")
	}
	a := s.acts[0]
	s.acts = s.acts[1:]
	return a, nil
}

func entries(n int) []SnapshotEntry[int] {
	out := make([]SnapshotEntry[int], n)
	for i := range out {
		out[i] = SnapshotEntry[int]{Row: SnapshotRow{Left: "row"}, Item: i}
	}
	return out
}

func TestRunSnapshotList_SnapshotIsStableAcrossPaging(t *testing.T) {
	fetches := 0
	cfg := SnapshotConfig[int]{
		Rows: 24,
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) {
			fetches++
			return entries(30), "AS OF X", "" // 30 rows, 14/page → 3 pages
		},
	}
	r := &scriptRenderer{acts: []ListAction{{PF: 8}, {PF: 8}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if fetches != 1 {
		t.Errorf("fetches = %d, want 1 (paging must page the held snapshot)", fetches)
	}
}

func TestRunSnapshotList_EnterRefetches(t *testing.T) {
	fetches := 0
	cfg := SnapshotConfig[int]{
		Rows:  24,
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) { fetches++; return entries(2), "X", "" },
	}
	r := &scriptRenderer{acts: []ListAction{{}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if fetches != 2 {
		t.Errorf("fetches = %d, want 2 (initial + Enter refresh)", fetches)
	}
}

func TestRunSnapshotList_SelectInvokesOnSelect(t *testing.T) {
	var picked int = -1
	cfg := SnapshotConfig[int]{
		Rows:  24,
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) { return entries(3), "X", "" },
		OnSelect: func(_ context.Context, r Renderer, item int) error {
			picked = item
			return r.Detail(DetailView{})
		},
	}
	r := &scriptRenderer{acts: []ListAction{{Cmd: 'S', Row: 1}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if picked != 1 {
		t.Errorf("OnSelect item = %d, want 1", picked)
	}
	if r.detailHit != 1 {
		t.Errorf("Detail calls = %d, want 1", r.detailHit)
	}
}

func TestRunSnapshotList_ActCmdConfirmThenCommit(t *testing.T) {
	acted := -1
	cfg := SnapshotConfig[int]{
		Rows:    24,
		Fetch:   func(context.Context) ([]SnapshotEntry[int], string, string) { return entries(3), "X", "" },
		ActCmd:  'D',
		Confirm: func(item int) (string, string) { return "CONFIRM - PRESS D AGAIN", "" },
		OnAct: func(_ context.Context, item int) (bool, string) {
			acted = item
			return true, ""
		},
	}
	r := &scriptRenderer{acts: []ListAction{{Cmd: 'D', Row: 1}, {Cmd: 'D', Row: 1}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if acted != 1 {
		t.Errorf("OnAct item = %d, want 1", acted)
	}
	if got := r.views[1].ErrMsg; got != "CONFIRM - PRESS D AGAIN" {
		t.Errorf("confirm prompt = %q, want the prompt on the message line", got)
	}
}

func TestRunSnapshotList_ActCmdBlockedVetoes(t *testing.T) {
	acted := false
	cfg := SnapshotConfig[int]{
		Rows:    24,
		Fetch:   func(context.Context) ([]SnapshotEntry[int], string, string) { return entries(2), "X", "" },
		ActCmd:  'D',
		Confirm: func(item int) (string, string) { return "", "BLOCKED" },
		OnAct:   func(_ context.Context, item int) (bool, string) { acted = true; return true, "" },
	}
	r := &scriptRenderer{acts: []ListAction{{Cmd: 'D', Row: 0}, {Cmd: 'D', Row: 0}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if acted {
		t.Error("a blocked Confirm must veto: OnAct must not run")
	}
	if got := r.views[1].ErrMsg; got != "BLOCKED" {
		t.Errorf("veto message = %q, want BLOCKED on the message line", got)
	}
}

func TestRunSnapshotList_OtherKeyCancelsPendingConfirm(t *testing.T) {
	acted := false
	cfg := SnapshotConfig[int]{
		Rows:    24,
		Fetch:   func(context.Context) ([]SnapshotEntry[int], string, string) { return entries(2), "X", "" },
		ActCmd:  'D',
		Confirm: func(item int) (string, string) { return "CONFIRM", "" },
		OnAct:   func(_ context.Context, item int) (bool, string) { acted = true; return true, "" },
	}
	r := &scriptRenderer{acts: []ListAction{{Cmd: 'D', Row: 0}, {PF: 8}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if acted {
		t.Error("a non-D action must cancel the pending confirm")
	}
}

func TestRunSnapshotList_EmptyShowsMarker(t *testing.T) {
	cfg := SnapshotConfig[int]{
		Rows: 24, Empty: "(none)",
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) { return nil, "X", "" },
	}
	r := &scriptRenderer{acts: []ListAction{{PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if r.views[0].Empty != "(none)" {
		t.Errorf("empty marker = %q, want (none)", r.views[0].Empty)
	}
}
