# dummy3270 Bridge Target Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `dummy3270`, a tiny standalone TN3270 server that paints a random, obviously-fake mockup welcome screen on each connection, for use as a demo/test bridge target.

**Architecture:** Approach A — a thin `cmd/dummy3270` wrapper over an `internal/dummy` package. `screens.go` holds pure `go3270.Screen` builders (3 mockups + a blinking red `DUMMY3270` marker + footer); `server.go` holds the Telnet-negotiating accept/repaint loop. No DB, no auth, no TLS, no config, no per-connection logging, no session state.

**Tech Stack:** Go 1.25, `github.com/racingmars/go3270` v0.9.13 (already a dependency). Module path `github.com/coffeemuse/tn3270proxy`.

---

## Conventions for every new `.go` file

Every new file MUST begin with the repo's standard GPL header (identical to the one atop `internal/screens/login.go` and `cmd/tn3270proxy/main.go`):

```go
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
```

In the code blocks below the header is omitted for brevity — **prepend it to every new file.**

## File structure

```
internal/dummy/screens.go        marker/footer/title helpers, screenA/B/C builders, screenFor(i)
internal/dummy/screens_test.go   asserts marker (red+blink), footer, no input fields, screenFor wrap
internal/dummy/server.go         Server, Listen/Serve/Close, ListenAndServe, serveConn, randomScreen
internal/dummy/server_test.go    bad-addr error + accept-loop smoke (no 3270 client needed)
cmd/dummy3270/main.go            -listen flag, one startup line, calls dummy.ListenAndServe
```

`go3270.Screen` is `[]go3270.Field`, so screens are iterable/indexable in tests. A `Field`'s `Col` is the **attribute byte**; displayed content begins at `Col+1` (this is why the 9-char marker uses `Col: 70` to land in columns 71–79).

---

### Task 1: Screen builders (`internal/dummy/screens.go`)

**Files:**
- Create: `internal/dummy/screens.go`
- Test: `internal/dummy/screens_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/dummy/screens_test.go` (after the GPL header):

```go
package dummy

import (
	"testing"

	"github.com/racingmars/go3270"
)

// hasContent reports whether any field on the screen renders exactly text.
func hasContent(s go3270.Screen, text string) bool {
	for _, f := range s {
		if f.Content == text {
			return true
		}
	}
	return false
}

// markerField returns the DUMMY3270 marker field, if present.
func markerField(s go3270.Screen) (go3270.Field, bool) {
	for _, f := range s {
		if f.Content == markerText {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestEachScreenHasBlinkingRedMarker(t *testing.T) {
	for i, build := range builders {
		f, ok := markerField(build())
		if !ok {
			t.Fatalf("screen %d: no %q marker field", i, markerText)
		}
		if f.Color != go3270.Red {
			t.Errorf("screen %d: marker color = %v, want Red", i, f.Color)
		}
		if f.Highlighting != go3270.Blink {
			t.Errorf("screen %d: marker highlight = %v, want Blink", i, f.Highlighting)
		}
	}
}

func TestEachScreenHasPA3Footer(t *testing.T) {
	for i, build := range builders {
		if !hasContent(build(), footerText) {
			t.Errorf("screen %d: missing footer %q", i, footerText)
		}
	}
}

func TestScreensHaveNoInputFields(t *testing.T) {
	for i, build := range builders {
		for _, f := range build() {
			if f.Write {
				t.Errorf("screen %d: unexpected writable field at (%d,%d)", i, f.Row, f.Col)
			}
		}
	}
}

func TestScreenForWrapsAndCoversAll(t *testing.T) {
	titleOf := func(s go3270.Screen) string { return s[0].Content } // title is field 0
	a, b, c := titleOf(screenFor(0)), titleOf(screenFor(1)), titleOf(screenFor(2))
	if a == b || b == c || a == c {
		t.Errorf("screens not distinct: %q %q %q", a, b, c)
	}
	if titleOf(screenFor(3)) != a {
		t.Errorf("screenFor(3) should wrap to screenFor(0) (%q), got %q", a, titleOf(screenFor(3)))
	}
	if titleOf(screenFor(-1)) != c {
		t.Errorf("screenFor(-1) should wrap to screenFor(2) (%q), got %q", c, titleOf(screenFor(-1)))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dummy/ -run TestEachScreen -v`
