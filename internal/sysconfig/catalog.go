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

// Entry describes one system parameter.
type Entry struct {
	Key      string          // canonical uppercase; used as the DB key and form field name
	Label    string          // display label shown on the System Parameters form
	Default  string          // initial value seeded into system_config by migrate()
	Validate func(string) string // returns an errMsg (uppercase) or "" if valid
}

// Catalog is the application-defined set of valid system parameters. The store
// seeds every key with its Default via INSERT OR IGNORE; admins may change the
// values through the admin UI. Keys are canonical uppercase.
var Catalog = []Entry{
	{
		Key:     "MOTD_FILE",
		Label:   "MOTD File:",
		Default: "",
		// Empty value disables the feature; any non-empty path is accepted.
		// Existence/readability of the file is checked at read time, not here.
		Validate: func(_ string) string { return "" },
	},
}
