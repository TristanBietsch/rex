package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPanelStyleSquareBorderExactWidth(t *testing.T) {
	out := panelStyle(40).Render("hello")
	if !strings.Contains(out, "┌") || strings.Contains(out, "╭") {
		t.Fatalf("expected square border:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w != 40 {
			t.Fatalf("line width %d != 40: %q", w, l)
		}
	}
}

func TestSectionHeaderWidth(t *testing.T) {
	for _, w := range []int{20, 40, 80} {
		if got := lipgloss.Width(sectionHeader("Keys", w)); got != w {
			t.Fatalf("width %d: got %d", w, got)
		}
	}
}

func TestPanelWidth(t *testing.T) {
	cases := []struct{ term, want, min, exp int }{
		{200, 80, 40, 80},
		{60, 80, 40, 56},
		{30, 80, 40, 28}, // the floor gives way: never wider than term-2
	}
	for _, c := range cases {
		if got := panelWidth(c.term, c.want, c.min); got != c.exp {
			t.Fatalf("panelWidth(%d,%d,%d)=%d want %d", c.term, c.want, c.min, got, c.exp)
		}
	}
}
