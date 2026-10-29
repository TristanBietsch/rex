package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSessionName(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"trim", "  hello  ", "hello"},
		{"collapse", "a   b\t\tc", "a b c"},
		{"newlines", "line1\nline2\r\nline3", "line1 line2 line3"},
		{"esc stripped", "red\x1b[31mtext", "red[31mtext"},
		{"c1 csi stripped", "a\u009bb", "ab"},
		{"bidi stripped", "abc\u202Edef\u2066", "abcdef"},
		{"line sep becomes space", "a\u2028b", "a b"},
		{"invalid utf8 dropped", "a\xffb", "ab"},
		{"empty", "   ", ""},
		{"cjk kept", "日本語 タスク", "日本語 タスク"},
		{"emoji kept", "fix 🐛 bug", "fix 🐛 bug"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeSessionName(c.in)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestSingleLine(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"multi-line prompt", "Prompt\n\nI am new\n\nThis repo", "Prompt I am new This repo"},
		{"crlf and tabs", "a\r\n\tb", "a b"},
		{"esc stripped", "x\x1b[2Jy", "x[2Jy"},
		{"c1 stripped", "a\u009bb", "ab"},
		{"bidi stripped", "a\u202eb", "ab"},
		{"no length cap", strings.Repeat("a", 500), strings.Repeat("a", 500)},
		{"empty", "\n\t ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, SingleLine(c.in))
		})
	}
}

func TestNormalizeSessionName_Limit(t *testing.T) {
	ok := strings.Repeat("界", MaxSessionNameRunes)
	got, err := NormalizeSessionName(ok)
	require.NoError(t, err)
	require.Equal(t, ok, got)

	_, err = NormalizeSessionName(ok + "x")
	require.ErrorIs(t, err, ErrSessionNameTooLong)

	// Whitespace collapsed before counting.
	_, err = NormalizeSessionName(strings.Repeat("a ", 32) + "    ")
	require.NoError(t, err)
}

func TestSessionSummary_NewFieldsOmittedWhenZero(t *testing.T) {
	b, err := json.Marshal(SessionSummary{ID: "x"})
	require.NoError(t, err)
	s := string(b)
	require.NotContains(t, s, "retire_at")
	require.NotContains(t, s, "working_since")
	require.NotContains(t, s, `"name"`)

	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	b, err = json.Marshal(SessionSummary{ID: "x", Name: "n", RetireAt: at, RetiredAt: at})
	require.NoError(t, err)
	var back SessionSummary
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, "n", back.Name)
	require.True(t, at.Equal(back.RetireAt))
	require.True(t, at.Equal(back.RetiredAt))
}

func TestSetSessionName_ClearIsExplicit(t *testing.T) {
	b, err := json.Marshal(SetSessionName{SessionID: "s", Name: ""})
	require.NoError(t, err)
	require.Contains(t, string(b), `"name":""`)
}
