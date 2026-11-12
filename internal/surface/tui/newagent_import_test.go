package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
)

// naFiles writes files (name → content) into the folder the page imports
// from; a name ending in "/" is a folder.
func naFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := os.Getwd()
	require.NoError(t, err)
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			require.NoError(t, os.MkdirAll(p, 0o700))
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	return root
}

// indexMsg runs cmd (and a batch's commands) and returns the walk's result.
func indexMsg(t *testing.T, cmd tea.Cmd) indexDoneMsg {
	t.Helper()
	require.NotNil(t, cmd, "opening the page starts the walk")
	switch msg := cmd().(type) {
	case indexDoneMsg:
		return msg
	case tea.BatchMsg:
		for _, c := range msg {
			if c == nil {
				continue
			}
			if done, ok := c().(indexDoneMsg); ok {
				return done
			}
		}
	}
	t.Fatal("no indexDoneMsg")
	return indexDoneMsg{}
}

// naIndexed opens the page and delivers its walk.
func naIndexed(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := naKey(m, key("n"))
	require.Equal(t, FocusNewAgent, m.Focus)
	next, _ := m.Update(indexMsg(t, cmd))
	return next.(Model)
}

// countWalks swaps the walk for one that counts its calls and keeps the
// last context, then runs the real walk.
func countWalks(t *testing.T) (*atomic.Int32, *context.Context) {
	t.Helper()
	var n atomic.Int32
	var last context.Context
	orig := walkWorkspace
	t.Cleanup(func() { walkWorkspace = orig })
	walkWorkspace = func(ctx context.Context, root string, maxEntries int, budget time.Duration, c *atomic.Int64) (spawnctx.Index, error) {
		n.Add(1)
		last = ctx
		require.Equal(t, 10000, maxEntries)
		require.Equal(t, 300*time.Millisecond, budget)
		return orig(ctx, root, maxEntries, budget, c)
	}
	return &n, &last
}

func TestImport_WalkOnlyWithTheFlag(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{"a.go": "package a\n"})
	walks, ctx := countWalks(t)

	off := setFlag(t, m, "new_agent_page", false)
	off, cmd := naKey(off, key("n"))
	require.Equal(t, FocusWizard, off.Focus)
	if cmd != nil {
		_ = cmd()
	}
	off, _ = naKey(off, tea.KeyMsg{Type: tea.KeyCtrlO})
	require.Zero(t, walks.Load(), "flag off: no workspace walk")

	m = setFlag(t, m, "new_agent_page", true) // the store is shared
	m = naIndexed(t, m)
	require.Equal(t, int32(1), walks.Load())
	require.Equal(t, []string{"a.go"}, m.NewAgent.Index.Paths)
	require.False(t, m.NewAgent.Indexing)

	// Closing the page cancels the walk's context.
	require.NoError(t, (*ctx).Err())
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)
	require.ErrorIs(t, (*ctx).Err(), context.Canceled)
	_ = off
}

func TestImport_StaleWalkIgnored(t *testing.T) {
	m := naModel(t)
	m, first := naKey(m, key("n"))
	stale := indexMsg(t, first)
	m, _ = naKey(m, key("esc"))
	m, _ = naKey(m, key("n")) // a new page, a new walk
	next, _ := m.Update(stale)
	m = next.(Model)
	require.True(t, m.NewAgent.Indexing, "an old page's walk is dropped")
}

func TestImport_LoadingThenReady(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{"internal/auth/auth_test.go": "package auth\n"})
	m, cmd := naKey(m, key("n"))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	require.Equal(t, naImportScreen, m.NewAgent.Screen)
	m.NewAgent.indexCount.Store(3812)
	v := naView(m)
	require.Contains(t, v, "indexing workspace… 3,812 files")
	require.NotContains(t, v, "files indexed")

	next, _ := m.Update(indexMsg(t, cmd))
	m = next.(Model)
	v = naView(m)
	require.Contains(t, v, "IMPORT")
	require.Contains(t, v, "3 files indexed")
	require.Contains(t, v, "internal/")
	require.NotContains(t, v, "indexing workspace")

	m.NewAgent.Index = spawnctx.Index{Paths: make([]string, 10000), Truncated: true}
	require.Contains(t, naView(m), "10,000+ files indexed")
}

