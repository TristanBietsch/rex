package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/export"
)

// exportModel is a board with two sessions whose transcripts are synthetic
// files under a fake HOME. It returns the model and the exports dir.
func exportModel(t *testing.T) (Model, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	m := layoutModel(timesNow, 120, spanSessions()...)
	m.SelectedID = "1"
	for _, s := range m.Sessions {
		dir := filepath.Join(home, ".local", "share", "rex", "sessions", s.ID)
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "transcript.log"),
			[]byte("\x1b[2J\x1b[Hscreen of "+s.Slug+"\r\n"), 0o600))
	}
	return m, filepath.Join(home, ".local", "share", "rex", "exports")
}

func TestExportCommand_writesJSONForTheSelectedSession(t *testing.T) {
	m, dir := exportModel(t)
	m, cmd := executeCommand(m, "export")
	require.Nil(t, cmd)
	require.Empty(t, m.Err)
	want := filepath.Join(dir, "dark-mode-7d4f.json")
	require.Contains(t, m.Notice, want)
	require.Contains(t, m.Notice, "secrets", "the notice reminds the user the file is sensitive")

	b, err := os.ReadFile(want)
	require.NoError(t, err)
	var d export.Document
	require.NoError(t, json.Unmarshal(b, &d))
	require.Equal(t, "7d4f", d.Session.ShortID)
	require.Equal(t, "screen of dark-mode", d.Frames[len(d.Frames)-1].Text)
}

func TestExportCommand_selectorAndFormat(t *testing.T) {
	cases := []struct{ line, file, prefix string }{
		{"export md", "dark-mode-7d4f.md", "# Session export"},
		{"export release-notes", "release-notes-b203.json", "{"},
		{"export release-notes yaml", "release-notes-b203.yaml", "version: 1"},
		{"export b203 txt", "release-notes-b203.txt", "rex session export"},
		{"export 7d4f md", "dark-mode-7d4f.md", "# Session export"},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			m, dir := exportModel(t)
			m, _ = executeCommand(m, c.line)
			require.Empty(t, m.Err)
			b, err := os.ReadFile(filepath.Join(dir, c.file))
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(string(b), c.prefix), string(b))
		})
	}
}

func TestExportCommand_fileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	m, dir := exportModel(t)
	m, _ = executeCommand(m, "export")
	require.Empty(t, m.Err)
	st, err := os.Stat(filepath.Join(dir, "dark-mode-7d4f.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	dst, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dst.Mode().Perm())
}

func TestExportCommand_exportingAgainReplacesTheFile(t *testing.T) {
	m, dir := exportModel(t)
	m, _ = executeCommand(m, "export")
	m, _ = executeCommand(m, "export")
	require.Empty(t, m.Err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestExportCommand_errors(t *testing.T) {
	m, dir := exportModel(t)
	for line, want := range map[string]string{
		"export nope":          "no match for nope",
		"export a b c":         "usage",
		"export md md":         "no match for md",
		"export release-notes": "",
	} {
		got, _ := executeCommand(m, line)
		if want == "" {
			require.Empty(t, got.Err, line)
			continue
		}
		require.Contains(t, got.Err, want, line)
	}
	_ = dir

	m.SelectedID = ""
	got, _ := executeCommand(m, "export")
	require.Contains(t, got.Err, "no session selected")

	m, _ = exportModel(t)
	require.NoError(t, os.Remove(filepath.Join(os.Getenv("HOME"), ".local", "share", "rex", "sessions", "1", "transcript.log")))
	got, _ = executeCommand(m, "export")
	require.Contains(t, got.Err, "no transcript")
	require.Empty(t, got.Notice)
}

func TestExportOff_IsInert(t *testing.T) {
	m, dir := exportModel(t)
	m.Store = flagOff(t, "session_export")
	got, cmd := executeCommand(m, "export")
	require.Equal(t, "off: enable in Settings → Features (rex config set session_export true)", got.Err)
	require.Nil(t, cmd)
	require.Empty(t, got.Notice)
	require.NoDirExists(t, dir, "nothing is written while the feature is off")
}

func TestExportCommand_isInTheHelpTable(t *testing.T) {
	c, ok := lookupCommand("export")
	require.True(t, ok)
	require.Equal(t, "session_export", c.Flag)
	require.Equal(t, ":export [sel] [json|yaml|md|txt]", c.helpKey())
}
