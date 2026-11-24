package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/spawnctx"
)

// naModel is a board with settings loaded, HOME isolated, the folder rex
// runs in a checkout inside it, and claude and codex installed.
func naModel(t *testing.T) Model {
	t.Helper()
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	proj := filepath.Join(home, "dev", "proj")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".git"), 0o700))
	t.Chdir(proj)
	m := boardModel()
	m.Width, m.Height = 120, 40
	m.Store = settings.NewStore()
	m.StorePath = filepath.Join(home, "config.yaml")
	m.reloader = func(context.Context, string) error { return nil } // never a real daemon
	m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex"), true
	return m
}

// naKey sends k through updateKey and returns the command too.
func naKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) { return updateKey(m, k) }

func naOpen(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = naKey(m, key("n"))
	require.Equal(t, FocusNewAgent, m.Focus)
	require.NotNil(t, m.NewAgent)
	return m
}

// naAt moves the open page to step s, as a jump would.
func naAt(m Model, s naStep) Model {
	naGo(m, s)
	return m
}

// naClose is esc: on the step the page opened on (the task) it closes the
// page, the draft kept.
func naClose(m Model) (Model, tea.Cmd) {
	return naKey(m, key("esc"))
}

func naType(m Model, s string) Model {
	for _, r := range s {
		m, _ = naKey(m, key(string(r)))
	}
	return m
}

// naOpenable makes a command-test board able to open the page: HOME
// isolated, claude and codex installed.
func naOpenable(t *testing.T, m Model) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex"), true
	return m
}

func setFlag(t *testing.T, m Model, id string, v any) Model {
	t.Helper()
	require.NoError(t, m.Store.Set(id, v))
	return m
}

// naStepKeys walks enter from step from and returns the steps landed on, up
// to and including stop.
func naWalk(t *testing.T, m Model, stop naStep) (Model, []naStep) {
	t.Helper()
	var seen []naStep
	for range int(naStepCount) {
		if m.NewAgent.Step == stop {
			return m, seen
		}
		m, _ = naKey(m, key("enter"))
		seen = append(seen, m.NewAgent.Step)
	}
	t.Fatalf("enter never reached step %d: %v", stop, seen)
	return m, nil
}

func TestNewAgent_PasteNeverLaunches(t *testing.T) {
	m := naOpen(t, naModel(t))
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix\nit\n"), Paste: true})
	require.Nil(t, cmd, "a paste never launches")
	require.Equal(t, FocusNewAgent, m.Focus)
	require.Equal(t, naStepTask, m.NewAgent.Step, "a paste never advances")
	require.Equal(t, "fix\nit\n", m.NewAgent.Task.String())
	require.Empty(t, m.PendingSelectSlug)
}

// A paste on any other step jumps to the task and inserts there; it ends
// the return to the summary.
func TestNewAgent_PasteOffTheTaskJumpsToTask(t *testing.T) {
	for _, step := range []naStep{naStepAgent, naStepModel, naStepContext, naStepTrust, naStepSummary} {
		m := naOpen(t, naModel(t))
		m = naAt(m, step)
		m.NewAgent.ReturnToSummary = true
		m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a paste"), Paste: true})
		require.Nil(t, cmd, "step %d", step)
		require.Equal(t, naStepTask, m.NewAgent.Step, "step %d", step)
		require.False(t, m.NewAgent.ReturnToSummary, "step %d", step)
		require.Equal(t, "a paste", m.NewAgent.Task.String(), "step %d", step)
		require.False(t, m.NewAgent.Launching)
	}
}

func TestNewAgent_NOpensPageAndCommandNewToo(t *testing.T) {
	m := naModel(t)
	on, _ := naKey(m, key("n"))
	require.Equal(t, FocusNewAgent, on.Focus)
	require.NotNil(t, on.NewAgent)
	require.Equal(t, naStepTask, on.NewAgent.Step, "the page opens on the task")
	cmdOn, _ := executeCommand(m, "new")
	require.Equal(t, FocusNewAgent, cmdOn.Focus)
}

