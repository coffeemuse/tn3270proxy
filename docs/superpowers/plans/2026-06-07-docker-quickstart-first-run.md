# Docker Quick-Start First-Run Provisioning — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `quickstart` subcommand that provisions a fresh data directory with a generated admin, two sample users, a `DEMO` service bridging to `dummy3270`, an MFA key, a self-signed cert, a MOTD, and a `SETUP-DEFAULTS.TXT`, wired into a Docker image so `docker compose up` is the only command a user runs.

**Architecture:** A new self-contained `internal/quickstart` package does all provisioning and is unit-testable; a thin `cmd/tn3270proxy/quickstart.go` subcommand wraps it. Provisioning is all-or-nothing (existing → no-op, partial → error, fresh → provision) and emits a config file the *existing* `serve`/`internal/config` already understand, so neither is modified. A multi-stage Dockerfile builds both `tn3270proxy` and `dummy3270`; `docker-compose.yml` runs them as two services on an internal network.

**Tech Stack:** Go (stdlib `crypto/rand`, `crypto/x509`, `crypto/tls`, `encoding/json`, `encoding/base64`); existing `internal/{store,seed,auth,mfa,sysconfig}` packages; Docker + Compose.

**Spec:** `docs/superpowers/specs/2026-06-07-docker-quickstart-first-run-design.md`

> **PREREQUISITE — branch base:** This plan must be implemented on a branch cut from
> current `origin/main`, which contains `cmd/dummy3270` and the merged smoke changes.
> The worktree where this plan was authored predates them. Before Task 1, confirm
> `cmd/dummy3270/main.go` exists in the working tree (`ls cmd/dummy3270`); if not, rebase
> onto `origin/main` first.

---

## File Structure

**New package `internal/quickstart/`** (all provisioning logic; one responsibility per file):
- `paths.go` — `Layout` struct mapping a data dir to every artifact path; `NewLayout(dir)`.
- `password.go` — `GenPassword()` 3270-safe one-time password generator.
- `cert.go` — `GenerateSelfSignedCert(hosts, validDays)` pure-Go self-signed cert/key PEMs.
- `config.go` — `renderConfigJSON(Layout)` → the `tn3270proxy.json` bytes.
- `motd.go` — `DefaultMOTD()` welcome text.
- `setup.go` — `Cred`, `Result` types; `renderSetupFile(Result)` → `SETUP-DEFAULTS.TXT` text.
- `seeddata.go` — `buildSeedData(Result)` → `seed.SeedData`.
- `provision.go` — `ErrAlreadyProvisioned`; `Provision(ctx, dir)` orchestrator + state detection.

**Modified:**
- `cmd/tn3270proxy/main.go` — add `quickstart` dispatch case.
- `cmd/tn3270proxy/quickstart.go` (new) — thin subcommand wrapper.
- `cmd/tn3270proxy/bootstrap.go` — `generateBootstrapPassword` delegates to `quickstart.GenPassword` (DRY).
- `CLAUDE.md` — note the new package + subcommand in the package map.

**New (infra/docs):**
- `Dockerfile` — multi-stage build of both binaries.
- `docker-compose.yml` — `tn3270proxy` + `dummy3270` services.
- `docs/quickstart.md` — user-facing quick start.
- `docs/security-hardening.md` — the long-form "secure it properly" doc referenced by the MOTD/SETUP file.
- `.claude/skills/s3270-smoke-testing/quickstart-smoke.sh` — provisioning smoke check.

**Package API surface (locked here, referenced by later tasks):**
```go
// internal/quickstart
type Layout struct{ Dir, DB, Config, MFAKey, CertDir, Cert, Key, MOTD, SetupFile string }
func NewLayout(dir string) Layout
func GenPassword() (string, error)
func GenerateSelfSignedCert(hosts []string, validDays int) (certPEM, keyPEM []byte, err error)
func DefaultMOTD() string

type Cred struct{ Username, Password string; Groups []string }
type Result struct {
    DataDir     string
    Admin       Cred
    Samples     []Cred
    PlainAddr   string
    TLSAddr     string
    DemoService string
}
var ErrAlreadyProvisioned = errors.New("quickstart: data dir already provisioned")
func Provision(ctx context.Context, dir string) (*Result, error)
```
**Constants** (define in `paths.go`): `PlainAddr = ":2323"`, `TLSAddr = ":2324"`,
`AdminUser = "ADMIN"`, `DemoGroup = "DEMO"`, `DemoServiceName = "DEMO"`,
`DemoBackendHost = "dummy3270"`, `DemoBackendPort = 3300`, `CertValidDays = 825`.
Sample users: `OPERATOR`, `GUEST`. Cert hosts: `localhost`, `127.0.0.1`, `::1`.

---

## Task 1: Layout paths

**Files:**
- Create: `internal/quickstart/paths.go`
- Test: `internal/quickstart/paths_test.go`

- [ ] **Step 1: Write the failing test**

```go
package quickstart

import (
	"path/filepath"
	"testing"
)

func TestNewLayout(t *testing.T) {
	l := NewLayout("/data")
	cases := map[string]string{
		"config":    l.Config,
		"db":        l.DB,
		"mfakey":    l.MFAKey,
		"cert":      l.Cert,
		"key":       l.Key,
		"motd":      l.MOTD,
		"setupfile": l.SetupFile,
	}
	want := map[string]string{
		"config":    "/data/tn3270proxy.json",
		"db":        "/data/proxy.db",
		"mfakey":    "/data/mfa.key",
		"cert":      "/data/tls/cert.pem",
		"key":       "/data/tls/key.pem",
		"motd":      "/data/motd.txt",
		"setupfile": "/data/SETUP-DEFAULTS.TXT",
	}
	for k, got := range cases {
		if got != filepath.FromSlash(want[k]) {
			t.Errorf("%s = %q, want %q", k, got, want[k])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/quickstart/ -run TestNewLayout -v`
Expected: FAIL — `undefined: NewLayout`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package quickstart provisions a fresh data directory for the opinionated
// Docker quick-start: a generated admin, sample users, a demo service, an MFA
// key, a self-signed cert, a MOTD, and a human-readable credentials file.
package quickstart

