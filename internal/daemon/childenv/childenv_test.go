package childenv

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuild_AllowlistKeepsBaseNames(t *testing.T) {
	parent := []string{"PATH=/bin", "HOME=/h", "LC_ALL=C", "XDG_CONFIG_HOME=/c", "https_proxy=http://p"}
	got := Build(parent, Options{Mode: ModeAllowlist, GOOS: "linux"})
	require.Equal(t, parent, got)
}

func TestBuild_AllowlistDropsOthers(t *testing.T) {
	parent := []string{"PATH=/bin", "ANTHROPIC_API_KEY=k", "GH_TOKEN=t", "AWS_SECRET_ACCESS_KEY=s"}
	got := Build(parent, Options{Mode: ModeAllowlist, GOOS: "linux"})
	require.Equal(t, []string{"PATH=/bin"}, got)
}

func TestBuild_PassthroughAddsNames(t *testing.T) {
	parent := []string{"PATH=/bin", "GH_TOKEN=t", "NVM_DIR=/n", "OTHER=x"}
	got := Build(parent, Options{Mode: ModeAllowlist, GOOS: "linux", Pass: []string{"GH_TOKEN", "NVM_*"}})
	require.Equal(t, []string{"PATH=/bin", "GH_TOKEN=t", "NVM_DIR=/n"}, got)
}

func TestBuild_RemovesSessionMarkersInBothModes(t *testing.T) {
	parent := []string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_CODE_SSE_PORT=1", "REX_DAEMON_ENV_FILTERED=1", "X=1"}
	require.Equal(t, []string{"PATH=/bin", "X=1"}, Build(parent, Options{Mode: ModeInherit, GOOS: "linux"}))
	got := Build(parent, Options{Mode: ModeAllowlist, GOOS: "linux", Pass: []string{"CLAUDE*", "REX_*", "X"}})
	require.Equal(t, []string{"PATH=/bin", "X=1"}, got)
}

func TestBuild_KeepsClaudeProviderVars(t *testing.T) {
	parent := []string{"CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_GIT_BASH_PATH=C:\\b", "CLAUDECODE=1"}
	want := []string{"CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_GIT_BASH_PATH=C:\\b"}
	require.Equal(t, want, Build(parent, Options{Mode: ModeInherit, GOOS: "linux"}))
	got := Build(parent, Options{Mode: ModeAllowlist, GOOS: "linux", Pass: []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_GIT_BASH_PATH"}})
	require.Equal(t, want, got)
	require.Empty(t, Build(parent, Options{Mode: ModeAllowlist, GOOS: "linux"}))
}

func TestBuild_SSHAgentToggle(t *testing.T) {
	parent := []string{"SSH_AUTH_SOCK=/s", "PATH=/bin"}
	require.Equal(t, parent, Build(parent, Options{Mode: ModeAllowlist, GOOS: "linux", SSHAgent: true}))
	require.Equal(t, []string{"PATH=/bin"}, Build(parent, Options{Mode: ModeAllowlist, GOOS: "linux"}))
}

func TestBuild_InheritKeepsEverythingElse(t *testing.T) {
	parent := []string{"A=1", "B=", "C=x=y", "NOEQ", "=bad", "CLAUDECODE=1"}
	got := Build(parent, Options{Mode: ModeInherit, GOOS: "linux"})
	require.Equal(t, []string{"A=1", "B=", "C=x=y"}, got)
}

func TestBuild_WindowsBaseKeepsSystemNames(t *testing.T) {
	parent := []string{`systemroot=C:\Windows`, `Path=C:\bin`, `ComSpec=cmd.exe`, "SECRET=1", "programfiles(x86)=x"}
	got := Build(parent, Options{Mode: ModeAllowlist, GOOS: "windows"})
	require.Equal(t, []string{`systemroot=C:\Windows`, `Path=C:\bin`, `ComSpec=cmd.exe`, "programfiles(x86)=x"}, got)
}

func TestBuild_WindowsMatchesCaseInsensitivelyOnlyOnWindows(t *testing.T) {
	parent := []string{"path=/x", "nvm_dir=/n", "claudecode=1"}
	require.Empty(t, Build(parent, Options{Mode: ModeAllowlist, GOOS: "linux", Pass: []string{"NVM_*"}}))
	got := Build(parent, Options{Mode: ModeAllowlist, GOOS: "windows", Pass: []string{"NVM_*", "CLAUDE*"}})
	require.Equal(t, []string{"path=/x", "nvm_dir=/n"}, got)
}

func TestBuild_WindowsPerDriveEntries(t *testing.T) {
	parent := []string{`=C:=C:\`, "A=1"}
	require.Equal(t, parent, Build(parent, Options{Mode: ModeInherit, GOOS: "windows"}))
	require.Equal(t, []string{"A=1"}, Build(parent, Options{Mode: ModeInherit, GOOS: "linux"}))
}

func TestBuild_DoesNotMutateInputAndDefaultsGOOS(t *testing.T) {
	parent := []string{"PATH=/bin", "X=1", "CLAUDECODE=1"}
	pass := []string{"X"}
	pc, ps := append([]string(nil), parent...), append([]string(nil), pass...)
	got := Build(parent, Options{Mode: ModeAllowlist, Pass: pass})
	require.Equal(t, []string{"PATH=/bin", "X=1"}, got)
	require.Equal(t, pc, parent)
	require.Equal(t, ps, pass)
	require.NotNil(t, Build(nil, Options{}))
}

func TestBuild_UnknownModeIsAllowlist(t *testing.T) {
	got := Build([]string{"PATH=/b", "X=1"}, Options{Mode: "bogus", GOOS: "linux"})
	require.Equal(t, []string{"PATH=/b"}, got)
}

func TestBaseNames(t *testing.T) {
	linux := BaseNames("linux", true)
	require.Contains(t, linux, "SSH_AUTH_SOCK")
	require.Contains(t, linux, "DBUS_SESSION_BUS_ADDRESS")
	require.NotContains(t, linux, "__CF_USER_TEXT_ENCODING")
	require.NotContains(t, BaseNames("linux", false), "SSH_AUTH_SOCK")
	require.Contains(t, BaseNames("darwin", false), "__CF_USER_TEXT_ENCODING")
	require.Contains(t, BaseNames("windows", false), "ProgramFiles(x86)")
	require.NotContains(t, BaseNames("windows", false), "DISPLAY")
	a := BaseNames("linux", false)
	a[0] = "MUTATED"
	require.Equal(t, "PATH", BaseNames("linux", false)[0])
}
