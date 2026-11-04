package inspect

import "io"

// Control bytes the escape-sequence stripper reacts to.
const (
	bel = 0x07 // terminates an OSC string (xterm)
	can = 0x18 // cancels the sequence in progress
	sub = 0x1a // cancels the sequence in progress
	esc = 0x1b // starts every escape sequence
	del = 0x7f // ignored inside sequences
)

// stripState is where the stripper is in an escape sequence.
type stripState uint8

const (
	stGround    stripState = iota // plain text
	stEsc                         // after ESC
	stEscInter                    // after ESC and intermediate bytes, e.g. ESC (
	stCSI                         // inside ESC [ … final byte
	stString                      // inside an OSC/DCS/SOS/PM/APC string
	stStringEsc                   // after ESC inside a string: ST or a new sequence
)

// ansiStripper removes terminal escape sequences from a byte stream so
// `rex log` prints transcript text without driving the user's terminal.
//
// It is a small ECMA-48 / DEC parser that drops whole sequences: CSI
// (ESC [ … final), strings (OSC ESC ], DCS ESC P, SOS ESC X, PM ESC ^,
// APC ESC _, ended by BEL or ST), and two-byte or intermediate escapes
// (ESC 7, ESC ( B). Invariants, checked by FuzzStripANSI:
//
//   - The output never contains ESC. Every ESC opens a sequence that is
//     dropped, so removed bytes can't leave a fused escape behind (F-02:
//     "\x1b\x1b[A[" used to become "\x1b[").
//   - Text without ESC passes through unchanged.
//   - State carries across calls, so splitting the input anywhere gives
//     the same output as stripping it whole (`rex log -f` reads chunks).
//
// Like a terminal, it executes C0 controls (newline, tab, …) that appear
// inside a sequence, and CAN/SUB cancel one. Unlike a terminal, a newline
// ends an unterminated string and a non-ASCII byte ends a malformed CSI or
// escape, so one broken sequence can't hide the rest of a transcript.
//
// The zero value is ready to use.
type ansiStripper struct {
	state stripState
}

// strip appends the text bytes of p to dst and returns the extended slice.
func (s *ansiStripper) strip(dst, p []byte) []byte {
	for _, b := range p {
		if s.step(b) {
			dst = append(dst, b)
		}
	}
	return dst
}

// step advances the parser by one byte and reports whether b is text.
func (s *ansiStripper) step(b byte) (emit bool) {
	switch s.state {
	case stGround:
		return s.ground(b)
	case stEsc:
		return s.escape(b)
	case stEscInter:
		return s.escIntermediate(b)
	case stCSI:
		return s.csi(b)
	case stString:
		return s.str(b)
	case stStringEsc:
		return s.strEsc(b)
	default:
		s.state = stGround
		return s.ground(b)
	}
}

func (s *ansiStripper) ground(b byte) bool {
	if b == esc {
		s.state = stEsc
		return false
	}
	return true
}

func (s *ansiStripper) escape(b byte) bool {
	if emit, ok := s.inSequence(b); ok {
		return emit
	}
	switch {
	case b == '[':
		s.state = stCSI
	case b == ']' || b == 'P' || b == 'X' || b == '^' || b == '_':
		s.state = stString
	case b >= 0x20 && b <= 0x2f:
		s.state = stEscInter
	default: // 0x30–0x7e: final byte of a two-byte escape
		s.state = stGround
	}
	return false
}

func (s *ansiStripper) escIntermediate(b byte) bool {
	if emit, ok := s.inSequence(b); ok {
		return emit
	}
	if b > 0x2f { // final byte; 0x20–0x2f are more intermediates
		s.state = stGround
	}
	return false
}

func (s *ansiStripper) csi(b byte) bool {
	if emit, ok := s.inSequence(b); ok {
		return emit
	}
	if b >= 0x40 { // final byte; 0x20–0x3f are parameters and intermediates
		s.state = stGround
	}
	return false
}

// inSequence handles the bytes that mean the same in every ESC and CSI
// state, and reports ok=false for printable ASCII (0x20–0x7e), which the
// caller interprets.
func (s *ansiStripper) inSequence(b byte) (emit, ok bool) {
	switch {
	case b == esc: // abandon the current sequence, start a new one
		s.state = stEsc
		return false, true
	case b == can || b == sub:
		s.state = stGround
		return false, true
	case b < 0x20: // other C0 controls are executed mid-sequence
		return true, true
	case b == del:
		return false, true
	case b > del: // not part of any sequence: end it, keep the text
		s.state = stGround
		return true, true
	default:
		return false, false
	}
}

func (s *ansiStripper) str(b byte) bool {
	switch b {
	case bel, can, sub:
		s.state = stGround
		return false
	case esc:
		s.state = stStringEsc
		return false
	case '\n':
		s.state = stGround
		return true
	default:
		return false
	}
}

// strEsc follows an ESC inside a string. ESC \ is the String Terminator;
// any other byte means the ESC began a new sequence.
func (s *ansiStripper) strEsc(b byte) bool {
	if b == '\\' {
		s.state = stGround
		return false
	}
	s.state = stEsc
	return s.escape(b)
}

// stripWriter strips escape sequences from everything written to it
// before passing the text on, keeping parser state across Writes.
type stripWriter struct {
	w   io.Writer
	s   ansiStripper
	buf []byte
}

func newStripWriter(w io.Writer) *stripWriter { return &stripWriter{w: w} }

// Write strips p and writes the remaining text. It reports len(p) on
// success because every input byte was consumed, even when stripped.
func (sw *stripWriter) Write(p []byte) (int, error) {
	sw.buf = sw.s.strip(sw.buf[:0], p)
	if len(sw.buf) == 0 {
		return len(p), nil
	}
	if _, err := sw.w.Write(sw.buf); err != nil {
		return 0, err
	}
	return len(p), nil
}
