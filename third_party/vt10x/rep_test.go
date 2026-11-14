package vt10x

import (
	"strings"
	"testing"
	"time"
)

func trimRow(term Terminal, row int) string {
	cols, _ := term.Size()
	return strings.TrimRight(extractStr(term, 0, cols-1, row), "\x00 ")
}

// ultraviolet emits "X CSI n b" for a run of identical cells and assumes the
// cursor moved n more cells. Dropping REP left every later cell on the row
// n cells too far left.
func TestREPRepeatsLastGraphic(t *testing.T) {
	term := New(WithSize(20, 3))
	term.Write([]byte("ab\x1b[3bc")) //nolint:errcheck
	if got := trimRow(term, 0); got != "abbbbc" {
		t.Fatalf("row = %q, want abbbbc", got)
	}
	if cur := term.Cursor(); cur.X != 6 || cur.Y != 0 {
		t.Fatalf("cursor = %d,%d, want 6,0", cur.X, cur.Y)
	}
}

func TestREPDefaultAndZeroCountRepeatOnce(t *testing.T) {
	term := New(WithSize(20, 3))
	term.Write([]byte("a\x1b[b\x1b[0b")) //nolint:errcheck
	if got := trimRow(term, 0); got != "aaa" {
		t.Fatalf("row = %q, want aaa", got)
	}
}

func TestREPAfterControlIsNoop(t *testing.T) {
	for name, ctl := range map[string]string{"CR": "\r", "LF": "\n", "HT": "\t", "BS": "\b", "CAN": "\x18"} {
		term := New(WithSize(20, 3))
		term.Write([]byte("a" + ctl + "\x1b[3b")) //nolint:errcheck
		if got := trimRow(term, 0); got != "a" {
			t.Errorf("%s: row 0 = %q, want a", name, got)
		}
		if got := trimRow(term, 1); got != "" {
			t.Errorf("%s: row 1 = %q, want empty", name, got)
		}
	}
}

func TestREPWithNothingPrintedIsNoop(t *testing.T) {
	term := New(WithSize(20, 3))
	term.Write([]byte("\x1b[5b")) //nolint:errcheck
	if got := trimRow(term, 0); got != "" {
		t.Fatalf("row = %q, want empty", got)
	}
}

func TestREPClearedByReset(t *testing.T) {
	term := New(WithSize(20, 3))
	term.Write([]byte("a\x1bc\x1b[3b")) //nolint:errcheck // RIS
	if got := trimRow(term, 0); got != "" {
		t.Fatalf("row = %q, want empty after RIS", got)
	}
}

// Cursor moves and attribute changes between the character and REP do not
// clear it (xterm keeps the last graphic character across them).
func TestREPSurvivesSGRAndCursorMoves(t *testing.T) {
	term := New(WithSize(20, 3))
	term.Write([]byte("a\x1b[31m\x1b[3C\x1b[2b")) //nolint:errcheck
	if got := trimRow(term, 0); got != "a   aa" {
		t.Fatalf("row = %q, want %q", got, "a   aa")
	}
	if g := term.Cell(5, 0); g.FG != Red {
		t.Fatalf("repeated cell fg = %v, want red (the current pen)", g.FG)
	}
}

func TestREPWrapsWithAutowrapAndClipsWithout(t *testing.T) {
	term := New(WithSize(5, 3))
	term.Write([]byte("ab\x1b[5b")) //nolint:errcheck
	if got := trimRow(term, 0); got != "abbbb" {
		t.Fatalf("row 0 = %q, want abbbb", got)
	}
	if got := trimRow(term, 1); got != "bb" {
		t.Fatalf("row 1 = %q, want bb (wrapped)", got)
	}

	term = New(WithSize(5, 3))
	term.Write([]byte("\x1b[?7lab\x1b[9b")) //nolint:errcheck // DECAWM off
	if got := trimRow(term, 0); got != "abbbb" {
		t.Fatalf("row 0 = %q, want abbbb", got)
	}
	if got := trimRow(term, 1); got != "" {
		t.Fatalf("row 1 = %q, want empty (clipped at the margin)", got)
	}
}

// REP goes through the same print path, so the line-drawing charset applies
// again on every repeat.
func TestREPUsesGfxCharset(t *testing.T) {
	term := New(WithSize(20, 3))
	term.Write([]byte("\x1b(0q\x1b[2b\x1b(Bq")) //nolint:errcheck
	if got := trimRow(term, 0); got != "───q" {
		t.Fatalf("row = %q, want ───q", got)
	}
}

// A hostile or buggy agent must not be able to spin the daemon's reader for
// 2^31 cell writes: the count is capped at cols*rows.
func TestREPCountCapped(t *testing.T) {
	term := New(WithSize(80, 24))
	start := time.Now()
	term.Write([]byte("x\x1b[2147483647b")) //nolint:errcheck
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("REP with a huge count took %v", d)
	}
	for y := 0; y < 23; y++ {
		if got := trimRow(term, y); len(got) != 80 || strings.Trim(got, "x") != "" {
			t.Fatalf("row %d = %q, want 80 x", y, got)
		}
	}
}

func TestREPPrivateMarkerIgnored(t *testing.T) {
	term := New(WithSize(20, 3))
	term.Write([]byte("a\x1b[?3b\x1b[>3b")) //nolint:errcheck
	if got := trimRow(term, 0); got != "a" {
		t.Fatalf("row = %q, want a", got)
	}
}
