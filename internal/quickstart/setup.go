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
	b.WriteString("To secure this deployment, see the security-hardening guide\n")
	b.WriteString("in the project documentation (Setup / Installation manual).\n\n")
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
