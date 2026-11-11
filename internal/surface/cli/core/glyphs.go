package core

import "github.com/tristanbietsch/rex/internal/wire/protocol"

// Glyphs is the symbol vocabulary of the human formats. The session-state
// markers match the TUI legend so the CLI and the board read the same.
type Glyphs struct {
	OK, Warn, Fail, Info string // check statuses
	Hint                 string // closing "next command" lines
	Sep                  string // inline separator between facts
	State                map[protocol.State]string
}

// spinnerFirstFrame is the first frame of each spinner set in the TUI's
// `spinner` setting. The CLI shows one still frame for "working".
var spinnerFirstFrame = map[string]string{
	"braille":    "⠋",
	"ascii_line": "|",
	"moon":       "◐",
	"pulse":      "·",
	"blocks":     "░",
}

// GlyphsFor returns the Unicode set, or the ASCII set when ascii is true.
// spinner is the `spinner` setting; an unknown or empty name means braille.
func GlyphsFor(ascii bool, spinner string) Glyphs {
	if ascii {
		return Glyphs{
			OK: "v", Warn: "!", Fail: "x", Info: "-", Hint: ">", Sep: "-",
			State: map[protocol.State]string{
				protocol.StateNeedsInput: "!",
				protocol.StateWorking:    "*",
				protocol.StateQueued:     ".",
				protocol.StateDone:       "o",
				protocol.StateFailed:     "x",
				protocol.StateCrashed:    "-",
			},
		}
	}
	work, ok := spinnerFirstFrame[spinner]
	if !ok {
		work = spinnerFirstFrame["braille"]
	}
	return Glyphs{
		OK: "✓", Warn: "!", Fail: "✗", Info: "·", Hint: "›", Sep: "·",
		State: map[protocol.State]string{
			protocol.StateNeedsInput: "◆",
			protocol.StateWorking:    work,
			protocol.StateQueued:     "◌",
			protocol.StateDone:       "●",
			protocol.StateFailed:     "✕",
			protocol.StateCrashed:    "○",
		},
	}
}

// useASCII reports whether the human format should use the ASCII glyph set:
// REX_ASCII=1, or a Windows console that is not a known Unicode-capable
// terminal (Windows Terminal sets WT_SESSION; others set TERM_PROGRAM).
func useASCII(getenv func(string) string, goos string) bool {
	if getenv("REX_ASCII") == "1" {
		return true
	}
	return goos == "windows" && getenv("WT_SESSION") == "" && getenv("TERM_PROGRAM") == ""
}
