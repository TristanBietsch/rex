package boot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func usageHome(t *testing.T, config string, userLine bool) (home, cwd string) {
	t.Helper()
	home, cwd = t.TempDir(), t.TempDir()
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
	return home, cwd
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
			_, cwd := usageHome(t, tc.config, tc.userLine)
			got := UsageTap("/some/other.sock")("sess-9", cwd)
			if !tc.inject {
				require.Empty(t, got)
				return
			}
			require.Contains(t, got, "statusline-tap --session sess-9")
			require.Contains(t, got, "--socket /some/other.sock", "a non-default socket is passed on")
		})
	}
}

func TestUsageTap_DefaultSocketIsNotRepeated(t *testing.T) {
	_, cwd := usageHome(t, "usage_statusline_tap: \"on\"\n", false)
	t.Setenv("XDG_RUNTIME_DIR", "")
	def := filepath.Join(os.Getenv("HOME"), ".cache", "rex", "rex.sock")
	got := UsageTap(def)("s", cwd)
	require.True(t, strings.HasSuffix(got, "statusline-tap --session s"), got)
}

// Off is inert: no tap command and nothing written under HOME.
func TestUsageTap_OffIsInert(t *testing.T) {
	home, cwd := usageHome(t, "usage: false\n", true)
	before := listTree(t, home)
	require.Empty(t, UsageTap("")("s", cwd))
	require.False(t, UsageEnabled())
	require.Equal(t, before, listTree(t, home), "nothing was created or changed")
	require.NoFileExists(t, filepath.Join(home, ".local", "share", "rex", "usage.jsonl"))
}

func TestUsageEnabled_DefaultOn(t *testing.T) {
	usageHome(t, "", false)
	require.True(t, UsageEnabled())
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
