package tui

import (
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

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

func TestSanitizeText(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"plain kept":         {"fix auth bug", "fix auth bug"},
		"unicode kept":       {"日本語 — ok ✓", "日本語 — ok ✓"},
		"newline to space":   {"a\nb\r\nc", "a b  c"},
		"tab to space":       {"a\tb", "a b"},
		"OSC 52 removed":     {"a\x1b]52;c;ZXZpbA==\x07b", "ab"},
		"OSC with ST":        {"a\x1b]0;title\x1b\\b", "ab"},
		"CSI removed":        {"a\x1b[2Jb\x1b[31mc", "abc"},
		"C0 and DEL dropped": {"a\x00b\x07c\x7f", "abc"},
		"C1 dropped":         {"a\u009bb\u0085c", "abc"},
		"invalid UTF-8":      {"a\xff\xfeb", "ab"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, sanitizeText(tc.in))
		})
	}
}

func FuzzSanitizeText(f *testing.F) {
	for _, s := range escapePayloads {
		f.Add(s)
	}
	f.Add("\u009b31m\xff")
	f.Fuzz(func(t *testing.T, in string) {
		out := sanitizeText(in)
		if !utf8.ValidString(out) {
			t.Fatalf("invalid UTF-8 out of %q: %q", in, out)
		}
		for _, r := range out {
			if unicode.IsControl(r) {
				t.Fatalf("control rune %U out of %q", r, in)
			}
		}
		if sanitizeText(out) != out {
			t.Fatalf("not idempotent on %q", in)
		}
	})
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