func TestNewAgent_EnterWalksStepsSkippingSkills(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naAt(m, naStepAgent)
	_, seen := naWalk(t, m, naStepSummary)
	require.Equal(t, []naStep{naStepModel, naStepTask, naStepContext, naStepTrust, naStepSummary}, seen,
		"no skill library: the skills step is skipped")

	// With a library the claude agent offers its skills.
	n := naModel(t)
	writeHomeSkill(t, filepathHome(t), "go-testing", "tests")
	n = naOpen(t, n)
	n = naAt(n, naStepAgent)
	_, seen = naWalk(t, n, naStepSummary)
	require.Equal(t, []naStep{naStepModel, naStepSkills, naStepTask, naStepContext, naStepTrust, naStepSummary}, seen)
}

func TestNewAgent_EnterInTheEditorGoesNextNeverLaunches(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = naType(m, "it")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = naType(m, "now")
	require.Equal(t, "fix\nit\nnow", m.NewAgent.Task.String())
	require.Equal(t, naStepTask, m.NewAgent.Step, "newlines never advance")

	m, cmd := naKey(m, key("enter"))
	require.Nil(t, cmd, "enter in the task editor never launches")
	require.False(t, m.NewAgent.Launching)
	require.Equal(t, naStepContext, m.NewAgent.Step, "it goes to the next step")
	require.Empty(t, m.PendingSelectSlug)
}

func TestNewAgent_SummaryLaunchesAndRemembers(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix")
	m, _ = naWalk(t, m, naStepSummary)
	st := m.NewAgent
	require.Equal(t, len(naSumTargets(st)), st.SumRow, "the cursor starts on the launch row")
	req, err := st.request(spawnCWD(m), existingSlugs(m))
	require.NoError(t, err)
	want := quickChoice{Tool: req.ToolID, Model: req.ModelID, Effort: req.Effort}

	m, cmd := naKey(m, key("enter"))
	require.NotNil(t, cmd, "enter on the launch row launches")
	require.True(t, m.NewAgent.Launching)
	require.Equal(t, req.Slug, m.PendingSelectSlug)
	m = naUpdate(m, newAgentLaunchedMsg{Slug: req.Slug, gen: m.NewAgent.launchGen})
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.Nil(t, m.newAgentDraft, "a launch clears the draft")
	require.Equal(t, &want, m.lastSpawn, "the launch is remembered (D3)")
}

func TestNewAgent_CtrlSLaunchesFromAnyStep(t *testing.T) {
	for step := range naStepCount {
		m := naOpen(t, naModel(t))
		if naSkipped(m.NewAgent, step) {
			continue
		}
		m = naAt(m, step)
		m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
		require.NotNil(t, cmd, "step %d", step)
		require.True(t, m.NewAgent.Launching, "step %d", step)
	}
}

func TestNewAgent_EscLadder(t *testing.T) {
	m := naOpen(t, naModel(t)) // opened on the task
	m, _ = naWalk(t, m, naStepSummary)
	for _, want := range []naStep{naStepTrust, naStepContext, naStepTask} {
		m, _ = naKey(m, key("esc"))
		require.Equal(t, want, m.NewAgent.Step)
		require.Equal(t, FocusNewAgent, m.Focus)
	}
	m, cmd := naKey(m, key("esc"))
	require.Nil(t, cmd)
	require.Equal(t, FocusBoard, m.Focus, "esc on the opened step closes the page")
	require.NotNil(t, m.newAgentDraft)

	// Opened on the agent step (a notice), esc walks no further back.
	n := naModel(t)
	n.lastSpawn = &quickChoice{Tool: "grok"}
	n = naOpen(t, n)
	require.Equal(t, naStepAgent, n.NewAgent.Step, "a notice opens on the agent step")
	n, _ = naKey(n, key("esc"))
	require.Equal(t, FocusBoard, n.Focus)
}

