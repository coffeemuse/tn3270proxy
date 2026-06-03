# Backend-side TLS Dialing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a menu service is marked TLS, dial the backend over TLS, with a per-service certificate-verification switch (default on, browser-like validation).

**Architecture:** A new `services.tls_verify` column (added via a guarded, idempotent migration) flows through `store.Service` and the seed format. `bridge.Bridge` gains a trailing `*tls.Config` parameter (nil = plaintext, as today). The `server` layer keeps `crypto/tls` out of the session state machine: the `Bridger` interface carries a small `BackendTLS{Enabled, Verify}` intent struct, and `realBridger` translates it into a `*tls.Config` (system roots, `ServerName` = the configured host).

**Tech Stack:** Go, `crypto/tls`, `modernc.org/sqlite` (pure-Go, driver name `"sqlite"`), `racingmars/go3270`.

**Spec:** `docs/superpowers/specs/2026-06-03-backend-tls-dialing-design.md`

**Conventions reminder:** TDD throughout. Run `go test ./... -race` (the bridge is concurrent). Conventional commit prefixes (`feat:`/`test:`/`docs:`). Never log credentials or cert contents.

---

## File map

- `internal/store/store.go` — schema const (+`tls_verify`), `migrate()` guard, `ensureColumn` helper, `Service.TLSVerify`, `CreateService(... , verify bool)`, `ListServicesForGroups` SELECT/scan.
- `internal/store/services_test.go` — caller updates + verify-default test.
- `internal/store/store_test.go` — legacy-DB migration test (raw `database/sql`).
- `internal/seed/seed.go` — `SeedService.Verify *bool`, resolve `nil→true` in `Apply`.
- `internal/seed/seed_test.go` — verify pointer assertion.
- `internal/bridge/bridge.go` — `Bridge(..., tlsCfg *tls.Config)`, branch the dial.
- `internal/bridge/bridge_test.go` — pass `nil` in existing tests; new TLS-backend tests + self-signed cert helper.
- `internal/server/session.go` — `BackendTLS` type, `Bridger` interface, build + pass intent.
- `internal/server/presenter.go` — `backendTLSConfig`, `realBridger.Bridge`.
- `internal/server/presenter_test.go` (new) — `backendTLSConfig` unit test.
- `internal/server/session_test.go` — `fakeBridger` signature + capture, new propagation test, `CreateService` caller fix.
- `seed.example.json`, `CLAUDE.md`, `docs/superpowers/ROADMAP.md` — docs.

---

## Task 1: store — `tls_verify` column, guarded migration, read path

**Files:**
- Modify: `internal/store/store.go` (schema const ~39-66, `migrate` ~68-73, `Service` ~162-169, `ListServicesForGroups` ~204-225)
- Test: `internal/store/services_test.go`, `internal/store/store_test.go`

This task does **not** change `CreateService`'s signature, so no existing callers break. New rows get `tls_verify` via `DEFAULT 1`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/services_test.go`:

```go
func TestServicesVerifyColumnDefaultsOn(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	ops, _ := st.CreateGroup(ctx, "ops")
	sid, _ := st.CreateService(ctx, "SEC", "sec.example", 992, true)
	st.LinkGroupService(ctx, ops, sid)

	svcs, err := st.ListServicesForGroups(ctx, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || !svcs[0].TLSVerify {
		t.Fatalf("want TLSVerify true by default, got %+v", svcs)
	}
}
```

Add to `internal/store/store_test.go` (and add `"database/sql"` to its imports):

```go
func TestMigrateAddsVerifyToLegacyDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Simulate a pre-tls_verify database: services table WITHOUT the column.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE services (
		id   INTEGER PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL,
		tls  INTEGER NOT NULL DEFAULT 0
	);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	// Open through the store: migrate() must ALTER in tls_verify (default 1).
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy db: %v", err)
	}
	defer st.Close()

	ops, _ := st.CreateGroup(ctx, "ops")
	sid, _ := st.CreateService(ctx, "SEC", "sec.example", 992, true)
	st.LinkGroupService(ctx, ops, sid)

	svcs, err := st.ListServicesForGroups(ctx, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || !svcs[0].TLSVerify {
		t.Fatalf("legacy migration: want TLSVerify true, got %+v", svcs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestServicesVerifyColumnDefaultsOn|TestMigrateAddsVerifyToLegacyDB' -v`
