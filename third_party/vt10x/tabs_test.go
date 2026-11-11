package vt10x

import (
	"strings"
	"testing"
)

// tabLanding writes "\r" + n tabs + "X" and returns the column X landed in.
func tabLanding(t *testing.T, term Terminal, n int) int {
	t.Helper()
	if _, err := term.Write([]byte("\r" + strings.Repeat("\t", n) + "X")); err != nil {
		t.Fatal(err)
	}
	cols, _ := term.Size()
	for x := 0; x < cols; x++ {
		if term.Cell(x, 0).Char == 'X' {
			return x
		}
	}
	t.Fatal("X not on row 0")
	return -1
}

// A grow past the original width used to write the new stops into the old
// slice, so every column beyond the old width had none and a tab from column
// 112 jumped to the last column. ultraviolet moves with hard tabs.
func TestResizeGrowExtendsTabStops(t *testing.T) {
	term := New(WithSize(120, 10))
	term.Resize(177, 10)
	if got := tabLanding(t, term, 15); got != 120 {
		t.Fatalf("15 tabs landed in column %d, want 120", got)
	}
	term.Write([]byte("\r\x1b[K")) //nolint:errcheck
	if got := tabLanding(t, term, 22); got != 176 {
		t.Fatalf("22 tabs landed in column %d, want 176", got)
	}
}

func TestResizeShrinkThenGrowTabs(t *testing.T) {
	term := New(WithSize(80, 5))
	term.Resize(40, 5)
	term.Resize(100, 5)
	for _, c := range []struct{ tabs, col int }{{4, 32}, {5, 40}, {6, 48}, {12, 96}} {
		term.Write([]byte("\r\x1b[K")) //nolint:errcheck
		if got := tabLanding(t, term, c.tabs); got != c.col {
			t.Fatalf("%d tabs landed in column %d, want %d", c.tabs, got, c.col)
		}
	}
}

// Clearing every stop and growing must not bring stops back inside the old
// width; only the new columns get default stops.
func TestTBCThenGrow(t *testing.T) {
	term := New(WithSize(40, 5))
	term.Write([]byte("\x1b[3g")) //nolint:errcheck
	term.Resize(100, 5)
	if got := tabLanding(t, term, 1); got != 40 {
		t.Fatalf("1 tab landed in column %d, want 40: the first stop is in the new area", got)
	}
	term.Write([]byte("\r\x1b[K")) //nolint:errcheck
	if got := tabLanding(t, term, 2); got != 48 {
		t.Fatalf("2 tabs landed in column %d, want 48", got)
	}
}

// Clearing one stop survives a grow.
func TestTBCCurrentStopSurvivesGrow(t *testing.T) {
	term := New(WithSize(40, 5))
	term.Write([]byte("\x1b[9G\x1b[g")) //nolint:errcheck // clear the stop at index 8
	term.Resize(60, 5)
	term.Write([]byte("\r")) //nolint:errcheck
	if got := tabLanding(t, term, 1); got != 16 {
		t.Fatalf("1 tab landed in column %d, want 16 (stop at 8 was cleared)", got)
	}
}

func TestDECST8CResetsTabs(t *testing.T) {
	term := New(WithSize(80, 5))
	term.Write([]byte("\x1b[3g")) //nolint:errcheck
	if got := tabLanding(t, term, 1); got != 79 {
		t.Fatalf("with no stops 1 tab landed in column %d, want 79", got)
	}
	term.Write([]byte("\r\x1b[K\x1b[?5W")) //nolint:errcheck
	if got := tabLanding(t, term, 1); got != 8 {
		t.Fatalf("after DECST8C 1 tab landed in column %d, want 8", got)
	}
	term.Write([]byte("\r\x1b[K")) //nolint:errcheck
	if got := tabLanding(t, term, 3); got != 24 {
		t.Fatalf("after DECST8C 3 tabs landed in column %d, want 24", got)
	}
}

// Only CSI ? 5 W is DECST8C; other W sequences are left alone.
func TestDECST8COnlyWithParam5(t *testing.T) {
	term := New(WithSize(80, 5))
	term.Write([]byte("\x1b[3g\x1b[?4W\x1b[5W\x1b[W")) //nolint:errcheck
	if got := tabLanding(t, term, 1); got != 79 {
		t.Fatalf("tab landed in column %d, want 79 (no stops restored)", got)
	}
}