func TestNewAgent_TabAndNumbers(t *testing.T) {
	m := naOpen(t, naModel(t)) // task
	tab := tea.KeyMsg{Type: tea.KeyTab}
	back := tea.KeyMsg{Type: tea.KeyShiftTab}
	m, _ = naKey(m, tab)
	require.Equal(t, naStepContext, m.NewAgent.Step)
	m, _ = naKey(m, tab)
	m, _ = naKey(m, tab)
	require.Equal(t, naStepSummary, m.NewAgent.Step)
	m, _ = naKey(m, tab)
	require.Equal(t, naStepSummary, m.NewAgent.Step, "no wrap at the end")
	m = naAt(m, naStepAgent)
	m, _ = naKey(m, back)
	require.Equal(t, naStepAgent, m.NewAgent.Step, "no wrap at the start")

	// 1-7 jump when not typing; a skipped step is a no-op; in the task they are text.
	m = naType(m, "5")
	require.Equal(t, naStepContext, m.NewAgent.Step)
	m = naType(m, "3")
	require.Equal(t, naStepContext, m.NewAgent.Step, "skills do not apply: no-op")
	m = naType(m, "4")
	require.Equal(t, naStepTask, m.NewAgent.Step)
	m = naType(m, "7")
	require.Equal(t, "7", m.NewAgent.Task.String(), "typing in the task")
	require.Equal(t, naStepTask, m.NewAgent.Step)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlG})
	require.Equal(t, naStepAgent, m.NewAgent.Step, "ctrl+g goes to the agent step")
	m = naType(m, "7")
	require.Equal(t, naStepSummary, m.NewAgent.Step)
}

func TestNewAgent_SummaryJumpAndReturn(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix")
	m, _ = naWalk(t, m, naStepSummary)
	launch := len(naSumTargets(m.NewAgent))

	// k walks up to the task row (agent, model, task, context, trust, launch).
	m = naType(m, "kkk")
	require.Equal(t, launch-3, m.NewAgent.SumRow)
	m, cmd := naKey(m, key("enter"))
	require.Nil(t, cmd)
	require.Equal(t, naStepTask, m.NewAgent.Step, "enter on a step row jumps to it")
	require.True(t, m.NewAgent.ReturnToSummary)
	m = naType(m, " more")
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepSummary, m.NewAgent.Step, "enter goes straight back")
	require.False(t, m.NewAgent.ReturnToSummary)
	require.Equal(t, launch-3, m.NewAgent.SumRow, "the cursor stays on the row jumped from")

	// e jumps too; esc returns to the summary.
	m, _ = naKey(m, key("e"))
	require.Equal(t, naStepTask, m.NewAgent.Step)
	m, _ = naKey(m, key("esc"))
	require.Equal(t, naStepSummary, m.NewAgent.Step)
	require.Equal(t, FocusNewAgent, m.Focus)

	// tab ends the return: the next enter does not jump back.
	m, _ = naKey(m, key("e"))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, naStepContext, m.NewAgent.Step)
	require.False(t, m.NewAgent.ReturnToSummary)
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepTrust, m.NewAgent.Step)
}

// After an agent change the return walks the model step (and skills, when
// they apply) before the summary; after a model change it goes on to skills.
func TestNewAgent_ReturnToSummaryWalksModelAndSkills(t *testing.T) {
	m := naModel(t)
	writeHomeSkill(t, filepathHome(t), "go-testing", "tests")
	m = naOpen(t, m)
	m, _ = naWalk(t, m, naStepSummary)
	m = naType(m, "kkkkkkk") // to the top: the agent row
	require.Equal(t, 0, m.NewAgent.SumRow)
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepAgent, m.NewAgent.Step)
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepModel, m.NewAgent.Step)
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepSkills, m.NewAgent.Step)
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepSummary, m.NewAgent.Step)

	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key("enter")) // the model row
	require.Equal(t, naStepModel, m.NewAgent.Step)
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepSkills, m.NewAgent.Step)
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepSummary, m.NewAgent.Step)
}

