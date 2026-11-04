// Package e2e drives the real rex and rex-daemon binaries through testscript
// scripts in testdata/script. Each script gets its own HOME, XDG dirs, log
// dir, socket and copy of the binaries, so it can never reach the user's
// real daemon or state, and every daemon it causes can be found and stopped.
package e2e

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// binDir holds the rex and rex-daemon binaries TestMain builds once. Each
// script copies them into its own $WORK/bin before use.
var binDir string

// systemPath is the PATH tail scripts see after their bin dir. It
// deliberately omits the caller's PATH so a script can never pick up an
// installed rex or rex-daemon (for example from ~/.local/bin).
const systemPath = "/usr/bin:/bin:/usr/sbin:/sbin"

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

// runMain builds the binaries, runs the tests and removes the build output.
// It is split from TestMain so the deferred cleanup runs before os.Exit.
func runMain(m *testing.M) int {
	if runtime.GOOS == "windows" {
		// TestScripts skips itself on Windows; nothing to build.
		return m.Run()
	}
	dir, err := os.MkdirTemp("", "rex-e2e-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: temp bin dir:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	for _, pkg := range []string{"./cmd/rex", "./cmd/rex-daemon"} {
		cmd := exec.Command("go", "build", "-o", dir, pkg)
		cmd.Dir = ".." // module root; `go test` runs this package from e2e/
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "e2e: go build %s: %v\n", pkg, err)
			return 1
		}
	}
	binDir = dir
	return m.Run()
}

func TestScripts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("e2e harness stops rex-daemon with SIGTERM and the echo test tool runs under bash; neither is available on Windows")
	}
	testscript.Run(t, testscript.Params{
		Dir:   "testdata/script",
		Setup: setupScriptEnv,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"daemon-start": cmdDaemonStart,
			"daemon-stop":  cmdDaemonStop,
			"exitcode":     cmdExitCode,
		},
	})
}

// Keys for values Setup hands to the custom commands through Env.Values.
type (
	envKeysKey struct{}
	harnessKey struct{}
)

// scriptHarness is the per-script state the custom commands share with the
// cleanup Setup registers.
type scriptHarness struct {
	// daemonBin is this script's private rex-daemon. Its path is unique to
	// the script, which is what lets cleanup find daemons rex auto-starts.
	daemonBin string
	// socket is the script's isolated rex.sock.
	socket string
	// daemon is the daemon daemon-start launched; nil when none is running.
	daemon *scriptDaemon
}

// setupScriptEnv isolates one script:
//   - HOME, the XDG variables and REX_LOG_DIR point under $WORK, and
//     XDG_RUNTIME_DIR at a short private dir in /tmp;
//   - OLLAMA_HOST is unset;
//   - PATH is $WORK/bin (private copies of the built binaries) plus system
//     dirs.
//
// rex starts rex-daemon from its own directory, so any daemon the script
// causes runs as $WORK/bin/rex-daemon. The registered cleanup stops the
// daemon-start daemon, then reaps any other process running that path, and
// fails the script if it had to.
func setupScriptEnv(env *testscript.Env) error {
	home := filepath.Join(env.WorkDir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	scriptBin := filepath.Join(env.WorkDir, "bin")
	if err := os.MkdirAll(scriptBin, 0o700); err != nil {
		return err
	}
	for _, name := range []string{"rex", "rex-daemon"} {
		if err := copyBinary(filepath.Join(binDir, name), filepath.Join(scriptBin, name)); err != nil {
			return err
		}
	}
	// macOS caps sun_path at 104 bytes and $WORK under /var/folders is
	// longer than that, so the socket dir must be short and live in /tmp.
	runtimeDir, err := os.MkdirTemp("/tmp", "rx")
	if err != nil {
		return err
	}
	// Registered first so it runs last: after every daemon is gone.
	env.Defer(func() { _ = os.RemoveAll(runtimeDir) })

	env.Vars = slices.DeleteFunc(env.Vars, func(kv string) bool {
		return strings.HasPrefix(kv, "OLLAMA_HOST=")
	})
	h := &scriptHarness{
		daemonBin: filepath.Join(scriptBin, "rex-daemon"),
		socket:    filepath.Join(runtimeDir, "rex.sock"),
	}
	env.Setenv("PATH", scriptBin+string(os.PathListSeparator)+systemPath)
	env.Setenv("HOME", home)
	env.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	env.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	env.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	env.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	env.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	env.Setenv("REX_LOG_DIR", filepath.Join(env.WorkDir, "logs"))
	env.Setenv("SOCKET", h.socket)

	keys := make([]string, 0, len(env.Vars))
	for _, kv := range env.Vars {
		if k, _, ok := strings.Cut(kv, "="); ok && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	env.Values[envKeysKey{}] = keys
	env.Values[harnessKey{}] = h

	t, _ := env.T().(testing.TB)
	env.Defer(func() {
		stopDaemon(h.daemon)
		if reaped := reapStrayDaemons(h.daemonBin); len(reaped) > 0 && t != nil {
			t.Errorf("reaped rex-daemon pids %v that daemon-start did not launch; start daemons with daemon-start", reaped)
		}
		// Last line of defence: the socket is private to this script, so
		// whatever still answers on it is ours to shut down.
		if dialable(h.socket) {
			shutdownViaSocket(h.socket)
			if t != nil {
				t.Errorf("a rex-daemon was still answering on %s after cleanup", h.socket)
			}
		}
	})
	return nil
}

// copyBinary copies the executable src to dst, owner-only (0700). dst
// sits in the script's $WORK/bin, so os.Executable reports it and rex
// looks for rex-daemon next to it.
//
// It must be a real copy, never a hard link. Hard links share one vnode,
// and when Gatekeeper assesses a new executable macOS hands syspolicyd one
// of that vnode's names. If the name belonged to a script that had
// already finished and removed its $WORK, syspolicyd could not open it and
// SIGKILLed the process at exec ("Terminating process due to Gatekeeper
// rejection"). A private copy has exactly one name, which lives as long as
// the script does.
func copyBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700) //nolint:gosec // G302: an executable needs its exec bit; 0700 is still owner-only.
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// scriptEnv rebuilds the script's environment for processes the harness
// starts itself, reflecting any `env` changes the script made.
func scriptEnv(ts *testscript.TestScript) []string {
	keys, _ := ts.Value(envKeysKey{}).([]string)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+ts.Getenv(k))
	}
	return out
}

// cmdExitCode runs a program and asserts its exact exit status, which plain
// `exec` and `! exec` cannot express. stdout and stderr stay available to
// later stdout/stderr assertions.
//
//	exitcode N program [args...]
func cmdExitCode(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: exitcode N program [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("exitcode: bad code %q", args[0])
	}
	got := 0
	if err := ts.Exec(args[1], args[2:]...); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			ts.Fatalf("exitcode: %v", err)
		}
		got = ee.ExitCode()
	}
	if got != want {
		ts.Fatalf("exitcode: %s exited %d, want %d", args[1], got, want)
	}
}
