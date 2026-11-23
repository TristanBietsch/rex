//go:build !windows

package boot

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var errTestExec = errors.New("exec failed")

type execCall struct {
	argv0 string
	argv  []string
	env   []string
}

// ownSandbox points the settings and tools files at an empty temp home.
func ownSandbox(t *testing.T, configYAML string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	t.Setenv("XDG_DATA_HOME", home+"/.local/share")
	t.Setenv("XDG_CACHE_HOME", home+"/.cache")
	t.Setenv("XDG_STATE_HOME", home+"/.local/state")
	t.Setenv("REX_DAEMON_ENV_FILTERED", "") // registers the restore
	_ = os.Unsetenv("REX_DAEMON_ENV_FILTERED")
	if configYAML != "" {
		p := filepath.Join(home, ".config", "rex", "config.yaml")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(configYAML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReexecFiltered_CallsExecOnceWithFilteredEnv(t *testing.T) {
	ownSandbox(t, "env_passthrough: MY_PASS\n")
	t.Setenv("SECRET_X", "s1")
	t.Setenv("MY_PASS", "p")
	t.Setenv("ANTHROPIC_API_KEY", "a") // a built-in tool's env_pass
	var calls []execCall
	err := ReexecFiltered(func(a string, argv, env []string) error {
		calls = append(calls, execCall{a, argv, env})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("exec called %d times, want 1", len(calls))
	}
	env := calls[0].env
	for _, w := range []string{"MY_PASS=p", "ANTHROPIC_API_KEY=a", "REX_DAEMON_ENV_FILTERED=1"} {
		if !slices.Contains(env, w) {
			t.Errorf("env lacks %s", w)
		}
	}
	if slices.Contains(env, "SECRET_X=s1") {
		t.Error("SECRET_X survived the filter")
	}
	if calls[0].argv0 == "" || len(calls[0].argv) == 0 {
		t.Errorf("exec args empty: %+v", calls[0])
	}
}

func TestReexecFiltered_NoExecWhenMarkedOrInherit(t *testing.T) {
	fail := func(string, []string, []string) error { t.Error("exec called"); return nil }
	ownSandbox(t, "")
	t.Setenv("REX_DAEMON_ENV_FILTERED", "1")
	if err := ReexecFiltered(fail); err != nil {
		t.Fatal(err)
	}
	ownSandbox(t, "child_env: inherit\n")
	if err := ReexecFiltered(fail); err != nil {
		t.Fatal(err)
	}
}

func TestReexecFiltered_ExecErrorReturned(t *testing.T) {
	ownSandbox(t, "")
	if err := ReexecFiltered(func(string, []string, []string) error { return errTestExec }); err == nil {
		t.Error("exec error swallowed")
	}
}
