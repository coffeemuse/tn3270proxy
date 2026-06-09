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

// RunDetail renders cfg.View until PF3, optionally offering one confirm-gated
// PF-key action (cfg.ActPF). It returns refresh=true when an action committed,
// so the caller re-fetches its list. A non-nil error is a dead connection.
//
// Two-press gate: first ActPF press consults Confirm (blocked vetoes; prompt
// arms); second press commits via OnAct, shows the returned status, disarms the
// action (ActPF goes inert, PFHelp → DonePFHelp), and stays up until PF3.
func RunDetail(ctx context.Context, r Renderer, cfg DetailConfig) (bool, error) {
	v := cfg.View
	var armed, acted, refresh bool
	for {
		actPF := cfg.ActPF
		if acted {
			actPF = 0 // committed: the action key is no longer live
		}
		act, err := r.DetailAct(v, actPF)
		if err != nil {
			return refresh, err
		}
		switch {
		case act.PF == 3:
			return refresh, nil
		case actPF != 0 && act.PF == actPF:
			if armed {
				armed = false
				acted = true
				status, rf := cfg.OnAct(ctx)
				v.Message = status
				v.PFHelp = cfg.DonePFHelp
				refresh = rf
			} else {
				prompt, blocked := cfg.Confirm()
				if blocked != "" {
					v.Message = blocked
				} else {
					v.Message = prompt
					armed = true
				}
			}
		default:
			armed = false
			if !acted {
				// Post-commit the status (e.g. "DISCONNECTED") must persist until
				// PF3 — a plain Enter cancels a pending confirm but never wipes it.
				v.Message = ""
			}
		}
	}
}
