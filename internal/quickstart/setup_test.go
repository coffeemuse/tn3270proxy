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
