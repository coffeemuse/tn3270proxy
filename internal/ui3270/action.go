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

package ui3270

import (
	"fmt"
	"strings"

	"github.com/racingmars/go3270"
)

// listAction maps a HandleScreen response to a ListAction. The first non-blank
// CMD field wins (one line command per Enter).
func listAction(resp go3270.Response, nRows int) ListAction {
	switch resp.AID {
	case go3270.AIDPF3:
		return ListAction{PF: 3}
	case go3270.AIDPF4:
		return ListAction{PF: 4}
	case go3270.AIDPF7:
		return ListAction{PF: 7}
	case go3270.AIDPF8:
		return ListAction{PF: 8}
	}
	for i := 0; i < nRows; i++ {
		cmd := strings.ToUpper(strings.TrimSpace(resp.Values[fmt.Sprintf("%s%d", fieldCmdPrefix, i)]))
		if cmd != "" {
			return ListAction{Cmd: cmd[0], Row: i}
		}
	}
	return ListAction{}
}

// pfAID maps a PF number to its go3270 AID for the keys a detail action
// supports. Intentionally minimal — only the action keys RunDetail uses. ok is
// false for an unsupported or zero number.
func pfAID(n int) (go3270.AID, bool) {
	switch n {
	case 11:
		return go3270.AIDPF11, true
	}
	return 0, false
}

// detailAction maps a detail-screen response to a ListAction. PF3 is always the
// back key; actPF (when supported and pressed) returns {PF: actPF}; everything
// else is the zero action (re-present).
func detailAction(resp go3270.Response, actPF int) ListAction {
	if resp.AID == go3270.AIDPF3 {
		return ListAction{PF: 3}
	}
	if a, ok := pfAID(actPF); ok && resp.AID == a {
		return ListAction{PF: actPF}
	}
	return ListAction{}
}

// formAction maps a HandleScreen response to a FormAction. Hidden (password)
// fields are never trimmed — whitespace may be significant.
func formAction(resp go3270.Response, fields []FormField) FormAction {
	if resp.AID == go3270.AIDPF3 {
		return FormAction{Cancel: true}
	}
	vals := make(map[string]string, len(fields))
	for _, f := range fields {
		v := resp.Values[f.Name]
		if !f.Hidden {
			v = strings.TrimSpace(v)
		}
		vals[f.Name] = v
	}
	return FormAction{Values: vals}
}
