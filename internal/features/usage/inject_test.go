package usage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

const userLine = `{"statusLine":{"type":"command","command":"echo mine"}}`

func fakeHome(t *testing.T, withUserLine bool) (home, cwd string) {
	t.Helper()
	home, cwd = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if withUserLine {
		writeFile(t, filepath.Join(home, ".claude", "settings.json"), userLine)
	}
	return home, cwd
}

func TestShouldInjectTap_Table(t *testing.T) {
	for _, tc := range []struct {
		usageOn  bool
		mode     string
		userLine bool
		want     bool
	}{
		{true, "auto", true, true},
		{true, "auto", false, false}, // never changes Claude's footer on its own
		{true, "", false, false},     // unknown mode reads as auto
		{true, "on", true, true},
		{true, "on", false, true},
		{true, "off", true, false},
		{true, "off", false, false},
		{false, "auto", true, false}, // usage off beats everything
		{false, "on", true, false},
	} {
		home, _ := fakeHome(t, tc.userLine)
		require.Equal(t, tc.want, ShouldInjectTap(tc.usageOn, tc.mode, home), "%+v", tc)
	}
}

func TestUserStatusLine_UserLevelOnly(t *testing.T) {
	home, cwd := fakeHome(t, true)
	require.Equal(t, "echo mine", UserStatusLine(home))

	// Project and local files never count, whatever they say.
	writeFile(t, filepath.Join(cwd, ".claude", "settings.json"), `{"statusLine":{"type":"command","command":"echo project"}}`)
	writeFile(t, filepath.Join(cwd, ".claude", "settings.local.json"), `{"statusLine":{"type":"command","command":"echo local"}}`)
	require.Equal(t, "echo mine", UserStatusLine(home))
}

// A status line set only in a project file is not "the user has a status
// line": auto does not inject because of it.
func TestShouldInjectTap_ProjectOnlyStatusLineDoesNotCount(t *testing.T) {
	home, cwd := fakeHome(t, false)
	writeFile(t, filepath.Join(cwd, ".claude", "settings.local.json"), userLine)
	writeFile(t, filepath.Join(cwd, ".claude", "settings.json"), userLine)
	require.Empty(t, UserStatusLine(home))
	require.False(t, ShouldInjectTap(true, "auto", home))
}

func TestUserStatusLine_SkipsUnusableEntries(t *testing.T) {
	home, _ := fakeHome(t, false)
	require.Empty(t, UserStatusLine(home), "no file")
	require.Empty(t, UserStatusLine(""), "no home")

	user := filepath.Join(home, ".claude", "settings.json")
	for name, body := range map[string]string{
		"broken json":   `{broken`,
		"wrong type":    `{"statusLine":{"type":"text","command":"echo x"}}`,
		"blank":         `{"statusLine":{"type":"command","command":"  "}}`,
		"no statusLine": `{"hooks":{}}`,
		// A command that already is the tap never counts as the user's own.
		"the tap itself": `{"statusLine":{"type":"command","command":"/x/rex-daemon statusline-tap --session a"}}`,
	} {
		writeFile(t, user, body)
		require.Empty(t, UserStatusLine(home), name)
	}
}

func TestUserStatusLine_ClaudeConfigDir(t *testing.T) {
	home, _ := fakeHome(t, true)
	other := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", other)
	require.Empty(t, UserStatusLine(home), "CLAUDE_CONFIG_DIR replaces ~/.claude")
	writeFile(t, filepath.Join(other, "settings.json"), `{"statusLine":{"type":"command","command":"echo cfg"}}`)
	require.Equal(t, "echo cfg", UserStatusLine(home))
	require.True(t, ShouldInjectTap(true, "auto", home))
}

func TestUserStatusLine_NeverWritesSettings(t *testing.T) {
	home, cwd := fakeHome(t, true)
	before, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	require.NoError(t, err)
	_ = UserStatusLine(home)
	_ = ShouldInjectTap(true, "on", home)
	after, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoDirExists(t, filepath.Join(cwd, ".claude"))
}

func TestTapCommand(t *testing.T) {
	require.Equal(t, "/opt/rex/rex-daemon statusline-tap --session abc-1", TapCommand("/opt/rex/rex-daemon", "abc-1", ""))
	require.Equal(t, `'/opt/my rex/rex-daemon' statusline-tap --session abc --socket /run/rex.sock`,
		TapCommand("/opt/my rex/rex-daemon", "abc", "/run/rex.sock"))
	require.Equal(t, `'/o'\''k/d' statusline-tap --session s`, TapCommand("/o'k/d", "s", ""))
}