Expected: build failure — `svc.TLSVerify undefined (type Service has no field or method TLSVerify)`.

- [ ] **Step 3: Add the column to the schema const**

In `internal/store/store.go`, change the `services` table in the `schema` const to add `tls_verify`:

```go
CREATE TABLE IF NOT EXISTS services (
	id         INTEGER PRIMARY KEY,
	name       TEXT UNIQUE NOT NULL,
	host       TEXT NOT NULL,
	port       INTEGER NOT NULL,
	tls        INTEGER NOT NULL DEFAULT 0,
	tls_verify INTEGER NOT NULL DEFAULT 1
);
```

- [ ] **Step 4: Add the guarded migration**

Replace `migrate()` in `internal/store/store.go` and add `ensureColumn` below it:

```go
func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	// Existing DBs predating tls_verify won't get it from CREATE TABLE IF NOT
	// EXISTS, so add it explicitly (idempotent: skipped when already present).
	if err := s.ensureColumn("services", "tls_verify",
		"ALTER TABLE services ADD COLUMN tls_verify INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	return nil
}

// ensureColumn runs alterSQL only if table lacks column. SQLite's
// ALTER TABLE ADD COLUMN errors if the column already exists, so we probe
// PRAGMA table_info first to keep migrate() idempotent.
func (s *Store) ensureColumn(table, column, alterSQL string) error {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("inspect %s: %w", table, err)
		}
		if name == column {
			return rows.Err() // already present
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	if _, err := s.db.Exec(alterSQL); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}
```

(`database/sql` and `_ "modernc.org/sqlite"` are already imported in `store.go`.)

- [ ] **Step 5: Add the struct field and read it**

In `internal/store/store.go`, add the field to `Service`:

```go
// Service is a backend TN3270 host the menu can offer.
type Service struct {
	ID        int64
	Name      string
	Host      string
	Port      int
	TLS       bool
	TLSVerify bool
}
```

In `ListServicesForGroups`, add `s.tls_verify` to the SELECT and scan it:

