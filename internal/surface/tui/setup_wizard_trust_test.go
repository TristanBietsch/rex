package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// trustTestHome points HOME at a fresh folder, resolved through symlinks so
// it matches os.Getwd on macOS (/var is a link to /private/var).
func trustTestHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// gitProject creates dir with a .git folder inside, like a checkout.
func gitProject(t *testing.T, dir string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	return dir
}

// setupKey adds tab and shift+tab to the package's key helper.
func setupKey(name string) tea.KeyMsg {
	switch name {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	}
	return key(name)
}

func pressSetup(t *testing.T, m SetupWizardModel, keys ...string) SetupWizardModel {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(setupKey(k))
		m = next.(SetupWizardModel)
	}
	return m
}

// trustWizard is a wizard on the folder trust step with a config path in a
// temp home. It starts in a checkout under ~/dev, so ~/dev is suggested.
func trustWizard(t *testing.T) (SetupWizardModel, string) {
	t.Helper()
	home := trustTestHome(t)
	t.Chdir(gitProject(t, filepath.Join(home, "dev", "project")))
	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	m := NewSetupWizardModel(cfg, "")
	m.step = setupStepTrust
	return m, cfg
}

// writeAndLoad runs the wizard's write and returns auto_trust_dirs as saved.
func writeAndLoad(t *testing.T, m SetupWizardModel, cfg string) string {
	t.Helper()
	msg, ok := m.doWriteCmd()().(setupWriteMsg)
	require.True(t, ok)
	require.NoError(t, msg.err)
	st := settings.NewStore()
	require.NoError(t, st.Load(cfg))
	return st.String("auto_trust_dirs")
}

func setupView(m SetupWizardModel) string { return ansi.Strip(m.View()) }

func TestSetupWizard_TrustDefaultsToNo(t *testing.T) {
	m, cfg := trustWizard(t)
	require.False(t, m.trustYes)
	require.Equal(t, "~"+string(filepath.Separator)+"dev", m.trustDir, "parent of the checkout is suggested")
	v := setupView(m)
	require.Contains(t, v, "Only pick folders whose code you trust.")
	require.Contains(t, v, "no, always ask")

	m = pressSetup(t, m, "enter")
	require.Equal(t, setupStepShell, m.step, "no: straight on")
	m = pressSetup(t, m, "enter")
	require.Equal(t, setupStepPreview, m.step)
	require.Contains(t, setupView(m), "auto-trust: off")
	require.Empty(t, writeAndLoad(t, m, cfg), "nothing trusted without a yes")
}

func TestSetupWizard_TrustAcceptSuggestion(t *testing.T) {
	m, cfg := trustWizard(t)
	m = pressSetup(t, m, "y", "enter")
	require.Equal(t, trustFolder, m.trustField)
	require.Contains(t, setupView(m), "~"+string(filepath.Separator)+"dev|")
	m = pressSetup(t, m, "enter", "enter")
	require.Equal(t, setupStepPreview, m.step)
	want := "~" + string(filepath.Separator) + "dev"
	require.Contains(t, setupView(m), "auto-trust: "+want)
	require.Equal(t, want, writeAndLoad(t, m, cfg))
}

func TestSetupWizard_TrustEditFolder(t *testing.T) {
	m, cfg := trustWizard(t)
	// j toggles to yes; letters a menu would use are plain text in the field.
	m = pressSetup(t, m, "j", "enter", "ctrl+u", "~/jkyn", "backspace", " ", "backspace")
	require.Equal(t, "~/jky", m.trustDir)
	m = pressSetup(t, m, "tab")
	require.Equal(t, setupStepShell, m.step)
	m = pressSetup(t, m, "enter")
	require.Equal(t, "~/jky", writeAndLoad(t, m, cfg))
}

