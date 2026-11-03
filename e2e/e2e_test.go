// Package e2e drives the real rex and rex-daemon binaries through testscript
// scripts in testdata/script. Each script gets its own HOME, XDG dirs, log
// dir and socket, so it can never reach the user's real daemon or state.
package e2e

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	// socketWait bounds how long daemon-start waits for the socket to accept.
	socketWait = 10 * time.Second
	// stopWait bounds how long daemon-stop and cleanup wait after SIGTERM
	// before falling back to SIGKILL.
	stopWait = 10 * time.Second
	// dialTimeout keeps each socket probe short so a wedged daemon cannot
	// stall the harness.
	dialTimeout = 200 * time.Millisecond
)

// binDir holds the rex and rex-daemon binaries TestMain builds once for all
// scripts. rex looks for rex-daemon next to its own executable, so both must
// live in the same directory.
var binDir string

// systemPath is the PATH tail scripts see after binDir. It deliberately
// omits the caller's PATH so a script can never pick up an installed rex or
// rex-daemon (for example ~/.local/bin) if the build output is missing.
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
	daemonKey  struct{}
)

// scriptDaemon is the one rex-daemon a script started with daemon-start.
type scriptDaemon struct {
	cmd    *exec.Cmd
	done   chan struct{} // closed once cmd.Wait returns
	err    error         // cmd.Wait result, valid after done is closed
	stderr string        // path of the file capturing the daemon's stderr
}

