// Package termfilter makes untrusted terminal output safe to print: it keeps
// text, colors and cursor movement, and drops the escape sequences that act
// outside the screen grid, such as clipboard writes (OSC 52), window titles,
// hyperlinks and device control strings.
package termfilter

import (
	"io"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// maxStringBytes caps the body of an OSC, DCS, APC, PM or SOS string. A
// longer body is abandoned and the bytes after it are treated as text, so an
// unterminated string cannot hide the rest of the stream.
const maxStringBytes = 64 << 10

// maxSeqBytes caps a CSI or ESC sequence held while it waits for its final
// byte. A longer one is dropped whole when it ends.
const maxSeqBytes = 1 << 10

// Bytes the filter reacts to.
const (
	bel = 0x07 // ends a string
	bs  = 0x08 // kept
	tab = 0x09 // kept
	lf  = 0x0a // kept
	cr  = 0x0d // kept
	can = 0x18 // cancels a sequence or string
	sub = 0x1a // cancels a sequence or string
	esc = 0x1b // starts every escape sequence
	del = 0x7f // ignored

	// c2 is the first byte of every UTF-8-encoded C1 rune; c2 followed by
	// st9c is U+009C, the 8-bit String Terminator.
	c2   = 0xc2
	st9c = 0x9c
)

// replacement is U+FFFD, written for invalid UTF-8.
const replacement = string(utf8.RuneError)

// state is where the filter is in the byte stream.
type state uint8

const (
	stGround    state = iota // text
	stUTF8                   // inside a multi-byte UTF-8 rune
	stEsc                    // after ESC
	stEscInter               // after ESC and intermediate bytes, e.g. ESC (
	stCSI                    // inside ESC [ … final byte
	stString                 // inside an OSC, DCS, APC, PM or SOS string
	stStringEsc              // after ESC inside a string: ST or a new sequence
	stStringC2               // after 0xC2 inside a string: U+009C may follow
)

// Writer filters terminal output before passing it to another io.Writer.
//
// It passes text, the C0 controls tab, newline, carriage return and
// backspace, every CSI sequence (colors, cursor movement, modes), ESC 7,
// ESC 8, ESC =, ESC >, the index moves ESC D, ESC E and ESC M, and charset
// designations (ESC ( B and the like). It drops everything else:
//
//   - OSC, DCS, APC, PM and SOS strings, whole, whether introduced by 7-bit
//     ESC ], ESC P, ESC _, ESC ^, ESC X or by the UTF-8-encoded C1 runes
//     U+009D, U+0090, U+009F, U+009E, U+0098. A string ends at ST (ESC \ or
//     U+009C), BEL, CAN or SUB, or at an ESC followed by anything other than
//     \, which then starts the next sequence. A body longer than
//     64 KiB is abandoned and the bytes after it are text.
//   - other two-byte and intermediate ESC sequences, such as ESC % @, which
//     would take the terminal out of UTF-8 mode;
//   - other C0 controls, DEL, C1 runes U+0080–U+009F and bidi controls;
//   - a raw byte 0x80–0x9F that is not part of valid UTF-8. It is one
//     invalid byte and never starts a string.
//
// Other invalid UTF-8 is written as U+FFFD, one for each invalid byte or
// truncated rune, so the output is always valid UTF-8.
//
// A sequence is written only once it is complete, so a dropped or
// abandoned one never leaves a fragment for the terminal to join with
// what follows. State carries across Writes: splitting the input anywhere
// gives the same output as writing it whole. A Writer is not safe for
// concurrent use.
type Writer struct {
	w     io.Writer
	state state

	seq         []byte // ESC sequence held until its final byte
	seqOverflow bool   // seq passed maxSeqBytes: drop it when it ends

	rune     [utf8.UTFMax]byte // UTF-8 rune being decoded
	runeLen  int               // bytes of rune received
	runeNeed int               // bytes rune needs, from its lead byte

	strLen int // body bytes of the current string

	out []byte // output of the current Write
}

// NewWriter returns a Writer that writes the filtered stream to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Write filters p and writes what remains to the underlying writer in one
// call. Bytes of an unfinished sequence or rune are held for the next Write.
// It returns len(p) on success, because every byte was consumed even when
// dropped, and 0 with the underlying writer's error on failure.
func (f *Writer) Write(p []byte) (int, error) {
	f.out = f.out[:0]
	for _, b := range p {
		f.step(b)
	}
	if len(f.out) == 0 {
		return len(p), nil
	}
	if _, err := f.w.Write(f.out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush ends the stream: it writes a pending partial UTF-8 rune as U+FFFD
// and drops a pending escape sequence or string. The next Write starts
// fresh, as text. Call it when the stream ends, not between chunks of one
// stream, or a string split across the Flush would end early.
func (f *Writer) Flush() error {
	partial := f.state == stUTF8
	f.state = stGround
	f.seq = f.seq[:0]
	if !partial {
		return nil
	}
	_, err := io.WriteString(f.w, replacement)
	return err
}

// step advances the filter by one byte.
func (f *Writer) step(b byte) {
	switch f.state {
	case stGround:
		f.ground(b)
	case stUTF8:
		f.utf8(b)
	case stEsc:
		f.escape(b)
	case stEscInter:
		f.escIntermediate(b)
	case stCSI:
		f.csi(b)
	case stString:
		f.str(b)
	case stStringEsc:
		f.strEsc(b)
	case stStringC2:
		f.strC2(b)
	}
}

func (f *Writer) ground(b byte) {
	switch {
	case b == esc:
		f.startEscape()
	case b < 0x20:
		f.control(b)
	case b < del:
		f.out = append(f.out, b)
	case b <= 0x9f:
		// DEL, or a raw C1 byte that is not part of a UTF-8 rune: dropped.
	default:
		if n := utf8Len(b); n > 0 {
			f.rune[0] = b
			f.runeLen = 1
			f.runeNeed = n
			f.state = stUTF8
			return
		}
		f.out = append(f.out, replacement...)
	}
}

// control handles a C0 control byte other than ESC.
func (f *Writer) control(b byte) {
	switch b {
	case tab, lf, cr, bs:
		f.out = append(f.out, b)
	}
}

// utf8 takes the next byte of a multi-byte rune. A byte that cannot
// continue it ends the rune as one U+FFFD and is then read afresh.
func (f *Writer) utf8(b byte) {
	if !utf8Continues(f.rune[0], f.runeLen, b) {
		f.out = append(f.out, replacement...)
		f.state = stGround
		f.ground(b)
		return
	}
	f.rune[f.runeLen] = b
	f.runeLen++
	if f.runeLen < f.runeNeed {
		return
	}
	f.state = stGround
	enc := f.rune[:f.runeLen]
	r, _ := utf8.DecodeRune(enc)
	switch {
	case isStringIntroducer(r):
		f.startString()
	case r <= 0x9f || protocol.IsBidiControl(r):
		// C1 control or bidi control: dropped.
	default:
		f.out = append(f.out, enc...)
	}
}

// utf8Len is the length of the rune that lead starts, or 0 when lead
// cannot start one.
func utf8Len(lead byte) int {
	switch {
	case lead >= 0xc2 && lead <= 0xdf:
		return 2
	case lead >= 0xe0 && lead <= 0xef:
		return 3
	case lead >= 0xf0 && lead <= 0xf4:
		return 4
	default:
		return 0
	}
}

// utf8Continues reports whether b is a valid byte at index i of a rune
// that starts with lead. The second byte's range depends on the lead, which
// rules out overlong forms, surrogates and runes above U+10FFFF.
func utf8Continues(lead byte, i int, b byte) bool {
	lo, hi := byte(0x80), byte(0xbf)
	if i == 1 {
		switch lead {
		case 0xe0:
			lo = 0xa0
		case 0xed:
			hi = 0x9f
		case 0xf0:
			lo = 0x90
		case 0xf4:
			hi = 0x8f
		}
	}
	return b >= lo && b <= hi
}

// isStringIntroducer reports whether r is a C1 control that opens a string:
// OSC, DCS, APC, PM or SOS.
func isStringIntroducer(r rune) bool {
	switch r {
	case '\u009d', '\u0090', '\u009f', '\u009e', '\u0098':
		return true
	default:
		return false
	}
}

func (f *Writer) startEscape() {
	f.seq = append(f.seq[:0], esc)
	f.seqOverflow = false
	f.state = stEsc
}

func (f *Writer) escape(b byte) {
	if f.inSequence(b) {
		return
	}
	switch {
	case b == '[':
		f.push(b)
		f.state = stCSI
	case b == ']' || b == 'P' || b == '_' || b == '^' || b == 'X':
		f.startString()
	case b <= 0x2f: // intermediate byte
		f.push(b)
		f.state = stEscInter
	default: // 0x30–0x7e: final byte of a two-byte escape
		if keepsEscFinal(b) {
			f.out = append(f.out, esc, b)
		}
		f.state = stGround
	}
}

// keepsEscFinal reports whether ESC b is a two-byte escape the filter
// passes: save and restore cursor (7, 8), keypad modes (=, >) and the index
// moves (D, E, M).
func keepsEscFinal(b byte) bool {
	switch b {
	case '7', '8', '=', '>', 'D', 'E', 'M':
		return true
	default:
		return false
	}
}

func (f *Writer) escIntermediate(b byte) {
	if f.inSequence(b) {
		return
	}
	f.push(b)
	if b <= 0x2f { // another intermediate
		return
	}
	if isCharsetDesignator(f.seq[1]) {
		f.emitSeq()
	}
	f.state = stGround
}

// isCharsetDesignator reports whether ESC b … designates a G0–G3
// character set (ESC ( B, ESC ) 0 and the like).
func isCharsetDesignator(b byte) bool {
	switch b {
	case '(', ')', '*', '+', '-', '.', '/':
		return true
	default:
		return false
	}
}

func (f *Writer) csi(b byte) {
	if f.inSequence(b) {
		return
	}
	f.push(b)
	if b >= 0x40 { // final byte; 0x20–0x3f are parameters and intermediates
		f.emitSeq()
		f.state = stGround
	}
}

// inSequence handles the bytes that mean the same in every ESC and CSI
// state and reports whether it consumed b. It leaves printable ASCII
// (0x20–0x7e) to the caller.
func (f *Writer) inSequence(b byte) bool {
	switch {
	case b == esc: // abandon the sequence, start a new one
		f.startEscape()
	case b == can || b == sub: // cancel the sequence
		f.state = stGround
	case b < 0x20: // a terminal acts on C0 controls mid-sequence
		f.control(b)
	case b == del: // ignored mid-sequence
	case b > del: // not part of any sequence: abandon it, read b as text
		f.state = stGround
		f.ground(b)
	default:
		return false
	}
	return true
}

// push adds b to the held sequence, or marks it too long.
func (f *Writer) push(b byte) {
	if len(f.seq) >= maxSeqBytes {
		f.seqOverflow = true
		return
	}
	f.seq = append(f.seq, b)
}

// emitSeq writes the completed sequence unless it was too long.
func (f *Writer) emitSeq() {
	if !f.seqOverflow {
		f.out = append(f.out, f.seq...)
	}
}

func (f *Writer) startString() {
	f.strLen = 0
	f.state = stString
}

// str drops one byte of a string body, ending the string at a terminator
// or when the body passes maxStringBytes.
func (f *Writer) str(b byte) {
	switch b {
	case bel, can, sub:
		f.state = stGround
		return
	case esc:
		f.state = stStringEsc
		return
	}
	if f.strLen >= maxStringBytes {
		f.state = stGround
		f.ground(b)
		return
	}
	f.strLen++
	if b == c2 {
		f.state = stStringC2
	}
}

// strEsc follows an ESC inside a string. ESC \ is the String Terminator;
// any other byte means the ESC began a new sequence.
func (f *Writer) strEsc(b byte) {
	if b == '\\' {
		f.state = stGround
		return
	}
	f.startEscape()
	f.escape(b)
}

// strC2 follows a 0xC2 byte inside a string. 0xC2 is always a UTF-8 lead
// byte, so 0xC2 0x9C is U+009C, the String Terminator.
func (f *Writer) strC2(b byte) {
	if b == st9c {
		f.state = stGround
		return
	}
	f.state = stString
	f.str(b)
}
