package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// multilinePrompt is the prompt from the user's report: a session started
// with it rendered its board row over five screen lines.
const multilinePrompt = "Prompt\n\nI am new\n\nThis repo is a copy"

// requireOneRow fails unless s is a single terminal row exactly w columns
// wide with no control bytes besides SGR escapes.
func requireOneRow(t *testing.T, s string, w int, msgAndArgs ...any) {
	t.Helper()
	require.NotContains(t, s, "\n", msgAndArgs...)
	require.NotContains(t, s, "\r", msgAndArgs...)
	require.NotContains(t, s, "\t", msgAndArgs...)
	require.NotContains(t, s, "\x1b[2J", msgAndArgs...)
	require.Equal(t, w, ansi.StringWidth(s), msgAndArgs...)
}

func TestRenderRow_MultilineTextStaysOneRow(t *testing.T) {
	now := time.Now()
	m := Model{Filter: "all", Focus: FocusBoard, Store: settings.NewStore(), Clock: func() time.Time { return now }}
	m.Sessions = []protocol.SessionSummary{
		{ID: "a", ShortID: "a\nb", Slug: "s\tlug", Title: multilinePrompt, ModelID: "m\x1b[2J",
			State: protocol.StateWorking, LastEventAt: now},
		{ID: "b", ShortID: "bbbb", Slug: "b", Name: "名前\nです", Description: "日本語\r\nの説明\x1b[2J",
			State: protocol.StateNeedsInput, LastEventAt: now},
	}
	for _, w := range []int{40, 80, 225} {
		cols := layoutSlots(boardSlots(m), now, w)
		for _, s := range m.Sessions {
			requireOneRow(t, renderRow(m, s, cols), w, "session %s width %d", s.ID, w)
		}
	}
	row := ansi.Strip(renderRow(m, m.Sessions[0], layoutSlots(boardSlots(m), now, 225)))
	require.Contains(t, row, "Prompt I am new This repo is a copy")
}

func TestTruncate_SanitizesToOneRow(t *testing.T) {
	require.Equal(t, "a b c[2J", truncate("a\nb\tc\x1b[2J", 20))
	require.Equal(t, "日本…", truncate("日本語\nテキスト", 6))
}

func TestRenderAnimFrame_OneRowExactColumns(t *testing.T) {
	start := time.Now()
	for _, effect := range []string{"typewriter", "decode", "wipe"} {
		a := DescAnim{From: "古い説明\nold", To: "日本語のテキスト\tand 😀 more\x1b[2J",
			Effect: effect, StartedAt: start, Duration: 300 * time.Millisecond}
		for _, w := range []int{1, 7, 10, 31} {
			for _, p := range []float64{0, 0.3, 0.5, 0.9, 1} {
				at := start.Add(time.Duration(float64(a.Duration) * p))
				requireOneRow(t, renderAnimFrame(a, w, at), w, "%s width %d p %v", effect, w, p)
			}
		}
	}
	end := start.Add(time.Second)
	got := renderAnimFrame(DescAnim{To: "a\nb", Effect: "typewriter", StartedAt: start, Duration: time.Millisecond}, 6, end)
	require.Equal(t, "a b   ", got)
}
