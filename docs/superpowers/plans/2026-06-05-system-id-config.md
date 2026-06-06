# Operator-configurable menu System ID — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hardcoded `systemIDPlaceholder = "PROXY"` in the menu status block with an operator-configurable `SYSTEM_ID` system parameter, edited via the admin System Parameters form.

**Architecture:** Add `SYSTEM_ID` to `sysconfig.Catalog` (which seeds the default and auto-builds the admin edit field). Extend the shared `sysconfig.Entry` struct with two backward-compatible fields — `Length` (per-entry admin input width) and `Normalize` (canonicalize-before-validate-and-store) — needed by this parameter's 7-char cap and uppercase/charset requirements. Read the value at menu-paint time via `GetConfig`, with a `"PROXY"` fallback.

**Tech Stack:** Go, `modernc.org/sqlite`, go3270, table-driven tests (`go test ./... -race`).

**Spec:** `docs/superpowers/specs/2026-06-05-system-id-config-design.md`

---

## File Structure

- **Modify** `internal/sysconfig/catalog.go` — extend `Entry` with `Length int` and `Normalize func(string) string`; add `KeySystemID` const, the `SYSTEM_ID` catalog entry, and `validateSystemID`.
- **Modify** `internal/sysconfig/catalog_test.go` — add `SYSTEM_ID` entry + validator/normalize tests.
- **Modify** `internal/server/admin_system.go` — use `e.Length` (fallback 64) for the form field width; apply `e.Normalize` before validate/compare/store in `Submit`.
- **Modify** `internal/server/admin_system_test.go` — add `"SYSTEM_ID"` to the three existing submit `Values` maps (keep suite green); add field-width, normalize, and reject tests.
- **Modify** `internal/server/session.go` — delete `systemIDPlaceholder`; add a `systemID(ctx)` helper; populate `status.SystemID` from it.
- **Modify** `internal/server/session_test.go` — keep the default `"PROXY"` assertion; add a configured-value case and an empty-value fallback case.
- **No change needed** `internal/store/config_test.go` — `TestMigrateSeedsCatalogDefaults` already iterates the whole catalog, so it auto-covers `SYSTEM_ID` seeding once the entry exists (verified in Task 4).

---

## Task 1: Catalog — `Entry` fields, `SYSTEM_ID` entry, validator

**Files:**
- Modify: `internal/sysconfig/catalog.go`
- Test: `internal/sysconfig/catalog_test.go`
- Modify (keep-green companion): `internal/server/admin_system_test.go:114,158,181`

- [ ] **Step 1: Write the failing test**

Add to `internal/sysconfig/catalog_test.go` (the `entryByKey` helper already exists in this file):

```go
func TestSystemIDEntry(t *testing.T) {
	e := entryByKey(t, KeySystemID)
	if KeySystemID != "SYSTEM_ID" {
		t.Errorf("KeySystemID = %q, want SYSTEM_ID", KeySystemID)
	}
	if e.Default != "PROXY" {
		t.Errorf("SYSTEM_ID default = %q, want PROXY", e.Default)
	}
	if e.Label != "System ID:" {
		t.Errorf("SYSTEM_ID label = %q, want %q", e.Label, "System ID:")
	}
	if e.Length != 7 {
		t.Errorf("SYSTEM_ID length = %d, want 7", e.Length)
	}
	if e.Normalize == nil {
		t.Fatal("SYSTEM_ID Normalize is nil")
	}
	if e.Validate == nil {
		t.Fatal("SYSTEM_ID Validate is nil")
	}
}

func TestSystemIDNormalize(t *testing.T) {
	n := entryByKey(t, KeySystemID).Normalize
	for in, want := range map[string]string{
		" proxy ": "PROXY",
		"sysa":    "SYSA",
		"SYS-01":  "SYS-01",
		"  a1":    "A1",
	} {
		if got := n(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSystemIDValidate(t *testing.T) {
	v := entryByKey(t, KeySystemID).Validate
	// Valid values (already canonical).
	for _, ok := range []string{"PROXY", "SYSA", "A1", "SYS-01", "A-B-C-D", "ABCDEFG"} {
		if msg := v(ok); msg != "" {
			t.Errorf("Validate(%q) = %q, want valid", ok, msg)
		}
	}
	// Invalid values mapped to their exact expected message.
	for in, want := range map[string]string{
		"":         "SYSTEM ID REQUIRED",
		"ABCDEFGH": "SYSTEM ID TOO LONG (MAX 7)",
		"-SYS":     "SYSTEM ID MUST NOT START WITH A DASH",
		"SYS_01":   "SYSTEM ID: USE A-Z 0-9 AND DASH",
		"SYS.1":    "SYSTEM ID: USE A-Z 0-9 AND DASH",
		"SYS@":     "SYSTEM ID: USE A-Z 0-9 AND DASH",
	} {
		if msg := v(in); msg != want {
			t.Errorf("Validate(%q) = %q, want %q", in, msg, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sysconfig/ -run 'TestSystemID' -v`
