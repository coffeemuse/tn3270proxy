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

// Package sysconfig defines the catalog of runtime system parameters that
// operators can edit via the admin UI. Adding a parameter is one declaration
// in Catalog; the store seeds the default and the form builds from the labels.
package sysconfig

import (
	"fmt"
	"strconv"
	"strings"
)

// Entry describes one system parameter.
type Entry struct {
	Key       string              // canonical uppercase; used as the DB key and form field name
	Label     string              // display label shown on the System Parameters form
	Default   string              // initial value seeded into system_config by migrate()
	Length    int                 // admin form input width; 0 ⇒ default (64)
	Normalize func(string) string // canonicalize before validate+store; nil ⇒ identity
	Validate  func(string) string // returns an errMsg (uppercase) or "" if valid
}

// KeyMOTDFile is the system_config key whose value is the absolute path to the
// MOTD/NEWS file shown after login. Empty disables the feature.
const KeyMOTDFile = "MOTD_FILE"

// KeyBrandingFile is the system_config key whose value is the absolute path to
// the login branding/art file rendered in the login screen body. Empty disables
// the feature (blank region). Mirrors KeyMOTDFile.
const KeyBrandingFile = "BRANDING_FILE"

// KeyMFAIssuer is the system_config key holding the TOTP issuer label shown in
// users' authenticator apps (and on the enrollment screen).
const KeyMFAIssuer = "MFA_ISSUER"

// KeySystemID is the system_config key holding the operator-set System ID shown
// in the menu status block (#53/#64). Canonical form: trimmed, upper-cased, and
// restricted to A-Z/0-9/'-' with a 7-rune budget (the status-block value column).
const KeySystemID = "SYSTEM_ID"

// Throttle params (GH #48): per-username failed-auth backoff. After each failed
// password or MFA attempt the session delays the next prompt by
// AUTH_DELAY_BASE_SECS * min(failcount, AUTH_MAX_TRIES) seconds; the per-username
// count decays after AUTH_FAIL_WINDOW_MINS of no failures. Setting the base to 0
// disables throttling entirely.
const (
	KeyAuthDelayBaseSecs  = "AUTH_DELAY_BASE_SECS"
	KeyAuthMaxTries       = "AUTH_MAX_TRIES"
	KeyAuthFailWindowMins = "AUTH_FAIL_WINDOW_MINS"

	DefaultAuthDelayBaseSecs  = 2
	DefaultAuthMaxTries       = 5
	DefaultAuthFailWindowMins = 15
)

// KeyAuditMaxRows caps how many audit rows the admin Audit Log viewer (GH #40)
// holds in a single snapshot. Clamped to [1, 10000]; read with a fallback to
// DefaultAuditMaxRows for a hand-edited DB.
const (
	KeyAuditMaxRows     = "AUDIT_MAX_ROWS"
	DefaultAuditMaxRows = 1000
)

// KeyAuditReverseDNS toggles the reverse-DNS (PTR) lookup on the Audit Log
// detail screen. "Y" (default) or "N"; N suppresses all outbound DNS the
// viewer would otherwise emit (air-gapped / egress-locked deployments).
const (
	KeyAuditReverseDNS     = "AUDIT_REVERSE_DNS"
	DefaultAuditReverseDNS = "Y"
)

