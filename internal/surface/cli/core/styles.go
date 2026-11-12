package core

import (
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Tone is the meaning a piece of text carries; a renderer maps it to a style.
type Tone uint8

// Tones. ToneNone is plain text.
const (
	ToneNone Tone = iota
	ToneDim
	ToneMuted
	ToneAccent
	ToneOK
	ToneWarn
	ToneErr
	// Session-state tones, colored like the TUI's state markers.
	ToneNeeds
	ToneWorking
	ToneQueued
	ToneDone
	ToneFailed
	ToneCrashed
)

// Styles is the CLI's style set, built from a theme palette. Every field is
// bound to one stream's renderer, so a stream that is not a terminal never
// receives ANSI.
type Styles struct {
	Bold, Title, Dim, Muted, Accent, HR, OK, Warn, Err lipgloss.Style
	State                                              map[protocol.State]lipgloss.Style
}

// Tone returns the style for t.
func (s Styles) Tone(t Tone) lipgloss.Style {
	switch t {
	case ToneDim:
		return s.Dim
	case ToneMuted:
		return s.Muted
	case ToneAccent:
		return s.Accent
	case ToneOK:
		return s.OK
	case ToneWarn:
		return s.Warn
	case ToneErr:
		return s.Err
	case ToneNeeds:
		return s.State[protocol.StateNeedsInput]
	case ToneWorking:
		return s.State[protocol.StateWorking]
	case ToneQueued:
		return s.State[protocol.StateQueued]
	case ToneDone:
		return s.State[protocol.StateDone]
	case ToneFailed:
		return s.State[protocol.StateFailed]
	case ToneCrashed:
		return s.State[protocol.StateCrashed]
	case ToneNone:
	}
	// A style with no attributes renders its text unchanged on any renderer.
	return lipgloss.NewStyle()
}

// NewStyles builds the style set from palette p on renderer r.
//
// Mapping (plan U1 section 2): Bold and Title use Fg, Dim FgDim, Muted
// FgMuted, Accent Accent, HR Border, OK Success, Warn Warn, Err Danger, and
// each session state its own token.
func NewStyles(p theme.Palette, r *lipgloss.Renderer) Styles {
	fg := func(c string) lipgloss.Style { return r.NewStyle().Foreground(lipgloss.Color(c)) }
	return Styles{
		Bold:   fg(p.Fg).Bold(true),
		Title:  fg(p.Fg).Bold(true),
		Dim:    fg(p.FgDim).Faint(p.Faint),
		Muted:  fg(p.FgMuted).Faint(p.Faint),
		Accent: fg(p.Accent).Bold(true),
		HR:     fg(p.Border).Faint(p.Faint),
		OK:     fg(p.Success),
		Warn:   fg(p.Warn),
		Err:    fg(p.Danger),
		State: map[protocol.State]lipgloss.Style{
			protocol.StateNeedsInput: fg(p.Needs),
			protocol.StateWorking:    fg(p.Working),
			protocol.StateQueued:     fg(p.Queued),
			protocol.StateDone:       fg(p.Done),
			protocol.StateFailed:     fg(p.Failed),
			protocol.StateCrashed:    fg(p.Crashed),
		},
	}
}

// streamStyles builds the styles for one stream from the named scheme (an
// unknown name means the default). A stream with color off gets an
// Ascii-profile renderer, so it can never emit ANSI. trueColor pins the
// TrueColor profile instead of detecting one (tests and goldens).
func streamStyles(w io.Writer, color, trueColor bool, scheme string) Styles {
	r := lipgloss.NewRenderer(w)
	switch {
	case !color:
		r.SetColorProfile(termenv.Ascii)
	case trueColor:
		r.SetColorProfile(termenv.TrueColor)
	case r.ColorProfile() == termenv.Ascii:
		// Forced (CLICOLOR_FORCE) into a pipe: detection finds nothing.
		r.SetColorProfile(termenv.ANSI256)
	}
	return NewStyles(resolvePalette(scheme), r)
}

// resolvePalette finds the named theme: a built-in, else one of the user's
// theme files (~/.config/rex/themes). A name that is neither, a file that
// failed validation, or a home directory that cannot be found all mean the
// default palette, silently: a command's output must not change shape, or
// grow a warning, because of a theme file. (rex theme list reports the
// skipped files.) Built-in names skip the directory read.
func resolvePalette(name string) theme.Palette {
	if _, builtin := theme.Builtin[name]; !builtin {
		if dir, err := paths.ThemesDir(); err == nil {
			cat := theme.LoadDir(dir)
			// config.lua's themes (memoised; its problems stay silent here).
			if _, rep, _, err := settings.LoadDefaultReport(); err == nil && len(rep.Themes) > 0 {
				cat = cat.WithLua(rep.Themes, nil)
			}
			p, _ := cat.Get(name)
			return p
		}
	}
	p, _ := theme.Get(name)
	return p
}

// loadAppearance reads theme and spinner from the user's config. Any
// failure means the defaults: styling must never break a command.
func loadAppearance() (scheme, spinner string) {
	store, _, err := settings.LoadDefault()
	if err != nil {
		return theme.DefaultName, ""
	}
	scheme, _ = store.Get("theme").(string)
	spinner, _ = store.Get("spinner").(string)
	return scheme, spinner
}

// StateLabel is the human name of a session state ("ready" for
// needs_input).
func StateLabel(s protocol.State) string {
	if s == protocol.StateNeedsInput {
		return "ready"
	}
	return string(s)
}

// StateTone is the tone a session state is colored with.
func StateTone(s protocol.State) Tone {
	switch s {
	case protocol.StateNeedsInput:
		return ToneNeeds
	case protocol.StateWorking:
		return ToneWorking
	case protocol.StateQueued:
		return ToneQueued
	case protocol.StateDone:
		return ToneDone
	case protocol.StateFailed:
		return ToneFailed
	case protocol.StateCrashed:
		return ToneCrashed
	}
	return ToneNone
}
