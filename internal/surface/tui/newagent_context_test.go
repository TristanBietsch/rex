package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
)

// naPaste sends text as one bracketed paste.
func naPaste(m Model, text string) Model {
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	return m
}

// nLines is n numbered lines joined by "\n".
func nLines(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "line " + string(rune('a'+i%26))
	}
	return strings.Join(parts, "\n")
}

func TestPaste_SmallInline(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "see ")
	m = naPaste(m, nLines(8)) // 8 lines: not more than 8
	st := m.NewAgent
	require.Empty(t, st.Items)
	require.Equal(t, "see "+nLines(8), st.Task.String())

	// 1 KiB exactly stays inline too.
	var s NewAgentState
	require.NoError(t, addPaste(&s, strings.Repeat("x", 1024)))
	require.Empty(t, s.Items)
	require.Equal(t, 1024, len(s.Task.String()))
}

func TestPaste_BigBecomesChip(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix ")
	m = naPaste(m, nLines(9)+"\n")
	st := m.NewAgent
	require.Len(t, st.Items, 1)
	it := st.Items[0]
	require.Equal(t, spawnctx.Paste, it.Kind)
	require.Equal(t, "paste 1", it.Name)
	require.Equal(t, len(nLines(9)), it.Bytes)
	require.Equal(t, "fix [paste 1 · 9 lines]", st.Task.String())

	// Over 1 KiB on one line is a chip too; the number goes on.
	require.NoError(t, addPaste(st, strings.Repeat("y", 1025)))
	require.Len(t, st.Items, 2)
	require.Equal(t, "paste 2", st.Items[1].Name)
	require.Contains(t, st.Task.String(), "[paste 2 · 1 line]")

	// The placeholder counts sanitized lines, as the block's lines attribute.
	var s NewAgentState
	require.NoError(t, addPaste(&s, strings.Repeat("a\r\n", 5)+strings.Repeat("b\r", 5)))
	require.Equal(t, "[paste 1 · 10 lines]", s.Task.String())
	prompt, err := spawnctx.Assemble(s.Task.String(), s.Items, spawnctx.DefaultCaps)
	require.NoError(t, err)
	require.Contains(t, prompt, `lines="10"`)
}

func TestPaste_SanitizedAndBinaryRefused(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naPaste(m, "a\x1b[31mred\x1b[0m‮\U000E0041b")
	require.Equal(t, "aredb", m.NewAgent.Task.String())

	m = naPaste(m, "\x00\x01\x02ELF\x00\x00")
	st := m.NewAgent
	require.True(t, strings.HasPrefix(st.Err, "✗ "), st.Err)
	require.Contains(t, st.Err, "binary")
	require.Equal(t, "aredb", st.Task.String(), "a binary paste changes nothing")
	require.Empty(t, st.Items)
	require.Contains(t, ansi.Strip(m.View()), "✗ ")

	// The error stays until the next key.
	m = naType(m, "c")
	require.Empty(t, m.NewAgent.Err)
}

func TestPaste_OverCapError(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naPaste(m, nLines(20))
	big := strings.Repeat(strings.Repeat("z", 1023)+"\n", 210)
	m = naPaste(m, big)
	st := m.NewAgent
	require.Len(t, st.Items, 1, "the paste over the cap is not added")
	require.True(t, strings.HasPrefix(st.Err, "✗ paste 2 is 210 KB; the limit for pasted text is 64 KB in total."), st.Err)
	m.Width, m.Height = 120, 40
	require.Contains(t, ansi.Strip(m.View()), "Save it to a file in the workspace and import it (ctrl+o).")

	// One paste over 32 KiB, inside the total.
	var s NewAgentState
	err := addPaste(&s, strings.Repeat("q", 40<<10))
	require.EqualError(t, err, "paste 1 is 40 KB; the limit for one paste is 32 KB.")

	// 32 items at most.
	for range 32 {
		s.Items = append(s.Items, spawnctx.Item{Kind: spawnctx.URL, Name: "https://example.com/x"})
	}
	require.EqualError(t, addPaste(&s, nLines(9)), "32 context items is the limit; remove one first.")
}

