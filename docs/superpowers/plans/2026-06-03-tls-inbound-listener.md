# TLS-terminated Inbound Listener Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the proxy accept TLS (immediate-TLS, not STARTTLS) TN3270 connections alongside plaintext, with both listeners independently enabled/disabled by an admin via a JSON config file.

**Architecture:** A new JSON config file (`tn3270proxy.json`) describes two named listeners (`plain`, `tls`) plus the DB path; `config.Load` merges `defaults < file < explicit flags`. A new `internal/listen.Build` turns the config into `[]net.Listener` (wrapping the TLS one in `tls.NewListener`). A new `server.ServeAll` runs one existing single-listener `Server.Serve` per listener, sharing one handler. Because `*tls.Conn` is a `net.Conn`, the session/negotiation/bridge layers are untouched.

**Tech Stack:** Go (stdlib `crypto/tls`, `crypto/x509`, `encoding/json`, `flag`, `net`), `modernc.org/sqlite`, go3270. No new third-party dependencies.

**Spec:** `docs/superpowers/specs/2026-06-03-tls-inbound-listener-design.md`

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/config/config.go` | Modify (rewrite) | New `Config` (DBPath + Plain + TLS), JSON file parsing, `defaults < file < flags` precedence, validation |
| `internal/config/config_test.go` | Modify (rewrite) | Precedence, default-file pickup, validation tests |
| `internal/listen/listen.go` | Create | `Build(config.Config) ([]net.Listener, error)` — plaintext + `tls.NewListener` |
| `internal/listen/listen_test.go` | Create | Ephemeral self-signed cert; plain + TLS listener tests; bad-cert error |
| `internal/server/server.go` | Modify | Add `ServeAll([]net.Listener, connHandler) error` (Server itself unchanged) |
| `internal/server/serveall_test.go` | Create | Multi-listener dispatch + teardown-on-error |
| `cmd/tn3270proxy/main.go` | Modify | `runServe` uses `config.Load` → `listen.Build` → `server.ServeAll` |
| `.gitignore` | Modify | Ignore local certs/keys |
| `README.md` | Modify | Config-file format + TLS quick-start |
| `CLAUDE.md` | Modify | Config notes |

**Note on a deliberate test refinement vs the spec:** the spec's TLS listener test described "go3270 negotiation completes over it." Completing a full go3270 handshake in a unit test needs a simulated emulator client, which we don't have. The hermetic unit test instead proves the TLS channel works end-to-end with a **byte round-trip** over an accepted `*tls.Conn`. Full go3270 negotiation over TLS stays in the manual emulator smoke test (Task 8). This is sufficient: `tls.NewListener` yields a `net.Conn` the existing (already-tested) session layer consumes unchanged.

---

## Task 1: New Config struct + flag precedence (no file yet)

Replace the flat `{ListenAddr, DBPath}` config with the two-listener structure and rebuild the flag-only behavior first (keeps the suite green before adding file parsing).

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Replace the config tests with the new struct shape**

Overwrite `internal/config/config_test.go`:

```go
package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	c, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil) error: %v", err)
	}
	if !c.Plain.Enabled || c.Plain.Addr != ":2323" {
		t.Errorf("Plain = %+v, want {Enabled:true Addr:\":2323\"}", c.Plain)
	}
	if c.TLS.Enabled {
		t.Errorf("TLS.Enabled = true, want false by default")
	}
	if c.DBPath != "tn3270proxy.db" {
		t.Errorf("DBPath = %q, want \"tn3270proxy.db\"", c.DBPath)
	}
}

