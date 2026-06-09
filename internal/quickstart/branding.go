package quickstart

// DefaultBranding is the example login art seeded on first run. It is authored
// pre-centered (the login screen renders columns 0-79 verbatim, no auto-indent)
// and sized to fit inside the MOD 2 branding region (18 rows). Rendered through
// the login screen body (see internal/screens/login.go).
func DefaultBranding() string {
	return "" +
		"            ____________________________________________            \n" +
		"           |                                            |           \n" +
		"           |        T N 3 2 7 0   G A T E W A Y         |           \n" +
		"           |                                            |           \n" +
		"           |            QUICK-START DEMO HOST           |           \n" +
		"           |____________________________________________|           \n" +
		"\n" +
		"              Edit BRANDING_FILE to customize this art.\n"
}