func TestSetupWizard_TrustRefusesWhatTheDaemonIgnores(t *testing.T) {
	home := trustTestHome(t)
	sep := string(filepath.ListSeparator)
	cases := map[string]string{
		"home":            "~/",
		"bare tilde":      "~",
		"home absolute":   home,
		"root":            filepath.VolumeName(home) + string(filepath.Separator),
		"relative":        "dev",
		"dot-dot":         "~/dev/../work",
		"separator split": filepath.Join(home, "work") + sep + "b",
		"empty":           "",
	}
	for name, folder := range cases {
		t.Run(name, func(t *testing.T) {
			m := NewSetupWizardModel(filepath.Join(home, "config.yaml"), "")
			m.step = setupStepTrust
			m = pressSetup(t, m, "y", "enter", "ctrl+u")
			if folder != "" {
				m = pressSetup(t, m, folder)
			}
			m = pressSetup(t, m, "enter")
			require.Equal(t, setupStepTrust, m.step, "refused: stays on the step")
			require.Error(t, m.trustErr)
			require.Contains(t, setupView(m), "✗")

			m = pressSetup(t, m, "ctrl+u")
			require.NoError(t, m.trustErr, "editing clears the message")
			m = pressSetup(t, m, "~/dev", "enter")
			require.Equal(t, setupStepShell, m.step)
		})
	}
}

func TestSetupWizard_TrustDeclineAfterYes(t *testing.T) {
	m, cfg := trustWizard(t)
	m = pressSetup(t, m, "y", "enter", "shift+tab")
	require.Equal(t, trustAnswer, m.trustField)
	m = pressSetup(t, m, "n", "enter")
	require.Equal(t, setupStepShell, m.step)
	m = pressSetup(t, m, "enter")
	require.Contains(t, setupView(m), "auto-trust: off")
	require.Empty(t, writeAndLoad(t, m, cfg))
}

func TestSetupWizard_TrustNavigation(t *testing.T) {
	m, _ := trustWizard(t)
	m.step, m.audioField = setupStepAudio, audioSoundset
	m = pressSetup(t, m, "enter")
	require.Equal(t, setupStepTrust, m.step, "audio leads to trust")
	m = pressSetup(t, m, "shift+tab")
	require.Equal(t, setupStepAudio, m.step, "and back")

	m = pressSetup(t, m, "enter", "y", "enter", "enter")
	require.Equal(t, setupStepShell, m.step)
	m = pressSetup(t, m, "shift+tab")
	require.Equal(t, setupStepTrust, m.step, "shell goes back to trust")
	require.Equal(t, trustFolder, m.trustField, "on the field it left")
	m = pressSetup(t, m, "shift+tab", "shift+tab")
	require.Equal(t, setupStepAudio, m.step)

	m.step = setupStepPreview
	m = pressSetup(t, m, "shift+tab")
	require.Equal(t, setupStepShell, m.step, "preview still goes back to shell")
}

func TestSetupWizard_TrustKeepsExistingOptIn(t *testing.T) {
	home := trustTestHome(t)
	cfg := filepath.Join(home, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: '~/dev'\n"), 0o600))
	m := NewSetupWizardModel(cfg, "")
	require.True(t, m.trustYes, "a valid saved opt-in stays on")
	require.Equal(t, "~/dev", m.trustDir)

	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: '~'\n"), 0o600))
	m = NewSetupWizardModel(cfg, "")
	require.False(t, m.trustYes, "one the daemon ignores starts at no")
	require.Equal(t, "~", m.trustDir, "shown so it can be fixed")
}

func TestSetupWizard_WithAutoTrust(t *testing.T) {
	m, _ := trustWizard(t)
	require.Equal(t, m, m.withAutoTrust(""))
	m = m.withAutoTrust("~/work")
	require.True(t, m.trustYes)
	require.Equal(t, "~/work", m.trustDir)
}

func TestSuggestTrustDir(t *testing.T) {
	home := trustTestHome(t)
	sep := string(filepath.Separator)
	require.Equal(t, "~"+sep+"dev", suggestTrustDir(gitProject(t, filepath.Join(home, "dev", "app"))))
	require.Empty(t, suggestTrustDir(filepath.Join(home, "dev", "plain")), "not a checkout")
	require.Empty(t, suggestTrustDir(gitProject(t, filepath.Join(home, "app"))), "parent is home")
	require.Empty(t, suggestTrustDir(gitProject(t, filepath.Join(t.TempDir(), "app"))), "outside home")
	require.Empty(t, suggestTrustDir(""))

	// A link inside home that leads out of it is not inside home.
	outside := filepath.Join(t.TempDir(), "code")
	gitProject(t, filepath.Join(outside, "app"))
	require.NoError(t, os.Symlink(outside, filepath.Join(home, "code")))
	require.Empty(t, suggestTrustDir(filepath.Join(home, "code", "app")), "link out of home")

	// One that stays inside home is fine.
	require.NoError(t, os.Symlink(filepath.Join(home, "dev"), filepath.Join(home, "link")))
	require.Equal(t, "~"+sep+"link", suggestTrustDir(filepath.Join(home, "link", "app")))

	// A folder named "a:" holding "b" reads back as "~/a" and "/b".
	list := string(filepath.ListSeparator)
	require.Empty(t, suggestTrustDir(gitProject(t, filepath.Join(home, "a"+list, "b", "app"))), "separator in a name")
	require.Empty(t, suggestTrustDir(gitProject(t, filepath.Join(home, "x"+list+"y", "app"))), "separator in a name")
	if runtime.GOOS != "windows" { // Windows does not allow a trailing space in a name
		require.Empty(t, suggestTrustDir(gitProject(t, filepath.Join(home, "dev ", "app"))), "outer space would be trimmed")
	}
}

// A value from config.yaml or a folder name may hold terminal escapes; the
// step and the preview show it without them.
func TestSetupWizard_TrustStripsEscapes(t *testing.T) {
	home := trustTestHome(t)
	cfg := filepath.Join(home, "config.yaml")
	const clear = "\x1b[2J"

	// Valid as a path, so the step starts at yes and shows it in the box.
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: \"~/dev\\e[2J\"\n"), 0o600))
	m := NewSetupWizardModel(cfg, "")
	require.True(t, m.trustYes)
	m.step = setupStepTrust
	require.NotContains(t, m.View(), clear)
	m = pressSetup(t, m, "enter")
	require.NotContains(t, m.View(), clear, "while editing")
	m.step = setupStepShell
	m = pressSetup(t, m, "enter")
	require.Equal(t, setupStepPreview, m.step)
	require.NotContains(t, m.View(), clear, "preview")
	require.Contains(t, setupView(m), "auto-trust: ~/dev", "the escape is dropped whole")

	// Not valid, so the step starts at no and shows it dimmed.
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: \"dev\\e[2J\"\n"), 0o600))
	m = NewSetupWizardModel(cfg, "")
	require.False(t, m.trustYes)
	m.step = setupStepTrust
	require.NotContains(t, m.View(), clear)
}

