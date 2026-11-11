package core

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestTruncate_MultiByte(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"claude-sonnet-4-6 · medium", 20, "claude-sonnet-4-6 ·…"},
		{"短い名前のセッション", 7, "短い名…"},
		{"abc", 3, "abc"},
		{"abcd", 1, "a"},
		{"abcd", 0, ""},
	}
	for _, c := range cases {
		got := Truncate(c.in, c.n)
		require.True(t, utf8.ValidString(got), got)
		require.LessOrEqual(t, ansi.StringWidth(got), c.n)
		require.Equal(t, c.want, got)
	}
}

func TestWriteSessionsTable_ValidUTF8AndAligned(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSessionsTable(&buf, []protocol.SessionSummary{
		{ShortID: "a1", State: protocol.StateWorking, ToolID: "claude", ModelID: "claude-sonnet-4-6", Effort: "medium", Slug: "短い名前のセッションですよろしく", LastEventAt: time.Now(), Name: "n"},
		{ShortID: "b2", State: protocol.StateDone, ToolID: "echo", ModelID: "m", Slug: "plain", LastEventAt: time.Now(), Name: "n"},
	})
	require.NoError(t, err)
	require.True(t, utf8.ValidString(buf.String()))
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 3)
	// The NAME column starts at the same display cell in every row.
	col := func(l string) int { return ansi.StringWidth(l[:strings.LastIndex(l, "  ")]) }
	require.Equal(t, col(lines[1]), col(lines[2]))
}