func TestImport_MarkAndAdd(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{
		"internal/auth/auth_test.go":     strings.Repeat("x", 4300),
		"internal/auth/testdata/a.json":  "{}",
		"internal/auth/testdata/b.json":  "{}",
		"cmd/rex/auth_test_helpers.go":   "package rex\n",
		"README.md":                      "# readme\n",
		"docs/plans/a/u/t/h/e/x/note.md": "n",
	})
	m = naIndexed(t, m)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	m = naType(m, "auth_te")
	v := naView(m)
	require.Contains(t, v, "> auth_te")
	require.Contains(t, v, "[ ] internal/auth/auth_test.go")
	require.Contains(t, v, "4.2 KB")

	m, _ = naKey(m, key(" ")) // mark the first row
	require.Contains(t, naView(m), "[x] internal/auth/auth_test.go")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = naKey(m, key(" "))
	require.Contains(t, naView(m), "enter add 2")
	m, _ = naKey(m, key("enter"))
	st := m.NewAgent
	require.Equal(t, naCompose, st.Screen)
	require.Equal(t, naContext, st.Section)
	require.Equal(t, []spawnctx.Item{
		{Kind: spawnctx.File, Name: "internal/auth/auth_test.go", Bytes: 4300},
		{Kind: spawnctx.File, Name: "cmd/rex/auth_test_helpers.go", Bytes: 12},
	}, st.Items)

	// Browse: → opens a folder, enter on a folder row adds it, ← goes up.
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	require.Empty(t, m.NewAgent.Import.Query, "the picker starts afresh")
	v = naView(m)
	require.Contains(t, v, "cmd/")
	require.Contains(t, v, "README.md")
	for m.NewAgent.importRows()[m.NewAgent.Import.Cur] != "internal/" {
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRight})
	require.Equal(t, "internal/", m.NewAgent.Import.Dir)
	require.Equal(t, []string{"internal/auth/"}, m.NewAgent.importRows())
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRight})
	require.Equal(t, "internal/auth/", m.NewAgent.Import.Dir)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	require.Equal(t, "internal/", m.NewAgent.Import.Dir)
	require.Equal(t, "internal/auth/", m.NewAgent.importRows()[m.NewAgent.Import.Cur], "the cursor is on the folder left")
	m, _ = naKey(m, key("enter"))
	require.Equal(t, spawnctx.Item{Kind: spawnctx.Dir, Name: "internal/auth", Files: 3}, m.NewAgent.Items[2])

	// The same path again is not added twice.
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	m = naType(m, "auth_test.go")
	m, _ = naKey(m, key("enter"))
	require.Len(t, m.NewAgent.Items, 3)

	// esc goes back without adding.
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusNewAgent, m.Focus)
	require.Equal(t, naCompose, m.NewAgent.Screen)
	require.Len(t, m.NewAgent.Items, 3)
}

func TestImport_DenyNeedsConfirm(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{".env.test": "TOKEN=x\n", "main.go": "package main\n"})
	m = naIndexed(t, m)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	m = naType(m, ".env")
	v := naView(m)
	require.Contains(t, v, "! .env.test")
	require.Contains(t, v, "looks like secrets: skip")

	m, _ = naKey(m, key("enter"))
	require.Empty(t, m.NewAgent.Items, "not added before the confirm")
	require.Contains(t, naView(m), "y adds it anyway")
	m, _ = naKey(m, key("n")) // any other key skips it
	require.Empty(t, m.NewAgent.Items)
	require.Empty(t, m.NewAgent.Pending)

	m, _ = naKey(m, key("enter"))
	m, _ = naKey(m, key("y"))
	require.Equal(t, []spawnctx.Item{{Kind: spawnctx.File, Name: ".env.test", Bytes: 8}}, m.NewAgent.Items)
	require.Equal(t, naCompose, m.NewAgent.Screen)
}

