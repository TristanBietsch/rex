package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// lineEditor is the editing state of a one-line text field: the session
// rename prompt and the settings editor share it. It only edits text; enter
// and esc stay with the caller, which decides what saving and cancelling mean.
type lineEditor struct {
	Text string
	// Max caps the text in runes; 0 means no cap.
	Max int
	// StripControl drops control runes (ESC, newline, C1) from typed or
	// pasted input, for fields whose text is echoed back on screen.
	StripControl bool
}

// Edit applies an editing key (backspace, ctrl+u, ctrl+w, space, typed or
// pasted runes) and reports whether k was one. Other keys are left to the
// caller.
func (e *lineEditor) Edit(k tea.KeyMsg) bool {
	switch k.Type {
	case tea.KeyBackspace:
		e.Text = dropLastRune(e.Text)
	case tea.KeyCtrlU:
		e.Text = ""
	case tea.KeyCtrlW:
		e.Text = dropLastWord(e.Text)
	case tea.KeySpace:
		e.add(" ")
	case tea.KeyRunes:
		e.add(string(k.Runes))
	default:
		return false
	}
	return true
}

// add appends s, cut so the text stays within Max runes.
func (e *lineEditor) add(s string) {
	if e.StripControl {
		s = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, s)
	}
	if e.Max > 0 {
		room := e.Max - utf8.RuneCountInString(e.Text)
		if room <= 0 {
			return
		}
		if rs := []rune(s); len(rs) > room {
			s = string(rs[:room])
		}
	}
	e.Text += s
}

// dropLastWord removes the final word and the white space after it.
func dropLastWord(s string) string {
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	i := strings.LastIndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return ""
	}
	return s[:i+1]
}
