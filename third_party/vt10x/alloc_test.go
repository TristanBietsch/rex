package vt10x

import (
	"strings"
	"testing"
)

// Every Write of agent output goes through the parser, in the rex daemon
// (one emulator per session) and in the TUI attach popup. Parser state
// changes and disabled debug traces used to allocate on almost every byte.
// What remains is CSI argument parsing, a few allocations per sequence.
func TestWriteAllocations(t *testing.T) {
	term := New(WithSize(80, 24))
	frame := "\x1b[2K\x1b[38;5;174m⏺\x1b[39m \x1b[1mUpdate\x1b[22m(file.go) plain text here\r\x1b[1B"
	const reps = 20
	chunk := []byte(strings.Repeat(frame, reps))
	csis := strings.Count(frame, "\x1b[") * reps
	allocs := testing.AllocsPerRun(50, func() {
		if _, err := term.Write(chunk); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("%d bytes, %d CSI sequences: %.0f allocs per Write", len(chunk), csis, allocs)
	if limit := float64(3 * csis); allocs > limit {
		t.Fatalf("%.0f allocs for %d bytes / %d CSI sequences, want at most %.0f", allocs, len(chunk), csis, limit)
	}
}
