package server

import (
	"slices"

	"github.com/racingmars/go3270"
)

// silentAIDList are data-less attention keys that must be silent no-ops on the
// proxy's own screens. PA3 is also the bridge escape key, but bridge.Bridge
// handles that independently — this only runs on proxy screens. CLEAR must
// re-present because the terminal blanks its display client-side.
//
// These MUST be passed to go3270.HandleScreenAlt as exit keys (see
// withSilentExits). go3270's loop only RETURNS an AID to the caller when it is
// an exit key or an expected pf key; any other AID — including PA1/PA2/PA3/
// Clear — falls into its internal "unknown key" branch, which flashes
// "<key>: unknown key" in the error field and re-presents WITHOUT returning.
// So wrapping the call in handleScreen alone is not enough: without these in
// exitkeys, handleScreen never sees the silent AID and the error still flashes.
var silentAIDList = []go3270.AID{
	go3270.AIDPA1,
	go3270.AIDPA2,
	go3270.AIDPA3,
	go3270.AIDClear,
}

// isSilentAID reports whether aid is one of the data-less attention keys that
// must be a silent no-op on the proxy's own screens.
func isSilentAID(aid go3270.AID) bool {
	return slices.Contains(silentAIDList, aid)
}

// withSilentExits returns exitkeys with the silent attention AIDs appended, so
// go3270.HandleScreenAlt returns control to handleScreen on PA1/PA2/PA3/Clear
// instead of flashing "unknown key" in its own internal loop. The caller's
// slice is not mutated.
func withSilentExits(exitkeys []go3270.AID) []go3270.AID {
	out := make([]go3270.AID, 0, len(exitkeys)+len(silentAIDList))
	out = append(out, exitkeys...)
	out = append(out, silentAIDList...)
	return out
}

// handleScreen calls present in a loop, silently re-presenting whenever
// PA1/PA2/PA3/Clear comes back. All other AIDs and errors pass through. The
// present closure MUST pass withSilentExits(...) as HandleScreenAlt's exitkeys
// for the silent AIDs to reach this loop at all.
func handleScreen(present func() (go3270.Response, error)) (go3270.Response, error) {
	for {
		resp, err := present()
		if err != nil || !isSilentAID(resp.AID) {
			return resp, err
		}
	}
}