func TestContextList_RemoveRenamePreview(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naPaste(m, nLines(9))
	m = naPaste(m, nLines(12))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab}) // Context
	st := m.NewAgent
	require.Equal(t, naContext, st.Section)
	require.Equal(t, 0, st.ItemIdx)

	m = naType(m, "j")
	require.Equal(t, 1, m.NewAgent.ItemIdx)
	m = naType(m, "j")
	require.Equal(t, 1, m.NewAgent.ItemIdx, "the cursor stops at the last item")

	// r renames: the name and its placeholder in the task.
	m = naType(m, "r")
	require.True(t, m.NewAgent.Renaming)
	m, _ = naKey(m, key("ctrl+u"))
	m = naType(m, "ci log")
	m, cmd := naKey(m, key("enter"))
	require.Nil(t, cmd, "enter saves the name, never launches")
	st = m.NewAgent
	require.False(t, st.Renaming)
	require.Equal(t, "ci log", st.Items[1].Name)
	require.Equal(t, "[paste 1 · 9 lines][ci log · 12 lines]", st.Task.String())

	// A name that would break the prompt is refused with ✗.
	m = naType(m, "r")
	m, _ = naKey(m, key("ctrl+u"))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a b")})
	m, _ = naKey(m, key("enter"))
	require.True(t, strings.HasPrefix(m.NewAgent.Err, "✗ "), m.NewAgent.Err)
	require.Equal(t, "ci log", m.NewAgent.Items[1].Name)
	require.True(t, m.NewAgent.Renaming, "a refused name stays in the editor")
	m, _ = naKey(m, key("esc"))
	require.False(t, m.NewAgent.Renaming)
	require.Equal(t, FocusNewAgent, m.Focus, "esc cancels the rename, not the page")
	require.Error(t, renameItem(m.NewAgent, 1, "paste 1"), "names stay unique")

	// enter previews.
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naPreview, m.NewAgent.Screen)
	m.Width, m.Height = 80, 24
	out := ansi.Strip(m.View())
	require.Contains(t, out, "CI LOG")
	require.Contains(t, out, "12 lines")
	require.Contains(t, out, "esc back")

	// x in the preview removes the item and goes back; the placeholder goes.
	m = naType(m, "x")
	st = m.NewAgent
	require.Equal(t, naCompose, st.Screen)
	require.Len(t, st.Items, 1)
	require.Equal(t, 0, st.ItemIdx)
	require.Equal(t, "[paste 1 · 9 lines]", st.Task.String())

	// x in the list removes too; the list is empty again.
	m = naType(m, "x")
	require.Empty(t, m.NewAgent.Items)
	require.Empty(t, m.NewAgent.Task.String())
	require.Contains(t, ansi.Strip(m.View()), "none yet")

	// The next paste keeps numbering on.
	m = naPaste(m, nLines(9))
	require.Equal(t, "paste 3", m.NewAgent.Items[0].Name)
}

func TestContextList_ViewAndSummary(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix the flaky test ")
	m = naPaste(m, "--- FAIL: TestLogin (0.31s)\n"+nLines(37))
	m.Width, m.Height = 80, 24
	out := ansi.Strip(m.View())
	require.Contains(t, out, "1 item · ")
	require.Contains(t, out, "paste  paste 1")
	require.Contains(t, out, `"--- FAIL`)
	require.Contains(t, out, "38 lines")
	require.Regexp(t, `Context +1`, out, "the rail counts the items")
	require.Contains(t, out, "prompt ")
	require.Contains(t, out, " of 96 KB · slug ")

	st := m.NewAgent
	rev := st.Rev
	s1 := naLaunchSummary(m, st)
	require.Equal(t, rev, st.sumRev, "the summary is cached by revision")
	require.Equal(t, s1, naLaunchSummary(m, st))
	m = naType(m, "x")
	require.Greater(t, m.NewAgent.Rev, rev, "a key bumps the revision")

	// prompt_paste tools are measured against 16 KB.
	p := naModel(t)
	p.DaemonTools = daemonTools(t, "claude", "ollama")
	p = naOpen(t, p)
	p.NewAgent.choose(quickChoice{Tool: "ollama"})
	require.Equal(t, "ollama", p.NewAgent.choice().Tool)
	p = naType(p, "hi")
	require.Contains(t, ansi.Strip(naLaunchSummary(p, p.NewAgent)), "prompt 2 B of 16 KB")
}