Expected: FAIL — compile error `undefined: KeySystemID` and `e.Length`/`e.Normalize` undefined.

- [ ] **Step 3: Add the `Entry` fields**

In `internal/sysconfig/catalog.go`, replace the `Entry` struct:

```go
// Entry describes one system parameter.
type Entry struct {
	Key       string              // canonical uppercase; used as the DB key and form field name
	Label     string              // display label shown on the System Parameters form
	Default   string              // initial value seeded into system_config by migrate()
	Length    int                 // admin form input width; 0 ⇒ default (64)
	Normalize func(string) string // canonicalize before validate+store; nil ⇒ identity
	Validate  func(string) string // returns an errMsg (uppercase) or "" if valid
}
```

- [ ] **Step 4: Add the const, entry, and validator**

In `internal/sysconfig/catalog.go`, add the const after `KeyMFAIssuer` (around line 44):

```go
// KeySystemID is the system_config key holding the operator-set System ID shown
// in the menu status block (#53/#64). Canonical form: trimmed, upper-cased, and
// restricted to A-Z/0-9/'-' with a 7-rune budget (the status-block value column).
const KeySystemID = "SYSTEM_ID"
```

In the `Catalog` var, add this entry immediately after the `KeyMFAIssuer` entry (so display-oriented params group together):

```go
	{
		Key:       KeySystemID,
		Label:     "System ID:",
		Default:   "PROXY",
		Length:    7,
		Normalize: func(v string) string { return strings.ToUpper(strings.TrimSpace(v)) },
		Validate:  validateSystemID,
	},
```

Add this function alongside the other validators (after `positiveInt`):

```go
// validateSystemID enforces the 7-rune status-block budget and a mainframe-ish
// charset. It re-normalizes defensively so it is correct regardless of whether
// the caller already applied Normalize.
func validateSystemID(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" {
		return "SYSTEM ID REQUIRED"
	}
	if len([]rune(v)) > 7 {
		return "SYSTEM ID TOO LONG (MAX 7)"
	}
	if v[0] == '-' {
		return "SYSTEM ID MUST NOT START WITH A DASH"
	}
	for _, r := range v {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
			return "SYSTEM ID: USE A-Z 0-9 AND DASH"
		}
	}
	return ""
}
```

(`strings` is already imported in this file.)

- [ ] **Step 5: Run sysconfig tests to verify they pass**

Run: `go test ./internal/sysconfig/ -v`
Expected: PASS (including `TestCatalogKeysAreUppercase`, `TestCatalogLabelsNonEmpty`, which auto-cover the new entry).

- [ ] **Step 6: Keep the server suite green — update existing submit tests**

Adding a *required* `SYSTEM_ID` makes the System Parameters `Submit` validate it on every save. The three existing submit tests in `internal/server/admin_system_test.go` omit it, which would now fail validation. Add `"SYSTEM_ID": "PROXY"` to each of these three `Values` maps:

