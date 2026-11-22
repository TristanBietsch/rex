//go:build !windows

// Package update_test runs tools/update/update.sh against stub go, git, rex
// and rex-daemon binaries in a temp checkout, so no real build, install or
// daemon is touched.
package update_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stubGo is `go -C SRC build ... -o DIR/ ./cmd/rex ./cmd/rex-daemon`: it
// copies the fixture's rex and rex-daemon into DIR.
const stubGo = `#!/bin/sh
echo "go $*" >>"$STUB_LOG"
if [ -f "$FIX/build_fail" ]; then echo "compile error" >&2; exit 1; fi
out= prev=
for a in "$@"; do [ "$prev" = -o ] && out=$a; prev=$a; done
cp "$FIX/rex" "${out}rex" && cp "$FIX/rex-daemon" "${out}rex-daemon"
`

// stubGit answers describe and rev-parse; pull rewrites update.sh so the
// test can tell the pulled script ran.
const stubGit = `#!/bin/sh
echo "git $*" >>"$STUB_LOG"
case "$*" in
  *describe*) echo v9.9.9 ;;
  *rev-parse*) echo main ;;
  *pull*)
    f="$2/tools/update/update.sh"
    { sed -n 1p "$f"; echo 'echo pulled-script-ran >>"$STUB_LOG"'; sed '1d' "$f"; } >"$f.new" && mv "$f.new" "$f"
    ;;
esac
`

// stubRex logs its argv; upgrade exits with the code in FIX/upgrade_rc.
// The build line makes two fixture builds differ byte for byte.
func stubRex(build string) string {
	return `#!/bin/sh
# build ` + build + `
echo "rex $*" >>"$STUB_LOG"
case "$1" in
  upgrade) rc=$(cat "$FIX/upgrade_rc" 2>/dev/null || echo 0); echo "upgrade report"; echo "upgrade warning" >&2; exit "$rc" ;;
esac
echo v9.9.9
`
}

func stubDaemon(build string) string {
	return `#!/bin/sh
# build ` + build + `
[ -f "$FIX/daemon_broken" ] && exit 1
echo v9.9.9
`
}

type env struct {
	t                         *testing.T
	shell                     string
	src, fix, bin, home, logf string
	stubs                     string
}

func newEnv(t *testing.T, shell string) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{t: t, shell: shell,
		src: filepath.Join(root, "src"), fix: filepath.Join(root, "fix"), stubs: filepath.Join(root, "stubs"),
		home: filepath.Join(root, "home"), bin: filepath.Join(root, "prefix", "bin"), logf: filepath.Join(root, "stub.log")}
	script, err := os.ReadFile("update.sh")
	require.NoError(t, err)
	for _, d := range []string{filepath.Join(e.src, "tools", "update"), filepath.Join(e.src, "cmd", "rex"), filepath.Join(e.src, "cmd", "rex-daemon"), e.fix, e.stubs, e.home} {
		require.NoError(t, os.MkdirAll(d, 0o755)) //nolint:gosec // G301: a fake checkout and PATH
	}
	write(t, filepath.Join(e.src, "tools", "update", "update.sh"), string(script))
	write(t, filepath.Join(e.src, "go.mod"), "module example\n")
	write(t, filepath.Join(e.stubs, "go"), stubGo)
	write(t, filepath.Join(e.stubs, "git"), stubGit)
	e.setBuild("A")
	return e
}

func write(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o755)) //nolint:gosec // a test stub must be executable
}

// setBuild makes the next go build produce build b of both binaries.
func (e *env) setBuild(b string) {
	write(e.t, filepath.Join(e.fix, "rex"), stubRex(b))
	write(e.t, filepath.Join(e.fix, "rex-daemon"), stubDaemon(b))
}

func (e *env) flag(name, body string) { write(e.t, filepath.Join(e.fix, name), body) }

