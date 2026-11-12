package core

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// Register the flags a typical command defines, as a parse would.
func registerFlagsForTest(names ...string) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for _, n := range names {
		fs.String(n, "", "")
	}
	noteRegisteredFlags(fs)
}

// logLine records one failure and returns the raw log line.
func logLine(t *testing.T, args []string, err error) string {
	t.Helper()
	opts, path := errLogOpts(t)
	SetOut(nil)
	RecordError(args, err, opts)
	raw, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	return string(raw)
}

type hostile struct {
	name string
	args []string
	err  error
	// secrets must not appear anywhere in the line.
	secrets []string
}

func hostileCases() []hostile {
	sock := "/run/user/501/rex-secret.sock"
	dial := &net.OpError{Op: "dial", Net: "unix", Addr: &net.UnixAddr{Name: sock, Net: "unix"},
		Err: os.NewSyscallError("connect", syscall.ENOENT)}
	return []hostile{
		{"prompt as command", []string{"please-fix-the-login-bug"},
			Errorf(ExitInvalidArgs, "rex --help", "unknown command %q (try `rex --help`)", "please-fix-the-login-bug").WithID("unknown_command"),
			[]string{"please", "login", "fix-the"}},
		{"short prompt as command", []string{"zq9"},
			Errorf(ExitInvalidArgs, "", "unknown command %q; did you mean %q?", "zq9", "ls"),
			[]string{"zq9"}},
		{"global flags before a prompt", []string{"--agent", "--no-color", "-o", "json", "hunter2-prompt"},
			Errorf(ExitInvalidArgs, "", "unknown command %q", "hunter2-prompt"),
			[]string{"hunter2"}},
		{"dash prompt", []string{"-fix-everything-now"},
			NewExitError(ExitInvalidArgs, "flag provided but not defined: -fix-everything-now"),
			[]string{"fix-everything", "everything"}},
		{"dash prompt after a command", []string{"ls", "-fix-everything-now"},
			NewExitError(ExitInvalidArgs, "flag provided but not defined: -fix-everything-now"),
			[]string{"fix-everything", "everything"}},
		{"quoted multi-line prompt", []string{"new", "-tool", "claude", "--", "line one\nsecret line two \"quoted\""},
			NewExitError(ExitGeneric, "spawn failed for \"line one\nsecret line two\""),
			[]string{"line one", "secret line", "quoted"}},
		{"flag values", []string{"new", "-cwd=/home/alice/proj", "-name", "private name", "-slug", "my-slug"},
			NewExitError(ExitInvalidArgs, "new: -cwd /home/alice/proj: no such file"),
			[]string{"alice", "proj", "private name", "my-slug"}},
		{"config set secret", []string{"config", "set", "master_volume", "hunter2-secret"},
			NewExitError(ExitInvalidArgs, `master_volume: "hunter2-secret" is not a number`),
			[]string{"hunter2", "secret"}},
		{"unix socket op error", []string{"ls", "-socket", sock},
			Errorf(ExitDaemonUnreachable, "rex daemon start", "rex-daemon is not running (no socket at %s): %w", sock, dial).WithID("daemon_unreachable"),
			[]string{"/run/user", "rex-secret", "501", "connect", "no such file", "ENOENT"}},
		{"export selector and path", []string{"export", "my-private-session", "--out", "/home/bob/out.json"},
			NewExitError(ExitSelectorNotFound, `no session matches "my-private-session"`).WithID("selector_not_found"),
			[]string{"my-private-session", "bob", "out.json"}},
		{"fleet selector and name", []string{"fleet", "set", "secret-session", "secret-fleet"},
			NewExitError(ExitSelectorNotFound, `no session matches "secret-session"`),
			[]string{"secret-session", "secret-fleet"}},
		{"update output", []string{"update", "-source", "/home/carol/rex"},
			NewExitError(ExitGeneric, "update: go install github.com/x/y@latest: exit status 1\nfatal: token=ghp_abcdefghijklmnop"),
			[]string{"carol", "ghp_", "token", "fatal"}},
		{"bearer token in a message", []string{"reply", "ab12", "--", "go"},
			NewExitError(ExitGeneric, "Authorization: Bearer YWJjZGVmZ2hpams= rejected"),
			[]string{"Bearer", "YWJj", "rejected"}},
		{"rm refusal with selector hint", []string{"rm", "my-private-slug"},
			Errorf(ExitOperationRefused, "rex rm my-private-slug -force", "rm: pass -force to delete my-private-slug").WithID("needs_confirmation"),
			[]string{"my-private-slug"}},
		{"env and unicode", []string{"ls", "-state", "é日本語-secret"},
			NewExitError(ExitGeneric, "bad state é日本語-secret"),
			[]string{"日本語", "secret"}},
	}
}

func TestErrLogStructureOnly_HostileInputs(t *testing.T) {
	registerFlagsForTest("tool", "cwd", "name", "slug", "socket", "state", "out", "source")
	for _, c := range hostileCases() {
		t.Run(c.name, func(t *testing.T) {
			raw := logLine(t, c.args, c.err)
			for _, s := range c.secrets {
				require.NotContains(t, raw, s, "the log must not hold %q:\n%s", s, raw)
			}
			require.NotContains(t, raw, `"error":`)
			require.NotContains(t, raw, `"hint"`)
			require.NotContains(t, raw, `"args"`)
		})
	}
}

