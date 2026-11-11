package core

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func logged(t *testing.T, args []string, err error) string {
	t.Helper()
	opts, path := errLogOpts(t)
	SetOut(nil)
	RecordError(args, err, opts)
	raw, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	return string(raw)
}

func requireAbsent(t *testing.T, raw string, leaks ...string) {
	t.Helper()
	for _, l := range leaks {
		require.NotContains(t, raw, l, "the log must not hold %q:\n%s", l, raw)
	}
}

func TestErrLogPrivacy_CwdPathInMessage(t *testing.T) {
	err := NewExitError(ExitInvalidArgs, "new: -cwd /home/alice/secret-proj: no such file or directory")
	raw := logged(t, []string{"new", "-cwd", "/home/alice/secret-proj", "--", "p"}, err)
	requireAbsent(t, raw, "alice", "secret-proj")
	require.Contains(t, raw, "new: -cwd")
}

func TestErrLogPrivacy_DaemonDownSocketPath(t *testing.T) {
	dial := &net.OpError{Op: "dial", Net: "unix", Addr: &net.UnixAddr{Name: "/run/user/501/rex.sock", Net: "unix"}, Err: os.NewSyscallError("connect", syscall.ENOENT)}
	err := Errorf(ExitDaemonUnreachable, "rex daemon start",
		"rex-daemon is not running (no socket at /run/user/501/rex.sock); start it with `rex daemon start`: %w", dial)
	raw := logged(t, []string{"ls"}, err)
	requireAbsent(t, raw, "/run/user", "rex.sock", "501", "connect", "no such file")
	require.Contains(t, raw, `"kind":"daemon_unreachable"`)
	require.Contains(t, raw, "net.OpError", "the chain keeps error kinds")
	require.Contains(t, raw, `"hint":"rex daemon start"`)
}

func TestErrLogPrivacy_HomeAndTildePaths(t *testing.T) {
	t.Setenv("HOME", "/Users/zed")
	raw := logged(t, []string{"setup"}, NewExitError(ExitGeneric, "cannot write ~/dev/x and /Users/zed/.config/rex and C:\\Users\\zed\\x"))
	requireAbsent(t, raw, "zed", "dev/x", ".config")
}

func TestErrLogPrivacy_ConfigSetValue(t *testing.T) {
	err := NewExitError(ExitInvalidArgs, `master_volume: "hunter2-secret" is not a number`)
	raw := logged(t, []string{"config", "set", "master_volume", "hunter2-secret"}, err)
	requireAbsent(t, raw, "hunter2")
	// Even a short value, and a wrapped one.
	err = Errorf(ExitInvalidArgs, "", "bad value: %w", errors.New("x=pw9 rejected"))
	raw = logged(t, []string{"config", "set", "spinner", "pw9"}, err)
	requireAbsent(t, raw, "pw9")
}

func TestErrLogPrivacy_AutoTrustValue(t *testing.T) {
	err := NewExitError(ExitInvalidArgs, "-auto-trust: rel/private/dir is not an absolute path")
	raw := logged(t, []string{"setup", "-auto-trust", "rel/private/dir"}, err)
	requireAbsent(t, raw, "private")
}

func TestErrLogPrivacy_RmHintHasNoSelector(t *testing.T) {
	err := Errorf(ExitOperationRefused, "rex rm my-private-slug -force", "rm: stdin is not a terminal; pass -force to delete my-private-slug without asking")
	raw := logged(t, []string{"rm", "my-private-slug"}, err)
	requireAbsent(t, raw, "my-private-slug")
	opts, path := errLogOpts(t)
	RecordError([]string{"rm", "my-private-slug"}, err, opts)
	recs, rerr := ReadErrLog(path)
	require.NoError(t, rerr)
	require.Equal(t, "rex rm <sel:15 bytes> -force", recs[0].Hint)
}

func TestErrLogPrivacy_ShortHexSelectorStaysInHint(t *testing.T) {
	err := Errorf(ExitOperationRefused, "rex rm 7d4f -force", "rm: refused")
	require.Contains(t, logged(t, []string{"rm", "7d4f"}, err), `"hint":"rex rm 7d4f -force"`)
}

// A prompt typed where a command goes is never logged.
func TestErrLogPrivacy_UnknownCommandName(t *testing.T) {
	prompt := "please-fix-the-login"
	err := Errorf(ExitInvalidArgs, "rex --help", "unknown command %q (try `rex --help`)", prompt)
	raw := logged(t, []string{prompt}, err)
	requireAbsent(t, raw, "login", "please")
	require.Contains(t, raw, `"cmd":"?"`)

	short := "zq"
	raw = logged(t, []string{short}, Errorf(ExitInvalidArgs, "", "unknown command %q; did you mean %q?", short, "ls"))
	requireAbsent(t, raw, `\"zq\"`, `zq`)
	require.Contains(t, raw, `"cmd":"?"`)

	raw = logged(t, []string{"ls", "-bogus"}, NewExitError(ExitInvalidArgs, "flag provided but not defined: -bogus"))
	require.Contains(t, raw, `"cmd":"ls"`)
}

//nolint:gosec // G101: fake tokens that exercise the scrubber.
func TestScrub_PaddedBearerAndJWT(t *testing.T) {
	for in, want := range map[string]string{
		"Authorization: Bearer YWJjZGVmZ2hpams=":                   "Authorization: <redacted>",
		"bearer eyJhbGciOi.eyJzdWIiOiIx.SflKxwRJSMeKKF2QT4fwpMeJf": "<redacted>",
		"Bearer dGVzdC10b2tlbi12YWx1ZQ==, next":                    "<redacted>, next",
		"token=abc==":                                              "token=<redacted>",
	} {
		require.Equal(t, want, Scrub(in), in)
	}
}

func TestErrChainKeepsKindsOnly(t *testing.T) {
	err := fmt.Errorf("top: %w", &net.OpError{Op: "dial", Net: "unix", Err: errors.New("connect: /secret/path")})
	chain := errChain(err)
	require.Equal(t, []string{"*net.OpError", "*errors.errorString"}, chain)
}
