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

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// naUpdate sends msg through Model.Update, as the program would.
func naUpdate(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

// naLaunch types task and presses enter; the page must start launching.
func naLaunch(t *testing.T, m Model, task string) (Model, tea.Cmd) {
	t.Helper()
	m = naType(m, task)
	m, cmd := naKey(m, key("enter"))
	require.NotNil(t, cmd, "enter launches")
	require.NotNil(t, m.NewAgent, "the page stays open until the daemon answers")
	require.True(t, m.NewAgent.Launching)
	return m, cmd
}

// naReq is the request the page would send now, through the trust step.
func naReq(t *testing.T, m Model) (Model, protocol.NewSession, bool) {
	t.Helper()
	cwd := spawnCWD(m)
	req, err := m.NewAgent.request(cwd, nil)
	require.NoError(t, err)
	return newAgentTrustRequest(m, cwd, req)
}

// TestNewAgent_TrustKeysMatchWizard runs P2's trust cases through the page:
// the same offers, the same refusals, and nothing trusted unless asked.
func TestNewAgent_TrustKeysMatchWizard(t *testing.T) {
	t.Run("ctrl+t trusts this session and toggles", func(t *testing.T) {
		m := naOpen(t, naModel(t))
		_, req, ok := naReq(t, m)
		require.True(t, ok)
		require.False(t, req.TrustFolder, "nothing is trusted unless the user acts")
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
		m, req, ok = naReq(t, m)
		require.True(t, ok)
		require.True(t, req.TrustFolder)
		require.Empty(t, m.Store.String("auto_trust_dirs"), "per-session trust saves nothing")
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
		_, req, _ = naReq(t, m)
		require.False(t, req.TrustFolder)
	})
	t.Run("ctrl+a saves the repo folder at launch", func(t *testing.T) {
		m := naOpen(t, naModel(t))
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
		require.Empty(t, m.Store.String("auto_trust_dirs"), "nothing is written until launch")
		m, req, ok := naReq(t, m)
		require.True(t, ok)
		require.True(t, req.TrustFolder)
		require.Equal(t, "~/dev/proj", m.Store.String("auto_trust_dirs"))
	})
	t.Run("the trust section names the exact folders", func(t *testing.T) {
		m := naOpen(t, naModel(t))
		m.Width, m.Height = 120, 40
		m.NewAgent.Section = naTrust
		out := ansi.Strip(m.View())
		var pane strings.Builder // the form column, without the rail
		for _, l := range strings.Split(out, "\n") {
			if _, right, ok := strings.Cut(l, "│"); ok {
				pane.WriteString(strings.Join(strings.Fields(right), ""))
			}
		}
		flat := pane.String()
		require.Contains(t, out, "folder not trusted")
		require.Contains(t, out, "trust this session only")
		require.Contains(t, out, "always trust (saved to auto_trust_dirs)")
		require.Contains(t, flat, spawnCWD(m), "ctrl+t's folder, exact")
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
		require.Contains(t, ansi.Strip(m.View()), selectGlyph()+" ctrl+t", "the chosen key is marked")
		for _, sz := range []goldenSize{{120, 40}, {80, 24}, {60, 20}, {59, 20}, {50, 20}, {40, 12}} {
			m.Width, m.Height = sz.w, sz.h
			lines := strings.Split(m.View(), "\n")
			require.Len(t, lines, sz.h, "%dx%d", sz.w, sz.h)
			for _, l := range lines {
				require.LessOrEqual(t, ansi.StringWidth(l), sz.w, "%dx%d %q", sz.w, sz.h, ansi.Strip(l))
			}
		}
	})
	t.Run("a covered folder needs nothing", func(t *testing.T) {
		m := naOpen(t, naModel(t))
		require.NoError(t, m.Store.Set("auto_trust_dirs", spawnCWD(m)))
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
		_, req, ok := naReq(t, m)
		require.True(t, ok)
		require.False(t, req.TrustFolder, "already covered: nothing to ask the daemon")
		m.NewAgent.Section = naTrust
		require.Contains(t, ansi.Strip(m.View()), "trusted (auto_trust_dirs)")
	})
	t.Run("the home folder is refused on the page", func(t *testing.T) {
		m := naOpen(t, naModel(t))
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		t.Chdir(home)
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
		require.Empty(t, m.Err, "the page shows it, not the board")
		require.Equal(t, "✗ trust: home folder: start rex in a project folder to trust it", m.NewAgent.Err)
		require.Equal(t, trustAsk, m.NewAgent.Trust)
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
		require.Empty(t, m.Store.String("auto_trust_dirs"))
		require.Contains(t, naView(m), "✗ trust: home folder")
	})
	t.Run("ctrl+a without settings fails the launch on the page", func(t *testing.T) {
		m := naOpen(t, naModel(t))
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
		m.Store = nil
		m = naType(m, "task")
		m, cmd := naKey(m, key("enter"))
		require.Nil(t, cmd, "nothing is sent")
		require.Equal(t, FocusNewAgent, m.Focus)
		require.False(t, m.NewAgent.Launching)
		require.Empty(t, m.Err)
		require.Contains(t, m.NewAgent.Err, "✗ trust: settings are not loaded")
		require.Empty(t, m.PendingSelectSlug)
	})
}

func TestNewAgent_LaunchKeepsPageUntilAnswer(t *testing.T) {
	m := naOpen(t, naModel(t))
	m.Width, m.Height = 80, 24
	m, _ = naLaunch(t, m, "fix the bug")
	require.Equal(t, FocusNewAgent, m.Focus)
	require.NotEmpty(t, m.PendingSelectSlug)
	require.Contains(t, naView(m), "starting…")

	// Keys other than esc do nothing while it starts.
	m = naType(m, "zz")
	require.Equal(t, "fix the bug", m.NewAgent.Task.String())
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.Nil(t, cmd, "no second launch")
	m, cmd = naKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pasted"), Paste: true})
	require.Nil(t, cmd)
	require.Equal(t, "fix the bug", m.NewAgent.Task.String())

	for _, sz := range []goldenSize{{120, 40}, {80, 24}, {50, 20}} {
		m.Width, m.Height = sz.w, sz.h
		lines := strings.Split(m.View(), "\n")
		require.Len(t, lines, sz.h)
		require.Contains(t, ansi.Strip(m.View()), "starting…", "%dx%d", sz.w, sz.h)
		for _, l := range lines {
			require.LessOrEqual(t, ansi.StringWidth(l), sz.w)
		}
	}

	// esc closes the page and keeps the draft.
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)
	require.NotNil(t, m.newAgentDraft)
	require.Equal(t, "fix the bug", m.newAgentDraft.Task.String())
}