At `internal/server/admin_system_test.go:114` change to:
```go
			{Values: map[string]string{"MOTD_FILE": "/etc/motd.txt", "MFA_ISSUER": "TN3270PROXY", "SYSTEM_ID": "PROXY", "AUTH_DELAY_BASE_SECS": "2", "AUTH_MAX_TRIES": "5", "AUTH_FAIL_WINDOW_MINS": "15"}}, // Enter: save, stay
```

At `internal/server/admin_system_test.go:158` change to:
```go
			{Values: map[string]string{"MOTD_FILE": "", "MFA_ISSUER": "TN3270PROXY", "SYSTEM_ID": "PROXY", "AUTH_DELAY_BASE_SECS": "2", "AUTH_MAX_TRIES": "5", "AUTH_FAIL_WINDOW_MINS": "15"}}, // Enter: no change, stay
```

At `internal/server/admin_system_test.go:181` change to:
```go
			{Values: map[string]string{"MOTD_FILE": "", "MFA_ISSUER": "TN3270PROXY", "SYSTEM_ID": "PROXY", "AUTH_DELAY_BASE_SECS": "2", "AUTH_MAX_TRIES": "5", "AUTH_FAIL_WINDOW_MINS": "15"}}, // Enter: clear (disable), stay
```

- [ ] **Step 7: Run the full suite to verify green**

Run: `go test ./... `
Expected: PASS. (`session.go` still uses the `systemIDPlaceholder` constant = `"PROXY"`, so the menu test is unaffected this task.)

- [ ] **Step 8: Commit**

```bash
git add internal/sysconfig/catalog.go internal/sysconfig/catalog_test.go internal/server/admin_system_test.go
git commit -m "feat(sysconfig): add SYSTEM_ID catalog entry with Length/Normalize — GH #64"
```

---

## Task 2: Admin form — per-entry `Length` and `Normalize` on save

**Files:**
- Modify: `internal/server/admin_system.go`
- Test: `internal/server/admin_system_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/server/admin_system_test.go`:

```go
// TestAdminSystemParamsFieldLength verifies the form uses each catalog entry's
// Length (SYSTEM_ID caps at 7; entries with Length 0 default to 64).
func TestAdminSystemParamsFieldLength(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 4}, {back: true}},
		forms: []ui3270.FormAction{{Cancel: true}},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)

	if len(p.gotForms) == 0 {
		t.Fatal("no form rendered")
	}
	for _, fld := range p.gotForms[0].Fields {
		switch fld.Name {
		case "SYSTEM_ID":
			if fld.Length != 7 {
				t.Errorf("SYSTEM_ID field length = %d, want 7", fld.Length)
			}
		default:
			if fld.Length != 64 {
				t.Errorf("%s field length = %d, want 64", fld.Name, fld.Length)
			}
		}
	}
}

// TestAdminSystemParamsNormalizesSystemID verifies a lowercase/whitespace value
// is canonicalized (trim + upper) before being stored and audited.
func TestAdminSystemParamsNormalizesSystemID(t *testing.T) {
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 4}, {back: true}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{"MOTD_FILE": "", "MFA_ISSUER": "TN3270PROXY", "SYSTEM_ID": " sysa ", "AUTH_DELAY_BASE_SECS": "2", "AUTH_MAX_TRIES": "5", "AUTH_FAIL_WINDOW_MINS": "15"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	var audited []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { audited = append(audited, ev) }

	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if val, _ := f.store.GetConfig(ctx, "SYSTEM_ID"); val != "SYSA" {
		t.Errorf("SYSTEM_ID = %q, want SYSA (normalized)", val)
	}
	if len(audited) != 1 || audited[0].Detail != "sysconfig set SYSTEM_ID: PROXY -> SYSA" {
		t.Errorf("audit = %+v, want one 'sysconfig set SYSTEM_ID: PROXY -> SYSA'", audited)
	}
}

// TestAdminSystemParamsRejectsInvalidSystemID verifies an invalid value blocks
// the save (errMsg shown, nothing written).
func TestAdminSystemParamsRejectsInvalidSystemID(t *testing.T) {
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 4}, {back: true}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{"MOTD_FILE": "", "MFA_ISSUER": "TN3270PROXY", "SYSTEM_ID": "-X", "AUTH_DELAY_BASE_SECS": "2", "AUTH_MAX_TRIES": "5", "AUTH_FAIL_WINDOW_MINS": "15"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// Save rejected: the form re-renders with the errMsg, then PF3 leaves.
	if len(p.gotForms) < 2 || p.gotForms[1].ErrMsg != "SYSTEM ID MUST NOT START WITH A DASH" {
		t.Errorf("expected errMsg on re-render, got forms = %+v", p.gotForms)
	}
	// Nothing persisted: SYSTEM_ID keeps its seeded default.
	if val, _ := f.store.GetConfig(ctx, "SYSTEM_ID"); val != "PROXY" {
		t.Errorf("SYSTEM_ID = %q, want PROXY (unchanged after rejected save)", val)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestAdminSystemParams(FieldLength|NormalizesSystemID|RejectsInvalidSystemID)' -v`
