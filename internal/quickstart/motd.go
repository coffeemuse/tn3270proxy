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