func TestNewAgent_TrustEnterNeverPicks(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naAt(m, naStepTrust)
	require.Equal(t, trustAsk, m.NewAgent.Trust)
	m, _ = naKey(m, key("j"))
	require.Equal(t, 1, m.NewAgent.TrustCur, "the cursor moved to this session")
	m, _ = naKey(m, key("enter"))
	require.Equal(t, trustAsk, m.NewAgent.Trust, "enter goes next, it never picks")
	require.Equal(t, naStepSummary, m.NewAgent.Step)

	m = naAt(m, naStepTrust)
	require.Equal(t, 0, m.NewAgent.TrustCur, "the cursor opens on the current choice")
	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key(" "))
	require.Equal(t, trustSession, m.NewAgent.Trust, "space picks")
	m = naAt(m, naStepSummary)
	m = naAt(m, naStepTrust)
	require.Equal(t, 1, m.NewAgent.TrustCur)
	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key(" "))
	require.Equal(t, trustAlways, m.NewAgent.Trust)
	m, _ = naKey(m, key("ctrl+t"))
	require.Equal(t, trustSession, m.NewAgent.Trust, "ctrl+t works on any step")
	require.Equal(t, 1, m.NewAgent.TrustCur, "and the cursor follows")
}

// Outside the home folder nothing can be saved: the always row is dimmed with
// its reason and the cursor skips it.
func TestNewAgent_TrustAlwaysOutsideHome(t *testing.T) {
	m := naModel(t)
	t.Chdir(t.TempDir())
	m = naOpen(t, m)
	m = naAt(m, naStepTrust)
	out := naView(m)
	require.Contains(t, out, "always: only folders inside your home can be saved")
	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key("j"))
	require.Equal(t, 1, m.NewAgent.TrustCur, "j stops on this session")
	m, _ = naKey(m, key(" "))
	require.Equal(t, trustSession, m.NewAgent.Trust)
}

func TestNewAgent_ContextNextRow(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naPaste(m, nLines(9))
	m = naAt(m, naStepContext)
	m, _ = naKey(m, key("j"))
	require.Equal(t, 1, m.NewAgent.ItemIdx, "the last row is next ›")
	require.Contains(t, naView(m), "▸ next ›")
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepTrust, m.NewAgent.Step, "enter on next goes to the next step")

	m = naAt(m, naStepContext)
	m, _ = naKey(m, key("k"))
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naPreview, m.NewAgent.Screen, "enter on an item previews")
	m, _ = naKey(m, key("esc"))

	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, naStepTrust, m.NewAgent.Step, "tab always goes next")

	// No items: enter goes next.
	e := naOpen(t, naModel(t))
	e = naAt(e, naStepContext)
	e, _ = naKey(e, key("enter"))
	require.Equal(t, naStepTrust, e.NewAgent.Step)
}

func TestNewAgent_EditorKeys(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix the bug")
	m, _ = naKey(m, key("ctrl+w"))
	require.Equal(t, "fix the ", m.NewAgent.Task.String())
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	require.Equal(t, "fix ", m.NewAgent.Task.String())
	m, _ = naKey(m, key(" "))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyHome})
	m = naType(m, ">")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyEnd})
	m = naType(m, "<")
	require.Equal(t, ">fix  <", m.NewAgent.Task.String())
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = naKey(m, key("backspace"))
	require.Equal(t, ">fix <", m.NewAgent.Task.String())
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = naType(m, "b")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyUp})
	require.Equal(t, 0, m.NewAgent.Task.Row)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, 1, m.NewAgent.Task.Row)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRight})
	require.Equal(t, 1, m.NewAgent.Task.Row)

	// ctrl+u clears the line; on an empty line it clears the task text only.
	m, _ = naKey(m, key("ctrl+u"))
	require.Equal(t, ">fix \n", m.NewAgent.Task.String())
	m.NewAgent.Trust = trustSession
	m.NewAgent.SkillSel = map[string]bool{"x": true}
	m = naPaste(m, nLines(9))
	m, _ = naKey(m, key("ctrl+u"))
	m, _ = naKey(m, key("ctrl+u"))
	require.True(t, m.NewAgent.Task.Empty())
	require.Equal(t, trustSession, m.NewAgent.Trust, "the trust choice stays")
	require.Len(t, m.NewAgent.Items, 1, "and the context")
	require.Len(t, m.NewAgent.SkillSel, 1, "and the skills")
}

