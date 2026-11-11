package usage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestUserSettingsPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	require.Equal(t, filepath.Join(home, ".claude", "settings.json"), UserSettingsPath(home))
	require.Empty(t, UserSettingsPath(""))
	require.Empty(t, UserSettingsPath("relative/home"))

	other := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", other)
	require.Equal(t, filepath.Join(other, "settings.json"), UserSettingsPath(home), "CLAUDE_CONFIG_DIR replaces ~/.claude")
	writeFile(t, filepath.Join(other, "settings.json"), `{"statusLine":{"type":"command","command":"echo cfg"}}`)
	require.Equal(t, "echo cfg", UserStatusLine(UserSettingsPath(home)))
	require.True(t, ShouldInjectTap(true, "auto", UserSettingsPath(home)))

	t.Setenv("CLAUDE_CONFIG_DIR", "relative/dir")
	require.Empty(t, UserSettingsPath(home), "a relative config dir is refused, not guessed")
}

func TestUserStatusLine_RefusesRelativeSymlinkAndNonRegular(t *testing.T) {
	home, _ := fakeHome(t, true)
	good := UserSettingsPath(home)
	require.Equal(t, "echo mine", UserStatusLine(good))
	require.Empty(t, UserStatusLine(""))
	require.Empty(t, UserStatusLine(filepath.Join(".claude", "settings.json")), "relative")

	link := filepath.Join(t.TempDir(), "settings.json")
	if err := os.Symlink(good, link); err == nil {
		require.Empty(t, UserStatusLine(link), "a symlinked settings file is refused")
	}
	require.Empty(t, UserStatusLine(filepath.Dir(good)), "a directory")
}

func TestTapCommand(t *testing.T) {
	require.Equal(t, "/opt/rex/rex-daemon statusline-tap --session abc-1", TapCommand("/opt/rex/rex-daemon", "abc-1", "", ""))
	require.Equal(t, `'/opt/my rex/rex-daemon' statusline-tap --session abc --socket /run/rex.sock`,
		TapCommand("/opt/my rex/rex-daemon", "abc", "/run/rex.sock", ""))
	require.Equal(t, `'/o'\''k/d' statusline-tap --session s`, TapCommand("/o'k/d", "s", "", ""))
	require.Equal(t, `/d statusline-tap --session s --user-settings '/home/a b/.claude/settings.json'`,
		TapCommand("/d", "s", "", "/home/a b/.claude/settings.json"))
}

func quietTap(in []byte, userSettings string, out *bytes.Buffer) TapOptions {
	return TapOptions{Session: "s", UserSettings: userSettings, Stdin: bytes.NewReader(in), Stdout: out, Stderr: &bytes.Buffer{},
		Send: func(context.Context, protocol.UsageSample) error { return nil }}
}

// Claude applies the env block of settings files, project ones included, to
// the processes it starts, so the tap's own CLAUDE_CONFIG_DIR and HOME are
// whatever an agent made them. Only the path the daemon baked in counts.
func TestRunTap_UsesOnlyTheBakedSettingsPath(t *testing.T) {
	skipWindows(t)
	real, _ := fakeHome(t, false)
	dump := filepath.Join(t.TempDir(), "in.bin")
	baked := filepath.Join(real, ".claude", "settings.json")
	writeFile(t, baked, `{"statusLine":{"type":"command","command":"cat > '`+dump+`'; printf real"}}`)

	evil := t.TempDir()
	marker := filepath.Join(t.TempDir(), "pwned")
	body := `{"statusLine":{"type":"command","command":"touch ` + marker + `; printf evil"}}`
	writeFile(t, filepath.Join(evil, "settings.json"), body)
	writeFile(t, filepath.Join(evil, ".claude", "settings.json"), body)
	t.Setenv("CLAUDE_CONFIG_DIR", evil)
	t.Setenv("HOME", evil)
	t.Setenv("USERPROFILE", evil)

	in := []byte(statusJSON + "\x00\xfe")
	var out bytes.Buffer
	require.Zero(t, RunTap(context.Background(), quietTap(in, baked, &out)))
	require.Equal(t, "real", out.String())
	require.NoFileExists(t, marker)
	got, err := os.ReadFile(dump)
	require.NoError(t, err)
	require.Equal(t, in, got, "stdin passes through byte for byte")

	// Without a usable baked path the tap runs nothing, whatever its env says.
	for name, path := range map[string]string{"empty": "", "relative": filepath.Join(".claude", "settings.json")} {
		out.Reset()
		require.Zero(t, RunTap(context.Background(), quietTap(in, path, &out)), name)
		require.Empty(t, out.String(), name)
		require.NoFileExists(t, marker, name)
	}
	link := filepath.Join(t.TempDir(), "settings.json")
	if err := os.Symlink(filepath.Join(evil, "settings.json"), link); err == nil {
		out.Reset()
		require.Zero(t, RunTap(context.Background(), quietTap(in, link, &out)))
		require.Empty(t, out.String())
		require.NoFileExists(t, marker)
	}
}

// A project settings file is never read, whichever folder the tap learns of.
func TestRunTap_NeverRunsProjectStatusLine(t *testing.T) {
	skipWindows(t)
	home, _ := fakeHome(t, false)
	proj := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	body := `{"statusLine":{"type":"command","command":"touch ` + marker + `; printf proj"}}`
	writeFile(t, filepath.Join(proj, ".claude", "settings.local.json"), body)
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), body)

	run := func(name, in string) {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			require.Zero(t, RunTap(context.Background(), quietTap([]byte(in), UserSettingsPath(home), &out)))
			require.Empty(t, out.String())
			require.NoFileExists(t, marker)
		})
	}
	run("cwd from the JSON", `{"workspace":{"current_dir":"`+proj+`"},"cwd":"`+proj+`","cost":{"total_cost_usd":1}}`)
	t.Chdir(proj)
	run("process cwd", `{"cost":{"total_cost_usd":1}}`)
}