func TestNewAgent_LaunchErrorKeepsDraft(t *testing.T) {
	failures := map[string]func(Model) tea.Msg{
		"CmdErrMsg": func(Model) tea.Msg {
			return CmdErrMsg{Op: "spawn", Err: errors.New("daemon is not running (rex daemon start)")}
		},
		"launched": func(m Model) tea.Msg {
			return newAgentLaunchedMsg{Err: errors.New("daemon is not running (rex daemon start)"), gen: m.NewAgent.launchGen}
		},
	}
	for name, fail := range failures {
		t.Run(name, func(t *testing.T) {
			m := naOpen(t, naModel(t))
			m.Width, m.Height = 80, 24
			m = naPaste(m, nLines(12))
			m, _ = naLaunch(t, m, " fix it")
			task := m.NewAgent.Task.String()
			m = naUpdate(m, fail(m))
			require.Equal(t, FocusNewAgent, m.Focus, "a failure never closes the page")
			st := m.NewAgent
			require.False(t, st.Launching)
			require.Equal(t, task, st.Task.String())
			require.Len(t, st.Items, 1)
			require.Equal(t, "✗ launch failed: daemon is not running (rex daemon start). Your draft is kept.", st.Err)
			require.Empty(t, m.Err, "not a board error too")
			require.Empty(t, m.PendingSelectSlug)
			flat := strings.Join(strings.Fields(naView(m)), " ")
			require.Contains(t, flat, "✗ launch failed: daemon is not running (rex daemon start). │ Your draft is kept.",
				"wrapped at a word, never cut")

			// The next key clears it, and a retry launches again.
			m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyEnd})
			require.Empty(t, m.NewAgent.Err)
			m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
			require.NotNil(t, cmd)
			require.True(t, m.NewAgent.Launching)
		})
	}

	t.Run("after esc the board says it", func(t *testing.T) {
		m := naOpen(t, naModel(t))
		m, _ = naLaunch(t, m, "fix it")
		gen := m.NewAgent.launchGen
		m, _ = naKey(m, key("esc"))
		m = naUpdate(m, newAgentLaunchedMsg{Err: errors.New("bad model"), gen: gen})
		require.Equal(t, "launch failed: bad model. Your draft is kept (n reopens it).", m.Err)
		require.NotNil(t, m.newAgentDraft)
		require.False(t, m.newAgentDraft.Launching)
		m = naOpen(t, m)
		require.Equal(t, "fix it", m.NewAgent.Task.String())
	})

	t.Run("a stale answer is ignored", func(t *testing.T) {
		m := naOpen(t, naModel(t))
		m, _ = naLaunch(t, m, "fix it")
		m = naUpdate(m, newAgentLaunchedMsg{Err: errors.New("old"), gen: m.NewAgent.launchGen + 100})
		require.True(t, m.NewAgent.Launching)
		require.Empty(t, m.NewAgent.Err)
		require.Empty(t, m.Err)
	})
}

