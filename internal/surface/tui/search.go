package tui

import (
	"path/filepath"
	"strings"
	"unicode"

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