func TestNewAgent_QuestionMarkOnAnEmptyTaskOpensHelp(t *testing.T) {
	m := naOpen(t, naModel(t))
	m, _ = naKey(m, key("?"))
	require.Equal(t, FocusHelp, m.Focus)
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusNewAgent, m.Focus)
	m = naType(m, "why")
	m, _ = naKey(m, key("?"))
	require.Equal(t, FocusNewAgent, m.Focus, "a letter once the task has text")
	require.Equal(t, "why?", m.NewAgent.Task.String())
}

func TestNewAgent_EscKeepsDraft(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "keep me")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = naType(m, "please")
	m, cmd := naKey(m, key("esc"))
	require.Nil(t, cmd)
	require.Equal(t, FocusBoard, m.Focus, "the page opened on the task: esc closes it")
	require.Nil(t, m.NewAgent)
	require.NotNil(t, m.newAgentDraft)

	m = naOpen(t, m)
	require.Equal(t, "keep me\nplease", m.NewAgent.Task.String())
	require.Contains(t, ansi.Strip(m.View()), "keep me")
}

func TestNewAgent_DraftRestoresItsStep(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "x")
	m = naAt(m, naStepTrust)
	m, _ = naKey(m, key("q"))
	require.Equal(t, FocusBoard, m.Focus)
	m = naOpen(t, m)
	require.Equal(t, naStepTrust, m.NewAgent.Step)
	require.Equal(t, naStepTrust, m.NewAgent.Opened, "esc closes from the step it opened on")
}

func TestNewAgent_TrustKeys(t *testing.T) {
	m := naOpen(t, naModel(t))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
	require.Empty(t, m.Err)
	require.Equal(t, trustSession, m.NewAgent.Trust)
	req, err := m.NewAgent.request(spawnCWD(m), nil)
	require.NoError(t, err)
	_, req, ok := newAgentTrustRequest(m, spawnCWD(m), req)
	require.True(t, ok)
	require.True(t, req.TrustFolder)

	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Equal(t, trustAlways, m.NewAgent.Trust)
	m = naAt(m, naStepTrust)
	require.Contains(t, naView(m), "(•) always: saves ~/dev/proj to auto_trust_dirs")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Equal(t, trustAsk, m.NewAgent.Trust, "a second press turns it off")
	require.Contains(t, naView(m), "(•) ask: the agent will ask")

	// ctrl+a saves the repo folder at launch.
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Empty(t, m.Store.String("auto_trust_dirs"), "nothing is written until launch")
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd)
	require.Equal(t, "~/dev/proj", m.Store.String("auto_trust_dirs"))
}

func TestNewAgent_DefaultsFromLastSpawn(t *testing.T) {
	m := naModel(t)
	first := naOpen(t, m)
	require.Equal(t, "claude", first.NewAgent.selection().selectedTool().ID, "default_spawn_tool")

	m.lastSpawn = &quickChoice{Tool: "codex"}
	m = naOpen(t, m)
	require.Equal(t, "codex", m.NewAgent.selection().selectedTool().ID, "the last launch wins")
	require.Equal(t, naStepTask, m.NewAgent.Step)

	// A remembered agent that is no longer offered falls back with a notice.
	m, _ = naKey(m, key("esc"))
	require.Equal(t, "codex", m.newAgentDraft.choice().Tool, "the draft keeps its agent")
	m.newAgentDraft = nil
	m.lastSpawn = &quickChoice{Tool: "grok"}
	m = naOpen(t, m)
	require.Equal(t, "claude", m.NewAgent.selection().selectedTool().ID)
	require.Contains(t, m.NewAgent.Notice, "grok isn't available")
	require.Empty(t, m.Notice, "the board never sees it")
	require.Equal(t, naStepAgent, m.NewAgent.Step, "a notice opens on the agent step")
	require.Contains(t, naView(m), "! grok isn't available; using claude")
	m, _ = naKey(m, key("enter"))
	require.Empty(t, m.NewAgent.Notice, "confirming the agent clears it")
}

