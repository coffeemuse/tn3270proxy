# Active Sessions `S`-Detail Screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `S` (Select) drill-down on the admin Active Sessions list that opens a read-only session-detail screen (full untruncated client address, reverse-DNS PTR, both timestamps + ages, user, service) with a confirm-gated **PF11=Disconnect** — and remove the `D` disconnect from the list itself.

**Architecture:** The list (`RunSnapshotList`) gains an `OnSelect` that builds a `DetailView` and runs a new, small `ui3270.RunDetail` driver. `RunDetail` mirrors `RunSnapshotList`'s two-press confirm gate but keys on a PF AID (PF11) instead of a typed line command — no input field, cursor stays home. The detail screen reuses the audit-detail builder; a new optional `DetailView.Message` line (row 2) carries the confirm prompt / `DISCONNECTED` status. Reverse DNS reuses the existing `AUDIT_REVERSE_DNS` flag and the `ptr`/`Resolver` seam. Layering holds: `internal/server` does the lookup + formatting; `internal/ui3270` stays pure-render.

**Tech Stack:** Go, `github.com/racingmars/go3270`, `modernc.org/sqlite`. TDD with `go test ./... -race`. 3270 protocol verification via the `s3270-smoke-testing` skill.

**Spec:** `docs/superpowers/specs/2026-06-09-active-sessions-detail-screen-design.md`

---

## File structure

- `internal/ui3270/types.go` — add `Message` to `DetailView`; add `DetailAct` to the `Renderer` interface; add `DetailConfig`.
- `internal/ui3270/snapshotscreen.go` — `buildDetailScreen`: render `Message` on row 2, shift fields to `bodyTopRow()` (row 3).
- `internal/ui3270/action.go` — add `detailAction` + `pfAID` helpers.
- `internal/ui3270/renderer.go` — add `(*go3270Renderer).DetailAct`.
- `internal/ui3270/detail.go` *(new)* — `RunDetail` driver.
- `internal/ui3270/snapshotlist.go` — widen `OnSelect` to `(refresh bool, fatal error)`; refresh on select.
- `internal/server/admin_audit.go` — update the audit `OnSelect` closure to the new signature.
- `internal/server/admin_sessions.go` — remove list disconnect; add `sessionDetail` + `OnSelect` wiring.
- Tests alongside each, plus the four fake `Renderer`s gain a `DetailAct` stub, and `internal/server/admin_sessions_test.go` is rewritten for the detail flow.
- `.claude/skills/s3270-smoke-testing/smoke.sh` — drill-down + disconnect-from-detail assertions.

**New `.go` files** (`detail.go`, `detail_test.go`) must carry the repo's full GPL-3.0 copyright header (copy the ~18-line block verbatim from any existing file in the same package, e.g. `internal/ui3270/snapshotlist.go`) — the abbreviated headers shown in this plan's code blocks are for brevity only.

**Renderer implementations that MUST gain `DetailAct` (compile gate):**
- `internal/ui3270/renderer.go` — `go3270Renderer` (production)
- `internal/ui3270/form_test.go` — `fakeRenderer`
- `internal/ui3270/snapshotlist_test.go` — `scriptRenderer`
- `internal/server/admin_test.go` — `fakeAdminPresenter`
- `internal/server/admin_sessions_test.go` — `sessRenderer`

---

## Task 1: `DetailView.Message` + row-2 message line in `buildDetailScreen`

Render an optional red message line on row 2 and move the field block to row 3 (`bodyTopRow()`), aligning the detail screen with the three-band ISPF layout. This shifts the audit-detail fields down one cosmetic row.

**Files:**
- Modify: `internal/ui3270/types.go` (the `DetailView` struct, ~line 149)
- Modify: `internal/ui3270/snapshotscreen.go` (`buildDetailScreen`, ~lines 146-179)
- Test: `internal/ui3270/snapshotscreen_test.go` (`TestBuildDetailScreen`, `TestBuildDetailScreen_DotLeader`, + new)

- [ ] **Step 1: Update the existing detail-screen tests to the new row layout (failing)**

In `internal/ui3270/snapshotscreen_test.go`, change `TestBuildDetailScreen` so the first label is on row 3 and the event value on row 4, and assert a message line. Replace the body of `TestBuildDetailScreen` (lines 104-128) with:

```go
func TestBuildDetailScreen(t *testing.T) {
	v := DetailView{
		Title:   "AUDIT DETAIL",
		Message: "CONFIRM DISCONNECT BOB - PRESS PF11 AGAIN",
		Fields: []DetailField{
			{Label: "Date/Time", Value: "2026-06-06 (2026.157) 14:28:07 UTC"},
			{Label: "Event", Value: "AUTH_FAIL", Color: go3270.Red},
		},
		BodyLabel: "Detail", Body: "delay=4s count=2",
		PFHelp: "PF3=Back",
	}
	screen, cur := buildDetailScreen(24, v)

	if f, ok := fieldAt(screen, 0, centerCol(len("AUDIT DETAIL"))); !ok || f.Content != "AUDIT DETAIL" || f.Color != go3270.White || !f.Intense {
		t.Errorf("title not centered-white: %+v ok=%v", f, ok)
	}
	// Message on row 2 (messageRow), red.
	if f, ok := fieldAt(screen, 2, 2); !ok || f.Content != v.Message || f.Color != go3270.Red {
		t.Errorf("message line wrong: %+v ok=%v", f, ok)
	}
	// Fields now begin at row 3 (bodyTopRow), not row 2.
	if f, ok := fieldAt(screen, 3, 2); !ok || f.Content != "Date/Time" {
		t.Errorf("first label wrong: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 4, detailValueCol); !ok || f.Content != "AUTH_FAIL" || f.Color != go3270.Red {
		t.Errorf("event value wrong/uncoloured: %+v ok=%v", f, ok)
	}
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("cursor = %+v, want home", cur)
	}
}
```

In `TestBuildDetailScreen_DotLeader` (lines 168-183), the field has no `Message`, so it stays on the field-start row — change the assertion row from 2 to 3:

```go
	if f, ok := fieldAt(screen, 3, labelAttrCol); !ok || f.Content != want {
		t.Errorf("dot-leader label = %q ok=%v, want %q", f.Content, ok, want)
	}
```

