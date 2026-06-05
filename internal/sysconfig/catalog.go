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
	"strconv"
	"strings"
)

// Entry describes one system parameter.
type Entry struct {
	Key      string              // canonical uppercase; used as the DB key and form field name
	Label    string              // display label shown on the System Parameters form
	Default  string              // initial value seeded into system_config by migrate()
	Validate func(string) string // returns an errMsg (uppercase) or "" if valid
}

// KeyMOTDFile is the system_config key whose value is the absolute path to the
// MOTD/NEWS file shown after login. Empty disables the feature.
const KeyMOTDFile = "MOTD_FILE"

// KeyMFAIssuer is the system_config key holding the TOTP issuer label shown in
// users' authenticator apps (and on the enrollment screen).
const KeyMFAIssuer = "MFA_ISSUER"

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

// Catalog is the application-defined set of valid system parameters. The store
// seeds every key with its Default via INSERT OR IGNORE; admins may change the
// values through the admin UI. Keys are canonical uppercase.
var Catalog = []Entry{
	{
		Key:     KeyMOTDFile,
		Label:   "MOTD File:",
		Default: "",
		// Empty value disables the feature; any non-empty path is accepted.
		// Existence/readability of the file is checked at read time, not here.
		Validate: func(_ string) string { return "" },
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