func (e *env) run(args ...string) (string, int) {
	e.t.Helper()
	cmd := exec.Command(e.shell, append([]string{filepath.Join(e.src, "tools", "update", "update.sh")}, args...)...)
	cmd.Env = []string{
		"PATH=" + e.stubs + ":/usr/bin:/bin",
		"HOME=" + e.home,
		"PREFIX=" + filepath.Dir(e.bin),
		"FIX=" + e.fix,
		"STUB_LOG=" + e.logf,
	}
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else {
		require.NoError(e.t, err)
	}
	return string(out), code
}

func (e *env) stubLog() string {
	b, _ := os.ReadFile(e.logf)
	return string(b)
}

func (e *env) resetLog() { _ = os.Remove(e.logf) }

func (e *env) stat(name string) os.FileInfo {
	fi, err := os.Stat(filepath.Join(e.bin, name))
	require.NoError(e.t, err)
	return fi
}

func (e *env) requireClean() {
	e.t.Helper()
	_, err := os.Lstat(filepath.Join(e.bin, ".rex-stage"))
	require.True(e.t, os.IsNotExist(err), "staging folder left behind")
	_, err = os.Lstat(filepath.Join(e.bin, ".rex-update.lock"))
	require.True(e.t, os.IsNotExist(err), "lock left behind")
}

// shells is /bin/sh, plus dash when it is installed (Debian's /bin/sh).
func shells() []string {
	s := []string{"/bin/sh"}
	if p, err := exec.LookPath("dash"); err == nil {
		s = append(s, p)
	}
	return s
}

func eachShell(t *testing.T, f func(t *testing.T, e *env)) {
	for _, sh := range shells() {
		t.Run(filepath.Base(sh), func(t *testing.T) { f(t, newEnv(t, sh)) })
	}
}

func TestUpdate_freshInstallThenNoChange(t *testing.T) {
	eachShell(t, func(t *testing.T, e *env) {
		out, code := e.run()
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "installed rex-daemon rex (v9.9.9)")
		require.Contains(t, e.stubLog(), "rex upgrade -o text -no-cold -if-stale")
		require.Contains(t, out, "upgrade report")
		e.requireClean()

		rex, daemon := e.stat("rex"), e.stat("rex-daemon")
		e.resetLog()
		out, code = e.run()
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "already has v9.9.9")
		require.True(t, os.SameFile(rex, e.stat("rex")), "an unchanged rex was renamed")
		require.True(t, os.SameFile(daemon, e.stat("rex-daemon")), "an unchanged rex-daemon was renamed")
		require.Contains(t, e.stubLog(), "rex upgrade -o text -no-cold -if-stale", "a stale daemon is upgrade's call")
		e.requireClean()
	})
}

// Only the binary that changed is renamed.
func TestUpdate_onlyChangedBinary(t *testing.T) {
	eachShell(t, func(t *testing.T, e *env) {
		_, code := e.run()
		require.Equal(t, 0, code)
		daemon := e.stat("rex-daemon")
		write(t, filepath.Join(e.fix, "rex"), stubRex("B"))
		out, code := e.run()
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "installed rex (v9.9.9)")
		require.True(t, os.SameFile(daemon, e.stat("rex-daemon")))
	})
}

func TestUpdate_noRestart(t *testing.T) {
	eachShell(t, func(t *testing.T, e *env) {
		out, code := e.run("--no-restart")
		require.Equal(t, 0, code, out)
		require.NotContains(t, e.stubLog(), "rex upgrade")
		require.Contains(t, out, "--no-restart")
	})
}

func TestUpdate_upgradeExitCodes(t *testing.T) {
	eachShell(t, func(t *testing.T, e *env) {
		e.flag("upgrade_rc", "4")
		out, code := e.run()
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "daemon is not running")
		require.NotContains(t, out, "upgrade warning", "the not-running error is noise")

		e.setBuild("B")
		e.flag("upgrade_rc", "6")
		out, code = e.run()
		require.Equal(t, 3, code, out)
		require.Contains(t, out, "upgrade warning")
		require.Contains(t, out, "not on the new build")
		require.Contains(t, out, "installed rex-daemon rex", "the binaries are in place either way")
		e.requireClean()
	})
}

