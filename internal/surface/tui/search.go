package tui

import (
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Search limits. They bound the CPU a pasted query can cost: every board
// render runs the terms over every row.
const (
	maxSearchTerms = 16
	maxSearchRunes = 128
)

// compileQuery turns the search text into lower-case terms. Control runes
// are dropped (white space among them still separates terms), the text is
// cut to maxSearchRunes, and at most maxSearchTerms terms are kept.
func compileQuery(q string) []string {
	if q == "" {
		return nil
	}
	var b strings.Builder
	n := 0
	for _, r := range q {
		if n >= maxSearchRunes {
			break
		}
		switch {
		case unicode.IsSpace(r):
			r = ' '
		case unicode.IsControl(r):
			continue
		}
		b.WriteRune(unicode.ToLower(r))
		n++
	}
	terms := strings.Fields(b.String())
	if len(terms) > maxSearchTerms {
		terms = terms[:maxSearchTerms]
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
}

// sessionMatches reports whether every term is a substring of the session's
// search haystack. No terms match everything.
func sessionMatches(s protocol.SessionSummary, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	hay := searchHaystack(s)
	for _, t := range terms {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

// searchHaystack is the lower-cased text a search looks in: what either
// board view shows (name, task or question, AI summary, model) plus the
// identifiers. Raw last_line is left out because it is spinner noise. Fields
// are joined by a newline so a term never spans two of them. The text is
// untrusted agent output: it is flattened and only ever matched, never
// rendered.
func searchHaystack(s protocol.SessionSummary) string {
	fields := [...]string{
		displayName(s),
		s.Slug,
		s.ShortID,
		rowText(s),
		rowDescription(s),
		s.ToolID,
		modelEffortLabel(s),
		s.Fleet,
		filepath.Base(s.CWD),
		s.ResumeID,
	}
	var b strings.Builder
	for _, f := range fields {
		if f == "" {
			continue
		}
		b.WriteString(protocol.FlattenDisplayText(f))
		b.WriteByte('\n')
	}
	return strings.ToLower(b.String())
}

// openSearch is `/`: open the bottom-bar editor on the current query. The
// query stays in force while editing; esc restores what it was.
func openSearch(m Model) (Model, tea.Cmd) {
	m.searchOrig = m.Search
	m.Focus = FocusSearch
	m.Err = ""
	if m.Audio != nil {
		m.Audio.Play(audio.EventCommand)
	}
	return m, nil
}

// updateSearchKey edits the query. Every edit refilters the board live.
func updateSearchKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.Search = m.searchOrig
		m.Focus = FocusBoard
		return refilter(m), nil
	case tea.KeyEnter:
		m.Focus = FocusBoard
		if strings.TrimSpace(m.Search) == "" {
			m.Search = ""
		}
		slog.Debug("tui: search kept", "query", m.Search)
		return refilter(m), nil
	case tea.KeyBackspace:
		m.Search = dropLastRune(m.Search)
	case tea.KeyCtrlU:
		m.Search = ""
	case tea.KeyCtrlW:
		m.Search = dropLastWord(m.Search)
	case tea.KeySpace:
		m.Search = appendSearch(m.Search, " ")
	case tea.KeyRunes:
		m.Search = appendSearch(m.Search, string(k.Runes))
	default:
		return m, nil
	}
	return refilter(m), nil
}

// appendSearch appends add to the query, dropping control runes (pasted
// escape sequences) and stopping at maxSearchRunes.
func appendSearch(s, add string) string {
	room := maxSearchRunes - utf8.RuneCountInString(s)
	var b strings.Builder
	b.WriteString(s)
	for _, r := range add {
		if room <= 0 {
			break
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
		room--
	}
	return b.String()
}

// searchMatchCount is how many rows match the current query in the shown
// sections.
func searchMatchCount(m Model) int {
	f := rowFilterOf(m)
	n := 0
	for _, g := range boardGroups {
		if f.showsSection(g) {
			n += len(filterByGroup(m.Sessions, g, f))
		}
	}
	return n
}

// searchHiddenCount is how many rows match the query in hidden sections.
func searchHiddenCount(m Model) int {
	f := rowFilterOf(m)
	n := 0
	for _, g := range boardGroups {
		if !f.showsSection(g) {
			n += len(filterByGroup(m.Sessions, g, f))
		}
	}
	return n
}

// matchesLabel is "3 matches", "1 match" or "no matches".
func matchesLabel(n int) string {
	switch n {
	case 0:
		return "no matches"
	case 1:
		return "1 match"
	}
	return strconv.Itoa(n) + " matches"
}

// renderSearchPrompt is the bottom-line editor: "/ query█   3 matches". The
// query is tail-truncated so the line never wraps.
func renderSearchPrompt(m Model, width int) string {
	head := styleArrowYellow.Render("/") + " "
	count := styleMuted.Render("   " + matchesLabel(searchMatchCount(m)))
	room := width - 2 - ansi.StringWidth(head) - ansi.StringWidth(count) - 1
	if room < 1 {
		room = 1
	}
	q := protocol.FlattenDisplayText(m.Search)
	if ansi.StringWidth(q) > room {
		q = ansi.TruncateLeft(q, ansi.StringWidth(q)-room+1, "…")
	}
	body := stylePrimary.Render(q) + cursorBlock(m)
	if m.Search == "" {
		body = cursorBlock(m) + styleDim.Render(truncate("every word must match", room))
	}
	return padLine("  "+head+body+count, width)
}

// searchHelpLine is the help line while the search editor is open.
func searchHelpLine(width int) string {
	parts := []string{
		styleHeaderApp.Render("enter") + styleDim.Render(" keep"),
		styleHeaderApp.Render("esc") + styleDim.Render(" cancel"),
		styleHeaderApp.Render("ctrl+u") + styleDim.Render(" clear"),
		styleHeaderApp.Render("ctrl+w") + styleDim.Render(" delete word"),
	}
	return padLine("  "+strings.Join(parts, styleMuted.Render(" · ")), width)
}
