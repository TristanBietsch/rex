package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestCompileQuery(t *testing.T) {
	require.Nil(t, compileQuery(""))
	require.Nil(t, compileQuery("   \t "))
	require.Equal(t, []string{"auth", "bug"}, compileQuery("  AUTH   Bug "))
	require.Equal(t, []string{"ab"}, compileQuery("a\x1bb"), "control runes are dropped")
	require.Equal(t, []string{"a", "b"}, compileQuery("a\tb"), "tab still separates")

	require.Len(t, compileQuery(strings.Repeat("x ", 40)), maxSearchTerms)

	got := compileQuery(strings.Repeat("a", 500))
	require.Len(t, got, 1)
	require.Len(t, got[0], maxSearchRunes)
}

func TestSessionMatches(t *testing.T) {
	base := protocol.SessionSummary{
		ID: "id-1", ShortID: "7d4f", Slug: "dark-mode", Name: "Theme Work",
		ToolID: "claude", ModelID: "opus", Effort: "high", Fleet: "Crew",
		CWD: "/home/me/Projects/Billing", Title: "fix the Login flow",
		Description: "adds OAuth callback", ResumeID: "0f8fad5b-d9cb-469f-a165-70867728950e",
		LastLine: "raw spinner noise", State: protocol.StateWorking,
	}
	cases := []struct {
		name  string
		query string
		want  bool
	}{
		{"empty matches all", "", true},
		{"display name", "theme", true},
		{"slug", "dark-mo", true},
		{"short id", "7d4f", true},
		{"task", "login", true},
		{"description", "oauth", true},
		{"tool", "claude", true},
		{"model", "opus", true},
		{"effort", "high", true},
		{"fleet", "crew", true},
		{"cwd base", "billing", true},
		{"cwd parent not searched", "projects", false},
		{"resume id", "70867728950e", true},
		{"case insensitive", "LOGIN", true},
		{"AND of terms", "login oauth", true},
		{"AND fails when one misses", "login nope", false},
		{"raw last line excluded", "spinner", false},
		{"no match", "zzz", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sessionMatches(base, compileQuery(tc.query)))
		})
	}
}

func TestSessionMatchesQuestionLine(t *testing.T) {
	s := protocol.SessionSummary{ID: "q", Slug: "q", State: protocol.StateNeedsInput, Title: "task", LastLine: "Which database?"}
	require.True(t, sessionMatches(s, compileQuery("database")), "needs-you row shows its question")
	s.State = protocol.StateWorking
	require.False(t, sessionMatches(s, compileQuery("database")))
}

func TestSessionMatchesHostileText(t *testing.T) {
	s := protocol.SessionSummary{ID: "h", Slug: "h", Title: "pay\x1b[31mroll\nnow " + string(rune(0x202e)) + "evil"}
	require.True(t, sessionMatches(s, compileQuery("payroll now evil")), "matches what the board renders")
}

func TestRowFilterTerms(t *testing.T) {
	a := protocol.SessionSummary{ID: "a", Slug: "auth", ToolID: "claude"}
	b := protocol.SessionSummary{ID: "b", Slug: "billing", ToolID: "claude"}
	f := rowFilter{terms: compileQuery("auth")}
	require.True(t, f.keep(a))
	require.False(t, f.keep(b))
	require.True(t, f.rowsActive())
	f.tool = "codex"
	require.False(t, f.keep(a), "tool and terms combine")
}

func TestRowFilterOfSearch(t *testing.T) {
	m := boardModel()
	m.Search = "Auth  bug"
	require.Equal(t, []string{"auth", "bug"}, rowFilterOf(m).terms)
}