// A failed build or smoke test installs nothing and leaves nothing.
func TestUpdate_buildOrSmokeFailureInstallsNothing(t *testing.T) {
	eachShell(t, func(t *testing.T, e *env) {
		e.flag("build_fail", "")
		out, code := e.run()
		require.Equal(t, 1, code, out)
		require.Contains(t, out, "compile error")
		require.Contains(t, out, "nothing was installed")
		_, err := os.Stat(filepath.Join(e.bin, "rex"))
		require.True(t, os.IsNotExist(err))
		e.requireClean()

		require.NoError(t, os.Remove(filepath.Join(e.fix, "build_fail")))
		e.flag("daemon_broken", "")
		out, code = e.run()
		require.Equal(t, 1, code, out)
		require.Contains(t, out, "the new rex-daemon does not run")
		_, err = os.Stat(filepath.Join(e.bin, "rex-daemon"))
		require.True(t, os.IsNotExist(err))
		e.requireClean()
	})
}

// A lock held by a running update.sh refuses with 75; a dead PID's lock and
// a PID reused by another program are taken over.
func TestUpdate_lock(t *testing.T) {
	eachShell(t, func(t *testing.T, e *env) {
		require.NoError(t, os.MkdirAll(e.bin, 0o755)) //nolint:gosec // G301: a fake PREFIX/bin
		lock := filepath.Join(e.bin, ".rex-update.lock")

		holder := exec.Command("/bin/sh", "-c", "sleep 60; :", "update.sh")
		require.NoError(t, holder.Start())
		t.Cleanup(func() { _ = holder.Process.Kill(); _ = holder.Wait() })
		waitPS(t, holder.Process.Pid, "update.sh")
		require.NoError(t, os.Symlink(strconv.Itoa(holder.Process.Pid), lock))
		out, code := e.run()
		require.Equal(t, 75, code, out)
		require.Contains(t, out, "another update is running")
		target, err := os.Readlink(lock)
		require.NoError(t, err)
		require.Equal(t, strconv.Itoa(holder.Process.Pid), target, "a busy lock is not touched")
		require.NoError(t, os.Remove(lock))

		dead := exec.Command("/bin/sh", "-c", "exit 0")
		require.NoError(t, dead.Run())
		require.NoError(t, os.Symlink(strconv.Itoa(dead.Process.Pid), lock))
		out, code = e.run()
		require.Equal(t, 0, code, out)
		e.requireClean()

		other := exec.Command("sleep", "60")
		require.NoError(t, other.Start())
		t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })
		require.NoError(t, os.Symlink(strconv.Itoa(other.Process.Pid), lock))
		out, code = e.run()
		require.Equal(t, 0, code, out)
		e.requireClean()
	})
}

// waitPS waits until ps shows want in pid's command line.
func waitPS(t *testing.T, pid int, want string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		out, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if strings.Contains(string(out), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid %d never showed %q", pid, want)
}

// --pull runs git pull once, then the pulled script, which keeps the lock.
func TestUpdate_pullReexecsPulledScript(t *testing.T) {
	eachShell(t, func(t *testing.T, e *env) {
		out, code := e.run("--pull")
		require.Equal(t, 0, code, out)
		log := e.stubLog()
		require.Equal(t, 1, strings.Count(log, "pull --ff-only"), log)
		require.Contains(t, log, "pulled-script-ran")
		require.Less(t, strings.Index(log, "pulled-script-ran"), strings.Index(log, "go -C"), "the pulled script built")
		e.requireClean()
	})
}

func TestUpdate_usage(t *testing.T) {
	eachShell(t, func(t *testing.T, e *env) {
		out, code := e.run("--bogus")
		require.Equal(t, 2, code)
		require.Contains(t, out, "unknown argument: --bogus")
		out, code = e.run("-h")
		require.Equal(t, 0, code)
		require.Contains(t, out, "Usage:")
	})
}