// setupScriptEnv isolates one script: every path rex derives from HOME, the
// XDG variables, REX_LOG_DIR and XDG_RUNTIME_DIR points under the script's
// own temp dirs, OLLAMA_HOST is unset, and PATH is binDir plus the system
// dirs. Its deferred cleanup stops the script's daemon, then fails the script
// if anything else still answers on the socket.
func setupScriptEnv(env *testscript.Env) error {
	home := filepath.Join(env.WorkDir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	// macOS caps sun_path at 104 bytes and $WORK under /var/folders is
	// longer than that, so the socket dir must be short and live in /tmp.
	runtimeDir, err := os.MkdirTemp("/tmp", "rx")
	if err != nil {
		return err
	}
	// Registered first so it runs last: after any daemon is gone.
	env.Defer(func() { _ = os.RemoveAll(runtimeDir) })

	env.Vars = slices.DeleteFunc(env.Vars, func(kv string) bool {
		return strings.HasPrefix(kv, "OLLAMA_HOST=")
	})
	env.Setenv("PATH", binDir+string(os.PathListSeparator)+systemPath)
	env.Setenv("HOME", home)
	env.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	env.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	env.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	env.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	env.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	env.Setenv("REX_LOG_DIR", filepath.Join(env.WorkDir, "logs"))
	env.Setenv("SOCKET", filepath.Join(runtimeDir, "rex.sock"))

	keys := make([]string, 0, len(env.Vars))
	for _, kv := range env.Vars {
		if k, _, ok := strings.Cut(kv, "="); ok && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	env.Values[envKeysKey{}] = keys
	daemon := new(*scriptDaemon)
	env.Values[daemonKey{}] = daemon

	t, _ := env.T().(testing.TB)
	sock := filepath.Join(runtimeDir, "rex.sock")
	env.Defer(func() {
		stopDaemon(*daemon)
		// Anything still answering on this script's socket was started
		// behind the harness's back (e.g. rex auto-starting the daemon).
		// The socket is unique to this script, so shutting down whatever
		// listens on it cannot touch any other daemon.
		if dialable(sock) {
			shutdownViaSocket(sock)
			if t != nil {
				t.Errorf("untracked rex-daemon was answering on %s; start daemons with daemon-start", sock)
			}
		}
	})
	return nil
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

// cmdDaemonStart execs rex-daemon on the script's isolated socket and state
// dir and waits until the socket accepts connections.
//
//	daemon-start [extra rex-daemon flags...]
//
// It refuses to start with the summarizer enabled, because the daemon would
// then call whatever Ollama listens on localhost.
func cmdDaemonStart(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("unsupported: ! daemon-start")
	}
	slot, _ := ts.Value(daemonKey{}).(**scriptDaemon)
	if *slot != nil {
		ts.Fatalf("daemon-start: daemon already running (pid %d)", (*slot).cmd.Process.Pid)
	}
	home := ts.Getenv("HOME")
	cfg := settings.NewStore()
	ts.Check(cfg.Load(filepath.Join(home, ".config", "rex", "config.yaml")))
	if on, _ := cfg.Get("summary_enabled").(bool); on {
		ts.Fatalf("daemon-start: summary_enabled is on; add home/.config/rex/config.yaml with `summary_enabled: false` to the script")
	}

	sock := ts.Getenv("SOCKET")
	stderrPath := ts.MkAbs("daemon.stderr")
	stderr, err := os.Create(stderrPath)
	ts.Check(err)
	defer stderr.Close()

	flags := append([]string{
		"-socket", sock,
		"-state-dir", filepath.Join(home, ".local", "share", "rex"),
		"-tools", filepath.Join(home, ".config", "rex", "tools.yaml"),
	}, args...)
	cmd := exec.Command(filepath.Join(binDir, "rex-daemon"), flags...)
	cmd.Dir = ts.MkAbs(".")
	cmd.Env = scriptEnv(ts)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	ts.Check(cmd.Start())

	d := &scriptDaemon{cmd: cmd, done: make(chan struct{}), stderr: stderrPath}
	go func() {
		d.err = cmd.Wait()
		close(d.done)
	}()
	// From here on Setup's deferred cleanup stops d if the script fails or
	// never calls daemon-stop. It only ever signals this PID.
	*slot = d

	deadline := time.Now().Add(socketWait)
	for !dialable(sock) {
		select {
		case <-d.done:
			ts.Fatalf("daemon-start: rex-daemon exited early (%v):\n%s", d.err, ts.ReadFile(stderrPath))
		default:
		}
		if time.Now().After(deadline) {
			ts.Fatalf("daemon-start: socket %s not dialable after %s:\n%s", sock, socketWait, ts.ReadFile(stderrPath))
		}
		time.Sleep(20 * time.Millisecond)
	}
	ts.Logf("daemon-start: rex-daemon pid %d on %s", cmd.Process.Pid, sock)
}

// cmdDaemonStop sends SIGTERM to the daemon daemon-start launched and waits
// for it to exit cleanly.
//
//	daemon-stop
func cmdDaemonStop(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: daemon-stop")
	}
	slot, _ := ts.Value(daemonKey{}).(**scriptDaemon)
	d := *slot
	if d == nil {
		ts.Fatalf("daemon-stop: no daemon started")
	}
	*slot = nil
	ts.Check(d.cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-d.done:
	case <-time.After(stopWait):
		_ = d.cmd.Process.Kill()
		<-d.done
		ts.Fatalf("daemon-stop: rex-daemon ignored SIGTERM for %s:\n%s", stopWait, ts.ReadFile(d.stderr))
	}
	if d.err != nil {
		ts.Fatalf("daemon-stop: rex-daemon exited with %v:\n%s", d.err, ts.ReadFile(d.stderr))
	}
}

// stopDaemon terminates d if it is still running: SIGTERM, then SIGKILL
// after stopWait. It is a no-op for nil or already-exited daemons.
func stopDaemon(d *scriptDaemon) {
	if d == nil {
		return
	}
	select {
	case <-d.done:
		return
	default:
	}
	_ = d.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-d.done:
	case <-time.After(stopWait):
		_ = d.cmd.Process.Kill()
		<-d.done
	}
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

// dialable reports whether something accepts connections on sock.
func dialable(sock string) bool {
	conn, err := net.DialTimeout("unix", sock, dialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// shutdownViaSocket asks the daemon on sock to exit with the Shutdown intent
// and waits for the socket to stop answering. Used only for daemons the
// harness has no PID for.
func shutdownViaSocket(sock string) {
	conn, err := net.DialTimeout("unix", sock, dialTimeout)
	if err != nil {
		return
	}
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	w := protocol.NewWriter(conn)
	_ = w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "rex-e2e"})
	_ = w.WriteIntent(protocol.IntentShutdown, "", struct{}{})
	// Close before waiting: the daemon does not finish shutting down while
	// a client is still connected.
	_ = conn.Close()
	deadline := time.Now().Add(stopWait)
	for dialable(sock) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}