// Catalog is the application-defined set of valid system parameters. The store
// seeds every key with its Default via INSERT OR IGNORE; admins may change the
// values through the admin UI. Keys are canonical uppercase.
// Order is grouped for the System Parameters form: identity (System ID, MOTD
// File), then the four auth parameters (the three failed-auth throttle knobs
// plus the MFA issuer), then the audit parameters.
var Catalog = []Entry{
	{
		Key:       KeySystemID,
		Label:     "System ID:",
		Default:   "PROXY",
		Length:    7,
		Normalize: func(v string) string { return strings.ToUpper(strings.TrimSpace(v)) },
		Validate:  validateSystemID,
	},
	{
		Key:     KeyMOTDFile,
		Label:   "MOTD File:",
		Default: "",
		// Empty value disables the feature; any non-empty path is accepted.
		// Existence/readability of the file is checked at read time, not here.
		Validate: func(_ string) string { return "" },
	},
	{
		Key:     KeyBrandingFile,
		Label:   "Branding File:",
		Default: "",
		// Empty disables (blank region); any non-empty path is accepted.
		// Existence/readability is checked at read time, not here (matches MOTD).
		Validate: func(_ string) string { return "" },
	},
	{
		Key:      KeyAuthDelayBaseSecs,
		Label:    "Auth Delay Base (sec):",
		Default:  strconv.Itoa(DefaultAuthDelayBaseSecs),
		Validate: nonNegativeInt,
	},
	{
		Key:      KeyAuthMaxTries,
		Label:    "Max Auth Tries:",
		Default:  strconv.Itoa(DefaultAuthMaxTries),
		Validate: nonNegativeInt,
	},
	{
		Key:      KeyAuthFailWindowMins,
		Label:    "Auth Fail Window (min):",
		Default:  strconv.Itoa(DefaultAuthFailWindowMins),
		Validate: positiveInt,
	},
	{
		Key:     KeyMFAIssuer,
		Label:   "MFA Issuer:",
		Default: "TN3270PROXY",
		Validate: func(v string) string {
			v = strings.TrimSpace(v)
			if v == "" {
				return "MFA ISSUER REQUIRED"
			}
			if strings.Contains(v, ":") {
				return "MFA ISSUER MUST NOT CONTAIN A COLON"
			}
			if len(v) > 40 {
				return "MFA ISSUER TOO LONG (MAX 40)"
			}
			return ""
		},
	},
	{
		Key:      KeyAuditMaxRows,
		Label:    "Audit View Max Rows:",
		Default:  strconv.Itoa(DefaultAuditMaxRows),
		Validate: intInRange(1, 10000),
	},
	{
		Key:       KeyAuditReverseDNS,
		Label:     "Audit Reverse DNS:",
		Default:   DefaultAuditReverseDNS,
		Length:    1,
		Normalize: func(v string) string { return strings.ToUpper(strings.TrimSpace(v)) },
		Validate:  yesNo,
	},
}

// intInRange returns a validator accepting an integer in [min, max] inclusive.
func intInRange(min, max int) func(string) string {
	return func(v string) string {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < min || n > max {
			return fmt.Sprintf("MUST BE AN INTEGER FROM %d TO %d", min, max)
		}
		return ""
	}
}

// yesNo accepts the single letters Y or N (case-insensitive, surrounding space
// trimmed). Used by boolean-toggle parameters.
func yesNo(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "Y", "N":
		return ""
	}
	return "MUST BE Y OR N"
}

// nonNegativeInt accepts "0" and positive integers (used by the delay base and
// max-tries params; 0 disables their effect).
func nonNegativeInt(v string) string {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return "MUST BE A NON-NEGATIVE INTEGER"
	}
	return ""
}

// positiveInt requires an integer >= 1 (used by the fail-window param).
func positiveInt(v string) string {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return "MUST BE A POSITIVE INTEGER"
	}
	return ""
}

// validateSystemID enforces the 7-rune status-block budget and a mainframe-ish
// charset. It re-normalizes defensively so it is correct regardless of whether
// the caller already applied Normalize.
func validateSystemID(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" {
		return "SYSTEM ID REQUIRED"
	}
	if len([]rune(v)) > 7 {
		return "SYSTEM ID TOO LONG (MAX 7)"
	}
	if v[0] == '-' {
		return "SYSTEM ID MUST NOT START WITH A DASH"
	}
	for _, r := range v {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
			return "SYSTEM ID: USE A-Z 0-9 AND DASH"
		}
	}
	return ""
}
