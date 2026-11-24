package ownenv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tristanbietsch/rex/internal/daemon/childenv"
)

func ownOpts() childenv.Options {
	return childenv.Options{Mode: childenv.ModeAllowlist, SSHAgent: true, GOOS: "linux", Pass: []string{"EXTRA_*"}}
}

func TestOwnEnv_FiltersAndMarks(t *testing.T) {
	parent := []string{"PATH=/bin", "HOME=/h", "SECRET_X=s1", "TOOL_KEY=k", "EXTRA_A=1", "SSH_AUTH_SOCK=/s", "CLAUDECODE=1"}
	snapshot := slices.Clone(parent)
	env, need := OwnEnv(parent, ownOpts(), []string{"TOOL_KEY"})
	if !need {
		t.Fatal("needExec = false, want true")
	}
	want := []string{"PATH=/bin", "HOME=/h", "TOOL_KEY=k", "EXTRA_A=1", "SSH_AUTH_SOCK=/s", "REX_DAEMON_ENV_FILTERED=1"}
	if !slices.Equal(env, want) {
		t.Errorf("env = %v, want %v", env, want)
	}
	if !slices.Equal(parent, snapshot) {
		t.Errorf("parent modified: %v", parent)
	}
	// The marker never reaches an agent: Build drops it in allowlist mode.
	for _, e := range childenv.Build(env, ownOpts()) {
		if e == "REX_DAEMON_ENV_FILTERED=1" {
			t.Error("marker reached the child environment")
		}
	}
}

func TestOwnEnv_NoExecWhenMarked(t *testing.T) {
	if _, need := OwnEnv([]string{"PATH=/bin", "REX_DAEMON_ENV_FILTERED=1"}, ownOpts(), nil); need {
		t.Error("needExec = true with the marker set")
	}
}

func TestOwnEnv_NoExecWhenInherit(t *testing.T) {
	o := ownOpts()
	o.Mode = childenv.ModeInherit
	if _, need := OwnEnv([]string{"PATH=/bin", "SECRET_X=1"}, o, nil); need {
		t.Error("needExec = true in inherit mode")
	}
}

func sandbox(t *testing.T, configYAML string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	t.Setenv("XDG_DATA_HOME", home+"/.local/share")
	t.Setenv("XDG_CACHE_HOME", home+"/.cache")
	t.Setenv("XDG_STATE_HOME", home+"/.local/state")
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

func TestFilteredEnv_DropsSecretsKeepsToolAndPassthroughNames(t *testing.T) {
	sandbox(t, "env_passthrough: MY_PASS\n")
	parent := []string{"PATH=/bin", "SECRET_X=s1", "MY_PASS=p", "ANTHROPIC_API_KEY=a"}
	env, need := FilteredEnv(parent, nil)
	if !need {
		t.Fatal("needExec = false, want true")
	}
	for _, w := range []string{"PATH=/bin", "MY_PASS=p", "ANTHROPIC_API_KEY=a", "REX_DAEMON_ENV_FILTERED=1"} {
		if !slices.Contains(env, w) {
			t.Errorf("env lacks %s", w)
		}
	}
	if slices.Contains(env, "SECRET_X=s1") {
		t.Error("SECRET_X survived the filter")
	}
}

func TestFilteredEnv_InheritAndMarked(t *testing.T) {
	sandbox(t, "child_env: inherit\n")
	if _, need := FilteredEnv([]string{"X=1"}, nil); need {
		t.Error("inherit: needExec = true")
	}
	sandbox(t, "")
	if _, need := FilteredEnv([]string{"X=1", "REX_DAEMON_ENV_FILTERED=1"}, nil); need {
		t.Error("marked: needExec = true")
	}
}

func TestFilteredEnv_FailsClosedOnBadSettings(t *testing.T) {
	sandbox(t, "env_passthrough: [not, a, string\n")
	env, need := FilteredEnv([]string{"PATH=/bin", "SECRET_X=s1"}, nil)
	if !need || slices.Contains(env, "SECRET_X=s1") {
		t.Errorf("bad settings did not fail closed: need=%v env=%v", need, env)
	}
}

func TestToolsPathFromArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-tools", "/a"}, "/a"},
		{[]string{"--tools=/b"}, "/b"},
		{[]string{"-socket", "/s", "-tools", "/c"}, "/c"},
	} {
		if got := toolsPathFromArgs(c.args); got != c.want {
			t.Errorf("%v = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestOwnEnv_KeepsRexConfigButChildrenDoNotGetIt(t *testing.T) {
	parent := []string{"PATH=/bin", "REX_LOG_LEVEL=debug", "REX_LOG_DIR=/l", "REX_DAEMONCTL_FAKE_MODE=x", "SECRET_X=s"}
	env, need := OwnEnv(parent, ownOpts(), nil)
	if !need {
		t.Fatal("needExec = false")
	}
	for _, w := range []string{"REX_LOG_LEVEL=debug", "REX_LOG_DIR=/l", "REX_DAEMONCTL_FAKE_MODE=x"} {
		if !slices.Contains(env, w) {
			t.Errorf("daemon env lacks %s", w)
		}
	}
	if slices.Contains(env, "SECRET_X=s") {
		t.Error("SECRET_X survived")
	}
	// The spawn filter is unchanged: an agent gets no REX_* the daemon holds.
	for _, e := range childenv.Build(env, ownOpts()) {
		if strings.HasPrefix(e, "REX_") {
			t.Errorf("child got %s", e)
		}
	}
}

func TestFilteredEnv_KeepsEveryToolsEnvPassEvenWithExtraAgentsOff(t *testing.T) {
	sandbox(t, "extra_agents: false\n")
	env, _ := FilteredEnv([]string{"PATH=/bin", "CURSOR_API_KEY=c", "GROK_CODE_XAI_API_KEY=g"}, nil)
	for _, w := range []string{"CURSOR_API_KEY=c", "GROK_CODE_XAI_API_KEY=g"} {
		if !slices.Contains(env, w) {
			t.Errorf("env lacks %s", w)
		}
	}
}

func TestFilteredEnv_NoReexecForProbes(t *testing.T) {
	sandbox(t, "")
	for _, a := range [][]string{{"-version"}, {"--version"}, {"-h"}, {"-help"}, {"-handoff-check", "/f"}, {"-handoff-check=/f"}} {
		if _, need := FilteredEnv([]string{"PATH=/bin"}, a); need {
			t.Errorf("%v: needExec = true", a)
		}
	}
	if _, need := FilteredEnv([]string{"PATH=/bin"}, []string{"-socket", "/s"}); !need {
		t.Error("normal start: needExec = false")
	}
}