import "path/filepath"

const (
	PlainAddr = ":2323"
	TLSAddr   = ":2324"

	AdminUser       = "ADMIN"
	DemoGroup       = "DEMO"
	DemoServiceName = "DEMO"
	DemoBackendHost = "dummy3270"
	DemoBackendPort = 3300

	CertValidDays = 825
)

// SampleUsers are the non-admin demo accounts seeded on first run.
var SampleUsers = []string{"OPERATOR", "GUEST"}

// CertHosts are the SAN entries for the generated self-signed cert.
var CertHosts = []string{"localhost", "127.0.0.1", "::1"}

// Layout maps a data directory to every artifact quickstart manages.
type Layout struct {
	Dir       string
	DB        string
	Config    string
	MFAKey    string
	CertDir   string
	Cert      string
	Key       string
	MOTD      string
	SetupFile string
}

// NewLayout derives all artifact paths under dir.
func NewLayout(dir string) Layout {
	return Layout{
		Dir:       dir,
		DB:        filepath.Join(dir, "proxy.db"),
		Config:    filepath.Join(dir, "tn3270proxy.json"),
		MFAKey:    filepath.Join(dir, "mfa.key"),
		CertDir:   filepath.Join(dir, "tls"),
		Cert:      filepath.Join(dir, "tls", "cert.pem"),
		Key:       filepath.Join(dir, "tls", "key.pem"),
		MOTD:      filepath.Join(dir, "motd.txt"),
		SetupFile: filepath.Join(dir, "SETUP-DEFAULTS.TXT"),
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/quickstart/ -run TestNewLayout -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/quickstart/paths.go internal/quickstart/paths_test.go
git commit -m "feat(quickstart): data-dir layout paths"
```

---

## Task 2: One-time password generator (shared with bootstrap)

**Files:**
- Create: `internal/quickstart/password.go`
- Test: `internal/quickstart/password_test.go`
- Modify: `cmd/tn3270proxy/bootstrap.go` (delegate to shared generator)

- [ ] **Step 1: Write the failing test**

```go
package quickstart

import (
	"regexp"
	"strings"
	"testing"
)

func TestGenPasswordFormat(t *testing.T) {
	re := regexp.MustCompile(`^[ABCDEFGHJKMNPQRSTVWXYZ23456789]{4}-[ABCDEFGHJKMNPQRSTVWXYZ23456789]{4}-[ABCDEFGHJKMNPQRSTVWXYZ23456789]{4}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		p, err := GenPassword()
		if err != nil {
			t.Fatalf("GenPassword: %v", err)
		}
		if !re.MatchString(p) {
			t.Fatalf("password %q does not match XXXX-XXXX-XXXX 3270-safe charset", p)
		}
		if strings.ContainsAny(p, "ILO01") {
			t.Fatalf("password %q contains an ambiguous character", p)
		}
		seen[p] = true
	}
	if len(seen) < 90 {
		t.Fatalf("expected high uniqueness, got %d distinct of 100", len(seen))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/quickstart/ -run TestGenPasswordFormat -v`
Expected: FAIL — `undefined: GenPassword`.

- [ ] **Step 3: Write minimal implementation**

```go
package quickstart

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// passwordCharset is the 3270-typeable unambiguous alphanumeric set used for
// one-time passwords: uppercase letters excluding I, L, O; digits excluding 0
// and 1. Eliminates common transcription errors on physical 3270 keyboards.
const passwordCharset = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

// GenPassword returns a random password in XXXX-XXXX-XXXX form using
// passwordCharset (12 characters grouped for readability).
func GenPassword() (string, error) {
	b := make([]byte, 12)
	n := big.NewInt(int64(len(passwordCharset)))
	for i := range b {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		b[i] = passwordCharset[idx.Int64()]
	}
	return fmt.Sprintf("%s-%s-%s", string(b[0:4]), string(b[4:8]), string(b[8:12])), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/quickstart/ -run TestGenPasswordFormat -v`
Expected: PASS.

- [ ] **Step 5: DRY — delegate the bootstrap generator to the shared one**

In `cmd/tn3270proxy/bootstrap.go`, add the import `"github.com/coffeemuse/tn3270proxy/internal/quickstart"`, delete the `bootstrapCharset` const and the body of `generateBootstrapPassword`, and replace it with:

```go
// generateBootstrapPassword returns a one-time password using the shared
// 3270-safe generator (see internal/quickstart.GenPassword).
func generateBootstrapPassword() (string, error) {
	return quickstart.GenPassword()
}
```

Remove the now-unused imports (`crypto/rand`, `math/big`) from `bootstrap.go` if they are no longer referenced elsewhere in the file.

- [ ] **Step 6: Run the affected tests**

Run: `go test ./cmd/tn3270proxy/ ./internal/quickstart/ -v`
Expected: PASS (existing bootstrap tests still pass; password format unchanged).

- [ ] **Step 7: Commit**

```bash
git add internal/quickstart/password.go internal/quickstart/password_test.go cmd/tn3270proxy/bootstrap.go
git commit -m "feat(quickstart): shared 3270-safe one-time password generator"
```

---

## Task 3: Self-signed certificate generator

**Files:**
- Create: `internal/quickstart/cert.go`
- Test: `internal/quickstart/cert_test.go`

- [ ] **Step 1: Write the failing test**

```go
package quickstart

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	certPEM, keyPEM, err := GenerateSelfSignedCert([]string{"localhost", "127.0.0.1", "::1"}, 825)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}
	// Loadable as a TLS keypair (what listen.Build does).
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	// Parse the leaf and assert SANs + validity window.
	pair, _ := tls.X509KeyPair(certPEM, keyPEM)
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if err := leaf.VerifyHostname("localhost"); err != nil {
		t.Errorf("VerifyHostname(localhost): %v", err)
	}
	if !leaf.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) && len(leaf.IPAddresses) < 1 {
		t.Errorf("expected 127.0.0.1 in IP SANs, got %v", leaf.IPAddresses)
	}
	wantNotAfter := leaf.NotBefore.Add(825 * 24 * time.Hour)
	if leaf.NotAfter.Sub(wantNotAfter) > time.Hour || wantNotAfter.Sub(leaf.NotAfter) > time.Hour {
		t.Errorf("NotAfter = %v, want ~%v", leaf.NotAfter, wantNotAfter)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/quickstart/ -run TestGenerateSelfSignedCert -v`
Expected: FAIL — `undefined: GenerateSelfSignedCert`.

- [ ] **Step 3: Write minimal implementation**

```go
package quickstart

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// GenerateSelfSignedCert returns PEM-encoded cert and private key for a
// self-signed leaf valid for validDays, with hosts split into DNS and IP SANs.
// Pure Go (no openssl). Intended for the quick-start TLS listener only.
func GenerateSelfSignedCert(hosts []string, validDays int) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("serial: %w", err)
	}
	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "localhost", Organization: []string{"tn3270proxy quick-start"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Duration(validDays) * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cert: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/quickstart/ -run TestGenerateSelfSignedCert -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/quickstart/cert.go internal/quickstart/cert_test.go
git commit -m "feat(quickstart): pure-Go self-signed cert generator"
```

---

## Task 4: Generated config JSON

**Files:**
- Create: `internal/quickstart/config.go`
- Test: `internal/quickstart/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
package quickstart

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderConfigJSON(t *testing.T) {
	l := NewLayout("/data")
	b, err := renderConfigJSON(l)
	if err != nil {
		t.Fatalf("renderConfigJSON: %v", err)
	}
	// Must unmarshal with unknown-field rejection (mirrors internal/config loader).
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var got map[string]any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("generated config has fields the loader would reject: %v", err)
	}
	if !strings.Contains(string(b), "\"db\": \"/data/proxy.db\"") &&
		!strings.Contains(string(b), "/data/proxy.db") {
		t.Errorf("db path missing from config:\n%s", b)
	}
	if !strings.Contains(string(b), TLSAddr) {
		t.Errorf("tls addr %q missing from config:\n%s", TLSAddr, b)
	}
	if !strings.Contains(string(b), "/data/mfa.key") {
		t.Errorf("mfa key_file missing from config:\n%s", b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/quickstart/ -run TestRenderConfigJSON -v`
Expected: FAIL — `undefined: renderConfigJSON`.

- [ ] **Step 3: Write minimal implementation**

```go
package quickstart

import "encoding/json"

// configDoc mirrors the subset of internal/config's on-disk JSON shape that
// quickstart sets. Field names/tags MUST match internal/config.fileConfig; a
// round-trip test through config.Load (Task 6) guards against drift.
type configDoc struct {
	DB        string `json:"db"`
	Listeners struct {
		Plain struct {
			Enabled bool   `json:"enabled"`
			Addr    string `json:"addr"`
		} `json:"plain"`
		TLS struct {
			Enabled bool   `json:"enabled"`
			Addr    string `json:"addr"`
			Cert    string `json:"cert"`
			Key     string `json:"key"`
		} `json:"tls"`
	} `json:"listeners"`
	MFA struct {
		KeyFile string `json:"key_file"`
	} `json:"mfa"`
}

// renderConfigJSON builds the quick-start tn3270proxy.json: db path, both
// listeners enabled, and the MFA key file. Returned bytes are indented.
func renderConfigJSON(l Layout) ([]byte, error) {
	var d configDoc
	d.DB = l.DB
	d.Listeners.Plain.Enabled = true
	d.Listeners.Plain.Addr = PlainAddr
	d.Listeners.TLS.Enabled = true
	d.Listeners.TLS.Addr = TLSAddr
	d.Listeners.TLS.Cert = l.Cert
	d.Listeners.TLS.Key = l.Key
	d.MFA.KeyFile = l.MFAKey
	return json.MarshalIndent(d, "", "  ")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/quickstart/ -run TestRenderConfigJSON -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/quickstart/config.go internal/quickstart/config_test.go
git commit -m "feat(quickstart): render generated tn3270proxy.json"
```

---

## Task 5: MOTD, SETUP file, and seed data

**Files:**
- Create: `internal/quickstart/motd.go`, `internal/quickstart/setup.go`, `internal/quickstart/seeddata.go`
- Test: `internal/quickstart/setup_test.go`, `internal/quickstart/seeddata_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/quickstart/setup_test.go`:
```go
package quickstart

import (
	"strings"
	"testing"
)

func sampleResult() Result {
	return Result{
		DataDir:     "/data",
		Admin:       Cred{Username: "ADMIN", Password: "AAAA-BBBB-CCCC", Groups: []string{"ZZADMIN", "DEMO"}},
		Samples:     []Cred{{Username: "OPERATOR", Password: "DDDD-EEEE-FFFF", Groups: []string{"DEMO"}}},
		PlainAddr:   ":2323",
		TLSAddr:     ":2324",
		DemoService: "DEMO",
	}
}

func TestRenderSetupFileContainsCredentialsAndWarnings(t *testing.T) {
	out := renderSetupFile(sampleResult())
	for _, want := range []string{
		"ADMIN", "AAAA-BBBB-CCCC", "OPERATOR", "DDDD-EEEE-FFFF",
		":2323", ":2324", "DEMO", "CHANGE THESE PASSWORDS",
		"QUICK START", "mfa.key", "security-hardening",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SETUP file missing %q:\n%s", want, out)
		}
	}
}

func TestDefaultMOTDEncouragesPasswordChange(t *testing.T) {
	m := DefaultMOTD()
	if !strings.Contains(strings.ToUpper(m), "PASSWORD") {
		t.Errorf("MOTD should mention changing passwords:\n%s", m)
	}
}
```

`internal/quickstart/seeddata_test.go`:
```go
package quickstart

import (
	"slices"
	"testing"
)

func TestBuildSeedData(t *testing.T) {
	r := Result{
		Admin:   Cred{Username: "ADMIN", Password: "p1", Groups: []string{"ZZADMIN", "DEMO"}},
		Samples: []Cred{{Username: "OPERATOR", Password: "p2", Groups: []string{"DEMO"}}, {Username: "GUEST", Password: "p3", Groups: []string{"DEMO"}}},
	}
	d := buildSeedData(r)
	if len(d.Users) != 3 {
		t.Fatalf("want 3 users, got %d", len(d.Users))
	}
	if len(d.Services) != 1 || d.Services[0].Name != "DEMO" || d.Services[0].Host != "dummy3270" || d.Services[0].Port != 3300 {
		t.Fatalf("want one DEMO service to dummy3270:3300, got %+v", d.Services)
	}
	if d.Services[0].TLS {
		t.Errorf("demo service must be plaintext")
	}
	if !slices.Contains(d.Services[0].Groups, "DEMO") {
		t.Errorf("demo service must be visible to DEMO group, got %v", d.Services[0].Groups)
	}
	var admin *struct{ ok bool }
	for _, u := range d.Users {
		if u.Username == "ADMIN" {
			admin = &struct{ ok bool }{true}
			if !slices.Contains(u.Groups, "ZZADMIN") || !slices.Contains(u.Groups, "DEMO") {
				t.Errorf("ADMIN must be in ZZADMIN and DEMO, got %v", u.Groups)
			}
		}
	}
	if admin == nil {
		t.Fatalf("ADMIN user not present in seed data")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/quickstart/ -run 'TestRenderSetupFile|TestDefaultMOTD|TestBuildSeedData' -v`
Expected: FAIL — `undefined: renderSetupFile`, `DefaultMOTD`, `buildSeedData`, `Result`, `Cred`.

- [ ] **Step 3: Write minimal implementations**

`internal/quickstart/setup.go`:
```go
package quickstart

import (
	"fmt"
	"strings"
)

// Cred is a generated account credential, surfaced once in SETUP-DEFAULTS.TXT.
type Cred struct {
	Username string
	Password string
	Groups   []string
}

// Result is everything a provisioning run generated, for the SETUP file and tests.
type Result struct {
	DataDir     string
	Admin       Cred
	Samples     []Cred
	PlainAddr   string
	TLSAddr     string
	DemoService string
}

// renderSetupFile produces the human-readable SETUP-DEFAULTS.TXT body.
func renderSetupFile(r Result) string {
	var b strings.Builder
	b.WriteString("================================================================\n")
	b.WriteString(" tn3270proxy — FRESH INSTALL, quick-start defaults generated\n")
	b.WriteString("================================================================\n\n")
	b.WriteString("This is the QUICK START. It is convenient, NOT a production setup.\n")
	b.WriteString("See docs/security-hardening.md to secure this deployment.\n\n")
	b.WriteString("ADMIN LOGIN\n")
	b.WriteString(fmt.Sprintf("  user: %s\n  password: %s\n  groups: %s\n\n",
		r.Admin.Username, r.Admin.Password, strings.Join(r.Admin.Groups, ", ")))
	b.WriteString("SAMPLE USERS\n")
	for _, s := range r.Samples {
		b.WriteString(fmt.Sprintf("  user: %s  password: %s  groups: %s\n",
			s.Username, s.Password, strings.Join(s.Groups, ", ")))
	}
	b.WriteString("\nCONNECT WITH A 3270 EMULATOR\n")
	b.WriteString(fmt.Sprintf("  plaintext : <host>%s\n", r.PlainAddr))
	b.WriteString(fmt.Sprintf("  TLS       : <host>%s  (self-signed; expect a trust prompt)\n\n", r.TLSAddr))
	b.WriteString(fmt.Sprintf("DEMO SERVICE\n  The %q menu entry bridges to the throwaway dummy3270 backend.\n", r.DemoService))
	b.WriteString("  Press PA3 on the dummy screen to return to the menu.\n\n")
	b.WriteString("MFA\n  Enabled and OPT-IN (no account is enrolled yet). The master key is\n")
	b.WriteString("  stored at mfa.key in this directory. BACK IT UP — losing it makes any\n")
	b.WriteString("  future enrollments unrecoverable.\n\n")
	b.WriteString("!!! CHANGE THESE PASSWORDS !!!\n")
	b.WriteString("  Log in as ADMIN, press 'A' for the admin UI, change the passwords,\n")
	b.WriteString("  then DELETE this file once you have recorded the credentials.\n")
	return b.String()
}
```

`internal/quickstart/motd.go`:
```go
package quickstart

// DefaultMOTD is the welcome text shown on first 3270 login. Rendered through
// the chrome-less MOTD/NEWS path (see internal/screens/news.go).
func DefaultMOTD() string {
	return "" +
		"WELCOME TO TN3270PROXY (QUICK-START DEMO DEPLOYMENT)\n" +
		"\n" +
		"This gateway was provisioned with quick-start defaults.\n" +
		"PLEASE CHANGE THE ADMIN AND SAMPLE PASSWORDS NOW:\n" +
		"  log in, press A for the admin UI, and update each account.\n" +
		"\n" +
		"This is NOT a hardened production setup. See docs/security-hardening.md.\n"
}
```

`internal/quickstart/seeddata.go`:
```go
package quickstart

import "github.com/coffeemuse/tn3270proxy/internal/seed"

// buildSeedData turns generated credentials into a seed.SeedData: all users
// with their group memberships, the DEMO group, and the DEMO service pointing
// at the dummy3270 sibling container (plaintext), visible to the DEMO group.
func buildSeedData(r Result) seed.SeedData {
	users := []seed.SeedUser{{Username: r.Admin.Username, Password: r.Admin.Password, Groups: r.Admin.Groups}}
	for _, s := range r.Samples {
		users = append(users, seed.SeedUser{Username: s.Username, Password: s.Password, Groups: s.Groups})
	}
	return seed.SeedData{
		Groups: []string{DemoGroup},
		Users:  users,
		Services: []seed.SeedService{{
			Name:        DemoServiceName,
			Description: "Demo service (dummy3270)",
			Host:        DemoBackendHost,
			Port:        DemoBackendPort,
			TLS:         false,
			Groups:      []string{DemoGroup},
		}},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/quickstart/ -run 'TestRenderSetupFile|TestDefaultMOTD|TestBuildSeedData' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/quickstart/motd.go internal/quickstart/setup.go internal/quickstart/seeddata.go internal/quickstart/setup_test.go internal/quickstart/seeddata_test.go
git commit -m "feat(quickstart): MOTD, SETUP-DEFAULTS.TXT, and seed data builders"
```

---

## Task 6: Provision orchestrator + state detection

**Files:**
- Create: `internal/quickstart/provision.go`
- Test: `internal/quickstart/provision_test.go`

- [ ] **Step 1: Write the failing test**

```go
package quickstart

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/auth"
	"github.com/coffeemuse/tn3270proxy/internal/config"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
)

func TestProvisionFresh(t *testing.T) {
	dir := t.TempDir()
	res, err := Provision(context.Background(), dir)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	l := NewLayout(dir)
	for _, p := range []string{l.Config, l.DB, l.MFAKey, l.Cert, l.Key, l.MOTD, l.SetupFile} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected artifact %s: %v", p, err)
		}
	}
	// Generated config loads through the real loader.
	cfg, err := config.Load([]string{"-config", l.Config})
	if err != nil {
		t.Fatalf("config.Load on generated config: %v", err)
	}
	if !cfg.Plain.Enabled || !cfg.TLS.Enabled {
		t.Errorf("both listeners should be enabled: %+v", cfg)
	}
	if len(cfg.MFA.Key) != 32 {
		t.Errorf("mfa key should decode to 32 bytes, got %d", len(cfg.MFA.Key))
	}
	// mfa.key file is base64 of 32 bytes.
	raw, _ := os.ReadFile(l.MFAKey)
	if dec, err := base64.StdEncoding.DecodeString(string(raw)); err != nil || len(dec) != 32 {
		t.Errorf("mfa.key not base64 of 32 bytes: err=%v len=%d", err, len(dec))
	}
	// Admin password verifies against the stored bcrypt hash.
	st, err := store.Open(l.DB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer st.Close()
	u, err := st.GetUserByUsername(context.Background(), res.Admin.Username)
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if _, err := auth.Authenticate(context.Background(), st, res.Admin.Username, res.Admin.Password); err != nil {
		t.Errorf("admin password should authenticate: %v", err)
	}
	_ = u
	// MOTD_FILE sysconfig points at the motd file.
	mv, err := st.GetConfig(context.Background(), sysconfig.KeyMOTDFile)
	if err != nil {
		t.Fatalf("get motd config: %v", err)
	}
	if mv != l.MOTD {
		t.Errorf("MOTD_FILE = %q, want %q", mv, l.MOTD)
	}
}

func TestProvisionExistingIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if _, err := Provision(context.Background(), dir); err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	// Tamper with the SETUP file to detect any rewrite.
	l := NewLayout(dir)
	if err := os.WriteFile(l.SetupFile, []byte("SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Provision(context.Background(), dir)
	if !errors.Is(err, ErrAlreadyProvisioned) {
		t.Fatalf("want ErrAlreadyProvisioned, got %v", err)
	}
	b, _ := os.ReadFile(l.SetupFile)
	if string(b) != "SENTINEL" {
		t.Errorf("existing dir was mutated; SETUP file changed")
	}
}

func TestProvisionPartialDirErrors(t *testing.T) {
	dir := t.TempDir()
	// proxy.db present but no config → partial/foreign.
	if err := os.WriteFile(filepath.Join(dir, "proxy.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Provision(context.Background(), dir)
	if err == nil || errors.Is(err, ErrAlreadyProvisioned) {
		t.Fatalf("want a partial-dir error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/quickstart/ -run TestProvision -v`
Expected: FAIL — `undefined: Provision`, `ErrAlreadyProvisioned`.

- [ ] **Step 3: Write minimal implementation**

```go
package quickstart

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/coffeemuse/tn3270proxy/internal/seed"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
)

// ErrAlreadyProvisioned signals that the data dir already has a config file and
// must not be touched (the normal post-first-boot path).
var ErrAlreadyProvisioned = errors.New("quickstart: data dir already provisioned")

// Provision classifies dir and, when fresh, generates every quick-start
// artifact, writing the config file last as the commit point. Returns
// ErrAlreadyProvisioned when a config file is already present, or a partial-dir
// error when proxy.db exists without a config.
func Provision(ctx context.Context, dir string) (*Result, error) {
	l := NewLayout(dir)
	if _, err := os.Stat(l.Config); err == nil {
		return nil, ErrAlreadyProvisioned
	}
	if _, err := os.Stat(l.DB); err == nil {
		return nil, fmt.Errorf("data dir %s is partially provisioned or not empty (found proxy.db but no tn3270proxy.json); remove its contents and retry", dir)
	}

	if err := os.MkdirAll(l.CertDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	st, err := store.Open(l.DB)
	if err != nil {
		return nil, err
	}
	defer st.Close()

	// Generate credentials.
	adminPw, err := GenPassword()
	if err != nil {
		return nil, err
	}
	res := &Result{
		DataDir:     dir,
		Admin:       Cred{Username: AdminUser, Password: adminPw, Groups: []string{store.AdminGroup, DemoGroup}},
		PlainAddr:   PlainAddr,
		TLSAddr:     TLSAddr,
		DemoService: DemoServiceName,
	}
	for _, name := range SampleUsers {
		pw, err := GenPassword()
		if err != nil {
			return nil, err
		}
		res.Samples = append(res.Samples, Cred{Username: name, Password: pw, Groups: []string{DemoGroup}})
	}

	// Seed users, groups, and the demo service in one shot.
	if err := seed.Apply(ctx, st, buildSeedData(*res)); err != nil {
		return nil, fmt.Errorf("seed: %w", err)
	}

	// MFA master key: 32 random bytes, base64, mode 0600.
	keyRaw := make([]byte, 32)
	if _, err := rand.Read(keyRaw); err != nil {
		return nil, fmt.Errorf("mfa key: %w", err)
	}
	if err := os.WriteFile(l.MFAKey, []byte(base64.StdEncoding.EncodeToString(keyRaw)), 0o600); err != nil {
		return nil, fmt.Errorf("write mfa key: %w", err)
	}

	// Self-signed cert.
	certPEM, keyPEM, err := GenerateSelfSignedCert(CertHosts, CertValidDays)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(l.Cert, certPEM, 0o644); err != nil {
		return nil, fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(l.Key, keyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}

	// MOTD file + sysconfig pointer.
	if err := os.WriteFile(l.MOTD, []byte(DefaultMOTD()), 0o644); err != nil {
		return nil, fmt.Errorf("write motd: %w", err)
	}
	if err := st.SetConfig(ctx, sysconfig.KeyMOTDFile, l.MOTD); err != nil {
		return nil, fmt.Errorf("set motd config: %w", err)
	}

	// Human-readable credential record. World-readable (0644) on purpose: it is
	// written into a root-owned bind mount and the host user must be able to open
	// it. Secrets (mfa.key, key.pem) stay 0600. The file tells the user to delete
	// it after recording the credentials.
	if err := os.WriteFile(l.SetupFile, []byte(renderSetupFile(*res)), 0o644); err != nil {
		return nil, fmt.Errorf("write setup file: %w", err)
	}

	// Config LAST — its presence marks the dir as fully provisioned.
	cfgBytes, err := renderConfigJSON(l)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(l.Config, cfgBytes, 0o644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}
	return res, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/quickstart/ -run TestProvision -v`
Expected: PASS.

- [ ] **Step 5: Run the whole package with the race detector**

Run: `go test ./internal/quickstart/ -race`
Expected: PASS (ok).

- [ ] **Step 6: Commit**

```bash
git add internal/quickstart/provision.go internal/quickstart/provision_test.go
git commit -m "feat(quickstart): provision orchestrator with all-or-nothing detection"
```

---

## Task 7: `quickstart` subcommand wiring

**Files:**
- Create: `cmd/tn3270proxy/quickstart.go`
- Modify: `cmd/tn3270proxy/main.go` (dispatch)
- Test: `cmd/tn3270proxy/quickstart_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunQuickstartFreshThenExisting(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runQuickstart([]string{"-data", dir}, &out); err != nil {
		t.Fatalf("fresh quickstart: %v", err)
	}
	if !strings.Contains(out.String(), "FRESH INSTALL") {
		t.Errorf("fresh run should announce a fresh install, got:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "tn3270proxy.json")); err != nil {
		t.Errorf("config not written: %v", err)
	}
	// Second run: existing → no-op, exit 0, different message.
	out.Reset()
	if err := runQuickstart([]string{"-data", dir}, &out); err != nil {
		t.Fatalf("second quickstart should succeed as no-op: %v", err)
	}
	if !strings.Contains(strings.ToLower(out.String()), "existing installation detected") {
		t.Errorf("second run should report existing install, got:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tn3270proxy/ -run TestRunQuickstart -v`
Expected: FAIL — `undefined: runQuickstart`.

- [ ] **Step 3: Write the subcommand**

`cmd/tn3270proxy/quickstart.go`:
```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/coffeemuse/tn3270proxy/internal/quickstart"
)

// runQuickstart implements the `quickstart` subcommand: idempotently provision a
// fresh data dir with opinionated defaults, or no-op on an already-provisioned
// dir. It is wired into the Docker image before `serve`; users never run it by hand.
func runQuickstart(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("quickstart", flag.ContinueOnError)
	dataDir := fs.String("data", "/data", "path to the data directory to provision")
	if err := fs.Parse(args); err != nil {
		return err
	}

	res, err := quickstart.Provision(context.Background(), *dataDir)
	if errors.Is(err, quickstart.ErrAlreadyProvisioned) {
		fmt.Fprintf(w, "Existing installation detected at %s — leaving it untouched.\n", *dataDir)
		return nil
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "============================================================\n")
	fmt.Fprintf(w, " FRESH INSTALL detected — quick-start defaults provisioned.\n")
	fmt.Fprintf(w, "============================================================\n")
	fmt.Fprintf(w, "Admin user: %s\n", res.Admin.Username)
	fmt.Fprintf(w, "Credentials written to: %s/SETUP-DEFAULTS.TXT\n", res.DataDir)
	fmt.Fprintf(w, "Listening (after serve starts): plaintext %s, TLS %s\n", res.PlainAddr, res.TLSAddr)
	fmt.Fprintf(w, "CHANGE THE GENERATED PASSWORDS AFTER FIRST LOGIN.\n")
	return nil
}
```

- [ ] **Step 4: Wire dispatch in `main.go`**

In `cmd/tn3270proxy/main.go`, inside `run(args []string)`, add this case alongside the other subcommand checks (e.g. immediately after the `bootstrap` case):
```go
	if len(args) > 0 && args[0] == "quickstart" {
		return runQuickstart(args[1:], os.Stdout)
	}
```

- [ ] **Step 5: Run tests + build**

Run: `go test ./cmd/tn3270proxy/ -run TestRunQuickstart -v && go build ./...`
Expected: PASS, build succeeds.

- [ ] **Step 6: Commit**

```bash
git add cmd/tn3270proxy/quickstart.go cmd/tn3270proxy/quickstart_test.go cmd/tn3270proxy/main.go
git commit -m "feat(quickstart): quickstart subcommand wired into main dispatch"
```

---

## Task 8: Dockerfile (both binaries)

**Files:**
- Create: `Dockerfile`

- [ ] **Step 1: Write the Dockerfile**

```dockerfile
# syntax=docker/dockerfile:1

# --- build stage ---
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -o /out/tn3270proxy ./cmd/tn3270proxy && \
    go build -o /out/dummy3270 ./cmd/dummy3270

# --- runtime stage ---
# alpine (not distroless) so the two-step quickstart+serve CMD has a real /bin/sh.
FROM alpine:3.20
COPY --from=build /out/tn3270proxy /usr/local/bin/tn3270proxy
COPY --from=build /out/dummy3270 /usr/local/bin/dummy3270
# Data dir is a bind mount in compose; declare it so bare `docker run` also works.
VOLUME ["/data"]
EXPOSE 2323 2324
# Runs as root deliberately: the quick start writes into a host-owned bind mount,
# and a non-root UID typically can't. This is a quick-start trade-off — the
# security-hardening doc covers running non-root. (SETUP-DEFAULTS.TXT is written
# world-readable so the host user can open it; secrets stay 0600.)
# No ENTRYPOINT: CMD is exec'd directly. The proxy service uses this default
# (provision then serve); the dummy3270 service overrides `command` in compose.
CMD ["sh", "-c", "tn3270proxy quickstart -data /data && exec tn3270proxy serve -config /data/tn3270proxy.json"]
```

- [ ] **Step 2: Build the image and verify both binaries run**

Run:
```bash
docker build -t tn3270proxy:quickstart .
docker run --rm tn3270proxy:quickstart tn3270proxy version
docker run --rm tn3270proxy:quickstart dummy3270 -listen :3300 &
sleep 1; docker ps --filter ancestor=tn3270proxy:quickstart -q | xargs -r docker stop
```
Expected: image builds; `tn3270proxy version` prints a version string; `dummy3270`
starts and logs `dummy3270 listening on :3300`. (Because there is no ENTRYPOINT, the
trailing args run the named binary directly.)

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "build: multi-stage Dockerfile building tn3270proxy and dummy3270"
```

---

## Task 9: docker-compose.yml

**Files:**
- Create: `docker-compose.yml`

- [ ] **Step 1: Write the compose file**

```yaml
services:
  tn3270proxy:
    build: .
    image: tn3270proxy:quickstart
    # No `command`: inherits the image CMD (quickstart then serve). Overriding it
    # here would double the `sh -c` wrapper.
    ports:
      - "2323:2323"   # plaintext TN3270
      - "2324:2324"   # TLS TN3270 (self-signed quick-start cert)
    volumes:
      - ./data:/data
    depends_on:
      - dummy3270
    restart: unless-stopped

  dummy3270:
    image: tn3270proxy:quickstart
    command: ["dummy3270", "-listen", ":3300"]
    # No published ports: only the proxy reaches it, over the internal network
    # at dummy3270:3300 (the seeded DEMO service host:port).
    restart: unless-stopped
```

- [ ] **Step 2: Validate and run end-to-end**

Run:
```bash
docker compose config >/dev/null && echo "compose valid"
mkdir -p ./data
docker compose up -d
sleep 3
cat ./data/SETUP-DEFAULTS.TXT
docker compose logs tn3270proxy | grep -i "FRESH INSTALL"
```
Expected: compose validates; `SETUP-DEFAULTS.TXT` appears in `./data` with an ADMIN
password and the warnings; the proxy log shows the fresh-install banner.

- [ ] **Step 3: Verify the no-op on restart**

Run:
```bash
docker compose restart tn3270proxy
sleep 2
docker compose logs tn3270proxy | grep -i "existing installation detected"
```
Expected: second boot reports the existing install and does not regenerate.

- [ ] **Step 4: Tear down and commit**

```bash
docker compose down
rm -rf ./data   # do not commit generated secrets
git add docker-compose.yml
git commit -m "build: docker-compose with tn3270proxy + dummy3270 demo backend"
```

> Add `/data/` to `.gitignore` in Task 10 so generated secrets are never committed.

---

## Task 10: Documentation

**Files:**
- Create: `docs/quickstart.md`
- Create: `docs/security-hardening.md`
- Modify: `.gitignore` (ignore `/data/`)

- [ ] **Step 1: Write `docs/quickstart.md`**

Content (verbatim):
```markdown
# Quick Start (Docker)

The fastest way to try tn3270proxy. **Not** a production setup — see
[security-hardening.md](security-hardening.md) before exposing it.

## Run it

```bash
mkdir -p data
docker compose up -d
```

On first boot the container provisions a fresh `./data` directory and writes
`./data/SETUP-DEFAULTS.TXT` with your generated **ADMIN** password and two sample
users. Open that file to get your login.

## Connect

Point a 3270 emulator at your host:

- Plaintext: `<host>:2323`
- TLS (self-signed, expect a trust prompt): `<host>:2324`

```bash
c3270 127.0.0.1:2323
```

Log in as `ADMIN`. The menu shows a **DEMO** entry that bridges to a throwaway
`dummy3270` backend (a sibling container). Select it to see the bridge work; press
**PA3** to return to the menu.

## First things to do

1. Change the ADMIN and sample passwords (log in, press `A` for the admin UI).
2. Delete `./data/SETUP-DEFAULTS.TXT` once you've recorded the credentials.
3. Point a real service at your own MVS/VM host via the admin UI.
4. Read [security-hardening.md](security-hardening.md) before going beyond your LAN.

## What got generated

Everything lives in the bind-mounted `./data` directory: the SQLite database, the
MFA master key (`mfa.key` — back it up), the self-signed TLS cert (`tls/`), the
MOTD (`motd.txt`), and the generated config (`tn3270proxy.json`). Subsequent
`docker compose up` runs detect the existing install and leave it untouched.
```

- [ ] **Step 2: Write `docs/security-hardening.md`**

Content (verbatim):
```markdown
# Securing tn3270proxy (beyond the quick start)

The Docker quick start optimizes for getting running in 30 seconds. It makes several
deliberate trade-offs you should reverse before any real deployment.

## 1. Replace the generated credentials

The quick start creates an `ADMIN` account and two sample users (`OPERATOR`,
`GUEST`) with generated passwords recorded in `SETUP-DEFAULTS.TXT`.

- Log in as ADMIN, create your own admin account, and delete the `ADMIN` and
  sample accounts via the admin UI (menu `A`).
- Delete `SETUP-DEFAULTS.TXT`.

## 2. Use a real TLS certificate

The quick start generates a self-signed cert for `localhost` (`data/tls/`). Replace
`tls/cert.pem` / `tls/key.pem` with a CA-issued certificate for your real hostname,
or point `listeners.tls.cert`/`key` in `data/tn3270proxy.json` at managed paths.

## 3. Move the MFA master key out of the data directory

The quick start stores the AES-256 MFA key at `data/mfa.key`, next to the database
it encrypts. For production, supply the key out-of-band instead:

- Set `TN3270PROXY_MFA_KEY` (base64 of 32 bytes) in the environment, or
- Point `mfa.key_file` at a path backed by a secret store.

Remove `mfa.key` from the data directory once the key is sourced externally. **Back
up the key** — losing it makes existing MFA enrollments unrecoverable.

## 4. Disable plaintext

The quick start enables plaintext on `:2323` for emulator convenience. For an
exposed gateway, set `listeners.plain.enabled = false` in `tn3270proxy.json` and
serve TLS only. Apply the DoS limits and `trusted_cidrs` described in the main
configuration docs.

## 5. Run the container as non-root

The quick-start image runs as **root** so it can write into a host-owned bind-mounted
data directory, and it writes `SETUP-DEFAULTS.TXT` world-readable so the host user can
open it. For production:

- Run the container as a dedicated non-root UID (e.g. compose `user: "10001:10001"`),
  with a data directory owned by that UID (or use a named volume).
- The MFA key and TLS private key are already `0600`; once you no longer need the host
  user to read `SETUP-DEFAULTS.TXT`, delete it.

## Run without the quick-start wrapper

The `quickstart` subcommand is only the convenience on-ramp. For a controlled
deployment, manage the data dir yourself: `tn3270proxy bootstrap -db <path>` to mint
the first admin, a hand-written `tn3270proxy.json`, and `tn3270proxy serve -config
<path>`.
```

- [ ] **Step 3: Ignore generated secrets**

Append to `.gitignore`:
```
# Docker quick-start generated data (secrets, db, certs)
/data/
```

- [ ] **Step 4: Commit**

```bash
git add docs/quickstart.md docs/security-hardening.md .gitignore
git commit -m "docs: quick-start and security-hardening guides"
```

---

## Task 11: Provisioning smoke check

**Files:**
- Create: `.claude/skills/s3270-smoke-testing/quickstart-smoke.sh`

- [ ] **Step 1: Write the smoke script**

```bash
#!/usr/bin/env bash
# quickstart-smoke.sh — verify `quickstart` provisions a usable data dir and that
# `serve` starts cleanly against the generated config. Binary-level (no Docker).
# The 3270 bridge path itself is covered by smoke.sh (bridges to dummy3270).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

BIN="$(mktemp -d)/tn3270proxy"
go build -o "$BIN" ./cmd/tn3270proxy

DATA="$(mktemp -d)"
echo "== provisioning fresh data dir: $DATA"
"$BIN" quickstart -data "$DATA" | tee "$DATA/.out"
grep -q "FRESH INSTALL" "$DATA/.out"

for f in tn3270proxy.json proxy.db mfa.key tls/cert.pem tls/key.pem motd.txt SETUP-DEFAULTS.TXT; do
  test -f "$DATA/$f" || { echo "MISSING artifact: $f"; exit 1; }
done
echo "== all artifacts present"

echo "== re-run must be a no-op"
"$BIN" quickstart -data "$DATA" | grep -qi "existing installation detected"

echo "== serve starts against the generated config"
"$BIN" serve -config "$DATA/tn3270proxy.json" &
SRV=$!
sleep 2
if ! kill -0 "$SRV" 2>/dev/null; then
  echo "serve exited prematurely"; exit 1
fi
kill "$SRV" 2>/dev/null || true
echo "QUICKSTART SMOKE: PASS"
```

- [ ] **Step 2: Make executable and run**

Run:
```bash
chmod +x .claude/skills/s3270-smoke-testing/quickstart-smoke.sh
.claude/skills/s3270-smoke-testing/quickstart-smoke.sh
```
Expected: ends with `QUICKSTART SMOKE: PASS`.

- [ ] **Step 3: Commit**

```bash
git add .claude/skills/s3270-smoke-testing/quickstart-smoke.sh
git commit -m "test(smoke): quickstart provisioning + serve-start smoke check"
```

---

## Task 12: Update CLAUDE.md package map

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add the subcommand to the `cmd/tn3270proxy` description**

In `CLAUDE.md`, in the `cmd/tn3270proxy` package-map entry, add `quickstart` to the
list of subcommands and note: "`quickstart` provisions a fresh `-data` dir with
opinionated Docker defaults (admin + sample users + DEMO service → dummy3270 + MFA
key + self-signed cert + MOTD + SETUP-DEFAULTS.TXT); idempotent, all-or-nothing;
emits a config the existing `serve` consumes."

- [ ] **Step 2: Add an `internal/quickstart` entry to the package map**

Add, in the package map list:
```
internal/quickstart  First-run provisioning for the Docker quick-start. Provision(ctx,dir)
                     generates the data dir (proxy.db via store + seed, mfa.key, self-signed
                     cert, motd.txt, SETUP-DEFAULTS.TXT) and writes tn3270proxy.json LAST as
                     the "provisioned" marker. Detection: config present → no-op
                     (ErrAlreadyProvisioned); proxy.db without config → partial-dir error;
                     else fresh. Pure-Go cert (no openssl); shares GenPassword with bootstrap.
                     Not the production path (see docs/security-hardening.md).
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: note quickstart subcommand and internal/quickstart in CLAUDE.md"
```

---

## Final verification

- [ ] **Run the full suite with the race detector**

Run: `go test ./... -race`
Expected: all packages PASS.

- [ ] **Build everything**

Run: `go build ./...`
Expected: success.

- [ ] **Run the provisioning smoke**

Run: `.claude/skills/s3270-smoke-testing/quickstart-smoke.sh`
Expected: `QUICKSTART SMOKE: PASS`.

- [ ] **(Manual / live) Compose end-to-end**

Run `docker compose up -d`, open `./data/SETUP-DEFAULTS.TXT`, connect `c3270
127.0.0.1:2323`, log in as ADMIN, select DEMO, confirm the dummy3270 screen, press
PA3 to return to the menu, then `docker compose down`.