func TestNewAgent_LaunchSuccessClearsDraft(t *testing.T) {
	m := naOpen(t, naModel(t))
	m, _ = naLaunch(t, m, "fix the bug")
	slug := m.PendingSelectSlug
	want := m.NewAgent.choice()
	require.Nil(t, m.lastSpawn, "remembered only once it started")
	m = naUpdate(m, newAgentLaunchedMsg{Slug: slug, gen: m.NewAgent.launchGen})
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.Nil(t, m.newAgentDraft, "a launch clears the draft")
	require.Equal(t, &want, m.lastSpawn)
	require.Empty(t, m.Err)

	// The new card is selected when it arrives.
	m = m.selectIfPending(protocol.SessionSummary{ID: "s-new", Slug: slug})
	require.Equal(t, "s-new", m.SelectedID)

	// After esc, a success still clears the draft.
	m = naOpen(t, m)
	m, _ = naLaunch(t, m, "second")
	gen := m.NewAgent.launchGen
	m, _ = naKey(m, key("esc"))
	require.NotNil(t, m.newAgentDraft)
	m = naUpdate(m, newAgentLaunchedMsg{Slug: "x", gen: gen})
	require.Nil(t, m.newAgentDraft)
}

func TestNewAgent_LaunchRevalidates(t *testing.T) {
	t.Run("a link swapped outside after add", func(t *testing.T) {
		m := naModel(t)
		root := naFiles(t, map[string]string{"a.go": "package a\n"})
		outside := filepath.Join(t.TempDir(), "key.txt")
		require.NoError(t, os.WriteFile(outside, []byte("k"), 0o600))
		link := filepath.Join(root, "notes.txt")
		if err := os.Symlink(filepath.Join(root, "a.go"), link); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		m = naIndexed(t, m)
		require.NoError(t, addPath(m.NewAgent, "notes.txt", false))
		require.NoError(t, os.Remove(link))
		require.NoError(t, os.Symlink(outside, link))

		m = naType(m, "read the notes")
		m, cmd := naKey(m, key("enter"))
		require.Nil(t, cmd, "nothing is sent")
		require.False(t, m.NewAgent.Launching)
		require.Contains(t, m.NewAgent.Err, "✗ notes.txt links to ")
		require.Contains(t, m.NewAgent.Err, "outside ~/dev/proj")
		require.Empty(t, m.PendingSelectSlug)
	})
	t.Run("a file gone since it was added", func(t *testing.T) {
		m := naModel(t)
		root := naFiles(t, map[string]string{"a.go": "package a\n"})
		m = naIndexed(t, m)
		require.NoError(t, addPath(m.NewAgent, "a.go", false))
		require.NoError(t, os.Remove(filepath.Join(root, "a.go")))
		m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
		require.Nil(t, cmd)
		require.Contains(t, m.NewAgent.Err, "✗ a.go does not exist (any more).")
	})
	t.Run("a draft reopened under another folder", func(t *testing.T) {
		m := naModel(t)
		naFiles(t, map[string]string{"a.go": "package a\n"})
		m = naIndexed(t, m)
		require.NoError(t, addPath(m.NewAgent, "a.go", false))
		m, _ = naKey(m, key("esc"))

		home, err := os.UserHomeDir()
		require.NoError(t, err)
		other := filepath.Join(home, "dev", "other")
		require.NoError(t, os.MkdirAll(other, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(other, "a.go"), []byte("package b\n"), 0o600))
		t.Chdir(other)
		m = naOpen(t, m)
		m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
		require.Nil(t, cmd, "the same name in another folder is another file")
		require.Contains(t, m.NewAgent.Err, "✗ a.go was added in ~/dev/proj; new agents now start in ~/dev/other.")
		require.Len(t, m.NewAgent.Items, 1, "the draft is kept")
	})
}

