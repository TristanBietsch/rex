package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A trust choice made for one folder never reaches the launch from another:
// the draft drops it on reopen, and the request clamps what the folder
// offers (refused, outside the home folder, covered).
func TestNewAgent_StaleTrustChoiceIsDropped(t *testing.T) {
	m := naOpen(t, naModel(t))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Equal(t, trustAlways, m.NewAgent.Trust)
	m, _ = naKey(m, key("esc"))

	other := filepath.Join(os.Getenv("HOME"), "dev", "other")
	require.NoError(t, os.MkdirAll(filepath.Join(other, ".git"), 0o700)) //nolint:gosec // G703: a test folder under its own temp HOME.
	t.Chdir(other)
	m = naOpen(t, m)
	require.Equal(t, trustAsk, m.NewAgent.Trust, "the choice was for another folder")

	// A request built from a stale choice in a refused folder (the home
	// folder) is not refused, and nothing is saved.
	st := m.NewAgent
	st.Trust, st.TrustFor = trustSession, ""
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	t.Chdir(home)
	req, err := st.request(spawnCWD(m), nil)
	require.NoError(t, err)
	m, req, ok := newAgentTrustRequest(m, spawnCWD(m), req)
	require.True(t, ok)
	require.False(t, req.TrustFolder)
	require.Equal(t, trustAsk, m.NewAgent.Trust)

	// "always" outside the home folder has no entry to save: back to ask.
	t.Chdir(t.TempDir())
	st.Trust = trustAlways
	req, err = st.request(spawnCWD(m), nil)
	require.NoError(t, err)
	m, req, ok = newAgentTrustRequest(m, spawnCWD(m), req)
	require.True(t, ok)
	require.False(t, req.TrustFolder)
	require.Empty(t, m.Store.String("auto_trust_dirs"))
	_ = m
}

// On a short screen the trust rows window on the cursor: the cursor's radio,
// label and folder stay together and visible.
func TestNewAgent_TrustStepWindowsOnTheCursor(t *testing.T) {
	m := naOpen(t, naModel(t))
	m.Width, m.Height = 40, 12
	m = naAt(m, naStepTrust)
	m.NewAgent.TrustCur = 2
	out := naView(m)
	lines := strings.Split(out, "\n")
	require.Len(t, lines, 12)
	require.Contains(t, out, "▸ ( ) always: saves")
	require.Contains(t, out, "› trust")
	i := -1
	for n, l := range lines {
		if strings.Contains(l, "▸ ( ) always") {
			i = n
		}
	}
	require.Greater(t, i, 0)
	require.NotEmpty(t, strings.TrimSpace(lines[i+1]), "the folder follows its radio")
	m, _ = naKey(m, key(" "))
	require.Equal(t, trustAlways, m.NewAgent.Trust)
}

// The notice, the progress line and a long error never push the launch row
// or the current line off a short screen.
func TestNewAgent_UnderLinesAreBoundedByTheBody(t *testing.T) {
	long := "✗ launch failed: " + strings.Repeat("the daemon is unhappy, ", 12) + "Your draft is kept."
	for _, step := range []naStep{naStepTask, naStepContext, naStepSummary} {
		for _, sz := range []goldenSize{{40, 12}, {40, 9}, {30, 8}} {
			m := naModel(t)
			m.lastSpawn = &quickChoice{Tool: "grok"}
			m = naOpen(t, m)
			m.Width, m.Height = sz.w, sz.h
			m = naAt(m, step)
			m.NewAgent.Err = long
			m.NewAgent.Launching = step == naStepTask
			lines := strings.Split(m.View(), "\n")
			require.Len(t, lines, sz.h)
			out := ansi.Strip(m.View())
			require.Contains(t, out, "› "+naStepNames[step], "step %d %dx%d", step, sz.w, sz.h)
			if step == naStepSummary && sz.h >= 9 {
				require.Contains(t, out, "▸ launch", "%dx%d: the launch row shows", sz.w, sz.h)
				if sz.h >= 12 {
					require.Contains(t, out, "✗ launch", "%dx%d: and the first error line", sz.w, sz.h)
				}
			}
		}
	}
}