func TestNewAgent_NoAgentInstalled(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t)
	m, _ = naKey(m, key("n"))
	require.Equal(t, FocusBoard, m.Focus)
	require.Contains(t, m.Err, "no agent is installed")
}

// While the daemon's tool list is not in, enter on the agent step waits.
func TestNewAgent_ToolsLoadingEnterIsANoOp(t *testing.T) {
	m := naModel(t)
	m.DaemonTools, m.toolsKnown = nil, false
	m = naAgentOpen(t, m)
	require.Contains(t, naView(m), "checking which agents are installed…")
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepAgent, m.NewAgent.Step)
	m = adoptDaemonTools(m, daemonTools(t, "claude", "codex"))
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepModel, m.NewAgent.Step)
}

func TestNewAgent_WheelMovesTheStepList(t *testing.T) {
	m := naAgentOpen(t, naModel(t))
	wheel := func(m Model, b tea.MouseButton) Model {
		return updateNewAgentMouse(m, tea.MouseMsg{Button: b, Action: tea.MouseActionPress})
	}
	m = wheel(m, tea.MouseButtonWheelDown)
	require.Equal(t, "codex", m.NewAgent.choice().Tool, "wheel down is j")
	m = wheel(m, tea.MouseButtonWheelUp)
	require.Equal(t, "claude", m.NewAgent.choice().Tool)

	m = naAt(m, naStepTask)
	m = naType(m, "abc")
	m = wheel(m, tea.MouseButtonWheelDown)
	require.Equal(t, "abc", m.NewAgent.Task.String(), "the task ignores the wheel")
	require.Equal(t, naStepTask, m.NewAgent.Step)

	m = naAt(m, naStepSummary)
	row := m.NewAgent.SumRow
	m = wheel(m, tea.MouseButtonWheelUp)
	require.Equal(t, row-1, m.NewAgent.SumRow)
}

// The failure of a launch lands where the fix is: a context problem on the
// context step, the rest on the summary under the launch row.
func TestNewAgent_LaunchFailureRouting(t *testing.T) {
	m := naOpen(t, naModel(t))
	m.NewAgent.Items = []spawnctx.Item{{Kind: spawnctx.File, Name: "gone.go", Bytes: 3}}
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.Nil(t, cmd)
	require.Equal(t, naStepContext, m.NewAgent.Step, "a path error goes to the context step")
	require.Contains(t, m.NewAgent.Err, "gone.go does not exist (any more). Go to context (5), x removes it.")
	require.Contains(t, naView(m), "✗ gone.go")

	n := naOpen(t, naModel(t))
	n.NewAgent.Launching, n.NewAgent.launchGen = true, -1
	n, _ = handleNewAgentLaunched(n, newAgentLaunchedMsg{Err: errors.New("daemon is not running"), gen: -1})
	require.Equal(t, naStepSummary, n.NewAgent.Step, "any other failure lands on the summary")
	out := naView(n)
	lines := strings.Split(out, "\n")
	launch, errLine := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "launch") && strings.Contains(l, "▸") {
			launch = i
		}
		if strings.Contains(l, "✗ launch failed") {
			errLine = i
		}
	}
	require.Greater(t, launch, 0)
	require.Greater(t, errLine, launch, "the error sits under the launch row")
}

func TestNewAgent_ViewFitsEverySize(t *testing.T) {
	for _, sz := range []goldenSize{{120, 40}, {80, 24}, {60, 20}, {59, 20}, {50, 20}, {40, 12}, {30, 8}} {
		for step := range naStepCount {
			m := naOpen(t, naModel(t))
			m.Width, m.Height = sz.w, sz.h
			m = naType(m, strings.Repeat("a long task line ", 12))
			m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
			m = naType(m, "日本語")
			m = naAt(m, step)
			lines := strings.Split(m.View(), "\n")
			require.Len(t, lines, sz.h, "step %d %dx%d", step, sz.w, sz.h)
			for i, l := range lines {
				require.LessOrEqual(t, ansi.StringWidth(l), sz.w, "step %d %dx%d line %d: %q", step, sz.w, sz.h, i, ansi.Strip(l))
			}
			if sz.h >= 12 {
				require.Contains(t, ansi.Strip(m.View()), "› "+naStepNames[step], "step %d %dx%d: the current line always shows", step, sz.w, sz.h)
			}
		}
	}
}