func TestLoadFlagOverrides(t *testing.T) {
	c, err := Load([]string{"-listen", "127.0.0.1:9999", "-db", "/tmp/x.db"})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Plain.Addr != "127.0.0.1:9999" {
		t.Errorf("Plain.Addr = %q", c.Plain.Addr)
	}
	if c.DBPath != "/tmp/x.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail to compile**

Run: `go test ./internal/config/`
Expected: FAIL — build error, `c.Plain`/`c.TLS` undefined (struct still flat).

- [ ] **Step 3: Rewrite config.go with the new struct and flag-only Load**

Overwrite `internal/config/config.go`:

```go
package config

import (
	"errors"
	"flag"
)

// Listener describes a plaintext TCP listener.
type Listener struct {
	Enabled bool
	Addr    string
}

// TLSListener describes a TLS-terminated TCP listener.
type TLSListener struct {
	Enabled bool
	Addr    string
	Cert    string
	Key     string
}

// Config holds runtime configuration for the proxy.
type Config struct {
	DBPath string
	Plain  Listener
	TLS    TLSListener
}

const (
	defaultDBPath     = "tn3270proxy.db"
	defaultPlainAddr  = ":2323"
	defaultConfigFile = "tn3270proxy.json"
)

func defaults() Config {
	return Config{
		DBPath: defaultDBPath,
		Plain:  Listener{Enabled: true, Addr: defaultPlainAddr},
		TLS:    TLSListener{Enabled: false},
	}
}

// Load parses args into a Config, applying precedence:
// defaults < config file < explicit flags.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to JSON config file (default tn3270proxy.json if present)")
	listenAddr := fs.String("listen", "", "plaintext TCP listen address (overrides config)")
	dbPath := fs.String("db", "", "path to SQLite database file (overrides config)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	cfg := defaults()

	// (config-file merge added in Task 2)
	_ = configPath

	if set["db"] {
		cfg.DBPath = *dbPath
	}
	if set["listen"] {
		cfg.Plain.Addr = *listenAddr
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if !cfg.Plain.Enabled && !cfg.TLS.Enabled {
		return errors.New("config: no listener enabled (enable plain and/or tls)")
	}
	if cfg.Plain.Enabled && cfg.Plain.Addr == "" {
		return errors.New("config: plain listener enabled but addr is empty")
	}
	if cfg.TLS.Enabled {
		if cfg.TLS.Addr == "" {
			return errors.New("config: tls listener enabled but addr is empty")
		}
		if cfg.TLS.Cert == "" || cfg.TLS.Key == "" {
			return errors.New("config: tls listener enabled but cert/key not set")
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the config tests to verify they pass**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Confirm the rest of the build is the only breakage (main.go uses old fields)**

Run: `go build ./... 2>&1 | head`
Expected: FAIL only in `cmd/tn3270proxy` (`cfg.ListenAddr` undefined). That is fixed in Task 6. The `config` and `server` packages build.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): two-listener Config struct with validation"
```

---

## Task 2: Config-file loading + precedence

Add JSON file parsing merged onto defaults, with the default-file auto-pickup and explicit-missing-file error.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add file-loading tests**

Append to `internal/config/config_test.go` (add imports `os`, `path/filepath` to the existing `import` block — final block becomes `import ( "os"; "path/filepath"; "testing" )`):

```go
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tn3270proxy.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigFile(t *testing.T) {
	p := writeConfig(t, `{
		"db": "/data/p.db",
		"listeners": {
			"plain": { "enabled": false, "addr": ":111" },
			"tls": { "enabled": true, "addr": ":3270", "cert": "c.pem", "key": "k.pem" }
		}
	}`)
	c, err := Load([]string{"-config", p})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.DBPath != "/data/p.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
	if c.Plain.Enabled {
		t.Errorf("Plain.Enabled = true, want false")
	}
	if !c.TLS.Enabled || c.TLS.Addr != ":3270" || c.TLS.Cert != "c.pem" || c.TLS.Key != "k.pem" {
		t.Errorf("TLS = %+v", c.TLS)
	}
}

func TestFlagOverridesFile(t *testing.T) {
	p := writeConfig(t, `{ "listeners": { "plain": { "enabled": true, "addr": ":111" } } }`)
	c, err := Load([]string{"-config", p, "-listen", ":222", "-db", "/flag.db"})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Plain.Addr != ":222" {
		t.Errorf("Plain.Addr = %q, want flag value \":222\"", c.Plain.Addr)
	}
	if c.DBPath != "/flag.db" {
		t.Errorf("DBPath = %q, want \"/flag.db\"", c.DBPath)
	}
}

func TestExplicitConfigMissingIsError(t *testing.T) {
	_, err := Load([]string{"-config", "/no/such/file.json"})
	if err == nil {
		t.Fatal("want error for explicit missing config, got nil")
	}
}

func TestDefaultConfigFilePickup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tn3270proxy.json"),
		[]byte(`{ "listeners": { "plain": { "addr": ":4444" } } }`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir) // Go 1.24+; repo toolchain is >=1.25
	c, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Plain.Addr != ":4444" {
		t.Errorf("Plain.Addr = %q, want \":4444\" from default-file pickup", c.Plain.Addr)
	}
}

func TestMissingDefaultConfigIsNotError(t *testing.T) {
	t.Chdir(t.TempDir()) // empty dir, no tn3270proxy.json
	c, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Plain.Addr != ":2323" {
		t.Errorf("Plain.Addr = %q, want default", c.Plain.Addr)
	}
}
```

- [ ] **Step 2: Run to verify the new tests fail**

Run: `go test ./internal/config/ -run 'ConfigFile|FlagOverridesFile|ExplicitConfig|DefaultConfig|MissingDefault'`
Expected: FAIL — file contents are ignored (no merge yet), e.g. `TestLoadConfigFile` sees defaults.

- [ ] **Step 3: Add the file-merge implementation**

In `internal/config/config.go`, update the import block to:

```go
import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
)
```

Add the on-disk shape type (pointers distinguish "absent" from "zero") below the `Config` type:

```go
// fileConfig is the on-disk JSON shape. Pointer fields distinguish
// "absent in file" (leave default) from "present and zero".
type fileConfig struct {
	DB        *string `json:"db"`
	Listeners struct {
		Plain *struct {
			Enabled *bool   `json:"enabled"`
			Addr    *string `json:"addr"`
		} `json:"plain"`
		TLS *struct {
			Enabled *bool   `json:"enabled"`
			Addr    *string `json:"addr"`
			Cert    *string `json:"cert"`
			Key     *string `json:"key"`
		} `json:"tls"`
	} `json:"listeners"`
}
```

In `Load`, replace the placeholder block:

```go
	// (config-file merge added in Task 2)
	_ = configPath
```

with:

```go
	path := defaultConfigFile
	explicit := set["config"]
	if explicit {
		path = *configPath
	}
	if err := mergeFile(&cfg, path, explicit); err != nil {
		return Config{}, err
	}
```

Add the merge helper (e.g. above `validate`):

```go
func mergeFile(cfg *Config, path string, explicit bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return nil // default file is optional
		}
		return fmt.Errorf("read config %q: %w", path, err)
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	if fc.DB != nil {
		cfg.DBPath = *fc.DB
	}
	if p := fc.Listeners.Plain; p != nil {
		if p.Enabled != nil {
			cfg.Plain.Enabled = *p.Enabled
		}
		if p.Addr != nil {
			cfg.Plain.Addr = *p.Addr
		}
	}
	if tl := fc.Listeners.TLS; tl != nil {
		if tl.Enabled != nil {
			cfg.TLS.Enabled = *tl.Enabled
		}
		if tl.Addr != nil {
			cfg.TLS.Addr = *tl.Addr
		}
		if tl.Cert != nil {
			cfg.TLS.Cert = *tl.Cert
		}
		if tl.Key != nil {
			cfg.TLS.Key = *tl.Key
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the full config package tests**

Run: `go test ./internal/config/`
Expected: PASS (all precedence, pickup, and missing-file tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): JSON config file with defaults<file<flags precedence"
```

---

## Task 3: Validation tests for disabled/missing-cert cases

Lock the validation rules with explicit tests (the implementation already exists from Task 1).

**Files:**
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add validation tests**

Append to `internal/config/config_test.go`:

```go
func TestValidateNoListenerEnabled(t *testing.T) {
	p := writeConfig(t, `{ "listeners": { "plain": { "enabled": false } } }`)
	_, err := Load([]string{"-config", p})
	if err == nil {
		t.Fatal("want error when no listener is enabled, got nil")
	}
}

func TestValidateTLSWithoutCert(t *testing.T) {
	p := writeConfig(t, `{ "listeners": {
		"plain": { "enabled": false },
		"tls": { "enabled": true, "addr": ":3270" }
	} }`)
	_, err := Load([]string{"-config", p})
	if err == nil {
		t.Fatal("want error when tls enabled without cert/key, got nil")
	}
}
```

- [ ] **Step 2: Run the validation tests**

Run: `go test ./internal/config/ -run Validate -v`
Expected: PASS (both errors returned).

- [ ] **Step 3: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test(config): cover no-listener and tls-without-cert validation"
```

---

## Task 4: `internal/listen.Build`

Turn a `Config` into live listeners: plaintext via `net.Listen`, TLS via `tls.NewListener`.

**Files:**
- Create: `internal/listen/listen.go`
- Create: `internal/listen/listen_test.go`

- [ ] **Step 1: Write the listen tests (with an ephemeral self-signed cert helper)**

Create `internal/listen/listen_test.go`:

```go
package listen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/config"
)