// The preview lists every folder in full, one per line, wrapping long ones,
// and still shows how to confirm.
func TestSetupWizard_TrustPreviewShowsAll(t *testing.T) {
	m, _ := trustWizard(t)
	var dirs []string
	for _, name := range []string{"one", "two", "three", "four", "five", "six", "seven", "eight"} {
		dirs = append(dirs, "~/"+strings.Repeat("d", 70)+"/"+name)
	}
	m.trustYes, m.trustDir = true, strings.Join(dirs, string(filepath.ListSeparator))
	m.step = setupStepShell
	m = pressSetup(t, m, "enter")
	v := setupView(m)
	for _, d := range dirs {
		for len(d) > 0 {
			n := min(len(d), setupWidth-18)
			require.Contains(t, v, d[:n])
			d = d[n:]
		}
	}
	require.Contains(t, v, "Press enter to write", "not cut off")
	for _, line := range strings.Split(v, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(line), setupWidth)
	}
}

func TestApplyDefaultsNonInteractive_AutoTrust(t *testing.T) {
	home := trustTestHome(t)
	cfg := filepath.Join(home, "config.yaml")
	load := func() string {
		st := settings.NewStore()
		require.NoError(t, st.Load(cfg))
		return st.String("auto_trust_dirs")
	}

	require.NoError(t, ApplyDefaultsNonInteractive(cfg, "", "", "", "", " ~/dev ", true))
	require.Equal(t, "~/dev", load(), "trimmed and written")

	require.NoError(t, ApplyDefaultsNonInteractive(cfg, "", "", "", "", "", true))
	require.Equal(t, "~/dev", load(), "no flag: left alone")

	err := ApplyDefaultsNonInteractive(cfg, "", "", "", "", "~/", true)
	require.ErrorContains(t, err, "home folder")
	require.Equal(t, "~/dev", load(), "refused: nothing written")
}

// The step fits the wizard's fixed-height body even with an error shown.
func TestSetupWizard_TrustViewFits(t *testing.T) {
	m, _ := trustWizard(t)
	m = pressSetup(t, m, "y", "enter", "ctrl+u", strings.Repeat("x", 120), "enter")
	require.Error(t, m.trustErr)
	v := setupView(m)
	require.Contains(t, v, "✗", "error line not clipped")
	require.Contains(t, v, "…", "long folder shows its end")
	for _, line := range strings.Split(v, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(line), setupWidth)
	}
}
