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

	"github.com/racingmars/go3270"
)

// Cursor is the initial input-cursor position, already adjusted for the 3270
// attribute-byte offset. Cursor{0,0} means "home" (no input field to land on).
type Cursor struct{ Row, Col int }

// ListView is what to paint for a paginated line-command list.
type ListView struct {
	Title, RowInfo, Header, Legend, ErrMsg, PFHelp string
	Rows                                           []string
}

// FormField is one labeled input on a form.
type FormField struct {
	Name, Label, Value string
	Hidden             bool   // non-display (passwords)
	ReadOnly           bool   // display-only: rendered as static content, never an input
	Length             int    // input columns; effective max 62 (stop field clamps at col 79)
	Section            string // compact layout only: emit a "--- Section ---" banner above this field
	Suffix             string // compact layout only: static hint text after the input (e.g. "Y/N")
	SameRow            bool   // compact layout only: render on the previous field's row, second column
}

// FormView is what to paint for a labeled-input form.
type FormView struct {
	Title, ErrMsg string
	Fields        []FormField
	DotLeader     bool // render labels with right-aligned-colon dot leaders
	Compact       bool // sectioned, single-spaced layout (Section/Suffix/SameRow honored; bottom message line)
}

// ListAction is what the user did on a list screen. Cmd==0 && PF==0 ⇒ plain Enter.
type ListAction struct {
	Cmd byte // upper-cased line command ('S', 'D', …), 0 if none
	Row int  // index into the rendered page (valid when Cmd != 0)
	PF  int  // 3 back, 4 add, 7/8 page; 0 otherwise
}

// FormAction is what the user did on a form screen.
type FormAction struct {
	Values map[string]string // by field name; visible fields trimmed
	Cancel bool              // PF3
}

// EditorLine is one document line in the editor. Protected lines (wider than
// the editable width) render as static yellow text: D/I/R still work via the
// prefix, but content changes require re-import — the editor never truncates.
type EditorLine struct {
	Text      string
	Protected bool
}

// EditorView is what to paint for the line editor.
type EditorView struct {
	Title, RowInfo, ErrMsg, PFHelp string
	Lines                          []EditorLine
}

// EditorAction is what the user did on an editor screen. Maps are keyed by
// VISIBLE row index (the driver adds the page offset). Text holds only the
// fields the device returned (protected lines never appear). PF: 3 save+end,
// 7/8 page, 12 cancel; 0 = plain Enter.
type EditorAction struct {
	Prefix map[int]byte
	Text   map[int]string
	PF     int
}

// Renderer paints a view and returns the user's action. The production
// implementation wraps go3270; tests inject a scripted fake.
type Renderer interface {
	List(ListView) (ListAction, error)
	Form(FormView) (FormAction, error)
	Snapshot(SnapshotView) (ListAction, error) // paged read-only list
	Detail(DetailView) error                   // read-only screen; returns on PF3
	// DetailAct renders a detail screen that exits on PF3 or the action PF
	// (actPF; 0 ⇒ PF3 only). Returns the action so a driver can run a
	// confirm-gated PF-key command (e.g. PF11=Disconnect).
	DetailAct(v DetailView, actPF int) (ListAction, error)
	// Editor renders a line-editor page and returns the user's action.
	Editor(EditorView) (EditorAction, error)
}

// Row pairs a pre-formatted display string with its domain payload. The driver
// never inspects Item; it hands it back to Command handlers.
type Row[T any] struct {
	Display string
	Item    T
}

// Command is one list line-command. Confirm is the optional confirm/veto
// capability: nil ⇒ the command commits immediately on keypress; non-nil ⇒ on
// keypress it is consulted (blocked != "" vetoes with that message; otherwise
// prompt is shown and Commit runs on the next plain Enter).
type Command[T any] struct {
	Key     byte
	Commit  func(ctx context.Context, r Renderer, item T) (errMsg string, fatal error)
	Confirm func(item T) (prompt, blocked string)
}

// ListConfig parameterizes RunList. Fetch re-runs every iteration so mutations
// show up. Add (PF4) is optional: nil ⇒ PF4 is inert.
type ListConfig[T any] struct {
	Title, Header, Legend, PFHelp string
	Rows                          int // terminal row count → page-size math
	Fetch                         func(ctx context.Context) (rows []Row[T], errMsg string)
	Cmds                          []Command[T]
	Add                           func(ctx context.Context, r Renderer) (errMsg string, fatal error)
}

// FormConfig parameterizes RunForm. Submit validates+commits; "" ⇒ done,
// non-"" ⇒ stay on the form showing that error.
type FormConfig struct {
	Title  string
	Fields []FormField
	Submit func(ctx context.Context, values map[string]string) (errMsg string, fatal error)
	// StayOnSave keeps the form on screen after a successful Submit (save in
	// place) instead of returning to the caller; the user leaves via PF3
	// (Cancel). Default false: a successful Submit returns, as add/edit forms do.
	StayOnSave bool
	// DotLeader renders the form's labels with right-aligned colons and dot
	// leaders (ISPF-style) so colons line up across rows. Default off.
	DotLeader bool
	// Compact renders the form in the sectioned, single-spaced layout: Section
	// banners group fields, Suffix adds trailing hint text, SameRow pairs a field
	// onto the previous row, and the message line moves to the bottom. Default
	// off (the flat double-spaced layout).
	Compact bool
}

// SnapshotRow is one row of a snapshot list, laid as three fields so the middle
// segment (Mid) can carry its own colour. Left/Mid/Right are pre-formatted and
// pre-padded by the caller; the builder places them at fixed columns.
type SnapshotRow struct {
	Left, Mid, Right string
	MidColor         go3270.Color // go3270.DefaultColor ⇒ no explicit colour
}

// SnapshotView is what to paint for a snapshot (read-only, paged) list. AsOf is
// a caller-formatted stamp shown on row 2; Head is the column-heading row;
// Empty is shown in place of rows when there are none.
type SnapshotView struct {
	Title, RowInfo, AsOf, Legend, ErrMsg, PFHelp, Empty string
	Head                                                SnapshotRow
	Rows                                                []SnapshotRow
	// Wide renders each row (and the heading) as one full-width field spanning
	// cols 8–79 instead of the three colour-segmented fields. Used by screens
	// with many plain columns and no per-segment colour (GH #91 active sessions).
	Wide bool
}

// DetailField is one label/value line on the detail screen. Color tints the
// value (used for the event field); go3270.DefaultColor leaves it plain.
type DetailField struct {
	Label, Value string
	Color        go3270.Color
}

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
