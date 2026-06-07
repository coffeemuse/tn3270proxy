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