// A tool list change that removes a step leaves the summary cursor on a step
// row, never on the launch row.
func TestNewAgent_RefreshKeepsSummaryCursorOffLaunch(t *testing.T) {
	m := naModel(t)
	writeHomeSkill(t, filepathHome(t), "go-testing", "tests")
	m.DaemonTools = daemonTools(t, "claude", "cursor")
	m = naOpen(t, m)
	m = naAt(m, naStepSummary)
	targets := naSumTargets(m.NewAgent)
	m.NewAgent.SumRow = len(targets) - 1 // the trust row
	m.NewAgent.SumRow = 2                // the skills row
	require.Equal(t, naStepSkills, targets[2])
	data, err := json.Marshal(protocol.ToolsChanged{Tools: daemonTools(t, "cursor")})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventToolsChanged, Data: data})
	st := m.NewAgent
	require.Less(t, st.SumRow, len(naSumTargets(st)), "the cursor is on a step row")
	m, cmd := naKey(m, key("enter"))
	require.Nil(t, cmd, "enter does not launch")
	require.False(t, m.NewAgent.Launching)
}

// Leaving the agent step by any route ends the open-time notice.
func TestNewAgent_NoticeEndsWhenLeavingTheAgentStep(t *testing.T) {
	for _, leave := range []tea.KeyMsg{{Type: tea.KeyTab}, key("4"), key("7")} {
		m := naModel(t)
		m.lastSpawn = &quickChoice{Tool: "grok"}
		m = naOpen(t, m)
		require.NotEmpty(t, m.NewAgent.Notice)
		m, _ = naKey(m, leave)
		require.Empty(t, m.NewAgent.Notice, "%v", leave)
	}
}

func TestNewAgent_RexNewLineDetails(t *testing.T) {
	m := naOpen(t, naModel(t))
	st := m.NewAgent
	st.Task.Insert("-rf the tests")
	line := naRexNewLine(st, "/w/my proj", 300)
	require.Contains(t, line, `-cwd '/w/my proj'`)
	require.True(t, strings.HasSuffix(line, `'-rf the tests'`), line)
	require.Equal(t, "'-x'", naShellQuote("-x"))
	require.Equal(t, "-tool", "-tool", "flags are not quoted")

	// An empty id leaves its flag out.
	st.Tools[st.ToolIdx].Models = nil
	require.NotContains(t, naRexNewLine(st, "/w", 300), "-model")

	// The comment is kept whole when the title is cut.
	st.Items = nil
	st.Task.Clear()
	st.Task.Insert(strings.Repeat("word ", 40))
	st.Trust = trustAlways
	cut := naRexNewLine(st, "/w", 80)
	require.LessOrEqual(t, ansi.StringWidth(cut), 80)
	require.True(t, strings.HasSuffix(cut, "  # + always trust"), cut)
	require.Contains(t, cut, "…'")
}

func TestNewAgent_NarrowHintsKeepEscAndHelp(t *testing.T) {
	m := naOpen(t, naModel(t))
	for _, step := range []naStep{naStepAgent, naStepModel, naStepSkills, naStepContext, naStepTrust, naStepSummary} {
		m = naAt(m, step)
		var keys []string
		for _, h := range naHints(m, m.NewAgent, true) {
			keys = append(keys, h[0])
		}
		require.Contains(t, keys, "esc", "step %d", step)
		require.Contains(t, keys, "?", "step %d", step)
		require.LessOrEqual(t, len(keys), 4)
	}
}

func TestNewAgent_RefusalTextWraps(t *testing.T) {
	m := naOpen(t, naModel(t))
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	t.Chdir(home)
	m = naAt(m, naStepTrust)
	for _, l := range naTrustBody(m, m.NewAgent, 24, 10) {
		require.LessOrEqual(t, ansi.StringWidth(l), 24, ansi.Strip(l))
	}
	require.Contains(t, strings.Join(strings.Fields(ansi.Strip(strings.Join(naTrustBody(m, m.NewAgent, 24, 10), " "))), " "), "home folder: start rex in a project folder to trust it")
}

// :new! drops the kept draft and opens a fresh page; :new restores it.
func TestCommand_NewBangClearsTheDraft(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "keep me")
	m, _ = naKey(m, key("esc"))
	require.NotNil(t, m.newAgentDraft)

	kept, _ := executeCommand(m, "new")
	require.Equal(t, "keep me", kept.NewAgent.Task.String())

	fresh, _ := executeCommand(m, "new!")
	require.Equal(t, FocusNewAgent, fresh.Focus)
	require.True(t, fresh.NewAgent.Task.Empty())
	require.Nil(t, fresh.newAgentDraft)
}