func TestContextList_LaunchAssemblesPrompt(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix it ")
	m = naPaste(m, nLines(9))
	st := m.NewAgent
	req, err := st.request(spawnCWD(m), existingSlugs(m))
	require.NoError(t, err)
	want, err := spawnctx.Assemble("fix it [paste 1 · 9 lines]", st.Items, spawnctx.DefaultCaps)
	require.NoError(t, err)
	require.Equal(t, want, req.InitialPrompt)
	require.Equal(t, spawnctx.Summary(st.Items), req.ContextSummary)

	// An Assemble error stays on the page and nothing launches.
	st.Items[0].Name = "bad\nname"
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.Nil(t, cmd)
	require.Equal(t, FocusNewAgent, m.Focus)
	require.True(t, strings.HasPrefix(m.NewAgent.Err, "✗ "), m.NewAgent.Err)
}

func TestContextList_DraftKeepsItems(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naPaste(m, nLines(9))
	m, _ = naKey(m, key("esc"))
	m = naOpen(t, m)
	require.Len(t, m.NewAgent.Items, 1)
	m = naPaste(m, nLines(9))
	require.Equal(t, "paste 2", m.NewAgent.Items[1].Name, "the draft keeps the paste number")

	// ctrl+u on an empty editor drops the items with the draft.
	m.NewAgent.Task.Clear()
	m, _ = naKey(m, key("ctrl+u"))
	require.Empty(t, m.NewAgent.Items)
}

// fakeExec stands in for tea.ExecProcess: it runs edit on the file, then the
// callback, without a terminal.
func fakeExec(t *testing.T, edit func(path string) error) {
	t.Helper()
	prev := execProcess
	execProcess = func(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
		return func() tea.Msg { return fn(edit(c.Args[len(c.Args)-1])) }
	}
	t.Cleanup(func() { execProcess = prev })
}

func TestEditor_TempFilePerms(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	var path string
	prev := editorCmd
	editorCmd = func(p string) *exec.Cmd {
		path = p
		return exec.Command("true", p)
	}
	t.Cleanup(func() { editorCmd = prev })
	fakeExec(t, func(p string) error {
		fi, err := os.Stat(p)
		require.NoError(t, err)
		di, err := os.Stat(filepath.Dir(p))
		require.NoError(t, err)
		if runtime.GOOS != "windows" {
			require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
			require.Equal(t, os.FileMode(0o700), di.Mode().Perm())
		}
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		require.Equal(t, "draft body", string(b))
		return os.WriteFile(p, []byte("edited\nbody\n"), 0o600)
	})

	msg := editInEditor(3, "draft body")()
	done, ok := msg.(editorDoneMsg)
	require.True(t, ok, "%T", msg)
	require.NoError(t, done.Err)
	require.Equal(t, 3, done.Item)
	require.Equal(t, "edited\nbody\n", done.Text)
	require.NotEmpty(t, path)
	_, err := os.Stat(filepath.Dir(path))
	require.True(t, errors.Is(err, os.ErrNotExist), "the temp folder is gone after the editor")

	// An editor that fails still removes the file.
	fakeExec(t, func(string) error { return errors.New("exit status 1") })
	done = editInEditor(-1, "x")().(editorDoneMsg)
	require.Error(t, done.Err)
	_, err = os.Stat(filepath.Dir(path))
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestEditor_CommandSplitsEditorWithoutShell(t *testing.T) {
	t.Setenv("EDITOR", "code --wait")
	c := editorCmd("/tmp/x/context.txt")
	require.Equal(t, []string{"code", "--wait", "/tmp/x/context.txt"}, c.Args)
	t.Setenv("EDITOR", "")
	c = editorCmd("/tmp/x/context.txt")
	require.Equal(t, "/tmp/x/context.txt", c.Args[len(c.Args)-1])
	require.NotContains(t, c.Args, "-c", "never a shell")
}

func TestEditor_DoneUpdatesPasteAndTask(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	fakeExec(t, func(p string) error { return os.WriteFile(p, []byte(nLines(10)+"\n"), 0o600) })
	prev := editorCmd
	editorCmd = func(p string) *exec.Cmd { return exec.Command("true", p) }
	t.Cleanup(func() { editorCmd = prev })

	m := naOpen(t, naModel(t))
	m = naPaste(m, nLines(9))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = naKey(m, key("enter")) // preview
	m, cmd := naKey(m, key("e"))
	require.NotNil(t, cmd)
	m, _ = update(m, cmd())
	st := m.NewAgent
	require.Equal(t, nLines(10)+"\n", st.Items[0].Text)
	require.Equal(t, "[paste 1 · 10 lines]", st.Task.String())

	// ctrl+e in the task editor edits the task.
	fakeExec(t, func(p string) error { return os.WriteFile(p, []byte("new task\r\n"), 0o600) })
	st.Screen, st.Section = naCompose, naTask
	m, cmd = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlE})
	require.NotNil(t, cmd)
	m, _ = update(m, cmd())
	require.Equal(t, "new task", m.NewAgent.Task.String())

	// A failed editor keeps the text and says so.
	fakeExec(t, func(string) error { return errors.New("exit status 2") })
	m, cmd = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlE})
	m, _ = update(m, cmd())
	require.Equal(t, "new task", m.NewAgent.Task.String())
	require.True(t, strings.HasPrefix(m.NewAgent.Err, "✗ editor: "), m.NewAgent.Err)
}

