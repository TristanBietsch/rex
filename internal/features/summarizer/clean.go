package summarizer

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Clean normalizes whatever the model produced into the form that belongs in
// the desc column: one line of display-safe text, no label or list preface,
// no surrounding quotes, no agent-subject opener ("Claude is …"), no trailing
// period, sentence case, and at most maxRunes runes cut on a word boundary.
// tool is the session's tool id; its name is also treated as a subject.
// It returns "" when nothing meaningful is left, and Clean(Clean(x)) equals
// Clean(x).
//
// The response is untrusted: the model paraphrases the agent's transcript,
// and every attached TUI prints the result verbatim. So it is reduced to the
// first line of printable text first (protocol.SanitizeDisplayText, the
// sanitizer the daemon applies to all free text), and the opener rules only
// ever see sanitized text (backlog S-03).
func Clean(s string, maxRunes int, tool string) string {
	if maxRunes <= 0 || maxRunes > protocol.MaxDescriptionRunes {
		maxRunes = protocol.MaxDescriptionRunes
	}
	s = protocol.SanitizeDisplayText(s, 0)
	// Each pass only ever shortens s, so this reaches a fixed point; that is
	// what makes Clean idempotent.
	for range 64 {
		prev := s
		s = stripLabel(s)
		s = stripListMarker(s)
		s = stripQuotes(s)
		s = stripOpener(s, tool)
		s = strings.TrimSpace(s)
		// A trailing period that isn't part of an ellipsis.
		if strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "..") {
			s = strings.TrimSpace(strings.TrimSuffix(s, "."))
		}
		if s == prev {
			break
		}
	}
	if letterCount(s) < 3 {
		return ""
	}
	s = clampWords(sentenceCase(s), maxRunes)
	if letterCount(s) < 3 { // the cut can leave only digits or punctuation
		return ""
	}
	return s
}

// cleanResponse is Clean at the original 60-rune cap and no tool; kept for
// tests and callers that have neither a detail level nor a tool.
func cleanResponse(s string) string { return Clean(s, maxDescription, "") }

var responseLabels = []string{"Output:", "Description:", "Activity:", "Status line:", "Status:", "Summary:"}

func stripLabel(s string) string {
	lower := strings.ToLower(s)
	for _, p := range responseLabels {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return strings.TrimSpace(s[len(p):])
		}
	}
	return s
}

func stripListMarker(s string) string {
	for strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "> ") || strings.HasPrefix(s, "* ") {
		s = strings.TrimSpace(s[2:])
	}
	return s
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		switch s[0] {
		case '"', '\'', '`':
			if s[len(s)-1] == s[0] {
				return strings.TrimSpace(s[1 : len(s)-1])
			}
		}
	}
	return s
}

// Opener rules. The subject list is every way a model names the agent; the
// tool id is added per call. All patterns are anchored RE2: linear time.
const (
	subjectBase = `agent|assistant|model|bot|session|claude(?:\s+code)?|codex|antigravity(?:\s+cli)?|agy|gemini(?:\s+cli)?|ollama|grok|it|he|she|they|we|i`
	lead        = `(?:(?:right\s+)?now,?\s+|currently,?\s+|at\s+the\s+moment,?\s+)?(?:the\s+|this\s+)?(?:(?:ai|coding|cli)\s+)?`
)

type openerRes struct {
	copula, possessive *regexp.Regexp
}

var (
	openerCache sync.Map // tool id (lowercase) -> *openerRes
	adverbRe    = regexp.MustCompile(`(?i)^(?:currently|right\s+now|now|at\s+the\s+moment),?\s+`)
)

func openersFor(tool string) *openerRes {
	tool = strings.ToLower(strings.TrimSpace(tool))
	if v, ok := openerCache.Load(tool); ok {
		return v.(*openerRes)
	}
	subj := subjectBase
	if tool != "" {
		subj += "|" + regexp.QuoteMeta(tool)
	}
	r := &openerRes{
		copula: regexp.MustCompile(`(?i)^` + lead + `(?:` + subj + `)` +
			`(?:\s+(?:is|are|am|was|has\s+been|have\s+been|will\s+be|seems\s+to\s+be|appears\s+to\s+be)|['’](?:re|m))` +
			`(?:\s+|$)(?:(?:currently|now|still|actively|busy|in\s+the\s+process\s+of)(?:\s+|$))*`),
		possessive: regexp.MustCompile(`(?i)^` + lead + `(` + subj + `)['’]s\s+(\S+)`),
	}
	openerCache.Store(tool, r)
	return r
}

// stripOpener removes one agent-subject opener, if any: "Claude is running"
// -> "running", "It's compiling" -> "compiling", "Currently, Claude is" -> "".
func stripOpener(s, tool string) string {
	o := openersFor(tool)
	if loc := o.copula.FindStringIndex(s); loc != nil {
		return strings.TrimSpace(s[loc[1]:])
	}
	// "'s" is a verb only before an -ing word ("Claude's editing x"), or after
	// a pronoun ("It's compiling"); "Claude's config updated" is a noun phrase.
	if m := o.possessive.FindStringSubmatchIndex(s); m != nil {
		subj := strings.ToLower(s[m[2]:m[3]])
		next := strings.ToLower(s[m[4]:m[5]])
		pronoun := subj == "it" || subj == "he" || subj == "she"
		if pronoun || strings.HasSuffix(next, "ing") {
			return strings.TrimSpace(s[m[4]:])
		}
		return s
	}
	if loc := adverbRe.FindStringIndex(s); loc != nil {
		return strings.TrimSpace(s[loc[1]:])
	}
	return s
}

func letterCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

// verbWords are lowercase first words that are not -ing/-ed forms but still
// read as a status verb, so sentence case applies ("done: tests pass").
var verbWords = map[string]bool{"done": true, "idle": true}

// sentenceCase upper-cases the first rune when the first word looks like a
// plain lowercase verb (running, fixed, done). Identifiers and commands
// (go.mod, pnpm, ./run.sh) are left alone.
func sentenceCase(s string) string {
	word := s
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		word = s[:i]
	}
	word = strings.TrimRight(word, ":,;.…")
	if word == "" {
		return s
	}
	for _, r := range word {
		if !unicode.IsLower(r) {
			return s
		}
	}
	if !strings.HasSuffix(word, "ing") && !strings.HasSuffix(word, "ed") && !verbWords[word] {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// clampWords cuts s to at most max runes on a word boundary, ending in "…".
func clampWords(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	cut := runes[:max-1]
	// Back up to the last space when it is in the final 40%: a mid-word cut
	// reads worse than a slightly shorter line.
	for i := len(cut) - 1; i >= len(cut)*6/10; i-- {
		if cut[i] == ' ' {
			cut = cut[:i]
			break
		}
	}
	out := strings.TrimRight(string(cut), " ,;:—-")
	return out + "…"
}
