package quickstart

import (
	"strings"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/screens"
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

func TestDefaultBrandingFitsRegion(t *testing.T) {
	lines := screens.SplitBranding(DefaultBranding())
	if len(lines) == 0 {
		t.Fatal("DefaultBranding produced no lines")
	}
	if h := screens.DefaultGeometry.LoginBrandingHeight(); len(lines) > h {
		t.Errorf("DefaultBranding has %d lines, exceeds MOD 2 region height %d", len(lines), h)
	}
	for i, ln := range lines {
		if len([]rune(ln)) > 79 {
			t.Errorf("line %d is %d cols, exceeds 79", i, len([]rune(ln)))
		}
	}
}
