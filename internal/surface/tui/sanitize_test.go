package tui

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Terminal-escape payloads a session string can carry (S-03): a line break
// that would split a board row, an OSC 52 clipboard write, a screen clear.
var escapePayloads = map[string]string{
	"newline": "line one\nline two",
	"osc52":   "copy \x1b]52;c;ZWNobyBwd25lZA==\x07me",
	"clear":   "wipe \x1b[2Jscreen",
}

// escapePayloadText is what each payload must render as: its printable
// text, on one line.
var escapePayloadText = map[string]string{
	"newline": "line one line two",
	"osc52":   "copy me",
	"clear":   "wipe screen",
}

// assertNoInjection fails when rendered output carries any byte a session
// string could have smuggled in. lipgloss renders without color under
// `go test` (no TTY), so no ESC at all is expected.
func assertNoInjection(t *testing.T, out string) {
	t.Helper()
	require.NotContains(t, out, "\x1b", "raw ESC reached the terminal")
	require.NotContains(t, out, "\x07", "raw BEL reached the terminal")
	for _, r := range out {
		require.False(t, r != '\n' && unicode.IsControl(r), "control rune %U in output", r)
	}
}

// Every session-provided string on the board (title, description, name,
// slug, last_line) renders as one row with no raw escape bytes in View().
func TestView_SessionStringsCannotInjectEscapes(t *testing.T) {
	for name, payload := range escapePayloads {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			m := boardModel(
				protocol.SessionSummary{ID: "d", ShortID: "dddd", Slug: "desc", Description: payload,
					State: protocol.StateWorking, Fleet: payload},
				protocol.SessionSummary{ID: "t", ShortID: "tttt", Slug: "title", Title: payload,
					State: protocol.StateNeedsInput},
				protocol.SessionSummary{ID: "n", ShortID: "nnnn", Slug: payload, Name: payload,
					State: protocol.StateDone, LastLine: payload},
			)
			m.Clock = func() time.Time { return now }
			m.Err = "daemon: " + payload
			assertNoInjection(t, m.View()) // simple view: titles and questions
			m.Detailed = true              // rows are found by short id
			out := m.View()
			assertNoInjection(t, out)
			for _, id := range []string{"dddd", "tttt", "nnnn"} {
				var rows []string
				for _, line := range strings.Split(out, "\n") {
					if strings.Contains(line, "  "+id+"  ") {
						rows = append(rows, line)
					}
				}
				require.Len(t, rows, 1, "%s renders as exactly one row", id)
				require.Contains(t, rows[0], escapePayloadText[name], "%s row holds the whole text", id)
			}
			require.Contains(t, out, "err: daemon: "+escapePayloadText[name])
		})
	}
}

// The attach popup's title bar shows the session's name and slug, and the
// fail popup its slug and last line: same rule.
func TestAttachAndFailChrome_SessionStringsCannotInjectEscapes(t *testing.T) {
	for name, payload := range escapePayloads {
		t.Run(name, func(t *testing.T) {
			sum := protocol.SessionSummary{ID: "a", ShortID: "aaaa", Slug: payload, Name: payload,
				ToolID: "claude", ModelID: payload, State: protocol.StateFailed, LastLine: payload}
			m := boardModel(sum)
			bar := attachTitleBar(m, &AttachState{SessionID: "a", Slug: payload, Cols: 80, Rows: 24}, 200)
			assertNoInjection(t, bar)
			require.NotContains(t, bar, "\n")
			require.Contains(t, bar, escapePayloadText[name])

			assertNoInjection(t, ansi.Strip(renderFailEntry(sum)))
			require.Equal(t, 2, len(strings.Split(renderFailEntry(sum), "\n")), "slug line + last line")

			m.PendingDeleteID = "a"
			assertNoInjection(t, renderDeleteConfirm(m, 200))
		})
	}
}