```go
	query := `SELECT DISTINCT s.id, s.name, s.host, s.port, s.tls, s.tls_verify
		FROM services s
		JOIN group_services gs ON gs.service_id = s.id
		JOIN groups g ON g.id = gs.group_id
		WHERE g.name IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY s.name`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		var svc Service
		var tlsInt, verifyInt int
		if err := rows.Scan(&svc.ID, &svc.Name, &svc.Host, &svc.Port, &tlsInt, &verifyInt); err != nil {
			return nil, err
		}
		svc.TLS = tlsInt != 0
		svc.TLSVerify = verifyInt != 0
		out = append(out, svc)
	}
	return out, rows.Err()
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/store/ -v`
Expected: PASS (new tests plus the existing `TestListServicesForGroups`, `TestOpenCreatesTables`, etc.).

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/services_test.go internal/store/store_test.go
git commit -m "feat(store): add per-service tls_verify column with guarded migration"
```

---

## Task 2: store + seed — thread `verify` through `CreateService` and the seed format

**Files:**
- Modify: `internal/store/store.go` (`CreateService` ~171-182)
- Modify: `internal/store/services_test.go` (2 callers), `internal/server/session_test.go` (1 caller)
- Modify: `internal/seed/seed.go` (`SeedService`, `Apply`)
- Test: `internal/seed/seed_test.go`

Changing `CreateService`'s signature ripples to all callers; they are all updated here so `go build ./...` stays green.

- [ ] **Step 1: Write the failing seed test**

Add to `internal/seed/seed_test.go`:

```go
func TestApplyVerifyDefaultsOnWhenOmitted(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	verifyOff := false
	data := SeedData{
		Groups: []string{"ops"},
		Services: []SeedService{
			// Verify omitted (nil) → must default to ON.
			{Name: "DEFON", Host: "a", Port: 992, TLS: true, Groups: []string{"ops"}},
			// Verify explicitly false → must stay OFF.
			{Name: "OFF", Host: "b", Port: 992, TLS: true, Verify: &verifyOff, Groups: []string{"ops"}},
		},
	}
	if err := Apply(ctx, st, data); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	svcs, err := st.ListServicesForGroups(ctx, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]store.Service{}
	for _, s := range svcs {
		byName[s.Name] = s
	}
	if !byName["DEFON"].TLSVerify {
		t.Errorf("omitted verify should default ON, got %+v", byName["DEFON"])
	}
	if byName["OFF"].TLSVerify {
		t.Errorf("explicit verify=false should stay OFF, got %+v", byName["OFF"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/seed/ -run TestApplyVerifyDefaultsOnWhenOmitted -v`
Expected: build failure — `unknown field Verify in struct literal` and `too few arguments` mismatch once `CreateService` changes; at this point: `unknown field 'Verify'`.

- [ ] **Step 3: Change `CreateService` to accept and store `verify`**

In `internal/store/store.go`:

```go
// CreateService inserts a service, or returns the existing service's id.
func (s *Store) CreateService(ctx context.Context, name, host string, port int, tls, verify bool) (int64, error) {
	tlsInt := 0
	if tls {
		tlsInt = 1
	}
	verifyInt := 0
	if verify {
		verifyInt = 1
	}
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO services (name, host, port, tls, tls_verify) VALUES (?, ?, ?, ?, ?)",
		[]any{name, host, port, tlsInt, verifyInt},
		"SELECT id FROM services WHERE name = ?",
		[]any{name})
}
```

- [ ] **Step 4: Update the seed struct and `Apply`**

In `internal/seed/seed.go`, add the pointer field to `SeedService`:

```go
// SeedService describes one service to create.
type SeedService struct {
	Name   string   `json:"name"`
	Host   string   `json:"host"`
	Port   int      `json:"port"`
	TLS    bool     `json:"tls"`
	Verify *bool    `json:"verify"` // omitted → verify ON (secure default)
	Groups []string `json:"groups"`
}
```

In `Apply`, resolve the pointer before calling `CreateService` (replace the `CreateService` call inside the `for _, svc := range data.Services` loop):

```go
	for _, svc := range data.Services {
		verify := true // secure default when "verify" is omitted
		if svc.Verify != nil {
			verify = *svc.Verify
		}
		sid, err := st.CreateService(ctx, svc.Name, svc.Host, svc.Port, svc.TLS, verify)
		if err != nil {
			return fmt.Errorf("create service %q: %w", svc.Name, err)
		}
		for _, g := range svc.Groups {
			gid, err := ensureGroup(g)
			if err != nil {
				return err
			}
			if err := st.LinkGroupService(ctx, gid, sid); err != nil {
				return fmt.Errorf("link %q to %q: %w", svc.Name, g, err)
			}
		}
	}
```

- [ ] **Step 5: Fix the remaining `CreateService` callers**

In `internal/store/services_test.go`, update the two calls (add a trailing verify arg):

```go
	prod, _ := st.CreateService(ctx, "PROD CICS", "prod.example", 23, false, true)
	test, _ := st.CreateService(ctx, "TEST CICS", "test.example", 992, true, true)
```

In `internal/store/services_test.go` `TestServicesVerifyColumnDefaultsOn` (added in Task 1), update its call to the new arity:

```go
	sid, _ := st.CreateService(ctx, "SEC", "sec.example", 992, true, true)
```

In `internal/store/store_test.go` `TestMigrateAddsVerifyToLegacyDB` (added in Task 1), update its call:

```go
	sid, _ := st.CreateService(ctx, "SEC", "sec.example", 992, true, true)
```

In `internal/server/session_test.go`, update `newTestSession` (line ~83):

```go
	sid, _ := st.CreateService(ctx, "PROD", "10.0.0.1", 23, false, false)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/seed/ ./internal/store/ ./internal/server/ -v`
Expected: PASS across all three packages.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/services_test.go internal/store/store_test.go internal/seed/seed.go internal/seed/seed_test.go internal/server/session_test.go
git commit -m "feat(seed): per-service verify flag (default on) threaded through CreateService"
```

---

## Task 3: bridge — TLS dial branch

**Files:**
- Modify: `internal/bridge/bridge.go` (`Bridge` ~31-70)
- Modify: `internal/bridge/bridge_test.go` (existing callers + new tests)
- Modify: `internal/server/presenter.go` (`realBridger.Bridge` — interim `nil`)

`tls.DialWithDialer` returns a `*tls.Conn` (a `net.Conn`), so the relay is unchanged.

- [ ] **Step 1: Write the failing TLS tests**

Add to `internal/bridge/bridge_test.go`. First add imports `crypto/ecdsa`, `crypto/elliptic`, `crypto/rand`, `crypto/tls`, `crypto/x509`, `crypto/x509/pkix`, `math/big` to the existing `import` block. Then:

```go
// selfSignedCert returns a TLS server certificate valid for 127.0.0.1 and a
// pool trusting it. Used to exercise the bridge's TLS dial path.
func selfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(1<<31-1, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// startFakeTLSBackend is the TLS twin of startFakeBackend: same Telnet/echo
// serve loop, behind a tls.NewListener.
func startFakeTLSBackend(t *testing.T, cert tls.Certificate) *fakeBackend {
	t.Helper()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := tls.NewListener(raw, &tls.Config{Certificates: []tls.Certificate{cert}})
	fb := &fakeBackend{ln: ln, termType: make(chan string, 1)}
	go fb.serve()
	t.Cleanup(func() { ln.Close() })
	return fb
}

func TestBridgeTLSBackendVerified(t *testing.T) {
	cert, pool := selfSignedCert(t)
	fb := startFakeTLSBackend(t, cert)

	clientConn, proxySide := net.Pipe()
	defer clientConn.Close()

	host, _, _ := net.SplitHostPort(fb.addr())
	tlsCfg := &tls.Config{RootCAs: pool, ServerName: host}

	done := make(chan Cause, 1)
	go func() {
		c, err := Bridge(proxySide, fb.addr(), "IBM-3278-2-E", aidPA3, tlsCfg)
		if err != nil {
			t.Errorf("Bridge error: %v", err)
		}
		done <- c
	}()

	select {
	case tt := <-fb.termType:
		if tt[:12] != "IBM-3278-2-E" {
			t.Errorf("backend got termtype %q", tt[:12])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend never received terminal type over TLS")
	}

	rec := []byte{0xF5, 0xC3, 0x11, 0x40, 0x40, cIAC, cEOR}
	if _, err := clientConn.Write(rec); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(rec))
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := readFull(clientConn, got); err != nil {
		t.Fatalf("reading echo over TLS: %v", err)
	}
	for i := range rec {
		if got[i] != rec[i] {
			t.Errorf("echo[%d] = %#x, want %#x", i, got[i], rec[i])
		}
	}
}

func TestBridgeTLSBackendUntrustedFails(t *testing.T) {
	cert, _ := selfSignedCert(t)
	fb := startFakeTLSBackend(t, cert)

	clientConn, proxySide := net.Pipe()
	defer clientConn.Close()

	host, _, _ := net.SplitHostPort(fb.addr())
	// No RootCAs → system roots → self-signed cert is untrusted → handshake fails.
	tlsCfg := &tls.Config{ServerName: host}

	c, err := Bridge(proxySide, fb.addr(), "IBM-3278-2-E", aidPA3, tlsCfg)
	if err == nil {
		t.Errorf("expected TLS verification error")
	}
	if c != CauseError {
		t.Errorf("cause = %v, want CauseError", c)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/bridge/ -run 'TestBridgeTLSBackend' -v`
Expected: build failure — `too many arguments in call to Bridge` (signature still 4 params).

- [ ] **Step 3: Add the `tlsCfg` parameter and branch the dial**

In `internal/bridge/bridge.go`, add `"crypto/tls"` to the imports, then change `Bridge`'s signature and the dial (lines ~31-36):

```go
// Bridge dials the backend at addr, negotiates the client leg of Telnet
// (offering termType), and relays the 3270 datastream between client and
// backend until one side closes or the user presses escapeAID. When tlsCfg is
// non-nil the backend is dialed over TLS; nil dials plaintext. The client
// connection is NOT closed (the caller reuses it for the menu); its deadlines
// are reset before returning.
func Bridge(client net.Conn, addr, termType string, escapeAID byte, tlsCfg *tls.Config) (Cause, error) {
	var backend net.Conn
	var err error
	if tlsCfg != nil {
		backend, err = tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", addr, tlsCfg)
	} else {
		backend, err = net.DialTimeout("tcp", addr, dialTimeout)
	}
	if err != nil {
		return CauseError, err
	}
	defer backend.Close()
```

The rest of `Bridge` (the `results` channel, goroutines, teardown) is unchanged.

- [ ] **Step 4: Update the existing bridge tests and the interim presenter caller**

In `internal/bridge/bridge_test.go`, add the trailing `nil` to the three existing `Bridge(...)` calls:

```go
		c, err := Bridge(proxySide, fb.addr(), "IBM-3278-2-E", aidPA3, nil) // TestBridgeNegotiatesAndRelays
```
```go
		c, _ := Bridge(proxySide, fb.addr(), "IBM-3278-2-E", aidPA3, nil) // TestBridgeEscapeOnPA3
```
```go
	c, err := Bridge(proxySide, "127.0.0.1:1", "IBM-3278-2-E", aidPA3, nil) // TestBridgeDialError
```

In `internal/server/presenter.go`, keep `realBridger` compiling by passing `nil` for now (Task 4 replaces this with the real config):

```go
func (realBridger) Bridge(client net.Conn, addr, termType string, escapeAID byte) (bridge.Cause, error) {
	return bridge.Bridge(client, addr, termType, escapeAID, nil)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/bridge/ -race -v`
Expected: PASS (new TLS tests + existing bridge tests). `go build ./...` succeeds (server still compiles via the interim `nil`).

- [ ] **Step 6: Commit**

```bash
git add internal/bridge/bridge.go internal/bridge/bridge_test.go internal/server/presenter.go
git commit -m "feat(bridge): dial backend over TLS when a tls.Config is supplied"
```

---

## Task 4: server — `BackendTLS` intent, interface, and translation

**Files:**
- Modify: `internal/server/session.go` (`Bridger` interface ~26-28, `BackendTLS` type, bridge call ~77)
- Modify: `internal/server/presenter.go` (`backendTLSConfig`, `realBridger.Bridge`)
- Create: `internal/server/presenter_test.go`
- Modify: `internal/server/session_test.go` (`fakeBridger`, new test)

- [ ] **Step 1: Write the failing translation test**

Create `internal/server/presenter_test.go`:

```go
package server

import (
	"testing"
)

func TestBackendTLSConfig(t *testing.T) {
	if cfg := backendTLSConfig("h:23", BackendTLS{Enabled: false, Verify: true}); cfg != nil {
		t.Errorf("disabled → want nil config, got %+v", cfg)
	}

	on := backendTLSConfig("cics.corp:992", BackendTLS{Enabled: true, Verify: true})
	if on == nil {
		t.Fatal("enabled → want non-nil config")
	}
	if on.InsecureSkipVerify {
		t.Errorf("verify on → InsecureSkipVerify must be false")
	}
	if on.ServerName != "cics.corp" {
		t.Errorf("ServerName = %q, want host %q", on.ServerName, "cics.corp")
	}

	off := backendTLSConfig("10.0.0.5:992", BackendTLS{Enabled: true, Verify: false})
	if off == nil || !off.InsecureSkipVerify {
		t.Errorf("verify off → InsecureSkipVerify must be true, got %+v", off)
	}
	if off.ServerName != "10.0.0.5" {
		t.Errorf("ServerName = %q, want host %q", off.ServerName, "10.0.0.5")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestBackendTLSConfig -v`
Expected: build failure — `undefined: backendTLSConfig` and `undefined: BackendTLS`.

- [ ] **Step 3: Add `BackendTLS` and widen the `Bridger` interface**

In `internal/server/session.go`, replace the `Bridger` interface and add the type:

```go
// BackendTLS expresses a service's backend-TLS intent. The server layer keeps
// crypto/tls out of the session machine; realBridger turns this into a
// *tls.Config.
type BackendTLS struct {
	Enabled bool // dial the backend over TLS
	Verify  bool // validate the backend cert (system roots + hostname)
}

// Bridger connects the client to a backend service.
type Bridger interface {
	Bridge(client net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error)
}
```

- [ ] **Step 4: Build and pass the intent from the session**

In `internal/server/session.go`, replace the bridge call (line ~76-77):

```go
		addr := net.JoinHostPort(selected.Host, strconv.Itoa(selected.Port))
		btls := BackendTLS{Enabled: selected.TLS, Verify: selected.TLSVerify}
		cause, berr := s.Bridger.Bridge(conn, addr, termType, s.EscapeAID, btls)
```

- [ ] **Step 5: Implement the translation in `realBridger`**

In `internal/server/presenter.go`, add `"crypto/tls"` to the imports, then replace `realBridger.Bridge` and add `backendTLSConfig`:

```go
func (realBridger) Bridge(client net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error) {
	return bridge.Bridge(client, addr, termType, escapeAID, backendTLSConfig(addr, btls))
}

// backendTLSConfig builds the dial-time tls.Config for a backend, or nil for a
// plaintext dial. ServerName is always the configured host; verification uses
// the system root store (browser-like). Verify=false encrypts without
// authenticating (for internal hosts with self-signed certs).
func backendTLSConfig(addr string, btls BackendTLS) *tls.Config {
	if !btls.Enabled {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         host,
		InsecureSkipVerify: !btls.Verify,
	}
}
```

- [ ] **Step 6: Update `fakeBridger` and add a propagation test**

In `internal/server/session_test.go`, update `fakeBridger` to the new signature and capture the intent:

```go
type fakeBridger struct {
	causes []bridge.Cause
	errs   []error
	calls  int
	gotTLS []BackendTLS
}

func (f *fakeBridger) Bridge(conn net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error) {
	f.gotTLS = append(f.gotTLS, btls)
	i := f.calls
	f.calls++
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return f.causes[i], err
}
```

Add a new test (the session reads TLS fields straight off the `*store.Service` the presenter returns, so no store wiring is needed):

```go
func TestSessionPassesTLSIntentToBridger(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "SEC", Host: "10.0.0.9", Port: 992, TLS: true, TLSVerify: true}},
			{quit: true},
		},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseUserEscaped}}
	s := newTestSession(t, p, b)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(b.gotTLS) != 1 {
		t.Fatalf("bridge called %d times, want 1", len(b.gotTLS))
	}
	if got := b.gotTLS[0]; !got.Enabled || !got.Verify {
		t.Errorf("intent = %+v, want {Enabled:true Verify:true}", got)
	}
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/server/ -race -v`
Expected: PASS (`TestBackendTLSConfig`, `TestSessionPassesTLSIntentToBridger`, and all existing session tests).

- [ ] **Step 8: Commit**

```bash
git add internal/server/session.go internal/server/presenter.go internal/server/presenter_test.go internal/server/session_test.go
git commit -m "feat(server): carry BackendTLS intent and build backend tls.Config"
```

---

## Task 5: docs + full verification

**Files:**
- Modify: `seed.example.json`, `CLAUDE.md`, `docs/superpowers/ROADMAP.md`

- [ ] **Step 1: Show `verify` in the seed example**

In `seed.example.json`, add a TLS service that demonstrates the flag (omitted = verify on; show an explicit `verify: false` for a self-signed internal host):

```json
{
  "groups": ["ops", "dev"],
  "users": [
    { "username": "alice", "password": "changeme", "groups": ["ops"] },
    { "username": "bob",   "password": "changeme", "groups": ["dev"] }
  ],
  "services": [
    { "name": "PUB TSO",  "host": "localhost",        "port": 3270, "groups": ["ops"] },
    { "name": "SEC CICS", "host": "cics.corp.example", "port": 992, "tls": true, "groups": ["ops"] },
    { "name": "LAB CICS", "host": "10.0.0.5",          "port": 992, "tls": true, "verify": false, "groups": ["dev"] }
  ]
}
```

- [ ] **Step 2: Update CLAUDE.md**

In `CLAUDE.md`, under the `internal/bridge` package description, note that backend TLS is now honored. Update the "Reserved hooks for future work" bullet: remove the "backend-TLS dialing is not implemented" claim and replace with a note that backend TLS is implemented with a per-service `tls_verify` flag (default on, system-root/browser-like verification; `verify:false` encrypts without authenticating). In the "Quick reference" mindset, the `services.tls` row is no longer "bridge ignores it."

Suggested edit to the reserved-hooks bullet:

```markdown
- **Backend TLS:** implemented. A service dials over TLS when `services.tls` is set; the
  per-service `services.tls_verify` column (default on) controls certificate verification
  (system roots + hostname, browser-like). `verify:false` encrypts without authenticating
  (for self-signed internal hosts). ServerName is always the configured host — connect-by-IP
  with verify on needs an IP SAN. The escape AID (`bridge.EscapeAIDPA3`) remains the one
  reserved hook meant to become configurable.
```

- [ ] **Step 3: Update the ROADMAP**

In `docs/superpowers/ROADMAP.md`:
- Change the `## 2.` heading to mark it done, mirroring item #1's "✅ **DONE**" style, with a one-paragraph summary block linking the spec (`docs/superpowers/specs/2026-06-03-backend-tls-dialing-design.md`) and this plan.
- Update the **Progress** line (remaining order becomes `5 → 7 → 3 → 4 → 6`; next up is #5 Audit logging).
- In the "what's already wired" table, change the Backend TLS row from "plumbed; bridge ignores it" to "✅ done — per-service `tls_verify`, `bridge.Bridge` takes a `*tls.Config`."

- [ ] **Step 4: Full verification**

Run: `go build ./... && go test ./... -race`
Expected: all packages PASS, race-clean.

- [ ] **Step 5: Commit**

```bash
git add seed.example.json CLAUDE.md docs/superpowers/ROADMAP.md
git commit -m "docs: document backend TLS dialing; mark roadmap #2 complete"
```

---

## Manual smoke test (do before declaring protocol-facing work done)

Per `CLAUDE.md`, the TLS/3270 protocol surface is only truly verified live. This needs a TLS-terminated TN3270 backend (a real host, or `openssl s_server`-style TLS wrapper in front of a TN3270 service).

- [ ] Seed a TLS service with `verify:false` pointing at a self-signed backend; connect with `c3270`/`x3270`, log in, select it → bridges successfully; PA3 returns to the menu.
- [ ] Seed a TLS service with verify **on** (omit `verify`) pointing at a host whose cert matches its name and chains to a system root → bridges. Point it at an untrusted/self-signed host → fails closed back to the menu with "Could not connect to <name>".
- [ ] Confirm a plaintext (`tls:false`) service still bridges unchanged.

---

## Self-review notes

- **Spec coverage:** schema+migration (Task 1), `Service.TLSVerify`/read (Task 1), `CreateService`+seed pointer default-on (Task 2), TLS dial branch (Task 3), `BackendTLS` interface + `ServerName`=host + `InsecureSkipVerify=!Verify` + TLS 1.2 floor + session decoupled from crypto/tls (Task 4), docs + out-of-scope confirmed (Task 5), manual smoke test (final section). All spec sections map to a task.
- **Type consistency:** `BackendTLS{Enabled,Verify}`, `backendTLSConfig(addr string, btls BackendTLS) *tls.Config`, `CreateService(ctx, name, host, port, tls, verify bool)`, `Service.TLSVerify`, and `Bridge(client, addr, termType, escapeAID, tlsCfg)` are used identically across every task that references them.
- **Build-green per commit:** every task updates all callers affected by a signature change in the same commit (Task 2 fixes all `CreateService` callers; Task 3 passes interim `nil` in `presenter.go`, replaced in Task 4).
