package bridge

// Telnet command and option constants (RFC 854 / TN3270).
const (
	cIAC  = 255 // Interpret As Command
	cDONT = 254
	cDO   = 253
	cWONT = 252
	cWILL = 251
	cSB   = 250 // begin subnegotiation
	cSE   = 240 // end subnegotiation
	cEOR  = 239 // End Of Record command (3270 record terminator)

	optBINARY   = 0
	optSGA      = 3
	optTERMTYPE = 24
	optEOR      = 25 // End Of Record option (negotiated; distinct from cEOR)

	ttIS   = 0
	ttSEND = 1
)

// aidPA3 is the 3270 AID byte for the PA3 key — the gateway's escape key.
const aidPA3 = 0x6B

// role identifies which side of a connection a processor models.
type role int

const (
	roleClient role = iota // proxy acting as a TN3270 client (toward backend)
	roleServer             // proxy acting as a TN3270 server (toward end user)
)

// pstate is the Telnet parser state.
type pstate int

const (
	stData pstate = iota
	stIAC
	stOption
	stSubData
	stSubIAC
)

// telnetProcessor parses one direction of a Telnet/TN3270 stream. It separates
// forwardable 3270 data (including IAC IAC and IAC EOR framing) from Telnet
// negotiation, which is answered locally. When escapeAID is non-zero it reports
// when a 3270 inbound record begins with that AID byte.
type telnetProcessor struct {
	role      role
	termType  string
	escapeAID byte

	state      pstate
	optCmd     byte   // pending WILL/WONT/DO/DONT command
	subneg     []byte // collected subnegotiation payload
	atRecStart bool   // next data byte begins a 3270 record
}

func newProcessor(r role, termType string, escapeAID byte) *telnetProcessor {
	return &telnetProcessor{
		role:       r,
		termType:   termType,
		escapeAID:  escapeAID,
		state:      stData,
		atRecStart: true,
	}
}

// process consumes a chunk of input bytes from this leg and returns:
//   - forward: bytes to write to the peer leg (3270 data + framing)
//   - reply:   Telnet negotiation bytes to write back on THIS leg
//   - escaped: true if a record began with escapeAID
//
// negotiate() and subnegReply() are implemented in Task 10; until then they
// return nil.
func (p *telnetProcessor) process(in []byte) (forward, reply []byte, escaped bool) {
	for _, b := range in {
		switch p.state {
		case stData:
			if b == cIAC {
				p.state = stIAC
				continue
			}
			if p.atRecStart {
				p.atRecStart = false
				if p.escapeAID != 0 && b == p.escapeAID {
					escaped = true
				}
			}
			forward = append(forward, b)

		case stIAC:
			switch b {
			case cIAC: // escaped literal 0xFF data byte
				if p.atRecStart {
					p.atRecStart = false // 0xFF is never the escape AID
				}
				forward = append(forward, cIAC, cIAC)
				p.state = stData
			case cEOR: // end of 3270 record
				forward = append(forward, cIAC, cEOR)
				p.atRecStart = true
				p.state = stData
			case cWILL, cWONT, cDO, cDONT:
				p.optCmd = b
				p.state = stOption
			case cSB:
				p.subneg = p.subneg[:0]
				p.state = stSubData
			default:
				p.state = stData // other commands (e.g. NOP) ignored
			}

		case stOption:
			reply = append(reply, p.negotiate(p.optCmd, b)...)
			p.state = stData

		case stSubData:
			if b == cIAC {
				p.state = stSubIAC
			} else {
				p.subneg = append(p.subneg, b)
			}

		case stSubIAC:
			if b == cSE {
				reply = append(reply, p.subnegReply()...)
				p.state = stData
			} else if b == cIAC {
				p.subneg = append(p.subneg, cIAC)
				p.state = stSubData
			} else {
				p.state = stData
			}
		}
	}
	return forward, reply, escaped
}

// agreeable reports whether this leg will enable the given option.
func (p *telnetProcessor) agreeable(opt byte) bool {
	switch opt {
	case optBINARY, optEOR, optSGA:
		return true
	case optTERMTYPE:
		// The client leg offers a terminal type; the server leg accepts a
		// peer's offer to send one.
		return true
	}
	return false
}

// negotiate answers a WILL/WONT/DO/DONT command on this leg.
func (p *telnetProcessor) negotiate(cmd, opt byte) []byte {
	switch cmd {
	case cDO:
		if p.agreeable(opt) {
			return []byte{cIAC, cWILL, opt}
		}
		return []byte{cIAC, cWONT, opt}
	case cWILL:
		if p.agreeable(opt) {
			return []byte{cIAC, cDO, opt}
		}
		return []byte{cIAC, cDONT, opt}
	case cWONT, cDONT:
		// Accept the peer's refusal silently to avoid negotiation loops.
		return nil
	}
	return nil
}

// subnegReply answers a subnegotiation. The only one we handle is a
// TERMINAL-TYPE SEND request, to which we reply with our terminal type.
func (p *telnetProcessor) subnegReply() []byte {
	if len(p.subneg) >= 2 && p.subneg[0] == optTERMTYPE && p.subneg[1] == ttSEND {
		out := []byte{cIAC, cSB, optTERMTYPE, ttIS}
		out = append(out, []byte(p.termType)...)
		out = append(out, cIAC, cSE)
		return out
	}
	return nil
}