// genSelfSigned writes a self-signed cert/key valid for 127.0.0.1 into a temp
// dir and returns their paths.
func genSelfSigned(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(1<<31-1, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	certOut.Close()

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyOut, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatal(err)
	}
	keyOut.Close()
	return certPath, keyPath
}

func TestBuildPlain(t *testing.T) {
	cfg := config.Config{Plain: config.Listener{Enabled: true, Addr: "127.0.0.1:0"}}
	lns, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if len(lns) != 1 {
		t.Fatalf("len(lns) = %d, want 1", len(lns))
	}
	defer lns[0].Close()

	conn, err := net.Dial("tcp", lns[0].Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
}

func TestBuildTLSRoundTrips(t *testing.T) {
	cert, key := genSelfSigned(t)
	cfg := config.Config{TLS: config.TLSListener{
		Enabled: true, Addr: "127.0.0.1:0", Cert: cert, Key: key,
	}}
	lns, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if len(lns) != 1 {
		t.Fatalf("len(lns) = %d, want 1", len(lns))
	}
	defer lns[0].Close()

	got := make(chan string, 1)
	go func() {
		c, err := lns[0].Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 4)
		n, _ := c.Read(buf)
		got <- string(buf[:n])
	}()

	client, err := tls.Dial("tcp", lns[0].Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("tls.Dial: %v", err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case s := <-got:
		if s != "ping" {
			t.Errorf("server read %q, want \"ping\"", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not read over TLS")
	}
}

func TestBuildTLSBadCertErrors(t *testing.T) {
	cfg := config.Config{TLS: config.TLSListener{
		Enabled: true, Addr: "127.0.0.1:0", Cert: "/no/cert.pem", Key: "/no/key.pem",
	}}
	if _, err := Build(cfg); err == nil {
		t.Fatal("want error for unreadable cert, got nil")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/listen/`
Expected: FAIL — `internal/listen` package / `Build` does not exist yet.

- [ ] **Step 3: Implement Build**

Create `internal/listen/listen.go`:

```go
// Package listen builds the proxy's network listeners (plaintext and TLS)
// from configuration. A TLS listener terminates TLS immediately on connect
// (not STARTTLS), which is what TN3270-over-TLS clients expect.
package listen

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/CoffeeMuse/tn3270proxy/internal/config"
)

// Build returns one net.Listener per enabled transport in cfg. On any error
// it closes listeners already opened so no socket leaks.
func Build(cfg config.Config) ([]net.Listener, error) {
	var lns []net.Listener

	if cfg.Plain.Enabled {
		ln, err := net.Listen("tcp", cfg.Plain.Addr)
		if err != nil {
			closeAll(lns)
			return nil, fmt.Errorf("plain listener on %s: %w", cfg.Plain.Addr, err)
		}
		lns = append(lns, ln)
	}

	if cfg.TLS.Enabled {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
		if err != nil {
			closeAll(lns)
			return nil, fmt.Errorf("load tls keypair: %w", err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		ln, err := net.Listen("tcp", cfg.TLS.Addr)
		if err != nil {
			closeAll(lns)
			return nil, fmt.Errorf("tls listener on %s: %w", cfg.TLS.Addr, err)
		}
		lns = append(lns, tls.NewListener(ln, tlsCfg))
	}

	if len(lns) == 0 {
		return nil, fmt.Errorf("no listeners enabled")
	}
	return lns, nil
}

func closeAll(lns []net.Listener) {
	for _, ln := range lns {
		ln.Close()
	}
}
```

- [ ] **Step 4: Run the listen tests (with race detector — TLS test uses goroutines)**

Run: `go test ./internal/listen/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/listen/listen.go internal/listen/listen_test.go
git commit -m "feat(listen): build plaintext and TLS listeners from config"
```

---

## Task 5: `server.ServeAll`

Run one accept loop per listener, all sharing one handler; first error tears the rest down.

**Files:**
- Modify: `internal/server/server.go`
- Create: `internal/server/serveall_test.go`

- [ ] **Step 1: Write the ServeAll tests**

Create `internal/server/serveall_test.go`:

```go
package server

import (
	"net"
	"testing"
	"time"
)

func TestServeAllDispatchesAcrossListeners(t *testing.T) {
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	handled := make(chan struct{}, 2)
	h := handlerFunc(func(c net.Conn) {
		defer c.Close()
		handled <- struct{}{}
	})

	done := make(chan error, 1)
	go func() { done <- ServeAll([]net.Listener{ln1, ln2}, h) }()

	for _, ln := range []net.Listener{ln1, ln2} {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("dial %s: %v", ln.Addr(), err)
		}
		conn.Close()
	}

	for i := 0; i < 2; i++ {
		select {
		case <-handled:
		case <-time.After(2 * time.Second):
			t.Fatal("a handler was not invoked")
		}
	}

	ln1.Close() // first listener error should tear everything down
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeAll did not return after a listener closed")
	}
}

func TestServeAllErrorsWithNoListeners(t *testing.T) {
	if err := ServeAll(nil, handlerFunc(func(net.Conn) {})); err == nil {
		t.Fatal("want error for empty listener set, got nil")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/server/ -run ServeAll`
Expected: FAIL — `ServeAll` undefined.

- [ ] **Step 3: Implement ServeAll**

In `internal/server/server.go`, update the import block to:

```go
import (
	"errors"
	"log"
	"net"
	"sync"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)
```

Add, after the existing `Serve`/`handle` methods:

```go
// ServeAll runs one accept loop per listener, all sharing handler. The first
// listener error closes the remaining listeners (unblocking their Accept) and
// is returned, so a single transport failure brings the process down cleanly
// rather than silently losing a listener.
func ServeAll(listeners []net.Listener, handler connHandler) error {
	if len(listeners) == 0 {
		return errors.New("server: no listeners")
	}
	errc := make(chan error, len(listeners))
	var once sync.Once
	closeAll := func() {
		for _, ln := range listeners {
			ln.Close()
		}
	}
	for _, ln := range listeners {
		srv := &Server{Listener: ln, Handler: handler}
		go func() {
			err := srv.Serve()
			once.Do(closeAll)
			errc <- err
		}()
	}
	first := <-errc
	for i := 1; i < len(listeners); i++ {
		<-errc
	}
	return first
}
```

- [ ] **Step 4: Run the server tests with the race detector**

Run: `go test ./internal/server/ -race`
Expected: PASS (existing `TestServerAcceptsAndDispatches` plus the two new ones).

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/serveall_test.go
git commit -m "feat(server): ServeAll runs multiple listeners with one handler"
```

---

## Task 6: Wire `cmd/tn3270proxy` to the new path

Replace the single-listener wiring with `config.Load` → `listen.Build` → `server.ServeAll`.

**Files:**
- Modify: `cmd/tn3270proxy/main.go`

- [ ] **Step 1: Update the imports**

In `cmd/tn3270proxy/main.go`, replace the import block with (drops `net`, adds `internal/listen`):

```go
import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/config"
	"github.com/CoffeeMuse/tn3270proxy/internal/listen"
	"github.com/CoffeeMuse/tn3270proxy/internal/seed"
	"github.com/CoffeeMuse/tn3270proxy/internal/server"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)
```

- [ ] **Step 2: Rewrite runServe**

Replace the entire `runServe` function with:

```go
func runServe(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	listeners, err := listen.Build(cfg)
	if err != nil {
		return err
	}

	if cfg.Plain.Enabled {
		fmt.Printf("tn3270proxy listening (plain) on %s (db=%s)\n", cfg.Plain.Addr, cfg.DBPath)
	}
	if cfg.TLS.Enabled {
		fmt.Printf("tn3270proxy listening (tls) on %s (db=%s)\n", cfg.TLS.Addr, cfg.DBPath)
	}

	handler := server.NewSessionHandler(st, bridge.EscapeAIDPA3)
	return server.ServeAll(listeners, handler)
}
```

- [ ] **Step 3: Build everything**

Run: `go build ./...`
Expected: success, no errors.

- [ ] **Step 4: Run the whole suite with the race detector**

Run: `go test ./... -race`
Expected: PASS across all packages.

- [ ] **Step 5: Commit**

```bash
git add cmd/tn3270proxy/main.go
git commit -m "feat(cmd): serve plaintext and TLS listeners via config + ServeAll"
```

---

## Task 7: Repo housekeeping — gitignore, README, CLAUDE.md, sample config

Document the new config surface and keep local certs/configs out of git.

**Files:**
- Modify: `.gitignore`
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Create: `tn3270proxy.example.json`

- [ ] **Step 1: Ignore local certs and the runtime config**

In `.gitignore`, replace the trailing `# Runtime SQLite databases` / `*.db` section with:

```
# Runtime SQLite databases
*.db

# Local TLS certs/keys (self-signed for testing) and runtime config
*.crt
*.key
*.pem
tn3270proxy.json
```

- [ ] **Step 2: Add a sample config (committed, not the live one)**

Create `tn3270proxy.example.json`:

```json
{
  "db": "tn3270proxy.db",
  "listeners": {
    "plain": { "enabled": true, "addr": ":2323" },
    "tls": {
      "enabled": false,
      "addr": ":3270",
      "cert": "server.crt",
      "key": "server.key"
    }
  }
}
```

- [ ] **Step 3: Update the README**

In `README.md`, replace the `## Run` section and the `## Status` section with:

```markdown
## Run

    ./bin/tn3270proxy serve -db proxy.db -listen :2323

Connect with any TN3270 emulator (e.g. `c3270 host:2323`). Press **PA3** during
a bridged session to return to the menu; **PF3** at the menu disconnects.

### Configuration file

Richer setup (e.g. TLS) uses a JSON config file. By default `serve` looks for
`tn3270proxy.json` in the working directory; pass `-config <path>` to choose
another. Flags (`-listen`, `-db`) override file values, which override built-in
defaults. See `tn3270proxy.example.json`:

    {
      "db": "tn3270proxy.db",
      "listeners": {
        "plain": { "enabled": true,  "addr": ":2323" },
        "tls":   { "enabled": true,  "addr": ":3270",
                   "cert": "server.crt", "key": "server.key" }
      }
    }

Both listeners are independent: enable either or both. TLS is terminated
immediately on connect (not STARTTLS), as TN3270-over-TLS clients expect.

Generate a self-signed cert/key for local testing (gitignored):

    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
      -nodes -keyout server.key -out server.crt -days 365 \
      -subj "/CN=localhost" -addext "subjectAltName=IP:127.0.0.1,DNS:localhost"

## Status

MVP + TLS inbound listener. Not yet implemented (see spec section 9 / ROADMAP):
backend TLS, admin UI, TN3270E, audit logging, session multiplexing.
```

- [ ] **Step 4: Update CLAUDE.md config notes**

In `CLAUDE.md`, replace the `internal/config` line in the package map:

```
internal/config   Config{ListenAddr, DBPath}; Load(args)
```

with:

```
internal/config   Config{DBPath, Plain, TLS}; Load(args) merges defaults<file<flags.
                  Optional JSON file (tn3270proxy.json) defines plain+tls listeners.
internal/listen   Build(cfg) → []net.Listener (plaintext + tls.NewListener, immediate TLS).
```

And in the same package map, update the `internal/server` description's first line to mention `ServeAll`:

```
internal/server   Session state machine (Negotiate→Login→Menu→Bridge loop) behind
                  Presenter/Bridger/Authenticator seams; go3270Presenter + realBridger are
                  the real impls; Server is the TCP accept loop (recovers per-conn panics);
                  ServeAll runs one Server per listener sharing a handler.
```

- [ ] **Step 5: Verify build/tests still pass and commit**

Run: `go build ./... && go test ./...`
Expected: PASS.

```bash
git add .gitignore README.md CLAUDE.md tn3270proxy.example.json
git commit -m "docs: document TLS config file; ignore local certs/config"
```

---

## Task 8: Verification — full suite + manual TLS smoke test

The 3270 protocol surface (screens, cursor, negotiation, PA3, bridging) is only truly verified against a live emulator. Confirm the TLS port carries a real session.

**Files:** none (verification only).

- [ ] **Step 1: Full race build/test**

Run: `go test ./... -race`
Expected: PASS, no data races.

- [ ] **Step 2: Build the binary**

Run: `go build -o bin/tn3270proxy ./cmd/tn3270proxy`
Expected: success.

- [ ] **Step 3: Generate a local self-signed cert (if not already present)**

Run:
```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
  -nodes -keyout server.key -out server.crt -days 365 \
  -subj "/CN=localhost" -addext "subjectAltName=IP:127.0.0.1,DNS:localhost"
```
Expected: `server.crt` and `server.key` created (gitignored).

- [ ] **Step 4: Seed and run with both listeners**

Create `tn3270proxy.json` (gitignored) with `plain` and `tls` both enabled (addrs `:2323` and `:3270`, cert/key as above), seed, then run:
```bash
./bin/tn3270proxy seed -db proxy.db -file seed.example.json
./bin/tn3270proxy serve -db proxy.db
```
Expected: logs show both "(plain) on :2323" and "(tls) on :3270".

- [ ] **Step 5: Connect a TLS-capable emulator and run the MVP smoke checklist**

With `c3270`, connect over TLS (e.g. `c3270 L:127.0.0.1:3270`, where the `L:` prefix requests TLS; accept the self-signed cert). Then walk the MVP smoke-test checklist (login → group-filtered menu → select service → bridge → PA3 returns to menu → PF3 disconnects).
Expected: full login→menu→bridge→PA3→PF3 loop works over TLS, identical to plaintext.

- [ ] **Step 6: Confirm plaintext still works**

Connect `c3270 127.0.0.1:2323` (no TLS) and confirm the same loop.
Expected: plaintext path unchanged.

---

## Self-Review notes (already applied)

- **Spec coverage:** JSON config + named plain/tls listeners (Tasks 1–2), default-file pickup + explicit-missing error (Task 2), `defaults<file<flags` precedence (Tasks 1–2), validation incl. tls-without-cert / no-listener (Tasks 1, 3), immediate-TLS via `tls.NewListener` + `MinVersion` TLS 1.2 (Task 4), multi-listener serving with shared handler + teardown-on-error (Task 5), backward-compatible wiring (Task 6), gitignore/README/CLAUDE.md + sample config (Task 7), ephemeral-cert hermetic tests + manual emulator smoke (Tasks 4, 8). Out-of-scope items (hot-reload, mTLS) are intentionally not implemented.
- **Type consistency:** `config.Config{DBPath, Plain, TLS}`, `config.Listener{Enabled, Addr}`, `config.TLSListener{Enabled, Addr, Cert, Key}`, `listen.Build(config.Config) ([]net.Listener, error)`, `server.ServeAll([]net.Listener, connHandler) error` are used identically across all tasks.
- **Deliberate test refinement:** TLS unit test verifies a byte round-trip over `*tls.Conn` (hermetic); full go3270-over-TLS negotiation is covered by the Task 8 manual smoke test (documented above).
```