func TestImport_RefusesOutsideAndCaps(t *testing.T) {
	m := naModel(t)
	root := naFiles(t, map[string]string{"a.go": "package a\n"})
	outside := filepath.Join(t.TempDir(), "key.txt")
	require.NoError(t, os.WriteFile(outside, []byte("k"), 0o600))
	if err := os.Symlink(outside, filepath.Join(root, "notes.txt")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	m = naIndexed(t, m)
	st := m.NewAgent

	err := addPath(st, "notes.txt", false)
	require.Error(t, err)
	require.Contains(t, naFail(err), "✗ notes.txt links to ")
	require.Contains(t, naFail(err), "only workspace files can be added.")
	err = addPath(st, "../escape.go", false)
	require.Contains(t, naFail(err), "✗ ../escape.go is outside ~/dev/proj: only workspace files can be added.")
	require.Empty(t, st.Items)

	for i := range spawnctx.DefaultCaps.Items {
		st.Items = append(st.Items, spawnctx.Item{Kind: spawnctx.URL, Name: "https://example.com/" + string(rune('a'+i))})
	}
	err = addPath(st, "a.go", false)
	require.Equal(t, "✗ 32 context items is the limit; remove one first.", naFail(err))

	// Through the picker the error shows on the page.
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	m = naType(m, "a.go")
	m, _ = naKey(m, key("enter"))
	require.Contains(t, naView(m), "✗ 32 context items is the limit")
}

func TestImport_BigFileWarns(t *testing.T) {
	m := naModel(t)
	root := naFiles(t, nil)
	f, err := os.Create(filepath.Join(root, "big.log"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(2<<20))
	require.NoError(t, f.Close())
	m = naIndexed(t, m)
	require.NoError(t, addPath(m.NewAgent, "big.log", false))
	require.Equal(t, "! big.log is 2.0 MB: the agent may not read all of it", m.NewAgent.Err)
	require.Contains(t, naView(m), "! big.log is 2.0 MB")
}

func TestAt_CompletionInsertsAndAdds(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{"internal/auth/auth_test.go": "package auth\n", "README.md": "r"})
	m = naIndexed(t, m)
	m = naType(m, "fix @auth_te")
	require.NotNil(t, m.NewAgent.Complete)
	v := naView(m)
	require.Contains(t, v, "▸ internal/auth/auth_test.go")

	m, _ = naKey(m, key("enter"))
	require.Equal(t, FocusNewAgent, m.Focus, "enter in the list never launches")
	require.Nil(t, m.NewAgent.Complete)
	require.Equal(t, "fix @internal/auth/auth_test.go", m.NewAgent.Task.String())
	require.Equal(t, []spawnctx.Item{{Kind: spawnctx.File, Name: "internal/auth/auth_test.go", Bytes: 13}}, m.NewAgent.Items)

	// tab takes the row too; a folder keeps its slash in the text.
	m = naType(m, " and @intern")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyUp})
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, naTask, m.NewAgent.Section, "tab takes the row, focus stays")
	require.Equal(t, "fix @internal/auth/auth_test.go and @internal/", m.NewAgent.Task.String())
	require.Equal(t, spawnctx.Item{Kind: spawnctx.Dir, Name: "internal", Files: 1}, m.NewAgent.Items[1])

	// No match: enter closes the list and does not launch.
	m = naType(m, " @zzzz")
	m, _ = naKey(m, key("enter"))
	require.Equal(t, FocusNewAgent, m.Focus)
	require.Nil(t, m.NewAgent.Complete)
	require.Len(t, m.NewAgent.Items, 2)
}

func TestAt_DeniedNeedsConfirm(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{".env": "A=1\n"})
	m = naIndexed(t, m)
	m = naType(m, "@.en")
	m, _ = naKey(m, key("enter"))
	require.Empty(t, m.NewAgent.Items)
	require.Equal(t, "@.en", m.NewAgent.Task.String())
	m, _ = naKey(m, key("y"))
	require.Equal(t, "@.env", m.NewAgent.Task.String())
	require.Len(t, m.NewAgent.Items, 1)
}