Expected: FAIL — `FieldLength` sees `64` for SYSTEM_ID (hardcoded), and `NormalizesSystemID` stores `" sysa "`/audit mismatch (no normalization yet).

- [ ] **Step 3: Implement Length + Normalize in `systemParams`**

In `internal/server/admin_system.go`, replace the field-build loop (currently `Length: 64`):

```go
	// Build form fields from the catalog, pre-populated with current store values.
	fields := make([]ui3270.FormField, len(sysconfig.Catalog))
	for i, e := range sysconfig.Catalog {
		val, err := f.store.GetConfig(ctx, e.Key)
		if err != nil {
			val = e.Default // fall back to catalog default on unexpected error
		}
		length := e.Length
		if length == 0 {
			length = 64 // default width for entries that don't set one
		}
		fields[i] = ui3270.FormField{
			Name:   e.Key,
			Label:  e.Label,
			Value:  val,
			Length: length,
		}
	}
```

Replace the `Submit` body with a normalize-first version (single normalized map drives both validation and persistence):

```go
		Submit: func(ctx context.Context, vals map[string]string) (string, error) {
			// Normalize + validate all fields before touching the store (all-or-nothing).
			norm := make(map[string]string, len(sysconfig.Catalog))
			for _, e := range sysconfig.Catalog {
				v := vals[e.Key]
				if e.Normalize != nil {
					v = e.Normalize(v)
				}
				norm[e.Key] = v
				if msg := e.Validate(v); msg != "" {
					return msg, nil
				}
			}
			// Persist only changed values; audit each effective change.
			for i, e := range sysconfig.Catalog {
				newVal := norm[e.Key]
				oldVal := fields[i].Value
				if newVal == oldVal {
					continue
				}
				if err := f.store.SetConfig(ctx, e.Key, newVal); err != nil {
					return f.storeErr("set config "+e.Key, err), nil
				}
				f.recordAdmin(ctx, "sysconfig set "+e.Key+": "+oldVal+" -> "+newVal)
				fields[i].Value = newVal // keep in-slice value current for next render
			}
			return "", nil
		},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestAdminSystemParams' -v`
Expected: PASS (new tests plus the previously-updated happy-path/no-audit/empty-valid tests).

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin_system.go internal/server/admin_system_test.go
git commit -m "feat(server): per-entry form width + normalize-on-save for sysparams — GH #64"
```

---

## Task 3: Menu wiring — read `SYSTEM_ID`, drop the placeholder

**Files:**
- Modify: `internal/server/session.go:46-49,331` (delete const, set status from helper; add helper near `mfaIssuer`)
- Test: `internal/server/session_test.go:848`

- [ ] **Step 1: Write the failing tests**

In `internal/server/session_test.go`, keep `TestSessionPopulatesMenuStatus` (it asserts the default `"PROXY"`, now sourced from the seeded config). Add two new tests after it:

```go
func TestSessionMenuStatusUsesConfiguredSystemID(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true},
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	if err := s.Store.SetConfig(context.Background(), "SYSTEM_ID", "SYSA"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.gotStatus) == 0 {
		t.Fatal("Menu was never called")
	}
	if got := p.gotStatus[0].SystemID; got != "SYSA" {
		t.Errorf("status.SystemID = %q, want SYSA", got)
	}
}