Expected: FAIL to compile — `undefined: builders`, `markerText`, `footerText`, `screenFor`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/dummy/screens.go` (after the GPL header):

```go
// Package dummy is a throwaway TN3270 server used as a bridge target for demos
// and tests. It authenticates nothing, stores nothing, and logs nothing; on
// each connection it paints one of a few obviously-fake mockup welcome screens.
package dummy

import "github.com/racingmars/go3270"

const (
	markerText = "DUMMY3270"
	footerText = "Press PA3 to disconnect."
)

// builders is the set of welcome screens; one is chosen at random per paint.
var builders = []func() go3270.Screen{screenA, screenB, screenC}

// screenFor returns the i-th welcome screen, wrapping i into range so any int
// (e.g. a rand result) is safe to pass.
func screenFor(i int) go3270.Screen {
	n := len(builders)
	return builders[((i%n)+n)%n]()
}

// titleField centers text on row 0 in intense white. (Exact 1-column attribute
// offset is not significant for this fixture.)
func titleField(text string) go3270.Field {
	return go3270.Field{Row: 0, Col: (80 - len(text)) / 2, Content: text, Color: go3270.White, Intense: true}
}

// footerField is the bottom-anchored PA3 hint shared by every screen.
func footerField() go3270.Field {
	return go3270.Field{Row: 22, Col: 2, Content: footerText, Color: go3270.Turquoise}
}

// markerFields returns the blinking red DUMMY3270 marker (row 0, columns 71-79)
// plus a stop field at (1,0) so the red/blink attribute does not bleed into the
// body. A Field's Col is the attribute byte; content begins at Col+1, so Col=70
// places the 9-char marker in columns 71-79.
func markerFields() go3270.Screen {
	return go3270.Screen{
		{Row: 0, Col: 70, Content: markerText, Color: go3270.Red, Highlighting: go3270.Blink},
		{Row: 1, Col: 0}, // stop field: resets attributes after the marker
	}
}

// compose assembles a full screen: title first (field 0), then body, then the
// shared footer and the blinking marker.
func compose(title go3270.Field, body go3270.Screen) go3270.Screen {
	s := go3270.Screen{title}
	s = append(s, body...)
	s = append(s, footerField())
	s = append(s, markerFields()...)
	return s
}

func screenA() go3270.Screen {
	body := go3270.Screen{
		{Row: 4, Col: 10, Content: "WELCOME TO CICSDEMO", Color: go3270.Green, Intense: true},
		{Row: 6, Col: 10, Content: "CICS/TS region CICSDEMO is now available.", Color: go3270.Turquoise},
		{Row: 8, Col: 10, Content: "This is a non-working demonstration host.", Color: go3270.Turquoise},
	}
	return compose(titleField("CICSDEMO"), body)
}

func screenB() go3270.Screen {
	body := go3270.Screen{
		{Row: 4, Col: 10, Content: "0  SETTINGS    Terminal and user parameters", Color: go3270.Turquoise},
		{Row: 5, Col: 10, Content: "1  VIEW        Display source data or listings", Color: go3270.Turquoise},
		{Row: 6, Col: 10, Content: "2  EDIT        Create or change source data", Color: go3270.Turquoise},
		{Row: 7, Col: 10, Content: "3  UTILITIES   Perform utility functions", Color: go3270.Turquoise},
		{Row: 9, Col: 10, Content: "These options are inert - this is a demo host.", Color: go3270.Green},
	}
	return compose(titleField("ISPF PRIMARY OPTION MENU"), body)
}