func TestPreview_ScrollAndBack(t *testing.T) {
	m := naOpen(t, naModel(t))
	m.Width, m.Height = 80, 24
	m = naPaste(m, nLines(40))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = naKey(m, key("enter"))
	st := m.NewAgent
	require.Equal(t, naPreview, st.Screen)
	require.Equal(t, 0, st.PrevScroll)
	out := ansi.Strip(m.View())
	require.Contains(t, out, "PASTE 1")
	require.Contains(t, out, "more lines")

	m = naType(m, "jjj")
	require.Equal(t, 3, m.NewAgent.PrevScroll)
	m = naType(m, "k")
	require.Equal(t, 2, m.NewAgent.PrevScroll)
	for range 100 {
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	end := m.NewAgent.PrevScroll
	require.Less(t, end, 40, "scrolling stops at the end")
	require.NotContains(t, ansi.Strip(m.View()), "more lines", "the end shows the last line")
	m = naType(m, "k")
	require.Equal(t, end-1, m.NewAgent.PrevScroll)

	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusNewAgent, m.Focus, "esc leaves the preview, not the page")
	require.Equal(t, naCompose, m.NewAgent.Screen)
	require.Equal(t, naContext, m.NewAgent.Section)
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)
}

func TestContextList_ViewFitsEverySize(t *testing.T) {
	for _, sz := range []goldenSize{{120, 40}, {80, 24}, {60, 20}, {59, 20}, {50, 20}, {40, 12}, {30, 8}} {
		m := naOpen(t, naModel(t))
		m.Width, m.Height = sz.w, sz.h
		m = naType(m, strings.Repeat("a long task line ", 6))
		for range 6 {
			m = naPaste(m, "日本語 "+nLines(12))
		}
		m = naPaste(m, "\x00\x00binary")
		for _, screen := range []string{"compose", "context", "preview"} {
			switch screen {
			case "context":
				m.NewAgent.Section = naContext
			case "preview":
				m, _ = naKey(m, key("enter"))
			}
			lines := strings.Split(m.View(), "\n")
			require.Len(t, lines, sz.h, "%s %dx%d", screen, sz.w, sz.h)
			for i, l := range lines {
				require.LessOrEqual(t, ansi.StringWidth(l), sz.w, "%s %dx%d line %d: %q", screen, sz.w, sz.h, i, ansi.Strip(l))
			}
		}
	}
}

func TestPaste_AutoNameSkipsTakenNames(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naPaste(m, nLines(9))
	require.NoError(t, renameItem(m.NewAgent, 0, "paste 2"))
	m = naPaste(m, nLines(9))
	st := m.NewAgent
	require.Len(t, st.Items, 2)
	require.Equal(t, "paste 2", st.Items[0].Name)
	require.Equal(t, "paste 3", st.Items[1].Name, "the taken number is skipped")
	require.Equal(t, "[paste 2 · 9 lines][paste 3 · 9 lines]", st.Task.String())
}

func TestRename_RefusesTakenName(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naPaste(m, nLines(9))
	m = naPaste(m, nLines(12))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab}) // Context
	m = naType(m, "j")
	m = naType(m, "r")
	m, _ = naKey(m, key("ctrl+u"))
	m = naType(m, "paste 1")
	m, _ = naKey(m, key("enter"))
	st := m.NewAgent
	require.True(t, strings.HasPrefix(st.Err, "✗ "), st.Err)
	require.Equal(t, "paste 2", st.Items[1].Name)
	require.Equal(t, "[paste 1 · 9 lines][paste 2 · 12 lines]", st.Task.String())
	require.True(t, st.Renaming, "a refused name stays in the editor")
}