func TestAt_EscKeepsText(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{"README.md": "r"})
	m = naIndexed(t, m)
	m = naType(m, "see @read")
	require.NotNil(t, m.NewAgent.Complete)
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusNewAgent, m.Focus, "esc closes the list first")
	require.Nil(t, m.NewAgent.Complete)
	require.Equal(t, "see @read", m.NewAgent.Task.String())
	require.Empty(t, m.NewAgent.Items)
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)
}

func TestAt_OpensOnlyAtAWordStart(t *testing.T) {
	m := naModel(t)
	m = naIndexed(t, m)
	m = naType(m, "mail a@b")
	require.Nil(t, m.NewAgent.Complete, "an address is not a file reference")
	m = naType(m, " @x")
	require.NotNil(t, m.NewAgent.Complete)
	require.Contains(t, naView(m), "no match")
	m = naType(m, " ")
	require.Nil(t, m.NewAgent.Complete, "a space ends the reference")
	m = naType(m, "@")
	require.NotNil(t, m.NewAgent.Complete)
	m, _ = naKey(m, key("backspace"))
	require.Nil(t, m.NewAgent.Complete, "deleting the @ closes it")
	m = naType(m, "@")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(nLines(12)), Paste: true})
	require.Nil(t, m.NewAgent.Complete, "a paste closes it")
}

func TestAt_LoadingRow(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "@a")
	require.Contains(t, naView(m), "indexing workspace…")
}

func TestPreview_FileAndFolder(t *testing.T) {
	m := naModel(t)
	naFiles(t, map[string]string{
		"main.go":         "package main\n\nfunc main() {}\n",
		"pkg/a.go":        "package pkg\n",
		"pkg/b.go":        "package pkg\n",
		"bin/tool":        "\x00\x00\x00\x00binary",
		"pkg/sub/deep.go": "package sub\n",
	})
	m = naIndexed(t, m)
	st := m.NewAgent
	require.NoError(t, addPath(st, "main.go", false))
	require.NoError(t, addPath(st, "pkg", false))
	require.NoError(t, addPath(st, "bin/tool", false))
	st.Section = naContext

	m, _ = naKey(m, key("enter"))
	v := naView(m)
	require.Contains(t, v, "MAIN.GO")
	require.Contains(t, v, "func main() {}")

	m, _ = naKey(m, key("esc"))
	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key("enter"))
	v = naView(m)
	require.Contains(t, v, "3 files")
	require.Contains(t, v, "pkg/sub/")
	require.Contains(t, v, "pkg/a.go")

	m, _ = naKey(m, key("esc"))
	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key("enter"))
	require.Contains(t, naView(m), "binary data")
}

func TestImport_ViewFitsEverySize(t *testing.T) {
	for _, sz := range []goldenSize{{120, 40}, {80, 24}, {60, 20}, {59, 20}, {50, 20}, {40, 12}, {30, 8}} {
		m := naModel(t)
		files := map[string]string{".env.local": "x"}
		for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
			files["internal/"+strings.Repeat(n, 30)+"/very_long_file_name_"+n+".go"] = "x"
		}
		naFiles(t, files)
		m = naIndexed(t, m)
		m.Width, m.Height = sz.w, sz.h
		check := func(what string) {
			t.Helper()
			lines := strings.Split(m.View(), "\n")
			require.Len(t, lines, sz.h, "%s %dx%d", what, sz.w, sz.h)
			for i, l := range lines {
				require.LessOrEqual(t, ansi.StringWidth(l), sz.w, "%s %dx%d line %d: %q", what, sz.w, sz.h, i, ansi.Strip(l))
			}
		}
		m = naType(m, "fix @go")
		check("completion")
		m, _ = naKey(m, key("esc"))
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
		m = naType(m, "go")
		check("import")
		m.NewAgent.Import.Query = []rune(".env")
		m, _ = naKey(m, key("enter"))
		check("confirm")
		m, _ = naKey(m, key("n"))
		m.NewAgent.Indexing = true
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
		check("loading")
	}
}
