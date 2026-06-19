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

import "context"

// RunForm loops a labeled-input form: render → on Cancel return nil → else
// Submit. A non-"" errMsg re-renders with that message; success returns nil
// (or, with cfg.StayOnSave, re-renders so the user saves in place and leaves
// via PF3). A non-nil error is a dead connection and propagates.
func RunForm(ctx context.Context, r Renderer, cfg FormConfig) error {
	errMsg := ""
	for {
		act, err := r.Form(FormView{Title: cfg.Title, Fields: cfg.Fields, Intro: cfg.Intro, PFHelp: cfg.PFHelp, ErrMsg: errMsg, DotLeader: cfg.DotLeader, Compact: cfg.Compact})
		if err != nil {
			return err
		}
		if act.Cancel {
			return nil
		}
		msg, ferr := cfg.Submit(ctx, act.Values)
		if ferr != nil {
			return ferr
		}
		if msg != "" {
			errMsg = msg
			continue
		}
		if cfg.StayOnSave {
			errMsg = "" // clear any prior error; save succeeded, stay on the form
			continue
		}
		return nil
	}
}
