package theme

// AutoName is the opt-in theme value that follows the terminal: default on a
// dark background, paper on a light one. Only the TUI looks at the terminal,
// once, before it starts (see AutoPick); everywhere else auto is the dark
// default, so Get(AutoName) returns default.
const AutoName = "auto"

// AutoPick is the built-in that AutoName stands for on a terminal whose
// background is dark (true) or light (false).
func AutoPick(dark bool) string {
	if dark {
		return DefaultName
	}
	return "paper"
}
