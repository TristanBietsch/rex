package boot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// homeStore is the settings in force for the config usageHome wrote.
func homeStore() func() *settings.Store {
	st, _, _ := settings.LoadDefault()
	return func() *settings.Store { return st }
}

func usageHome(t *testing.T, config string, userLine bool) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if config != "" {
		cfg := filepath.Join(home, ".config", "rex", "config.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
		require.NoError(t, os.WriteFile(cfg, []byte(config), 0o600))
	}
	if userLine {
		p := filepath.Join(home, ".claude", "settings.json")
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte(`{"statusLine":{"type":"command","command":"echo mine"}}`), 0o600))
	}
	return home
}

// The auto / on / off table, with and without the user's own status line.
func TestUsageTap_Table(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		userLine     bool
		inject       bool
	}{
		{"default (auto) with user line", "", true, true},
		{"default (auto) without user line", "", false, false},
		{"auto with user line", "usage_statusline_tap: auto\n", true, true},
		{"on without user line", "usage_statusline_tap: \"on\"\n", false, true},
		{"on with user line", "usage_statusline_tap: \"on\"\n", true, true},
		{"off with user line", "usage_statusline_tap: \"off\"\n", true, false},
		{"usage off beats on", "usage: false\nusage_statusline_tap: \"on\"\n", true, false},
		{"usage off beats auto", "usage: false\n", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usageHome(t, tc.config, tc.userLine)
			got := UsageTap("/some/other.sock", homeStore())("sess-9")
			if !tc.inject {
				require.Empty(t, got)
				return
			}
			require.Contains(t, got, "statusline-tap --session sess-9")
			require.Contains(t, got, "--socket /some/other.sock", "a non-default socket is passed on")
			// The daemon bakes in the absolute user-level settings path.
			require.Contains(t, got, "--user-settings ")
			// Slashed on Windows: Claude runs the command through Git Bash.
			want := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
			if runtime.GOOS == "windows" {
				want = filepath.ToSlash(want)
			}
			require.Contains(t, got, want)
		})
	}
}

func TestUsageTap_DefaultSocketIsNotRepeated(t *testing.T) {
	usageHome(t, "usage_statusline_tap: \"on\"\n", false)
	t.Setenv("XDG_RUNTIME_DIR", "")
	def := filepath.Join(os.Getenv("HOME"), ".cache", "rex", "rex.sock")
	got := UsageTap(def, homeStore())("s")
	require.Contains(t, got, "statusline-tap --session s ")
	require.NotContains(t, got, "--socket", got)
}

// Off is inert: no tap command and nothing written under HOME.
func TestUsageTap_OffIsInert(t *testing.T) {
	home := usageHome(t, "usage: false\n", true)
	before := listTree(t, home)
	require.Empty(t, UsageTap("", homeStore())("s"))
	require.False(t, settings.FeatureOn(homeStore()(), "usage"))
	require.Equal(t, before, listTree(t, home), "nothing was created or changed")
	require.NoFileExists(t, filepath.Join(home, ".local", "share", "rex", "usage.jsonl"))
}

func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		out = append(out, p+"|"+info.ModTime().String())
		return nil
	}))
	return out
}