Add a test that a `DetailView` with no `Message` emits nothing on row 2:

```go
func TestBuildDetailScreen_NoMessageLeavesRow2Empty(t *testing.T) {
	v := DetailView{Title: "X", Fields: []DetailField{{Label: "A", Value: "b"}}, PFHelp: "PF3=Back"}
	screen, _ := buildDetailScreen(24, v)
	for _, f := range screen {
		if f.Row == 2 {
			t.Errorf("row 2 must be empty without a Message; got %+v", f)
		}
	}
	if f, ok := fieldAt(screen, 3, labelAttrCol); !ok || f.Content != "A" {
		t.Errorf("first field should be on row 3: %+v ok=%v", f, ok)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui3270/ -run TestBuildDetailScreen -v`
Expected: FAIL — `Message` is not a field of `DetailView` (compile error), and/or row assertions mismatch.

- [ ] **Step 3: Add the `Message` field to `DetailView`**

In `internal/ui3270/types.go`, the `DetailView` struct (~line 149). Add the `Message` field with a doc note:

```go
// DetailView is what to paint for a read-only detail screen: a column of
// label/value fields, then a full-width wrapped free-text block under BodyLabel.
// DotLeader renders the field labels with right-aligned colons and ISPF-style
// dot leaders (matching the form view); off leaves them plain. Message, when
// non-empty, renders a red line on the message row (row 2) — used by RunDetail
// for the confirm prompt and the post-action status.
type DetailView struct {
	Title, BodyLabel, Body, PFHelp string
	Message                        string
	Fields                         []DetailField
	DotLeader                      bool
}
```

- [ ] **Step 4: Render the message line and shift fields to row 3**

In `internal/ui3270/snapshotscreen.go`, `buildDetailScreen` (~lines 146-164). Replace the screen seed + `row := 2` with a message line and `row := bodyTopRow()`:

```go
func buildDetailScreen(rows int, v DetailView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
	}
	if v.Message != "" {
		screen = append(screen, go3270.Field{
			Row: messageRow(), Col: labelAttrCol, Color: go3270.Red, Intense: true, Content: v.Message,
		})
	}
	row := bodyTopRow()
	// Dot-leadered labels right-align their colon just before the value column
	// (col detailValueCol), matching the form view's geometry.
	labelMax := formLabelMax(detailValueCol)
	for _, f := range v.Fields {
```

