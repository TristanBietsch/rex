package inspect

import (
	"io"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

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
//   - The output is valid UTF-8 with no C0 control except tab and newline
//     (CR, BEL, BS, … are dropped), no C1 rune (U+0080 to U+009F, which
//     includes UTF-8-encoded CSI and OSC) and no bidi control. Each invalid
//     UTF-8 byte, raw 0x80 to 0x9F included, becomes U+FFFD.
//   - Other text passes through unchanged. A rune split between two calls
//     is held until its end arrives; finish turns one still cut off at the
//     end of the stream into U+FFFD.
//   - State carries across calls, so splitting the input anywhere gives
//     the same output as stripping it whole (`rex log -f` reads chunks).
//
// Like a terminal, it executes C0 controls (only newline and tab are kept)
// that appear inside a sequence, and CAN/SUB cancel one. Unlike a terminal, a newline
// ends an unterminated string and a non-ASCII byte ends a malformed CSI or
// escape, so one broken sequence can't hide the rest of a transcript.
//
// The zero value is ready to use.
type ansiStripper struct {
	state stripState
	pend  [utf8.UTFMax]byte // leading bytes of a rune cut off by the end of a write
	n     int               // how many bytes of pend are held
}

// strip appends the text of p to dst and returns the extended slice. A
// UTF-8 rune cut off at the end of p is held until the next call.
func (s *ansiStripper) strip(dst, p []byte) []byte {
	for _, b := range p {
		dst = s.step(dst, b)
	}
	return dst
}

// finish ends the stream: a rune still cut off becomes one U+FFFD per
// held byte, as if the text had continued with a byte that cannot follow.
func (s *ansiStripper) finish(dst []byte) []byte {
	for ; s.n > 0; s.n-- {
		dst = utf8.AppendRune(dst, utf8.RuneError)
	}
	return dst
}

// step advances the parser by one byte and appends any text it yields.
func (s *ansiStripper) step(dst []byte, b byte) []byte {
	switch s.state {
	case stGround:
		return s.ground(dst, b)
	case stEsc:
		return s.escape(dst, b)
	case stEscInter:
		return s.escIntermediate(dst, b)
	case stCSI:
		return s.csi(dst, b)
	case stString:
		return s.str(dst, b)
	case stStringEsc:
		return s.strEsc(dst, b)
	default:
		s.state = stGround
		return s.ground(dst, b)
	}
}

// keep reports whether the ASCII byte b is text: tab and newline only.
// CR, BEL, BS and the other C0 controls are dropped.
func keep(b byte) bool { return b == '\t' || b == '\n' }

// ground handles plain text. It decodes UTF-8, holding a partial rune
// across calls; an invalid byte becomes U+FFFD, and C1 runes (U+0080 to
// U+009F, including the UTF-8 form of CSI and OSC) and bidi controls are
// dropped.
func (s *ansiStripper) ground(dst []byte, b byte) []byte {
	if s.n == 0 {
		switch {
		case b == esc:
			s.state = stEsc
			return dst
		case b < utf8.RuneSelf:
			if b >= 0x20 || keep(b) {
				dst = append(dst, b)
			}
			return dst
		}
	}
	s.pend[s.n] = b
	s.n++
	if !utf8.FullRune(s.pend[:s.n]) {
		return dst
	}
	r, size := utf8.DecodeRune(s.pend[:s.n])
	if r == utf8.RuneError && size == 1 {
		// The first held byte is invalid. Replace it, then read the rest
		// again: any of them may start a rune or an escape.
		rest := s.pend
		n := s.n
		s.n = 0
		dst = utf8.AppendRune(dst, utf8.RuneError)
		for _, c := range rest[1:n] {
			dst = s.ground(dst, c)
		}
		return dst
	}
	s.n = 0
	if (r >= 0x80 && r <= 0x9f) || protocol.IsBidiControl(r) {
		return dst
	}
	return utf8.AppendRune(dst, r)
}

func (s *ansiStripper) escape(dst []byte, b byte) []byte {
	if out, ok := s.inSequence(dst, b); ok {
		return out
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
	return dst
}

func (s *ansiStripper) escIntermediate(dst []byte, b byte) []byte {
	if out, ok := s.inSequence(dst, b); ok {
		return out
	}
	if b > 0x2f { // final byte; 0x20–0x2f are more intermediates
		s.state = stGround
	}
	return dst
}

func (s *ansiStripper) csi(dst []byte, b byte) []byte {
	if out, ok := s.inSequence(dst, b); ok {
		return out
	}
	if b >= 0x40 { // final byte; 0x20–0x3f are parameters and intermediates
		s.state = stGround
	}
	return dst
}

// inSequence handles the bytes that mean the same in every ESC and CSI
// state, and reports ok=false for printable ASCII (0x20–0x7e), which the
// caller interprets.
func (s *ansiStripper) inSequence(dst []byte, b byte) (out []byte, ok bool) {
	switch {
	case b == esc: // abandon the current sequence, start a new one
		s.state = stEsc
		return dst, true
	case b == can || b == sub:
		s.state = stGround
		return dst, true
	case b < 0x20: // other C0 controls are executed mid-sequence
		if keep(b) {
			dst = append(dst, b)
		}
		return dst, true
	case b == del:
		return dst, true
	case b > del: // not part of any sequence: end it, keep the text
		s.state = stGround
		return s.ground(dst, b), true
	default:
		return dst, false
	}
}

func (s *ansiStripper) str(dst []byte, b byte) []byte {
	switch b {
	case bel, can, sub:
		s.state = stGround
	case esc:
		s.state = stStringEsc
	case '\n':
		s.state = stGround
		dst = append(dst, b)
	}
	return dst
}

// strEsc follows an ESC inside a string. ESC \ is the String Terminator;
// any other byte means the ESC began a new sequence.
func (s *ansiStripper) strEsc(dst []byte, b byte) []byte {
	if b == '\\' {
		s.state = stGround
		return dst
	}
	s.state = stEsc
	return s.escape(dst, b)
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
	if err := sw.emit(); err != nil {
		return 0, err
	}
	return len(p), nil
}

// flush ends the stream: a rune cut off at the end is written as U+FFFD.
func (sw *stripWriter) flush() error {
	sw.buf = sw.s.finish(sw.buf[:0])
	return sw.emit()
}

// emit writes the buffered text, if any.
func (sw *stripWriter) emit() error {
	if len(sw.buf) == 0 {
		return nil
	}
	_, err := sw.w.Write(sw.buf)
	return err
}
