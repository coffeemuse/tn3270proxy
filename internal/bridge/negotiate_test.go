package bridge

import (
	"bytes"
	"testing"
)

func TestNegotiateClientAgreesToCoreOptions(t *testing.T) {
	p := newProcessor(roleClient, "IBM-3278-2-E", 0)
	// Backend (server) sends: DO TERMTYPE, DO EOR, DO BINARY.
	in := []byte{
		cIAC, cDO, optTERMTYPE,
		cIAC, cDO, optEOR,
		cIAC, cDO, optBINARY,
	}
	fwd, reply, _ := p.process(in)
	if len(fwd) != 0 {
		t.Errorf("negotiation must not be forwarded; fwd = % x", fwd)
	}
	want := []byte{
		cIAC, cWILL, optTERMTYPE,
		cIAC, cWILL, optEOR,
		cIAC, cWILL, optBINARY,
	}
	if !bytes.Equal(reply, want) {
		t.Errorf("reply = % x, want % x", reply, want)
	}
}

func TestNegotiateClientRefusesUnknownDO(t *testing.T) {
	p := newProcessor(roleClient, "IBM-3278-2-E", 0)
	_, reply, _ := p.process([]byte{cIAC, cDO, 99})
	want := []byte{cIAC, cWONT, 99}
	if !bytes.Equal(reply, want) {
		t.Errorf("reply = % x, want % x", reply, want)
	}
}

func TestNegotiateTermTypeSubneg(t *testing.T) {
	p := newProcessor(roleClient, "IBM-3278-2-E", 0)
	// Backend: SB TERMINAL-TYPE SEND IAC SE
	in := []byte{cIAC, cSB, optTERMTYPE, ttSEND, cIAC, cSE}
	_, reply, _ := p.process(in)

	want := []byte{cIAC, cSB, optTERMTYPE, ttIS}
	want = append(want, []byte("IBM-3278-2-E")...)
	want = append(want, cIAC, cSE)
	if !bytes.Equal(reply, want) {
		t.Errorf("reply = % x, want % x", reply, want)
	}
}

func TestNegotiateWillEORGetsDo(t *testing.T) {
	p := newProcessor(roleClient, "IBM-3278-2-E", 0)
	_, reply, _ := p.process([]byte{cIAC, cWILL, optEOR})
	want := []byte{cIAC, cDO, optEOR}
	if !bytes.Equal(reply, want) {
		t.Errorf("reply = % x, want % x", reply, want)
	}
}