func TestNewAgent_TranscriptShowsOnlyFinishedSteps(t *testing.T) {
	m := naOpen(t, naModel(t))
	m.Width, m.Height = 80, 24
	m = naAt(m, naStepTask)
	out := naView(m)
	for _, want := range []string{
		"∴ REX › NEW AGENT", "step 4/7",
		"  ✓ agent    ✻ Claude Code", "  ✓ model    opus · effort max", "  – skills   not available",
		"  › task     what should it do?", "describe the task",
	} {
		require.Contains(t, out, want)
	}
	require.NotContains(t, out, "✓ context", "steps not reached are not drawn")
	require.NotContains(t, out, "trust")

	m = naAt(m, naStepAgent)
	require.NotContains(t, naView(m), "✓", "nothing is finished on the first step")
	require.Contains(t, naView(m), "step 1/7")

	m.Width = 50
	narrow := naView(m)
	require.Contains(t, narrow, "step 1/7", "the step count stays under 60 columns")
	require.NotContains(t, narrow, "STEPS")
}

func TestNewAgent_RexNewLine(t *testing.T) {
	m := naOpen(t, naModel(t))
	st := m.NewAgent
	st.SkillSel = map[string]bool{}
	st.Task.Insert("fix the 'auth' test")
	line := naRexNewLine(st, "/w/proj", 300)
	require.Equal(t, `$ rex new -tool claude -model opus -effort max -cwd /w/proj 'fix the '\''auth'\'' test'`, line)

	st.Trust = trustSession
	require.Contains(t, naRexNewLine(st, "/w/proj", 300), " -trust ")
	st.Trust = trustAlways
	line = naRexNewLine(st, "/w/proj", 300)
	require.NotContains(t, line, "-trust")
	require.True(t, strings.HasSuffix(line, "  # + always trust"), line)
	st.Items = []spawnctx.Item{{Kind: spawnctx.Paste, Name: "paste 1", Text: "x"}}
	require.True(t, strings.HasSuffix(naRexNewLine(st, "/w/proj", 300), "  # + context + always trust"))

	st.Task.Clear()
	st.Task.Insert("fix it")
	st.Trust = trustAsk
	st.Items = nil
	require.Equal(t, `$ rex new -tool claude -model opus -effort max -cwd /w/proj 'fix it'`, naRexNewLine(st, "/w/proj", 300))
	require.LessOrEqual(t, ansi.StringWidth(naRexNewLine(st, "/w/proj", 30)), 30)
	require.Contains(t, naRexNewLine(st, "/w/proj", 30), "…")
}

func TestNewAgent_SummaryWindowedAndCollapsed(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix")
	for range 7 {
		m = naPaste(m, nLines(9))
	}
	m = naAt(m, naStepSummary)

	// 120x40: everything shows, the sub-rows and the rex new line too.
	m.Width, m.Height = 120, 40
	out := naView(m)
	require.Contains(t, out, "paste 6")
	require.NotContains(t, out, "paste 7")
	require.Contains(t, out, "… +1 more")
	require.Contains(t, out, "$ rex new -tool claude")
	require.Contains(t, out, "▸ launch")

	// 50x20: the sub-rows collapse, the launch row stays pinned.
	m.Width, m.Height = 50, 20
	out = naView(m)
	require.NotContains(t, out, "paste 6")
	require.Contains(t, out, "context  7 items")
	require.Contains(t, out, "▸ launch")

	// 50x12: the rows window around the cursor.
	m.Width, m.Height = 50, 12
	lines := strings.Split(naView(m), "\n")
	require.Len(t, lines, 12)
	require.Contains(t, strings.Join(lines, "\n"), "▸ launch")
	for range 7 {
		m, _ = naKey(m, key("k"))
	}
	require.Contains(t, naView(m), "▸ agent")
}
