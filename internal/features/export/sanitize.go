package export

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// unsafeRune reports a rune that must not reach an export: every control
// rune (C0, DEL and the C1 range, which a terminal decodes as escape
// introducers) except line break and tab, the bidi overrides and the two
// noncharacters YAML refuses. The emulator filters only C0 and DEL.
func unsafeRune(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case unicode.IsControl(r), r == 0xFFFE, r == 0xFFFF:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// dropUnsafe removes the unsafeRune runes from s.
func dropUnsafe(s string) string {
	return strings.Map(func(r rune) rune {
		if unsafeRune(r) {
			return -1
		}
		return r
	}, s)
}

// meta cleans one session metadata string: escape sequences and unsafe
// runes gone, one line.
func meta(s string) string {
	return strings.Join(strings.Fields(dropUnsafe(ansi.Strip(strings.ToValidUTF8(s, "�")))), " ")
}

// cleanSession returns s with every string field cleaned (a session name
// or description is free text an agent or user wrote).
func cleanSession(s protocol.SessionSummary) protocol.SessionSummary {
	for _, p := range []*string{&s.ID, &s.ShortID, &s.ToolID, &s.ModelID, &s.Effort, &s.Slug, &s.Title,
		&s.CWD, &s.LastLine, &s.Description, &s.Fleet, &s.Name, &s.ResumeID, &s.WaitingOn, &s.WaitReason} {
		*p = meta(*p)
	}
	if s.Skills != nil {
		clean := make([]string, len(s.Skills))
		for i, k := range s.Skills {
			clean[i] = meta(k)
		}
		s.Skills = clean
	}
	return s
}