// Prompts are never mistaken for commands or flags.
func TestErrLogStructure_CommandsAndFlags(t *testing.T) {
	registerFlagsForTest("tool", "cwd")
	recs := func(args []string, err error) ErrLogRecord {
		opts, path := errLogOpts(t)
		SetOut(nil)
		RecordError(args, err, opts)
		r, rerr := ReadErrLog(path)
		require.NoError(t, rerr)
		require.Len(t, r, 1)
		return r[0]
	}
	r := recs([]string{"new", "-tool", "claude", "-cwd=/x/y", "-fix-it", "--", "prompt"}, NewExitError(ExitGeneric, "x"))
	require.Equal(t, "new", r.Cmd)
	require.Equal(t, []string{"-tool", "-cwd"}, r.Flags)
	require.Equal(t, 1, r.UnknownArgs, "-fix-it names no flag")
	require.Equal(t, 7, r.ArgCount)

	r = recs([]string{"fleet", "set", "sel", "name"}, NewExitError(ExitGeneric, "x"))
	require.Equal(t, "fleet", r.Cmd)
	require.Equal(t, "set", r.Sub)

	r = recs([]string{"fleet", "secret-sub"}, NewExitError(ExitGeneric, "x"))
	require.Empty(t, r.Sub, "an unknown subcommand is not kept")

	r = recs([]string{"--agent", "-o", "json", "ls"}, NewExitError(ExitGeneric, "x"))
	require.Equal(t, "ls", r.Cmd)
	require.Equal(t, []string{"-agent", "-o"}, r.Flags, "global output flags are known names")

	r = recs([]string{"frobnicate", "-tool", "x"}, NewExitError(ExitInvalidArgs, "x"))
	require.Equal(t, "?", r.Cmd)
	require.Empty(t, r.Flags, "under an unknown command no flag is trusted")
	require.Equal(t, 1, r.UnknownArgs)
}

func TestErrLogStructure_CodesAndChain(t *testing.T) {
	opts, path := errLogOpts(t)
	SetOut(nil)
	dial := &net.OpError{Op: "dial", Net: "unix", Err: os.NewSyscallError("connect", syscall.ENOENT)}
	err := Errorf(ExitDaemonUnreachable, "", "no socket: %w", dial).WithID("daemon_unreachable")
	RecordError([]string{"ls"}, err, opts)
	RecordError([]string{"ls"}, NewExitError(ExitGeneric, "x").WithID("made-up-with-secret-text"), opts)
	RecordError([]string{"ls"}, errors.New("plain"), opts)
	recs, rerr := ReadErrLog(path)
	require.NoError(t, rerr)
	require.Len(t, recs, 3)
	require.Equal(t, "", recs[0].Code, "plain errors have no code")
	require.Equal(t, []string{"*errors.errorString"}, recs[0].Chain)
	require.Equal(t, "", recs[1].Code, "an ID outside the allowlist is dropped")
	require.Equal(t, "daemon_unreachable", recs[2].Code)
	require.Equal(t, "daemon_unreachable", recs[2].Kind)
	require.Equal(t, []string{"core.ExitError", "*net.OpError", "*os.SyscallError", "syscall.Errno"}, recs[2].Chain)
	require.Equal(t, 4, recs[2].Exit)
}

func TestErrChainIsBounded(t *testing.T) {
	deep := errors.New("end")
	for i := range 20 {
		deep = fmt.Errorf("level %d: %w", i, deep)
	}
	require.Len(t, errChain(deep), errLogMaxChain)
	joined := errors.Join(errors.New("a"), fmt.Errorf("w: %w", errors.New("b")))
	require.Equal(t, []string{"*errors.joinError", "*errors.errorString", "*fmt.wrapError", "*errors.errorString"}, errChain(joined))
}

// Random arguments never leak into the record: any argument value of three
// or more bytes that is not a command or flag name is absent from the line.
func FuzzErrLogNeverHoldsArgValues(f *testing.F) {
	for _, seed := range []string{"hunter2", "-secret", "/home/x/y", "line1\nline2", "é日本語", "--", "a=b-c"} {
		f.Add(seed, "ls", seed+"x")
	}
	registerFlagsForTest("tool", "cwd", "state")
	known := map[string]bool{}
	for _, n := range CommandNames() {
		known[n] = true
	}
	for _, n := range []string{"tool", "cwd", "state", "o", "output", "agent", "no-color", "json", "h", "help",
		"ls", "set", "get", "list", "start", "stop", "status", "restart", "logs", "show", "unset", "reset", "edit"} {
		known[n] = true
	}
	f.Fuzz(func(t *testing.T, a, b, c string) {
		args := []string{a, b, c}
		opts, path := errLogOpts(t)
		SetOut(nil)
		RecordError(args, NewExitError(ExitGeneric, strings.Join(args, " ")), opts)
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		line := string(raw)
		for _, arg := range args {
			v := arg
			if strings.HasPrefix(v, "-") {
				_, after, ok := strings.Cut(v, "=")
				if !ok {
					continue // a flag token: only its name can appear
				}
				v = after
			}
			if len(v) < 3 || known[strings.TrimLeft(v, "-")] || strings.Contains(structuralJSON, v) {
				continue
			}
			require.NotContains(t, line, v, "arg %q leaked into %s", arg, line)
		}
	})
}

// structuralJSON is the text every record carries whatever the input: field
// names, versions and type names. An argument that happens to equal part of
// it is not a leak.
var structuralJSON = `{"v":1,"time":"version":"v1","os":"darwin/arm64 linux/amd64 windows","pid":"cmd":"flags":[],"arg_count":"unknown_args":"format":"txt","agent_auto":false,"tty":{"in":"out":},"exit":"kind":"error invalid_args not_found ambiguous daemon_unreachable refused timeout","code":"chain":["core.ExitError"]}`
