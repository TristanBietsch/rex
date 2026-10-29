package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Regression for the user's report: a session started with a multi-line
// prompt rendered its row over five screen lines and pushed the bottom HR,
// prompt and help bar off the screen. Every text field that reaches the
// screen, and every input bar, gets hostile text; at each size the frame
// must keep its shape.

var hostileTexts = map[string]string{
	"controls":  "a\nb\tc\x1b[2J\r\x07d‮e",
	"prompt":    multilinePrompt,
	"cjk":       strings.Repeat("日本語のテキストとても長い説明文です。", 8),
	"emoji":     "🚀 deploy 👩‍💻 fix 🎉🎉🎉 " + strings.Repeat("😀", 60),
	"10kb":      strings.Repeat("lorem ipsum dolor\n", 600),
	"10kb-wide": strings.Repeat("長", 5000),
}

var hostileSizes = []struct{ w, h int }{{225, 69}, {80, 24}, {40, 10}}

// hostileFields sets one screen-reaching text field on m to s. Sessions
// are set raw, as the boot snapshot stores them.
var hostileFields = map[string]func(m *Model, s string){
	"title":       func(m *Model, s string) { m.Sessions[0].Title = s },
	"description": func(m *Model, s string) { m.Sessions[1].Description = s },
	"name":        func(m *Model, s string) { m.Sessions[0].Name = s },
	"slug":        func(m *Model, s string) { m.Sessions[1].Slug = s },
	"fleet":       func(m *Model, s string) { m.Sessions[2].Fleet = s },
	"model":       func(m *Model, s string) { m.Sessions[2].ModelID = s },
	"reason": func(m *Model, s string) {
		m.BackendUnavailable = true
		m.BackendUnavailableReason = s
	},
	"err":    func(m *Model, s string) { m.Err = s },
	"notice": func(m *Model, s string) { m.Notice = s },
	"anim": func(m *Model, s string) {
		now := m.now()
		m.DescAnim = map[string]DescAnim{"2": {From: s, To: s, Effect: "wipe",
			StartedAt: now.Add(-100 * time.Millisecond), Duration: 250 * time.Millisecond}}
	},
}

func hostileModel(w, h int) Model {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := Model{Filter: "all", Focus: FocusBoard, Width: w, Height: h, Store: settings.NewStore(),
		Clock: func() time.Time { return now }}
	for i, st := range []protocol.State{protocol.StateNeedsInput, protocol.StateWorking, protocol.StateWorking,
		protocol.StateDone} {
		m.Sessions = append(m.Sessions, protocol.SessionSummary{
			ID: fmt.Sprint(i + 1), ShortID: fmt.Sprintf("s%03d", i+1), ToolID: "claude", ModelID: "opus",
			Slug: fmt.Sprintf("slug-%d", i+1), Title: "task", State: st, LastEventAt: now.Add(-time.Minute),
			WorkingSince: now.Add(-3 * time.Hour), Tokens: 12345,
		})
	}
	m.SelectedID = "2"
	return m
}

// requireFrame checks the full-screen invariants on m.View().
func requireFrame(t *testing.T, m Model, label string) {
	t.Helper()
	w, h := m.Width, m.Height
	view := m.View()
	lines := strings.Split(view, "\n")
	require.Len(t, lines, h, label)
	require.NotContains(t, view, "\x1b[2J", label)
	require.NotContains(t, view, "\t", label)
	require.NotContains(t, view, "\r", label)
	for i, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), w, "%s: line %d too wide", label, i)
	}
	if m.Wizard != nil || m.Attach != nil {
		return // a popup may cover the bottom bars
	}
	require.Equal(t, strings.Repeat("─", w), ansi.Strip(lines[h-3]), "%s: bottom HR", label)
	require.Equal(t, ansi.Strip(firstLine(renderHelpLine(m, w))), ansi.Strip(lines[h-1]), "%s: help bar last", label)
	require.Len(t, strings.Split(renderHeader(m, w), "\n"), headerHeight(m), "%s: header height", label)
}

// requireHits checks that the board hit-test maps every rendered session
// row to its session, and nothing else to a session.
func requireHits(t *testing.T, m Model, label string) {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	for y, l := range lines {
		plain := ansi.Strip(l)
		want := ""
		if y >= boardTop(m) && y < boardTop(m)+boardHeight(m) {
			for _, s := range m.Sessions {
				if strings.Contains(plain, " "+s.ShortID+" ") {
					want = s.ID
				}
			}
		}
		require.Equal(t, want, sessionAtScreenRow(m, y), "%s: hit row %d %q", label, y, plain)
	}
}

func TestHostileText_FieldsKeepFrameShape(t *testing.T) {
	for _, sz := range hostileSizes {
		for field, set := range hostileFields {
			for name, text := range hostileTexts {
				m := hostileModel(sz.w, sz.h)
				set(&m, text)
				label := fmt.Sprintf("%dx%d %s=%s", sz.w, sz.h, field, name)
				requireFrame(t, m, label)
				requireHits(t, m, label)
			}
		}
	}
}

func TestHostileText_InputSitesKeepFrameShape(t *testing.T) {
	for _, sz := range hostileSizes {
		for _, site := range inputSites {
			for name, text := range hostileTexts {
				m := site.open(hostileModel(sz.w, sz.h))
				m, _ = updateKey(m, pasteKey(text))
				label := fmt.Sprintf("%dx%d %s=%s", sz.w, sz.h, site.name, name)
				require.NotContains(t, site.text(m), "\n", label)
				requireFrame(t, m, label)
				if m.Attach != nil {
					// The attach popup is title + HR + rows + HR + footer.
					popup := strings.Split(renderAttach(m), "\n")
					require.Len(t, popup, m.Attach.Rows+4, label)
					for _, l := range popup {
						require.LessOrEqual(t, ansi.StringWidth(l), m.Attach.Cols, label)
					}
				}
			}
		}
	}
}

func TestHostileText_TinyScreensKeepBottomBars(t *testing.T) {
	for h := 1; h <= 12; h++ {
		m := hostileModel(30, h)
		m.BackendUnavailable = true
		lines := strings.Split(m.View(), "\n")
		require.Len(t, lines, h, "h=%d", h)
		require.Equal(t, ansi.Strip(firstLine(renderHelpLine(m, 30))), ansi.Strip(lines[h-1]), "h=%d", h)
		if h >= 3 {
			require.Equal(t, strings.Repeat("─", 30), ansi.Strip(lines[h-3]), "h=%d", h)
		}
		requireHits(t, m, fmt.Sprintf("h=%d", h))
	}
}