func TestNewAgent_PTYPasteCap(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t, "claude", "ollama")
	m.lastSpawn = &quickChoice{Tool: "ollama"}
	m = naOpen(t, m)
	require.Equal(t, "ollama", m.NewAgent.choice().Tool)
	m = naPaste(m, strings.Repeat("0123456789abcde\n", 17<<10/16))
	require.Len(t, m.NewAgent.Items, 1)

	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.Nil(t, cmd, "over 16 KB is refused for a prompt_paste agent")
	require.False(t, m.NewAgent.Launching)
	require.Contains(t, m.NewAgent.Err, "✗ the prompt is 17 KB; Ollama (local) takes it as a terminal paste and the limit for that is 16 KB.")
	require.Contains(t, m.NewAgent.Err, "\n", "with a hint line")

	// The same draft fits another agent's 96 KB.
	m.NewAgent.choose(quickChoice{Tool: "claude"})
	m, cmd = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd)
	require.True(t, m.NewAgent.Launching)
}

func TestNewAgent_PromptCapInKB(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naPaste(m, strings.Repeat("x", 32<<10-1))
	m = naPaste(m, strings.Repeat("y", 32<<10-1))
	m.NewAgent.Task.Insert(strings.Repeat("t", 34<<10))
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.Nil(t, cmd)
	lines := strings.Split(m.NewAgent.Err, "\n")
	require.Equal(t, "✗ the prompt is 98 KB; the limit is 96 KB.", lines[0])
	require.Equal(t, "Save big pastes to a file in the workspace and import them (ctrl+o).", lines[1])
	require.NotContains(t, m.NewAgent.Err, "bytes")
	require.NotContains(t, m.NewAgent.Err, "too big")
}

func TestNewAgent_FlagOffKeepsDraft(t *testing.T) {
	m := naOpen(t, naModel(t))
	m.Width, m.Height = 80, 24
	m = naType(m, "keep this")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = naType(m, "and this")
	m = naPaste(m, nLines(12))
	m = setFlag(t, m, "new_agent_page", false)
	require.NotContains(t, ansi.Strip(m.View()), "NEW AGENT", "never drawn while off")

	// Any message closes it, not only a key.
	m = naUpdate(m, SpinnerTickMsg{Gen: -1})
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.NotNil(t, m.newAgentDraft)
	require.Len(t, m.newAgentDraft.Items, 1)

	// The older wizard gets the task text only, and says so.
	m, _ = naKey(m, key("n"))
	require.Equal(t, FocusWizard, m.Focus)
	require.Equal(t, "keep this and this[paste 1 · 12 lines]", m.Wizard.TaskText)
	require.Contains(t, m.Notice, "1 context item was dropped")
	require.NotNil(t, m.newAgentDraft, "the draft stays for when the page is back")
}

