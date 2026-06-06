# Design: Fix System Parameters form label/input overlap (GH #71)

**Date:** 2026-06-05
**Status:** Approved (pending user spec review)
**Area:** `internal/ui3270` (form renderer)

## Problem

The admin **System Parameters** form cannot save *any* value. The s3270 smoke
suite tests 12a/12b/12c (MOTD save + persist) and 13a/13b (MOTD display, which
cascades from 12) fail on a clean `main` checkout — a pre-existing,
release-blocking functional bug, not caused by the GH #64 SYSTEM_ID work.

### Root cause

`buildFormScreen` in `internal/ui3270/screen.go` hardcodes, for every form row:

- the **label** protected field attribute at `Col: 2` (content begins col 3), and
- the **input** writable field attribute at `Col: 16` (content begins col 17).

That leaves columns 3–15 — **13 characters** — for label content before the
input field's attribute byte. Every other admin form (users / groups / services /
networks) deliberately pads its labels to a 12-char dot-leader
(`"Group name ."`, `"Userid . . ."`), fitting cols 3–14 with a one-column
gutter. The System Parameters form is the only one that feeds *raw* catalog
labels straight through, and three of them exceed 13 chars:

| Catalog label | Length |
|---|---|
| `Auth Delay Base (sec):` | 22 |
| `Auth Fail Window (min):` | 23 |
| `Max Auth Tries:` | 15 |

A label longer than 13 chars is written into the buffer starting at col 3 and
runs *past* the input field's attribute byte at col 16. Worked example
(`"Auth Delay Base (sec):"`, value `"2"`):

```
col: 3              16 17
     Auth Delay Ba  •  2 (sec):        ← • = input attribute byte
     └─ label text ─┘  └─ input field's buffer ─┘
```

The label's tail (`(sec):`) lands inside the input field's data region
(col 17 → stop field). On submit, that field's value reads back as
`"2 (sec):"`, which `nonNegativeInt` rejects with
`MUST BE A NON-NEGATIVE INTEGER`. Because `systemParams`' `Submit` validates
**every** catalog entry before writing **any** (all-or-nothing), this single
corrupted field blocks saving every field — MOTD_FILE, MFA_ISSUER, and the rest
included. This matches the reported `Ascii()` dump (`Auth Delay Ba 2 (sec):`
plus the error line) byte-for-byte.

The short-label fields (`MOTD File:` 10, `MFA Issuer:` 11) are uncorrupted and
would save on their own, but the all-or-nothing rule sinks the whole save.

## Goals

- The System Parameters form saves correctly; labels never collide with the
  input column.
- The renderer becomes **self-protecting**: a long label can no longer corrupt
  an input field's value. This failure class becomes structurally impossible,
  not merely avoided by hand-padded callers.
- No regression to the existing forms or screens. Cursor positioning stays
  correct (`field.Col + 1`, the documented go3270 gotcha). All forms stay within
  columns 0–79 on 24×80 and on larger MOD 3/4/5 geometries.

## Non-goals

- No change to the catalog labels themselves (they stay descriptive).
- No change to the all-or-nothing `Submit` semantics.
- No broader 3270-screen polish (that is the separate GH #65 work).

## Design

### Dynamic input column, computed per form

`buildFormScreen` computes the input column from the longest label in the form
rather than hardcoding col 16:

```
labelCol  = 2                       // label attribute byte; content at col 3
gutter    = 1                       // ≥1 blank column between label end and input attr
inputCol  = max(16, 3 + maxLabelLen + gutter)   // input attribute byte
```

where `maxLabelLen` is the longest `len(f.Label)` over the form's fields. The
floor at 16 means any form whose labels are ≤12 chars (i.e. every existing
form) computes `inputCol == 16` and renders **byte-identical to today** — so no
existing form, screen, or cursor position changes. Only the System Parameters
form (max label 23) widens: `inputCol = 3 + 23 + 1 = 27`.

Derived positions (replacing today's hardcoded 16 / 17 / `17+Length`):

- input field attribute at `inputCol`; input content begins `inputCol + 1`
- stop field at `inputCol + 1 + f.Length`, clamped to 79
- initial cursor at `{row, inputCol + 1}`
- ReadOnly value rendered as static content at `inputCol + 1`

At `inputCol = 27` the System Parameters inputs run cols 28–78 ≈ **51 columns**,
ample for the small integers and short paths these parameters hold.

### Defensive truncation guard

To make the corruption structurally impossible (and bound pathological labels),
clamp `inputCol` to a ceiling that preserves a minimum input width, and truncate
label *content* so it can never reach the input attribute byte:

```
const minInputWidth = 16
inputColCeil = 79 - minInputWidth        // 63: keep ≥16 input columns
if inputCol > inputColCeil { inputCol = inputColCeil }
labelMax = inputCol - gutter - labelCol - 1   // last safe label content length
// render label content truncated to labelMax
```

In the normal case `labelMax == maxLabelLen`, so truncation is a no-op — at the
ceiling `labelMax` is 59, so it only engages for a pathological (>59-char)
label, which no current catalog produces.
This is belt-and-suspenders: the geometry already prevents overlap, and the
truncation guarantees it even if the math is later changed.

### Touched code

- `internal/ui3270/screen.go` — `buildFormScreen` only. The list renderer
  (`buildListScreen`) is unaffected (line-command lists, not labeled inputs).
- No interface, type, or caller changes. `FormField`/`FormView`/`FormConfig`
  are untouched. `systemParams` and the other admin form builders are untouched.

## Testing

TDD — write the failing tests first, then the renderer change.

### Unit tests (`internal/ui3270/screen_test.go`)

- **Long-label form pushes the input column right (the bug).** A form whose
  longest label is 23 chars (`"Auth Fail Window (min):"`) places its input
  attribute at col 27 and content at col 28; assert the label content does not
  overlap the input attribute column, for a representative set of label lengths
  (10, 12, 13, 15, 22, 23).
- **Short-label forms are unchanged.** A form with ≤12-char labels keeps the
  input attribute at col 16 / content at col 17 (regression guard for every
  existing form). Existing `TestBuildFormScreenCursor` (`{3,17}`) and
  `TestBuildFormScreenReadOnlyField` (`{5,17}`) must still pass unchanged.
- **Cursor tracks the dynamic column.** First input's cursor is
  `{row, inputCol + 1}` for a long-label form.
- **ReadOnly value uses the dynamic column.** A read-only field's static value
  is placed at `inputCol + 1`, not a hardcoded 16.
- **Stop field rebases on the dynamic column.** Stop field at
  `inputCol + 1 + Length`, clamped to 79.
- **Truncation guard.** A pathological label longer than the ceiling allows is
  truncated so its content cannot reach the input attribute column.

### Integration / protocol

- `go test ./... -race` green.
- The s3270 smoke suite (`.claude/skills/s3270-smoke-testing/smoke.sh`) reaches
  **47/47**, with tests 12a–12c and 13a–13b passing.

## GitHub issue

File a dedicated issue for this concrete save-blocking functional bug (none
exists; GH #65 is broader UI polish). Reference the smoke tests it fixes.
