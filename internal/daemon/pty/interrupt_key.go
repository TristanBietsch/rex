package pty

// interruptKeys are the input chunks that interrupt a working agent: Esc
// and Ctrl+C, plain (the TUI attach popup, a raw terminal) or in the kitty
// keyboard and xterm modifyOtherKeys encodings a raw `rex attach` terminal
// may use once the agent turned them on. A whole chunk must match: a key
// read from a terminal arrives alone, and an Esc that starts another
// sequence (an arrow key, a paste) is not an interrupt.
var interruptKeys = map[string]bool{
	"\x1b":          true, // Esc
	"\x03":          true, // Ctrl+C
	"\x1b[27u":      true, // kitty Esc
	"\x1b[27;1u":    true,
	"\x1b[27;1:1u":  true, // kitty Esc, press event
	"\x1b[99;5u":    true, // kitty Ctrl+C
	"\x1b[99;5:1u":  true,
	"\x1b[27;5;99~": true, // modifyOtherKeys Ctrl+C
}

// isInterruptKey reports whether b, one input chunk from a client, is an
// interrupt key.
func isInterruptKey(b []byte) bool {
	return len(b) <= 16 && interruptKeys[string(b)]
}
