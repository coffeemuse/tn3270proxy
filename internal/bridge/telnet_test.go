package bridge

import (
	"bytes"
	"testing"
)

// Telnet command bytes used in tests.
const (
	tIAC = 0xFF
	tEOR = 0xEF
)

func TestProcessForwardsDataAndDetectsPA3(t *testing.T) {
	p := newProcessor(roleServer, "", aidPA3)
	// A 3270 inbound record beginning with PA3 (0x6B), then some data,
	// terminated by IAC EOR.
	in := []byte{aidPA3, 0x01, 0x02, tIAC, tEOR}
	fwd, reply, escaped := p.process(in)

	if !escaped {
		t.Errorf("expected escape detection for PA3 at record start")
	}
	if len(reply) != 0 {
		t.Errorf("reply = %v, want none", reply)
	}
	want := []byte{aidPA3, 0x01, 0x02, tIAC, tEOR}
	if !bytes.Equal(fwd, want) {
		t.Errorf("fwd = % x, want % x", fwd, want)
	}
}

func TestProcessNoEscapeForNonPA3AID(t *testing.T) {
	p := newProcessor(roleServer, "", aidPA3)
	_, _, escaped := p.process([]byte{0x7D, 0x40, 0x40})
	if escaped {
		t.Errorf("Enter should not trigger escape")
	}
}

func TestProcessPA3MidRecordNotEscape(t *testing.T) {
	p := newProcessor(roleServer, "", aidPA3)
	_, _, escaped := p.process([]byte{0x7D, aidPA3, 0x6B})
	if escaped {
		t.Errorf("0x6B mid-record is data, not an escape AID")
	}
}

func TestProcessIACIACForwardedAndNotEscape(t *testing.T) {
	p := newProcessor(roleServer, "", aidPA3)
	// Record starting with an escaped literal 0xFF data byte (IAC IAC).
	fwd, _, escaped := p.process([]byte{tIAC, tIAC, 0x01})
	if escaped {
		t.Errorf("literal 0xFF must not be treated as escape AID")
	}
	want := []byte{tIAC, tIAC, 0x01}
	if !bytes.Equal(fwd, want) {
		t.Errorf("fwd = % x, want % x", fwd, want)
	}
}

func TestProcessEORStartsNewRecord(t *testing.T) {
	p := newProcessor(roleServer, "", aidPA3)
	// First record: Enter. End record. Second record begins with PA3.
	in := []byte{0x7D, 0x40, tIAC, tEOR, aidPA3}
	_, _, escaped := p.process(in)
	if !escaped {
		t.Errorf("PA3 at start of the second record should escape")
	}
}

// TestProcessNegotiationInterleavedWithData feeds a single process call a
// buffer that contains a Telnet negotiation command immediately followed by a
// 3270 data record. The negotiation must be answered locally (reply) and must
// NOT appear in the forwarded bytes; only the data record should be forwarded.
func TestProcessNegotiationInterleavedWithData(t *testing.T) {
	// roleClient: receives DO optBINARY → must reply IAC WILL optBINARY.
	p := newProcessor(roleClient, "IBM-3279-2-E", 0)
	in := []byte{
		cIAC, cDO, optBINARY, // negotiation: DO BINARY
		0x7D, 0x40, 0x40, cIAC, cEOR, // data record: Enter AID + field + EOR
	}
	fwd, reply, escaped := p.process(in)

	wantReply := []byte{cIAC, cWILL, optBINARY}
	if !bytes.Equal(reply, wantReply) {
		t.Errorf("reply = % x, want % x", reply, wantReply)
	}

	wantFwd := []byte{0x7D, 0x40, 0x40, cIAC, cEOR}
	if !bytes.Equal(fwd, wantFwd) {
		t.Errorf("fwd = % x, want % x", fwd, wantFwd)
	}

	if escaped {
		t.Errorf("escaped = true, want false (escapeAID is 0)")
	}
}

// TestProcessSplitChunks exercises cross-call state for two split scenarios.
//
// (b1) IAC EOR arrives split across two calls. The roleServer processor watches
// for aidPA3 at record starts. The first call ends with a lone IAC; the second
// call supplies the EOR to complete the record and then a PA3 that begins a new
// record and should trigger escape.
//
// (b2) A DO command split across two calls for roleClient: first call carries
// IAC DO, second call carries the option byte. The combined reply must be
// IAC WILL optEOR.
func TestProcessSplitChunks(t *testing.T) {
	t.Run("b1_IAC_EOR_split", func(t *testing.T) {
		p := newProcessor(roleServer, "", aidPA3)

		// Call 1: an Enter record data followed by a lone IAC (EOR not yet received).
		fwd1, reply1, escaped1 := p.process([]byte{0x7D, 0x40, cIAC})
		if escaped1 {
			t.Errorf("call 1: escaped = true, want false")
		}
		if len(reply1) != 0 {
			t.Errorf("call 1: reply = % x, want none", reply1)
		}

		// Call 2: EOR completes the previous record; aidPA3 begins the next one.
		fwd2, reply2, escaped2 := p.process([]byte{cEOR, aidPA3})
		if !escaped2 {
			t.Errorf("call 2: escaped = false, want true (PA3 at new record start)")
		}
		if len(reply2) != 0 {
			t.Errorf("call 2: reply = % x, want none", reply2)
		}

		// The forwarded bytes across both calls reconstruct the full stream.
		combined := append(fwd1, fwd2...)
		wantCombined := []byte{0x7D, 0x40, cIAC, cEOR, aidPA3}
		if !bytes.Equal(combined, wantCombined) {
			t.Errorf("combined fwd = % x, want % x", combined, wantCombined)
		}
	})

	t.Run("b2_DO_command_split", func(t *testing.T) {
		p := newProcessor(roleClient, "IBM-3279-2-E", 0)

		// Call 1: IAC DO arrives without the option byte.
		_, reply1, _ := p.process([]byte{cIAC, cDO})
		if len(reply1) != 0 {
			t.Errorf("call 1: reply = % x, want none (option byte not yet received)", reply1)
		}

		// Call 2: the option byte completes the DO command.
		_, reply2, _ := p.process([]byte{optEOR})
		wantReply := []byte{cIAC, cWILL, optEOR}
		if !bytes.Equal(reply2, wantReply) {
			t.Errorf("call 2: reply = % x, want % x", reply2, wantReply)
		}
	})
}