func screenC() go3270.Screen {
	body := go3270.Screen{
		{Row: 5, Col: 10, Content: "*** SYSTEM AVAILABLE ***", Color: go3270.Green, Intense: true},
		{Row: 7, Col: 10, Content: "LPAR: DEMO1     SYSID: DUM1", Color: go3270.Turquoise},
		{Row: 9, Col: 10, Content: "No application is running here - demonstration only.", Color: go3270.Turquoise},
	}
	return compose(titleField("SYSTEM STATUS"), body)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dummy/ -v`
Expected: PASS — `TestEachScreenHasBlinkingRedMarker`, `TestEachScreenHasPA3Footer`, `TestScreensHaveNoInputFields`, `TestScreenForWrapsAndCoversAll`.

- [ ] **Step 5: Commit**

```bash
git add internal/dummy/screens.go internal/dummy/screens_test.go
git commit -m "feat(dummy): random mockup welcome screens with blinking DUMMY3270 marker"
```

---

### Task 2: Server / accept loop (`internal/dummy/server.go`)

**Files:**
- Create: `internal/dummy/server.go`
- Test: `internal/dummy/server_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/dummy/server_test.go` (after the GPL header):

```go
package dummy

import (
	"net"
	"testing"
	"time"
)

func TestListenRejectsBadAddr(t *testing.T) {
	if _, err := Listen("not-an-addr"); err == nil {
		t.Fatal("expected error binding a bad address")
	}
}

func TestServeAcceptsConnections(t *testing.T) {
	s, err := Listen("127.0.0.1:0") // port 0 -> OS picks a free port
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer s.Close()
	go s.Serve()

	conn, err := net.DialTimeout("tcp", s.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("dial accepted listener: %v", err)
	}
	conn.Close()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dummy/ -run TestServe -v`
Expected: FAIL to compile — `undefined: Listen`, `(*Server).Serve`, `Addr`, `Close`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/dummy/server.go` (after the GPL header):

```go
package dummy

import (
	"math/rand"
	"net"

	"github.com/racingmars/go3270"
)

// allExitAIDs lists every attention key so HandleScreen returns to us on ANY
// keypress; we then repaint a freshly-random screen. There are no input fields,
// so nothing is a validated "submit" and pfkeys is nil.
var allExitAIDs = []go3270.AID{
	go3270.AIDEnter, go3270.AIDClear,
	go3270.AIDPA1, go3270.AIDPA2, go3270.AIDPA3,
	go3270.AIDPF1, go3270.AIDPF2, go3270.AIDPF3, go3270.AIDPF4,
	go3270.AIDPF5, go3270.AIDPF6, go3270.AIDPF7, go3270.AIDPF8,
	go3270.AIDPF9, go3270.AIDPF10, go3270.AIDPF11, go3270.AIDPF12,
	go3270.AIDPF13, go3270.AIDPF14, go3270.AIDPF15, go3270.AIDPF16,
	go3270.AIDPF17, go3270.AIDPF18, go3270.AIDPF19, go3270.AIDPF20,
	go3270.AIDPF21, go3270.AIDPF22, go3270.AIDPF23, go3270.AIDPF24,
}

// Server is a minimal TN3270 server that paints a random mockup welcome screen
// on each connection. It keeps no session state.
type Server struct {
	ln net.Listener
}

// Listen binds a plaintext TCP listener on addr (e.g. ":3300").
func Listen(addr string) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Server{ln: ln}, nil
}

// Addr reports the bound address (useful when addr requested port 0).
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Close stops the listener.
func (s *Server) Close() error { return s.ln.Close() }

// Serve accepts connections until the listener is closed, handling each in its
// own goroutine. It returns the Accept error that stopped it.
func (s *Server) Serve() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return err
		}
		go serveConn(conn)
	}
}

// ListenAndServe binds addr and serves until the listener errors.
func ListenAndServe(addr string) error {
	s, err := Listen(addr)
	if err != nil {
		return err
	}
	return s.Serve()
}

// serveConn negotiates Telnet then repaints a random welcome screen on every
// keypress until the client disconnects. A recover() keeps one bad client from
// taking down the listener. No logging, by design.
func serveConn(conn net.Conn) {
	defer conn.Close()
	defer func() { _ = recover() }()

	if _, err := go3270.NegotiateTelnet(conn); err != nil {
		return
	}
	for {
		if _, err := go3270.HandleScreen(
			randomScreen(), nil, map[string]string{},
			nil, allExitAIDs, "", 0, 0, conn,
		); err != nil {
			return
		}
	}
}

// randomScreen returns one of the welcome screens at random.
func randomScreen() go3270.Screen { return screenFor(rand.Intn(len(builders))) }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dummy/ -race -v`
Expected: PASS — all Task 1 tests plus `TestListenRejectsBadAddr` and `TestServeAcceptsConnections`.

- [ ] **Step 5: Commit**

```bash
git add internal/dummy/server.go internal/dummy/server_test.go
git commit -m "feat(dummy): Telnet-negotiating accept/repaint server loop"
```

---

### Task 3: CLI binary (`cmd/dummy3270/main.go`)

**Files:**
- Create: `cmd/dummy3270/main.go`

- [ ] **Step 1: Write the implementation**

Create `cmd/dummy3270/main.go` (after the GPL header):

```go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/coffeemuse/tn3270proxy/internal/dummy"
)

func main() {
	listen := flag.String("listen", ":3300", "address to listen on (plaintext TN3270, no TLS)")
	flag.Parse()

	// One startup line only; the server logs nothing per-connection by design.
	fmt.Fprintf(os.Stderr, "dummy3270 listening on %s\n", *listen)
	if err := dummy.ListenAndServe(*listen); err != nil {
		fmt.Fprintln(os.Stderr, "dummy3270:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Build everything**

Run: `go build ./... && go vet ./internal/dummy/ ./cmd/dummy3270/`
Expected: no output, exit 0.

- [ ] **Step 3: Build the binary**

Run: `go build -o bin/dummy3270 ./cmd/dummy3270`
Expected: no output; `bin/dummy3270` exists (gitignored — do not commit it).

- [ ] **Step 4: Smoke-run it manually**

Run (in one shell): `./bin/dummy3270 -listen 127.0.0.1:3300`
Expected stderr: `dummy3270 listening on 127.0.0.1:3300`, then it blocks.

If `c3270` is available, in another shell: `c3270 127.0.0.1:3300`
Expected: a mockup welcome screen with a blinking red `DUMMY3270` in the upper-right and a `Press PA3 to disconnect.` footer. Pressing Enter/PF keys repaints a (possibly different) random screen. Ctrl-] then `quit` (or close c3270) to exit. Stop the server with Ctrl-C.

(If no emulator is on this machine, this step is best-effort; the protocol surface is otherwise covered by the optional smoke wiring in Task 5.)

- [ ] **Step 5: Commit**

```bash
git add cmd/dummy3270/main.go
git commit -m "feat(dummy3270): standalone CLI bridge-target binary"
```

---

### Task 4: Document the binary in CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (Commands section + Architecture package map)

- [ ] **Step 1: Add a build/run line to the Commands block**

In `CLAUDE.md`, inside the main ```` ```bash ```` Commands block, after the
`go build -o bin/tn3270proxy ./cmd/tn3270proxy` line, add:

```bash
go build -o bin/dummy3270 ./cmd/dummy3270                     # demo/test TN3270 bridge target
./bin/dummy3270 -listen :3300                                 # run the dummy backend (no TLS, no auth, no logging)
```

- [ ] **Step 2: Add package-map entries**

In the Architecture package map, add an entry near `cmd/tn3270proxy`:

```
cmd/dummy3270     main: tiny standalone TN3270 server (-listen, no TLS/DB/auth/logging)
                  used as a demo/test bridge target; wraps internal/dummy.
```

and near the internal packages, add:

```
internal/dummy    Throwaway TN3270 server: pure go3270 screen builders (3 random
                  mockup welcome screens with a blinking red DUMMY3270 marker +
                  "Press PA3 to disconnect." footer) + a Telnet-negotiating
                  accept/repaint loop. No state, no logging.
```

- [ ] **Step 3: Verify the doc still builds nothing (sanity) and commit**

Run: `go build ./...`
Expected: exit 0 (no code changed, just confirming a clean tree).

```bash
git add CLAUDE.md
git commit -m "docs: note dummy3270 bridge target in CLAUDE.md"
```

---

### Task 5 (OPTIONAL): Repoint smoke.sh's BACKEND at dummy3270

> Only do this if you want the s3270 smoke test's bridge scenario to land on a
> realistic app screen instead of a second proxy's login screen. This changes
> grep expectations and is intentionally separable from the core feature.

**Files:**
- Modify: `.claude/skills/s3270-smoke-testing/smoke.sh`

- [ ] **Step 1: Read the current bridge scenario**

Run: `grep -n "BACK_PORT\|BACKEND\|BACK_PID\|tn3270proxy serve" .claude/skills/s3270-smoke-testing/smoke.sh`
Expected: shows where the second proxy is started on `BACK_PORT` and where the
`BACKEND` service / bridge assertions live.

- [ ] **Step 2: Decide and implement**

Replace the second-proxy backend launch with `bin/dummy3270 -listen 127.0.0.1:$BACK_PORT`,
and update the bridge-scenario `check` pattern that currently greps for the proxy's
login text (e.g. `TN3270 GATEWAY LOGIN`) to instead grep for a `dummy3270` screen
marker that appears on every paint — `DUMMY3270` (the marker) is the stable choice
since the three bodies vary. Leave all non-bridge scenarios untouched.

- [ ] **Step 3: Run the smoke test**

Run: `.claude/skills/s3270-smoke-testing/smoke.sh`
Expected: all scenarios PASS, including the bridge scenario now asserting `DUMMY3270`.

- [ ] **Step 4: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/smoke.sh
git commit -m "test(smoke): bridge to dummy3270 instead of a second proxy"
```

---

## Self-review

**Spec coverage:**
- Single plaintext port, no TLS → Task 2 `Listen`/`ListenAndServe`, Task 3 `-listen`. ✓
- Telnet negotiation via go3270 → Task 2 `serveConn` `NegotiateTelnet`. ✓
- 3 random mockup screens, no state → Task 1 `screenA/B/C` + `screenFor`/`randomScreen`. ✓
- Repaint on any key, exit on disconnect → Task 2 `allExitAIDs` + loop. ✓
- Blinking red `DUMMY3270` marker upper-right + stop field → Task 1 `markerFields` + test. ✓
- `Press PA3 to disconnect.` footer → Task 1 `footerField` + test. ✓
- No per-connection logging; one startup line → Task 2 (silent `serveConn`) + Task 3. ✓
- `-listen :3300` default → Task 3. ✓
- TDD on builders + chooser; net loop at smoke/emulator boundary → Tasks 1–2 tests + Task 3 manual + Task 5. ✓
- Optional smoke repointing → Task 5. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code; every run step shows the command and expected result. ✓

**Type consistency:** `builders`, `screenFor`, `markerText`, `footerText` defined in Task 1 and used by Task 1 tests + Task 2 `randomScreen`/`allExitAIDs`. `Listen`/`Serve`/`Addr`/`Close`/`ListenAndServe`/`serveConn`/`randomScreen` defined in Task 2 and consumed by Task 2 tests + Task 3 `main`. `go3270.HandleScreen` call matches the real signature `(screen, rules, values, pfkeys, exitkeys, errorField, crow, ccol, conn, codepage...)`. ✓
```
