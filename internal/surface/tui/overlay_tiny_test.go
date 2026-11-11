package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// Tiny terminals never panic and never draw past the width, in every overlay
// and the bottom-bar confirms.
func TestOverlaysSurviveTinyTerminals(t *testing.T) {
	for w := 1; w <= 20; w++ {
		for h := 1; h <= 10; h++ {
			models := overlayModels(t, w, h)
			quit := models["help"]
			quit.Focus = FocusConfirmQuit
			models["confirm quit"] = quit
			del := models["help"]
			del.Focus = FocusConfirmDelete
			models["confirm delete"] = del
			models["board"] = layoutModel(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), w)
			for name, m := range models {
				m.Height = h
				require.NotPanics(t, func() {
					for i, l := range strings.Split(m.View(), "\n") {
						require.LessOrEqual(t, ansi.StringWidth(l), w, "%s %dx%d line %d", name, w, h, i)
					}
				}, "%s %dx%d", name, w, h)
			}
		}
	}
}
