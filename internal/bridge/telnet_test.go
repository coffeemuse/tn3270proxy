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
