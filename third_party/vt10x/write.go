package vt10x

import "unicode/utf8"

// write parses p into the state. A UTF-8 rune split across two writes (a
// PTY read can end anywhere) is kept until the rest arrives; upstream
// dropped its first bytes and then skipped the rest as invalid, losing the
// rune and shifting the row. Invalid bytes are skipped. The caller holds
// the lock. (rex fork.)
func (t *State) write(p []byte) {
	buf := p
	if len(t.partial) > 0 {
		buf = append(t.partial, p...)
		t.partial = nil
	}
	for len(buf) > 0 {
		c, sz := utf8.DecodeRune(buf)
		if c == utf8.RuneError && sz == 1 {
			if !utf8.FullRune(buf) {
				// The start of a rune whose remaining bytes are still to come.
				t.partial = append([]byte(nil), buf...)
				return
			}
			t.logln("invalid utf8 sequence")
			buf = buf[1:]
			continue
		}
		t.put(c)
		buf = buf[sz:]
	}
}

// PartialRune returns the bytes of an incomplete UTF-8 rune at the end of
// the last Write, held until the next Write completes it; nil when there is
// none. (rex fork addition.)
func (t *State) PartialRune() []byte {
	return append([]byte(nil), t.partial...)
}