(The rest of the loop and body rendering is unchanged.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui3270/ -run TestBuildDetailScreen -v`
Expected: PASS (all three).

- [ ] **Step 6: Commit**

```bash
git add internal/ui3270/types.go internal/ui3270/snapshotscreen.go internal/ui3270/snapshotscreen_test.go
git commit -m "feat(ui3270): add DetailView.Message row-2 line; fields start at row 3"
```

---

## Task 2: `DetailAct` renderer seam (`detailAction` + `pfAID` + interface + all fakes)

Add an action-capable detail render that exits on PF3 **or** a configurable PF (PF11). This changes the `Renderer` interface, so every implementation must gain the method in the same commit to keep the tree compiling.

**Files:**
- Modify: `internal/ui3270/action.go` (add helpers)
- Modify: `internal/ui3270/types.go` (`Renderer` interface, ~line 68)
- Modify: `internal/ui3270/renderer.go` (production method)
- Modify: `internal/ui3270/form_test.go`, `internal/ui3270/snapshotlist_test.go`, `internal/server/admin_test.go`, `internal/server/admin_sessions_test.go` (fake stubs)
- Test: `internal/ui3270/action_test.go`

- [ ] **Step 1: Write the failing test for `detailAction`**

In `internal/ui3270/action_test.go`, add:

```go
func TestDetailAction(t *testing.T) {
	if got := detailAction(go3270.Response{AID: go3270.AIDPF3}, 11); got != (ListAction{PF: 3}) {
		t.Errorf("PF3 → %+v, want {PF:3}", got)
	}
	if got := detailAction(go3270.Response{AID: go3270.AIDPF11}, 11); got != (ListAction{PF: 11}) {
		t.Errorf("PF11 with actPF=11 → %+v, want {PF:11}", got)
	}
	// PF11 is inert when it is not the configured action key.
	if got := detailAction(go3270.Response{AID: go3270.AIDPF11}, 0); got != (ListAction{}) {
		t.Errorf("PF11 with actPF=0 → %+v, want {}", got)
	}
	// Enter (and anything else) maps to the zero action.
	if got := detailAction(go3270.Response{AID: go3270.AIDEnter}, 11); got != (ListAction{}) {
		t.Errorf("Enter → %+v, want {}", got)
	}
}
```

Confirm `internal/ui3270/action_test.go` imports `github.com/racingmars/go3270` (add it if missing).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui3270/ -run TestDetailAction -v`
Expected: FAIL — `detailAction` / `pfAID` undefined.

- [ ] **Step 3: Add the `detailAction` + `pfAID` helpers**

In `internal/ui3270/action.go`, append:

```go
// pfAID maps a PF number to its go3270 AID for the keys a detail action
// supports. Intentionally minimal — only the action keys RunDetail uses. ok is
// false for an unsupported or zero number.
func pfAID(n int) (go3270.AID, bool) {
	switch n {
	case 11:
		return go3270.AIDPF11, true
	}
	return 0, false
}

// detailAction maps a detail-screen response to a ListAction. PF3 is always the
// back key; actPF (when supported and pressed) returns {PF: actPF}; everything
// else is the zero action (re-present).
func detailAction(resp go3270.Response, actPF int) ListAction {
	if resp.AID == go3270.AIDPF3 {
		return ListAction{PF: 3}
	}
	if a, ok := pfAID(actPF); ok && resp.AID == a {
		return ListAction{PF: actPF}
	}
	return ListAction{}
}
```

Confirm `internal/ui3270/action.go` already imports `github.com/racingmars/go3270` (it uses `go3270.Response` in `listAction`, so it does).

- [ ] **Step 4: Run the helper test to verify it passes**

Run: `go test ./internal/ui3270/ -run TestDetailAction -v`
Expected: PASS.

- [ ] **Step 5: Add `DetailAct` to the `Renderer` interface**

In `internal/ui3270/types.go`, the `Renderer` interface (~line 68). Add the method:

```go
type Renderer interface {
	List(ListView) (ListAction, error)
	Form(FormView) (FormAction, error)
	Snapshot(SnapshotView) (ListAction, error) // paged read-only list
	Detail(DetailView) error                   // read-only screen; returns on PF3
	// DetailAct renders a detail screen that exits on PF3 or the action PF
	// (actPF; 0 ⇒ PF3 only). Returns the action so a driver can run a
	// confirm-gated PF-key command (e.g. PF11=Disconnect).
	DetailAct(v DetailView, actPF int) (ListAction, error)
}
```

- [ ] **Step 6: Implement the production `DetailAct`**

In `internal/ui3270/renderer.go`, after `Detail` (~line 116), add:

```go
func (g *go3270Renderer) DetailAct(v DetailView, actPF int) (ListAction, error) {
	screen, cur := buildDetailScreen(g.rows, v)
	exit := []go3270.AID{go3270.AIDPF3}
	if a, ok := pfAID(actPF); ok {
		exit = append(exit, a)
	}
	resp, err := g.call(screen, exit, cur)
	if err != nil {
		return ListAction{}, err
	}
	return detailAction(resp, actPF), nil
}
```

- [ ] **Step 7: Add `DetailAct` stubs to the four fake renderers**

`internal/ui3270/form_test.go` — after the `Detail` method (~line 55):

```go
func (f *fakeRenderer) DetailAct(DetailView, int) (ListAction, error) { return ListAction{}, nil }
```

`internal/ui3270/snapshotlist_test.go` — extend `scriptRenderer` with a scripted detail-action queue. Replace the struct (lines 29-33) and add the method:

```go
type scriptRenderer struct {
	acts      []ListAction
	views     []SnapshotView
	detailHit int
	detActs   []ListAction
	dets      []DetailView
}

func (s *scriptRenderer) DetailAct(v DetailView, _ int) (ListAction, error) {
	s.dets = append(s.dets, v)
	if len(s.detActs) == 0 {
		panic("unexpected DetailAct call")
	}
	a := s.detActs[0]
	s.detActs = s.detActs[1:]
	return a, nil
}
```

`internal/server/admin_test.go` — after `fakeAdminPresenter.Detail` (~line 106):

```go
func (f *fakeAdminPresenter) DetailAct(v ui3270.DetailView, _ int) (ui3270.ListAction, error) {
	f.gotDets = append(f.gotDets, v)
	if len(f.detActs) == 0 {
		panic("unexpected DetailAct call")
	}
	a := f.detActs[0]
	f.detActs = f.detActs[1:]
	return a, nil
}
```

…and add a `detActs []ui3270.ListAction` field to the `fakeAdminPresenter` struct (after `snaps`, ~line 55):

```go
	snaps    []ui3270.ListAction
	detActs  []ui3270.ListAction
	gotSnaps []ui3270.SnapshotView
	gotDets  []ui3270.DetailView
```

`internal/server/admin_sessions_test.go` — the `sessRenderer` is fully rewritten in Task 5; for now add a minimal stub so the package compiles after the interface change. After `sessRenderer.Detail` (~line 55):

```go
func (r *sessRenderer) DetailAct(ui3270.DetailView, int) (ui3270.ListAction, error) {
	return ui3270.ListAction{}, nil
}
```

- [ ] **Step 8: Build + run the whole ui3270 and server suites**

Run: `go build ./... && go test ./internal/ui3270/ ./internal/server/ -race`
Expected: PASS (interface satisfied everywhere; behavior unchanged).

- [ ] **Step 9: Commit**

```bash
git add internal/ui3270/action.go internal/ui3270/action_test.go internal/ui3270/types.go internal/ui3270/renderer.go internal/ui3270/form_test.go internal/ui3270/snapshotlist_test.go internal/server/admin_test.go internal/server/admin_sessions_test.go
git commit -m "feat(ui3270): add DetailAct renderer seam (PF3/PF11 detail action)"
```

---

## Task 3: `RunDetail` driver

A confirm-gated detail driver mirroring `RunSnapshotList`'s two-press gate, keyed on `ActPF`. Returns `refresh` so the caller re-fetches its list when an action committed.

**Files:**
- Create: `internal/ui3270/detail.go`
- Modify: `internal/ui3270/types.go` (add `DetailConfig`)
- Test: `internal/ui3270/detail_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `internal/ui3270/detail_test.go`:

```go
/*
 * Copyright 2026 by CoffeeMuse.
 *
 * This file is part of tn3270proxy. See LICENSE / GPL-3.0-or-later.
 */

package ui3270

import (
	"context"
	"testing"
)

func baseDetailCfg(onActStatus string, refresh bool, acted *bool) DetailConfig {
	return DetailConfig{
		View:       DetailView{Title: "SESSION DETAIL", PFHelp: "PF11=Disconnect   PF3=Back"},
		ActPF:      11,
		DonePFHelp: "PF3=Back",
		Confirm:    func() (string, string) { return "CONFIRM DISCONNECT BOB - PRESS PF11 AGAIN", "" },
		OnAct: func(context.Context) (string, bool) {
			*acted = true
			return onActStatus, refresh
		},
	}
}

func TestRunDetail_TwoPressCommit(t *testing.T) {
	acted := false
	cfg := baseDetailCfg("DISCONNECTED", true, &acted)
	r := &scriptRenderer{detActs: []ListAction{{PF: 11}, {PF: 11}, {PF: 3}}}
	refresh, err := RunDetail(context.Background(), r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !acted {
		t.Error("OnAct must run on the second PF11")
	}
	if !refresh {
		t.Error("refresh must propagate from OnAct")
	}
	// Render 2 (after the first PF11) shows the confirm prompt.
	if r.dets[1].Message != "CONFIRM DISCONNECT BOB - PRESS PF11 AGAIN" {
		t.Errorf("prompt = %q", r.dets[1].Message)
	}
	// Render 3 (after the second PF11) shows the status and the done PF help.
	if r.dets[2].Message != "DISCONNECTED" {
		t.Errorf("status = %q", r.dets[2].Message)
	}
	if r.dets[2].PFHelp != "PF3=Back" {
		t.Errorf("done PFHelp = %q, want PF3=Back", r.dets[2].PFHelp)
	}
}

func TestRunDetail_VetoNeverCommits(t *testing.T) {
	acted := false
	cfg := baseDetailCfg("DISCONNECTED", true, &acted)
	cfg.Confirm = func() (string, string) { return "", "CANNOT DISCONNECT YOUR OWN SESSION" }
	r := &scriptRenderer{detActs: []ListAction{{PF: 11}, {PF: 3}}}
	refresh, err := RunDetail(context.Background(), r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if acted {
		t.Error("OnAct must not run after a veto")
	}
	if refresh {
		t.Error("a vetoed action must not request a refresh")
	}
	if r.dets[1].Message != "CANNOT DISCONNECT YOUR OWN SESSION" {
		t.Errorf("veto message = %q", r.dets[1].Message)
	}
}

func TestRunDetail_PF3WithoutActionDoesNotRefresh(t *testing.T) {
	acted := false
	cfg := baseDetailCfg("DISCONNECTED", true, &acted)
	r := &scriptRenderer{detActs: []ListAction{{PF: 3}}}
	refresh, err := RunDetail(context.Background(), r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if acted || refresh {
		t.Errorf("plain PF3 must neither act nor refresh; acted=%v refresh=%v", acted, refresh)
	}
}

func TestRunDetail_ActionDisarmsAfterCommit(t *testing.T) {
	// After commit, the driver must call DetailAct with actPF=0 so PF11 is no
	// longer a live exit key. We assert via a recording renderer.
	acted := false
	cfg := baseDetailCfg("DISCONNECTED", true, &acted)
	rec := &recordPFRenderer{detActs: []ListAction{{PF: 11}, {PF: 11}, {PF: 3}}}
	if _, err := RunDetail(context.Background(), rec, cfg); err != nil {
		t.Fatal(err)
	}
	// First two calls see actPF=11; the third (post-commit) sees 0.
	if got := rec.actPFs; len(got) != 3 || got[0] != 11 || got[1] != 11 || got[2] != 0 {
		t.Errorf("actPF sequence = %v, want [11 11 0]", got)
	}
}

// recordPFRenderer records the actPF passed to each DetailAct call.
type recordPFRenderer struct {
	detActs []ListAction
	actPFs  []int
}

func (r *recordPFRenderer) List(ListView) (ListAction, error)         { panic("unused") }
func (r *recordPFRenderer) Form(FormView) (FormAction, error)         { panic("unused") }
func (r *recordPFRenderer) Snapshot(SnapshotView) (ListAction, error) { panic("unused") }
func (r *recordPFRenderer) Detail(DetailView) error                   { return nil }
func (r *recordPFRenderer) DetailAct(_ DetailView, actPF int) (ListAction, error) {
	r.actPFs = append(r.actPFs, actPF)
	a := r.detActs[0]
	r.detActs = r.detActs[1:]
	return a, nil
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui3270/ -run TestRunDetail -v`
Expected: FAIL — `RunDetail` / `DetailConfig` undefined.

- [ ] **Step 3: Add `DetailConfig` to `types.go`**

In `internal/ui3270/types.go`, after the `DetailView` struct, add:

```go
// DetailConfig parameterizes RunDetail: a read-only detail screen carrying at
// most one confirm-gated PF-key action. ActPF is the action key (e.g. 11 for
// PF11=Disconnect); 0 ⇒ pure read-only (PF3 only). Confirm and OnAct must both
// be non-nil when ActPF != 0 — the action is always confirm-gated (no
// immediate-commit path), mirroring RunSnapshotList's ActCmd contract.
//
// First ActPF press consults Confirm: blocked != "" vetoes with that message;
// otherwise prompt is shown and the action arms. Second ActPF press commits via
// OnAct, which returns (status, refresh): status replaces the message line, the
// action disarms (ActPF goes inert, PFHelp becomes DonePFHelp), and the screen
// stays up until PF3. refresh is returned from RunDetail so the caller re-fetches
// its list.
type DetailConfig struct {
	View       DetailView
	ActPF      int
	DonePFHelp string
	Confirm    func() (prompt, blocked string)
	OnAct      func(ctx context.Context) (status string, refresh bool)
}
```

Confirm `internal/ui3270/types.go` imports `context` (it does — `ListConfig`/`Command` use it).

- [ ] **Step 4: Create the `RunDetail` driver**

Create `internal/ui3270/detail.go`:

```go
/*
 * Copyright 2026 by CoffeeMuse.
 *
 * This file is part of tn3270proxy. See LICENSE / GPL-3.0-or-later.
 */

package ui3270

import "context"

// RunDetail renders cfg.View until PF3, optionally offering one confirm-gated
// PF-key action (cfg.ActPF). It returns refresh=true when an action committed,
// so the caller re-fetches its list. A non-nil error is a dead connection.
//
// Two-press gate: first ActPF press consults Confirm (blocked vetoes; prompt
// arms); second press commits via OnAct, shows the returned status, disarms the
// action (ActPF goes inert, PFHelp → DonePFHelp), and stays up until PF3.
func RunDetail(ctx context.Context, r Renderer, cfg DetailConfig) (bool, error) {
	v := cfg.View
	var armed, acted, refresh bool
	for {
		actPF := cfg.ActPF
		if acted {
			actPF = 0 // committed: the action key is no longer live
		}
		act, err := r.DetailAct(v, actPF)
		if err != nil {
			return refresh, err
		}
		switch {
		case act.PF == 3:
			return refresh, nil
		case actPF != 0 && act.PF == actPF:
			if armed {
				armed = false
				acted = true
				status, rf := cfg.OnAct(ctx)
				v.Message = status
				v.PFHelp = cfg.DonePFHelp
				refresh = rf
			} else {
				prompt, blocked := cfg.Confirm()
				if blocked != "" {
					v.Message = blocked
				} else {
					v.Message = prompt
					armed = true
				}
			}
		default:
			armed = false
			v.Message = ""
		}
	}
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/ui3270/ -run TestRunDetail -race -v`
Expected: PASS (all four).

- [ ] **Step 6: Commit**

```bash
git add internal/ui3270/detail.go internal/ui3270/detail_test.go internal/ui3270/types.go
git commit -m "feat(ui3270): add RunDetail confirm-gated detail-action driver"
```

---

## Task 4: Widen `OnSelect` to `(refresh bool, fatal error)` + refresh-on-select

`OnSelect` can now mutate (disconnect-from-detail), so it must be able to tell `RunSnapshotList` to re-fetch.

**Files:**
- Modify: `internal/ui3270/snapshotlist.go` (`OnSelect` type ~line 42; the `'S'` branch ~lines 142-148)
- Modify: `internal/server/admin_audit.go` (`OnSelect` closure ~line 115)
- Test: `internal/ui3270/snapshotlist_test.go` (`TestRunSnapshotList_SelectInvokesOnSelect` + new)

- [ ] **Step 1: Update + add the failing tests**

In `internal/ui3270/snapshotlist_test.go`, replace `TestRunSnapshotList_SelectInvokesOnSelect` (lines 89-109) with a version using the new signature, and add a refresh test:

```go
func TestRunSnapshotList_SelectInvokesOnSelect(t *testing.T) {
	var picked int = -1
	cfg := SnapshotConfig[int]{
		Rows:  24,
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) { return entries(3), "X", "" },
		OnSelect: func(_ context.Context, r Renderer, item int) (bool, error) {
			picked = item
			return false, r.Detail(DetailView{})
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

func TestRunSnapshotList_SelectRefreshRefetches(t *testing.T) {
	fetches := 0
	cfg := SnapshotConfig[int]{
		Rows: 24,
		Fetch: func(context.Context) ([]SnapshotEntry[int], string, string) {
			fetches++
			return entries(3), "X", ""
		},
		OnSelect: func(context.Context, Renderer, int) (bool, error) { return true, nil },
	}
	// S drills in (OnSelect asks for refresh), then PF3 exits.
	r := &scriptRenderer{acts: []ListAction{{Cmd: 'S', Row: 0}, {PF: 3}}}
	if err := RunSnapshotList(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if fetches != 2 {
		t.Errorf("fetches = %d, want 2 (initial + refresh after select)", fetches)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui3270/ -run TestRunSnapshotList_Select -v`
Expected: FAIL — signature mismatch (`OnSelect` returns one value) / refresh not honored.

- [ ] **Step 3: Widen the `OnSelect` type**

In `internal/ui3270/snapshotlist.go`, the `SnapshotConfig` struct (~line 42). Update the field and its doc:

```go
	// OnSelect handles the 'S' line command (nil ⇒ 'S' is inert). It returns
	// (refresh, fatal): refresh re-fetches the snapshot on return (used when the
	// detail screen mutated state, e.g. a disconnect); fatal is a dead connection.
	OnSelect func(ctx context.Context, r Renderer, item T) (refresh bool, fatal error)
```

- [ ] **Step 4: Honor the refresh flag in the `'S'` branch**

In `internal/ui3270/snapshotlist.go`, the `case act.Cmd == 'S'` branch (~lines 142-148). Replace with:

```go
		case act.Cmd == 'S':
			pending = nil
			if cfg.OnSelect != nil && act.Row < len(pageRows) {
				refresh, ferr := cfg.OnSelect(ctx, r, pageRows[act.Row].Item)
				if ferr != nil {
					return ferr
				}
				if refresh {
					loaded = false
					page = 0
				}
			}
```

- [ ] **Step 5: Update the audit `OnSelect` closure**

In `internal/server/admin_audit.go`, the `OnSelect` closure (~lines 115-117). Update to the new signature:

```go
		OnSelect: func(ctx context.Context, r ui3270.Renderer, ev store.AuditEvent) (bool, error) {
			return false, r.Detail(f.auditDetail(ctx, ev))
		},
```

- [ ] **Step 6: Run the ui3270 + server suites**

Run: `go build ./... && go test ./internal/ui3270/ ./internal/server/ -race`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui3270/snapshotlist.go internal/ui3270/snapshotlist_test.go internal/server/admin_audit.go
git commit -m "feat(ui3270): OnSelect returns refresh so a drill-down can re-fetch the list"
```

---

## Task 5: Server `sessionDetail` + rewire `activeSessions` (remove list `D`, add `S` detail)

The list becomes read-only nav (`S = detail`); disconnect moves to the detail screen via `RunDetail`.

**Files:**
- Modify: `internal/server/admin_sessions.go` (remove `ActCmd`/`Confirm`/`OnAct`; add `sessionDetail` + `OnSelect`)
- Test: `internal/server/admin_sessions_test.go` (rewrite the disconnect-flow tests; add `sessionDetail` tests)

- [ ] **Step 1: Rewrite the test scaffolding + disconnect-flow tests (failing)**

In `internal/server/admin_sessions_test.go`:

Replace the `sessRenderer` (lines 47-61) with a version that scripts both snapshot and detail actions and records detail views:

```go
// sessRenderer scripts Snapshot + DetailAct actions for the activeSessions flow.
type sessRenderer struct {
	acts    []ui3270.ListAction
	views   []ui3270.SnapshotView
	detActs []ui3270.ListAction
	dets    []ui3270.DetailView
}

func (r *sessRenderer) List(ui3270.ListView) (ui3270.ListAction, error) { panic("unused") }
func (r *sessRenderer) Form(ui3270.FormView) (ui3270.FormAction, error) { panic("unused") }
func (r *sessRenderer) Detail(ui3270.DetailView) error                  { return nil }
func (r *sessRenderer) DetailAct(v ui3270.DetailView, _ int) (ui3270.ListAction, error) {
	r.dets = append(r.dets, v)
	if len(r.detActs) == 0 {
		panic("unexpected DetailAct call")
	}
	a := r.detActs[0]
	r.detActs = r.detActs[1:]
	return a, nil
}
func (r *sessRenderer) Snapshot(v ui3270.SnapshotView) (ui3270.ListAction, error) {
	r.views = append(r.views, v)
	a := r.acts[0]
	r.acts = r.acts[1:]
	return a, nil
}
```

Replace `newSessionsFlow` (lines 63-72) to open a real store (so `auditReverseDNS`/`GetConfig` works) and default reverse DNS off (disconnect-flow tests need no resolver):

```go
func newSessionsFlow(t *testing.T, reg SessionRegistry, selfID uint64, r ui3270.Renderer, audit *[]store.AuditEvent) *adminFlow {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SetConfig(context.Background(), sysconfig.KeyAuditReverseDNS, "N"); err != nil {
		t.Fatal(err)
	}
	return &adminFlow{
		store:         st,
		term:          Term{Rows: 24, Cols: 80},
		renderer:      func(_ net.Conn) ui3270.Renderer { return r },
		sessions:      reg,
		selfSessionID: selfID,
		now:           func() time.Time { return time.Unix(1_000_000, 0).UTC() },
		audit:         func(_ context.Context, ev store.AuditEvent) { *audit = append(*audit, ev) },
	}
}
```

Add the `sysconfig` import to the test file's import block:

```go
	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
```

Rewrite `TestActiveSessions_DisconnectAuditsSubject` (drill in with `S`, disconnect with PF11×2):

```go
func TestActiveSessions_DisconnectAuditsSubject(t *testing.T) {
	reg := &fakeRegistry{
		views: []SessionView{
			{ID: 1, RemoteAddr: "10.0.0.9:5050", ConnectedAt: time.Unix(999_000, 0), LoggedInAt: time.Unix(999_500, 0), Username: "BOB"},
		},
		disconnects: map[uint64]SessionView{1: {ID: 1, RemoteAddr: "10.0.0.9:5050", Username: "BOB"}},
		okFor:       map[uint64]bool{1: true},
	}
	var audits []store.AuditEvent
	r := &sessRenderer{
		acts:    []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		detActs: []ui3270.ListAction{{PF: 11}, {PF: 11}, {PF: 3}},
	}
	f := newSessionsFlow(t, reg, 7 /*self is id 7, not present*/, r, &audits)

	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 1 || reg.disconnect[0] != 1 {
		t.Fatalf("Disconnect calls = %v, want [1]", reg.disconnect)
	}
	if len(audits) != 1 || audits[0].Kind != store.AuditSessionDisconnect || audits[0].Username != "BOB" {
		t.Fatalf("audit = %+v, want one session_disconnect for BOB", audits)
	}
	// The confirm prompt appears on the second detail render; DISCONNECTED on the third.
	if r.dets[1].Message != "CONFIRM DISCONNECT BOB - PRESS PF11 AGAIN" {
		t.Errorf("confirm prompt = %q", r.dets[1].Message)
	}
	if r.dets[2].Message != "DISCONNECTED" {
		t.Errorf("status = %q, want DISCONNECTED", r.dets[2].Message)
	}
}
```

Rewrite `TestActiveSessions_SelfDisconnectVetoed`:

```go
func TestActiveSessions_SelfDisconnectVetoed(t *testing.T) {
	reg := &fakeRegistry{
		views:       []SessionView{{ID: 7, RemoteAddr: "10.0.0.4:6060", LoggedInAt: time.Unix(1, 0), Username: "ADMIN"}},
		disconnects: map[uint64]SessionView{},
		okFor:       map[uint64]bool{},
	}
	var audits []store.AuditEvent
	r := &sessRenderer{
		acts:    []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		detActs: []ui3270.ListAction{{PF: 11}, {PF: 3}}, // PF11 vetoed, then back
	}
	f := newSessionsFlow(t, reg, 7, r, &audits)

	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 0 {
		t.Errorf("self-session must not be disconnected; got calls %v", reg.disconnect)
	}
	if len(audits) != 0 {
		t.Errorf("no audit on a vetoed self-disconnect; got %+v", audits)
	}
	if r.dets[1].Message != "CANNOT DISCONNECT YOUR OWN SESSION" {
		t.Errorf("veto message = %q", r.dets[1].Message)
	}
}
```

Rewrite `TestActiveSessions_DisconnectAlreadyGone`:

```go
func TestActiveSessions_DisconnectAlreadyGone(t *testing.T) {
	reg := &fakeRegistry{
		views:       []SessionView{{ID: 5, RemoteAddr: "1.2.3.4:9999", LoggedInAt: time.Unix(1, 0), Username: "GONE"}},
		disconnects: map[uint64]SessionView{5: {}},
		okFor:       map[uint64]bool{5: false}, // already ended
	}
	var audits []store.AuditEvent
	r := &sessRenderer{
		acts:    []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		detActs: []ui3270.ListAction{{PF: 11}, {PF: 11}, {PF: 3}},
	}
	f := newSessionsFlow(t, reg, 99, r, &audits)
	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 1 || reg.disconnect[0] != 5 {
		t.Fatalf("Disconnect calls = %v, want [5]", reg.disconnect)
	}
	if len(audits) != 0 {
		t.Errorf("no audit expected for an already-ended session; got %+v", audits)
	}
	if r.dets[2].Message != "SESSION ALREADY ENDED" {
		t.Errorf("status = %q, want SESSION ALREADY ENDED", r.dets[2].Message)
	}
}
```

Update the `TestActiveSessions_RowFormatting` caller to pass `t` (it uses `newSessionsFlow`):

```go
	f := newSessionsFlow(t, reg, 99 /*self not present*/, r, &audits)
```

Add `sessionDetail` field tests:

```go
func TestSessionDetail_Fields(t *testing.T) {
	r := &sessRenderer{}
	var audits []store.AuditEvent
	f := newSessionsFlow(t, &fakeRegistry{}, 0, r, &audits) // reverse DNS off
	v := SessionView{
		ID: 14, RemoteAddr: "[2001:db8:85a3:8d3:1319:8a2e:370:7348]:65535",
		ConnectedAt: time.Unix(999_000, 0), LoggedInAt: time.Unix(999_500, 0),
		Username: "DARROW", Service: "DEMO",
	}
	dv := f.sessionDetail(context.Background(), v)

	want := map[string]string{
		"Session": "14",
		"Client":  "[2001:db8:85a3:8d3:1319:8a2e:370:7348]:65535",
		"User":    "DARROW",
		"Service": "DEMO",
	}
	got := map[string]string{}
	for _, fld := range dv.Fields {
		got[fld.Label] = fld.Value
	}
	for label, w := range want {
		if got[label] != w {
			t.Errorf("field %q = %q, want %q", label, got[label], w)
		}
	}
	// No PTR field when reverse DNS is off.
	if _, ok := got["PTR"]; ok {
		t.Errorf("PTR must be absent when reverse DNS is off; fields=%+v", dv.Fields)
	}
	// Connected age = now(1_000_000) - 999_000 = 1000s = 00:16:40.
	if !strings.Contains(got["Connected"], "00:16:40") {
		t.Errorf("Connected = %q, want elapsed 00:16:40", got["Connected"])
	}
	if dv.PFHelp != "PF11=Disconnect   PF3=Back" {
		t.Errorf("PFHelp = %q", dv.PFHelp)
	}
}

func TestSessionDetail_PreAuthAndUnbridged(t *testing.T) {
	r := &sessRenderer{}
	var audits []store.AuditEvent
	f := newSessionsFlow(t, &fakeRegistry{}, 0, r, &audits)
	v := SessionView{ID: 2, RemoteAddr: "10.0.0.8:5051", ConnectedAt: time.Unix(999_900, 0)} // pre-auth, no service
	dv := f.sessionDetail(context.Background(), v)
	got := map[string]string{}
	for _, fld := range dv.Fields {
		got[fld.Label] = fld.Value
	}
	if got["User"] != "(login)" {
		t.Errorf("User = %q, want (login)", got["User"])
	}
	if got["Service"] != "-" {
		t.Errorf("Service = %q, want -", got["Service"])
	}
	if got["Logged in"] != "(not logged in)" {
		t.Errorf("Logged in = %q, want (not logged in)", got["Logged in"])
	}
}

func TestSessionDetail_PTRWhenEnabled(t *testing.T) {
	r := &sessRenderer{}
	var audits []store.AuditEvent
	f := newSessionsFlow(t, &fakeRegistry{}, 0, r, &audits)
	if err := f.store.SetConfig(context.Background(), sysconfig.KeyAuditReverseDNS, "Y"); err != nil {
		t.Fatal(err)
	}
	f.resolver = fakeResolver{names: []string{"host.example.de."}}
	v := SessionView{ID: 3, RemoteAddr: "203.0.113.9:5050", ConnectedAt: time.Unix(999_000, 0)}
	dv := f.sessionDetail(context.Background(), v)
	var ptrVal string
	for _, fld := range dv.Fields {
		if fld.Label == "PTR" {
			ptrVal = fld.Value
		}
	}
	if ptrVal != "host.example.de" {
		t.Errorf("PTR = %q, want host.example.de", ptrVal)
	}
}
```

Note: `f.store.SetConfig` — `adminFlow.store` is the `AdminStore` interface, which does not expose `SetConfig`. Call it on the concrete store instead: `f.store.(*store.Store).SetConfig(...)`. Use that form in `TestSessionDetail_PTRWhenEnabled`:

```go
	if err := f.store.(*store.Store).SetConfig(context.Background(), sysconfig.KeyAuditReverseDNS, "Y"); err != nil {
		t.Fatal(err)
	}
```

…and likewise inside `newSessionsFlow` use the concrete `st.SetConfig(...)` (it already holds `*store.Store`, so that is fine as written above).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run 'TestActiveSessions|TestSessionDetail' -v`
Expected: FAIL — `sessionDetail` undefined; `activeSessions` still has `ActCmd` wiring; signature mismatches.

- [ ] **Step 3: Remove the list disconnect + add `OnSelect` in `activeSessions`**

In `internal/server/admin_sessions.go`, replace the `activeSessions` config (lines 102-146) so the list is read-only nav and disconnect lives in `OnSelect`/`RunDetail`:

```go
	r := f.renderer(conn)
	cfg := ui3270.SnapshotConfig[SessionView]{
		Title:  "ACTIVE SESSIONS",
		Wide:   true,
		Head:   ui3270.SnapshotRow{Left: sessionHeader()},
		Legend: "S = detail",
		PFHelp: "PF3=Admin Menu  PF7=Up  PF8=Down  Enter=Refresh",
		Empty:  "(no active sessions)",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.SnapshotEntry[SessionView], string, string) {
			now := f.clock()
			views := f.sessions.Snapshot()
			rows := make([]ui3270.SnapshotEntry[SessionView], len(views))
			for i, v := range views {
				rows[i] = ui3270.SnapshotEntry[SessionView]{Row: fmtSessionRow(v, now, f.selfSessionID), Item: v}
			}
			asOf := "AS OF " + julianStamp(now) + "  " + now.Format("15:04") + " UTC"
			return rows, asOf, ""
		},
		OnSelect: func(ctx context.Context, r ui3270.Renderer, v SessionView) (bool, error) {
			return ui3270.RunDetail(ctx, r, ui3270.DetailConfig{
				View:       f.sessionDetail(ctx, v),
				ActPF:      11,
				DonePFHelp: "PF3=Back",
				Confirm: func() (string, string) {
					if v.ID == f.selfSessionID {
						return "", "CANNOT DISCONNECT YOUR OWN SESSION"
					}
					who := v.Username
					if who == "" {
						who = v.RemoteAddr
					}
					return "CONFIRM DISCONNECT " + who + " - PRESS PF11 AGAIN", ""
				},
				OnAct: func(ctx context.Context) (string, bool) {
					booted, ok := f.sessions.Disconnect(v.ID)
					if !ok {
						return "SESSION ALREADY ENDED", true
					}
					if f.audit != nil {
						f.audit(ctx, store.AuditEvent{
							Kind:     store.AuditSessionDisconnect,
							Username: booted.Username,
							Detail:   fmt.Sprintf("disconnected session %d (%s)", booted.ID, booted.RemoteAddr),
						})
					}
					return "DISCONNECTED", true
				},
			})
		},
	}
	return ui3270.RunSnapshotList(ctx, r, cfg)
```

- [ ] **Step 4: Add the `sessionDetail` builder**

In `internal/server/admin_sessions.go`, add (after `activeSessions`):

```go
// sessionDetail builds the read-only detail view for one live session: the full
// (untruncated) client address, an optional reverse-DNS PTR (when AUDIT_REVERSE_DNS
// is on — the same flag the audit detail uses), both timestamps with a Julian
// stamp and elapsed age, the user, and the bridged service.
func (f *adminFlow) sessionDetail(ctx context.Context, v SessionView) ui3270.DetailView {
	now := f.clock()
	connAt := v.ConnectedAt.UTC()

	loggedIn := "(not logged in)"
	user := "(login)"
	if !v.LoggedInAt.IsZero() {
		at := v.LoggedInAt.UTC()
		loggedIn = julianStamp(at) + " " + at.Format("15:04:05") + " UTC   (" + hhmmss(now.Sub(v.LoggedInAt)) + ")"
		user = v.Username
	}
	service := v.Service
	if service == "" {
		service = "-"
	}

	fields := []ui3270.DetailField{
		{Label: "Session", Value: strconv.FormatUint(v.ID, 10)},
		{Label: "Client", Value: v.RemoteAddr},
	}
	if f.auditReverseDNS(ctx) {
		res := f.resolver
		if res == nil {
			res = net.DefaultResolver
		}
		if name := ptr(ctx, res, v.RemoteAddr); name != "" {
			fields = append(fields, ui3270.DetailField{Label: "PTR", Value: name})
		}
	}
	fields = append(fields,
		ui3270.DetailField{Label: "Connected", Value: julianStamp(connAt) + " " + connAt.Format("15:04:05") + " UTC   (" + hhmmss(now.Sub(v.ConnectedAt)) + ")"},
		ui3270.DetailField{Label: "Logged in", Value: loggedIn},
		ui3270.DetailField{Label: "User", Value: user},
		ui3270.DetailField{Label: "Service", Value: service},
	)
	return ui3270.DetailView{
		Title:     "SESSION DETAIL",
		Fields:    fields,
		PFHelp:    "PF11=Disconnect   PF3=Back",
		DotLeader: true,
	}
}
```

(The existing imports — `context`, `fmt`, `net`, `strconv`, `time`, `store`, `ui3270` — already cover this; `ptr`, `julianStamp`, `hhmmss`, `auditReverseDNS` are existing package functions.)

- [ ] **Step 5: Run the server suite**

Run: `go test ./internal/server/ -run 'TestActiveSessions|TestSessionDetail' -race -v`
Expected: PASS.

- [ ] **Step 6: Full build + test**

Run: `go build ./... && go test ./... -race`
Expected: PASS across all packages.

- [ ] **Step 7: Commit**

```bash
git add internal/server/admin_sessions.go internal/server/admin_sessions_test.go
git commit -m "feat(server): move Active Sessions disconnect to an S-detail screen (#93)"
```

---

## Task 6: s3270 smoke assertions (drill-down + disconnect-from-detail)

Add a protocol-level check that `S` opens the detail with the full client address and PF11×2 disconnects, returning to a list without the session.

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`

- [ ] **Step 1: Read the existing active-sessions / admin section of the smoke script**

Run: `grep -n "ACTIVE SESSIONS\|activeSessions\|Disconnect\|admin menu\|Snapshot\|PF11\|PF(3" .claude/skills/s3270-smoke-testing/smoke.sh`
Expected: locate the admin-menu navigation block (the existing `S`-drill-down for the user list at ~line 514 is a model for the `Ascii`/`String`/cursor-assert idiom).

- [ ] **Step 2: Add the session-detail smoke block**

The script drives `s3270` via a heredoc of `String(...)`/`Enter`/`PF(n)`/`Ascii()`
commands, dumps the screen to a `$WORK/tN.out` file, and asserts with
`check "<name>" "<grep-BRE-pattern>" "$WORK/tN.out"` (and `ncheck` for
must-not-appear). Follow that exact idiom. After the active-sessions list is
reached with a second (non-self) client connected — reuse the existing two-client
setup from the commit `a98afff` block (smoke active-sessions render + two-client
disconnect) — add:

```bash
# --- Active Sessions 'S' detail + PF11 disconnect-from-detail (GH #93) ---
# Drill into row 0 with S; the detail shows the FULL client address and the
# PF11=Disconnect / PF3=Back help. PF11 twice disconnects; PF3 returns to a list
# that no longer lists that session.
s3270 <<EOF >/dev/null 2>&1
Connect($HOST:$PORT)
... (replay login + admin nav as the existing active-sessions block does) ...
String(S)
Enter
Ascii()
PF(11)
Ascii()
PF(11)
Ascii()
PF(3)
Ascii()
EOF
# (Capture the relevant Ascii() dumps into $WORK/t-detail.out as the existing
#  blocks do — typically by teeing the s3270 session transcript.)

check  "93a S opens session detail"        "SESSION DETAIL"     "$WORK/t-detail.out"
check  "93b detail shows Disconnect help"  "PF11=Disconnect"    "$WORK/t-detail.out"
check  "93c detail shows full client addr" "Client"             "$WORK/t-detail.out"
check  "93d first PF11 arms the confirm"   "PRESS PF11 AGAIN"   "$WORK/t-detail.out"
check  "93e second PF11 disconnects"       "DISCONNECTED"       "$WORK/t-detail.out"
check  "93f PF3 returns to the list"       "ACTIVE SESSIONS"    "$WORK/t-detail.out"
```

Match the script's actual capture mechanism (how it routes `Ascii()` output to the
`$WORK/tN.out` file) and login/nav replay from the existing active-sessions block —
that block is the working template; copy its connection + admin-menu navigation
verbatim and append the detail drill-down above.

- [ ] **Step 3: Run the smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all checks pass, including the new SESSION DETAIL / DISCONNECTED assertions.

- [ ] **Step 4: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "test(smoke): Active Sessions S-detail + PF11 disconnect-from-detail (#93)"
```

---

## Final verification

- [ ] **Run the complete suite with the race detector**

Run: `go build ./... && go test ./... -race`
Expected: PASS across every package.

- [ ] **Run the s3270 smoke test end-to-end**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all checks pass (cursor, screen content, the new detail/disconnect flow).

- [ ] **Manual emulator pass (final word on visual polish)**

Connect with `c3270` and confirm: the list legend reads `S = detail` (no `D`), `S` opens the detail with the full IPv6 address untruncated, PTR appears when reverse DNS is on, PF11 arms then commits showing `DISCONNECTED`, and PF3 returns to a list missing the session. Re-verify the audit detail still renders correctly after the row-2 message-line shift.