func TestSessionMenuStatusSystemIDFallback(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true},
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	// An empty stored value must fall back to PROXY (menu never renders blank).
	if err := s.Store.SetConfig(context.Background(), "SYSTEM_ID", ""); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.gotStatus) == 0 {
		t.Fatal("Menu was never called")
	}
	if got := p.gotStatus[0].SystemID; got != "PROXY" {
		t.Errorf("status.SystemID = %q, want PROXY (fallback)", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestSessionMenuStatus' -v`
Expected: FAIL — `UsesConfiguredSystemID` gets `"PROXY"` (still the hardcoded placeholder), so the SYSA assertion fails.

- [ ] **Step 3: Delete the placeholder constant**

In `internal/server/session.go`, remove these lines (46–49):

```go
// systemIDPlaceholder is shown in the menu status block's "System ID" row.
// TODO(#53): replace with a DB-backed system-config value entered via the
// future System Configuration admin screen; hardcoded for now.
const systemIDPlaceholder = "PROXY"
```

- [ ] **Step 4: Add the `systemID` helper**

In `internal/server/session.go`, add directly after `mfaIssuer` (around line 162):

```go
// systemID reads the configured System ID for the menu status block, falling
// back to "PROXY" on any read error or empty value so the menu never renders a
// blank System ID row.
func (s *Session) systemID(ctx context.Context) string {
	v, err := s.Store.GetConfig(ctx, sysconfig.KeySystemID)
	if err != nil || strings.TrimSpace(v) == "" {
		return "PROXY"
	}
	return v
}
```

(`sysconfig` and `strings` are already imported in this file.)

- [ ] **Step 5: Populate status from the helper**

In `internal/server/session.go`, in the menu loop, change the `status` build (line ~331):

```go
			status := screens.MenuStatus{
				Username: identity.Username,
				SystemID: s.systemID(ctx),
				Release:  s.Release,
			}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestSession' -v`
Expected: PASS (default, configured, and fallback cases).

- [ ] **Step 7: Commit**

```bash
git add internal/server/session.go internal/server/session_test.go
git commit -m "feat(server): source menu System ID from SYSTEM_ID config — GH #64"
```

---

## Task 4: Full verification + protocol smoke

**Files:** none (verification only).

- [ ] **Step 1: Build**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 2: Full test suite with race detector**

Run: `go test ./... -race`
Expected: PASS for all packages. This also exercises `internal/store`'s `TestMigrateSeedsCatalogDefaults`, which now asserts `GetConfig("SYSTEM_ID") == "PROXY"` automatically (catalog-driven).

- [ ] **Step 3: Protocol smoke test**

Invoke the `s3270-smoke-testing` skill and run `.claude/skills/s3270-smoke-testing/smoke.sh`. Confirm:
- the menu status block shows `System ID: PROXY` by default;
- after an admin edits System Parameters (e.g. to `SYSA`) and returns to the menu, the block shows the new value;
- cursor-position assertions still pass.

Expected: smoke script passes; status block reflects the configured value.

- [ ] **Step 4: Final confirmation**

Confirm the acceptance criteria in the spec are all met (catalog entry + seed, placeholder removed, admin edit round-trips, validation messages, unit tests, emulator verification). No commit needed unless the smoke run required a fix.

---

## Self-Review Notes

- **Spec coverage:** catalog entry+seed (Task 1), placeholder removal + menu read + fallback (Task 3), admin Length cap + normalize/validate round-trip (Task 2), store seed round-trip (Task 4 via existing test), unit tests (Tasks 1–3), emulator smoke (Task 4). All spec sections mapped.
- **Type consistency:** `Entry.Length`/`Entry.Normalize` defined in Task 1 are consumed identically in Task 2; `KeySystemID` defined in Task 1 is used in Tasks 2–3; `systemID(ctx)` defined once in Task 3.
- **Green at every commit:** the keep-green companion edits (Task 1 Step 6) ensure the required new field never red-fails the pre-existing submit tests.