func TestNewAgent_RequestCarriesSummary(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{"a.go": "package a\n", "pkg/b.go": "package b\n"})
	m = naIndexed(t, m)
	st := m.NewAgent
	require.NoError(t, addPath(st, "a.go", false))
	require.NoError(t, addPath(st, "pkg/", false))
	m = naPaste(m, nLines(12))
	m = naType(m, " fix")
	req, err := m.NewAgent.request(spawnCWD(m), nil)
	require.NoError(t, err)
	require.Equal(t, []protocol.ContextRef{
		{Kind: "file", Name: "a.go", Bytes: 10},
		{Kind: "dir", Name: "pkg", Bytes: 0},
		{Kind: "paste", Name: "paste 1", Bytes: m.NewAgent.Items[2].Bytes},
	}, req.ContextSummary)
	require.Contains(t, req.InitialPrompt, "Context from rex (read before starting):\n- file: a.go\n- dir: pkg/ (1 files)")
	require.Contains(t, req.InitialPrompt, `<pasted name="paste 1" lines="12">`)
	require.Equal(t, spawnCWD(m), req.CWD)
}

func TestNewAgent_ContextLoadingRow(t *testing.T) {
	m := naOpen(t, naModel(t)) // the walk never comes back
	m.Width, m.Height = 80, 24
	m.NewAgent.indexCount.Store(3812)
	out := naView(m)
	require.Contains(t, out, "indexing workspace… 3,812 files")
	require.Contains(t, out, "(picker usable after)")
	require.NotContains(t, out, "none yet")

	m.NewAgent.Indexing = false
	out = naView(m)
	require.Contains(t, out, "none yet: type @path, paste, or ctrl+o to import")
	require.NotContains(t, out, "indexing workspace")
}

// naFakeSpawner is the daemon side of a launch.
type naFakeSpawner struct {
	doErr   error
	sent    []protocol.NewSession
	doCalls int
}

func (f *naFakeSpawner) Do(_ context.Context, intent string, payload any) (protocol.Envelope, error) {
	f.doCalls++
	if f.doErr != nil {
		return protocol.Envelope{}, f.doErr
	}
	return protocol.Envelope{Type: protocol.EventAck}, nil
}

func (f *naFakeSpawner) NewSession(req protocol.NewSession) error {
	f.sent = append(f.sent, req)
	return nil
}

func TestNewAgentLaunchCmd(t *testing.T) {
	msg := newAgentLaunchCmd(nil, protocol.NewSession{Slug: "a"})()
	got, ok := msg.(newAgentLaunchedMsg)
	require.True(t, ok)
	require.EqualError(t, got.Err, "daemon is not running (rex daemon start)")

	ok1 := naSpawn(context.Background(), &naFakeSpawner{}, protocol.NewSession{Slug: "s"})
	require.NoError(t, ok1)

	refused := naSpawn(context.Background(), &naFakeSpawner{doErr: &client.Error{Code: protocol.ErrCodeBadIntent, Message: "unknown tool"}}, protocol.NewSession{})
	require.EqualError(t, refused, "unknown tool")

	slow := naSpawn(context.Background(), &naFakeSpawner{doErr: context.DeadlineExceeded}, protocol.NewSession{})
	require.ErrorContains(t, slow, "no answer from rex-daemon in 30s")

	closed := naSpawn(context.Background(), &naFakeSpawner{doErr: client.ErrClosed}, protocol.NewSession{})
	require.EqualError(t, closed, "daemon is not running (rex daemon start)")

	// A daemon without acks gets the request the old way.
	old := &naFakeSpawner{doErr: client.ErrNoAcks}
	require.NoError(t, naSpawn(context.Background(), old, protocol.NewSession{Slug: "s"}))
	require.Len(t, old.sent, 1)

	// An empty slug is "session", as the wizard sends it.
	empty := &naFakeSpawner{doErr: client.ErrNoAcks}
	require.NoError(t, naSpawn(context.Background(), empty, protocol.NewSession{}))
	require.Equal(t, "session", empty.sent[0].Slug)
}
