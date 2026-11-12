package setup

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// trustValue is a value each trust setting accepts that grants trust.
func trustValue(t *testing.T, id string) string {
	t.Helper()
	switch id {
	case "skip_permissions":
		return "true"
	case "auto_trust_dirs":
		return "~/dev"
	}
	t.Fatalf("no test value for trust setting %q: add one", id)
	return ""
}

func requireNeedsTerminal(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	var ee core.ExitError
	require.True(t, errors.As(err, &ee), "%v %v", err, msgAndArgs)
	require.Equal(t, core.ExitOperationRefused, ee.Code, msgAndArgs...)
	require.Equal(t, "needs_terminal", ee.ID, msgAndArgs...)
	require.ErrorContains(t, err, "only at a terminal", msgAndArgs...)
}

func readConfigFile(t *testing.T) string {
	t.Helper()
	_, path, err := loadStore()
	require.NoError(t, err)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return string(b)
}

// rex config set and reset change a trust setting only for a person at a
// terminal, as rex trust: agent mode, a data format, or stdin or stdout not
// a terminal exit 6 (needs_terminal) and write nothing. Every other setting,
// and reading any setting, stays scriptable.
func TestConfigSetReset_TrustSettingsNeedATerminal(t *testing.T) {
	trustHome(t)
	require.NoError(t, configSet([]string{"spinner", "moon"}))
	before := readConfigFile(t)

	for _, id := range settings.TrustSettingIDs {
		v := trustValue(t, id)
		for _, format := range []string{"text", "json", "agent", "txt"} {
			_, err := themeRun(t, format, func() error { return configSet([]string{id, v}) })
			requireNeedsTerminal(t, err, id, format)
			_, err = themeRun(t, format, func() error { return configReset([]string{id}) })
			requireNeedsTerminal(t, err, id, format, "reset")
		}
		for _, tty := range []core.Env{{InTTY: true}, {OutTTY: true, ErrTTY: true}} {
			atFakeTTY(t, tty)
			requireNeedsTerminal(t, configSet([]string{id, v}), id)
			requireNeedsTerminal(t, configReset([]string{id}), id)
		}
		require.Equal(t, before, readConfigFile(t), id)

		// Reading stays scriptable.
		_, err := themeRun(t, "agent", func() error { return configGet([]string{id}) })
		require.NoError(t, err, id)
	}
	_, err := themeRun(t, "json", func() error { return configList(nil) })
	require.NoError(t, err)

	// Other settings stay scriptable.
	_, err = themeRun(t, "agent", func() error { return configSet([]string{"spinner", "pulse"}) })
	require.NoError(t, err)
	_, err = themeRun(t, "json", func() error { return configReset([]string{"spinner"}) })
	require.NoError(t, err)

	// At a terminal, it works.
	for _, id := range settings.TrustSettingIDs {
		atTerminal(t)
		require.NoError(t, configSet([]string{id, trustValue(t, id)}), id)
		st, _, err := settings.LoadDefault()
		require.NoError(t, err)
		require.NotEqual(t, settings.SourceDefault, st.Source(id), id)
		require.NoError(t, configReset([]string{id}), id)
	}
}

// A value the checks refuse is reported first (exit 5), as rex trust does.
func TestConfigSet_TrustSettingBadValueBeforeTerminalCheck(t *testing.T) {
	trustHome(t)
	_, err := themeRun(t, "agent", func() error { return configSet([]string{"auto_trust_dirs", "/"}) })
	requireExit(t, err, core.ExitInvalidArgs)
	_, err = themeRun(t, "agent", func() error { return configSet([]string{"skip_permissions", "maybe"}) })
	requireExit(t, err, core.ExitInvalidArgs)
}

// rex setup -auto-trust writes auto_trust_dirs, so it needs a terminal too,
// with -yes or without. -preview writes nothing and stays scriptable, and
// -yes without -auto-trust stays scriptable.
func TestRunSetup_AutoTrustNeedsATerminal(t *testing.T) {
	_, cfg := setupHome(t)
	for _, format := range []string{"text", "json", "agent", "txt"} {
		for _, extra := range [][]string{{"-yes"}, nil} {
			args := append(append([]string{}, extra...), "-no-shell", "-config", cfg, "-auto-trust", "~/dev")
			_, err := themeRun(t, format, func() error { return RunSetup(args) })
			requireNeedsTerminal(t, err, format, args)
			require.NoFileExists(t, cfg)
		}
	}
	atFakeTTY(t, core.Env{InTTY: true})
	requireNeedsTerminal(t, RunSetup([]string{"-yes", "-no-shell", "-config", cfg, "-auto-trust", "~/dev"}))
	require.NoFileExists(t, cfg)

	_, err := themeRun(t, "agent", func() error {
		return RunSetup([]string{"-preview", "-no-shell", "-config", cfg, "-auto-trust", "~/dev"})
	})
	require.NoError(t, err)
	require.NoFileExists(t, cfg)

	_, err = themeRun(t, "agent", func() error { return RunSetup([]string{"-yes", "-no-shell", "-config", cfg}) })
	require.NoError(t, err)
	st := settings.NewStore()
	require.NoError(t, st.Load(cfg))
	require.Empty(t, st.String("auto_trust_dirs"))
	require.NoError(t, os.Remove(cfg))

	atTerminal(t)
	require.NoError(t, RunSetup([]string{"-yes", "-no-shell", "-config", cfg, "-auto-trust", "~/dev"}))
	require.NoError(t, st.Load(cfg))
	require.Equal(t, "~/dev", st.String("auto_trust_dirs"))
}
