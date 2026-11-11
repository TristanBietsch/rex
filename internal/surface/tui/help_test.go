package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderHelpFits(t *testing.T) {
	for _, sz := range [][2]int{{120, 40}, {80, 30}, {50, 20}} {
		w, h := sz[0], sz[1]
		out := renderHelp(Model{}, w, h)
		lines := strings.Split(out, "\n")
		if len(lines) > h {
			t.Errorf("%dx%d: height %d > %d", w, h, len(lines), h)
		}
		for i, l := range lines {
			if lw := ansi.StringWidth(l); lw > w {
				t.Errorf("%dx%d: line %d width %d > %d", w, h, i, lw, w)
			}
		}
		plain := ansi.Strip(out)
		if !strings.Contains(plain, "┌") || !strings.Contains(plain, "┘") {
			t.Errorf("%dx%d: missing square corners", w, h)
		}
		if strings.Contains(plain, "╭") {
			t.Errorf("%dx%d: rounded corner", w, h)
		}
		if !strings.Contains(plain, "Navigation") {
			t.Errorf("%dx%d: missing Navigation", w, h)
		}
	}
}

func TestRenderHelpGroupTitles(t *testing.T) {
	plain := ansi.Strip(renderHelp(Model{}, 120, 64))
	for _, gs := range [][]helpGroup{helpLeft(Model{}), helpRight} {
		for _, g := range gs {
			if !strings.Contains(plain, g.Title) {
				t.Errorf("missing group %q", g.Title)
			}
		}
	}
}

func TestRenderHelpCommandVerbs(t *testing.T) {
	plain := ansi.Strip(renderHelp(Model{}, 120, 60))
	for _, v := range []string{"q", "quit", "q!", "bg", "detach", "help", "settings", "reload",
		"filter", "rm", "rename", "name", "done", "undone", "complete", "new", "fail", "fails",
		"failed", "stats", "search"} {
		if !strings.Contains(plain, ":"+v) {
			t.Errorf("missing command :%s", v)
		}
	}
}

func TestRenderHelpTwoColShorter(t *testing.T) {
	two := lipgloss.Height(helpBox(Model{}, 110, 2, map[string]bool{}, false))
	one := lipgloss.Height(helpBox(Model{}, 110, 1, map[string]bool{}, false))
	if two >= one {
		t.Errorf("two-col height %d not < single-col %d", two, one)
	}
}
