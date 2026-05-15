package cli

import (
	"github.com/tristanbietsch/rex/internal/tui"
)

// RunTUI opens the Bubble Tea board (no-args entry).
func RunTUI() error {
	return tui.Run(DefaultSocket())
}
