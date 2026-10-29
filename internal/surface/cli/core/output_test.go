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

func TestWriteSessionsTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, WriteSessionsTable(&buf, nil))
	require.Contains(t, buf.String(), "no sessions")
}

func TestWriteSessionsTable_Rows(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ShortID: "7d4f", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus", Effort: "high", Slug: "demo", LastEventAt: time.Now().Add(-2 * time.Minute)},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteSessionsTable(&buf, sessions))
	out := buf.String()
	require.Contains(t, out, "7d4f")
	require.Contains(t, out, "working")
	require.Contains(t, out, "demo")
	require.Contains(t, out, "opus · high")
}

func TestTruncate(t *testing.T) {
	require.Equal(t, "short", Truncate("short", 10))
	require.Equal(t, "abcd…", Truncate("abcdefgh", 5))
	// Byte slicing used to cut the 2-byte "·" in half (invalid UTF-8).
	got := Truncate("claude-opus · high", 14)
	require.True(t, utf8.ValidString(got), "%q", got)
	require.Equal(t, "claude-opus ·…", got)
	// Wide runes are two cells and are never split.
	got = Truncate("日本語のスラッグ", 7)
	require.Equal(t, "日本語…", got)
	require.LessOrEqual(t, ansi.StringWidth(got), 7)
}

// Columns line up by terminal cells, and multi-line or escape-laden text
// from the daemon stays on one row.
func TestWriteSessionsTable_Sanitized(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ShortID: "aaaa", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus", Slug: "日本語", Name: "line one\nline two\x1b[31m"},
		{ShortID: "bbbb", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus", Slug: "ascii", Name: "x"},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteSessionsTable(&buf, sessions))
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 3, "header + one line per session:\n%s", buf.String())
	require.NotContains(t, buf.String(), "\x1b")
	require.Contains(t, lines[1], "line one line two")
	// LAST EVENT starts at the same cell on both rows.
	col := func(line string) int {
		i := strings.Index(line, "-  ")
		require.GreaterOrEqual(t, i, 0, "no LAST EVENT cell in %q", line)
		return ansi.StringWidth(line[:i])
	}
	require.Equal(t, col(lines[2]), col(lines[1]))
}

func TestWriteAggregateLine(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{State: protocol.StateWorking},
		{State: protocol.StateWorking},
		{State: protocol.StateNeedsInput},
		{State: protocol.StateDone},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteAggregateLine(&buf, sessions))
	out := buf.String()
	require.True(t, strings.Contains(out, "1 awaiting input · 2 working · 1 completed"))
}
