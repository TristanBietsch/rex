package loopcli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/features/loop"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	t.Cleanup(func() { os.Stdout = saved; core.SetOut(nil) })
	core.SetOut(nil)
	fn()
	os.Stdout = saved
	require.NoError(t, w.Close())
	return <-done
}

func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	return ec.ExitCode()
}

// home points HOME at a fresh folder with config (may be "") and returns it.
func home(t *testing.T, config string) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	if config != "" {
		cfg := filepath.Join(h, ".config", "rex", "config.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
		require.NoError(t, os.WriteFile(cfg, []byte(config), 0o600))
	}
	return h
}

const loopsOn = "loops: true\n"

const fixYAML = `name: fix
kind: repeat
tool: claude
model: sonnet
prompt: "fix it"
until:
  check: ["make", "check"]
  max_iterations: 4
`

func writeLoopFile(t *testing.T, h, name, body string) {
	t.Helper()
	dir := filepath.Join(h, ".config", "rex", "loops")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o600))
}

type fakeLoops struct {
	mu    sync.Mutex
	calls []string
	start protocol.StartLoop
	err   error
	loops []protocol.LoopSummary
}

func (f *fakeLoops) rec(s string) (protocol.LoopSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
	if f.err != nil {
		return protocol.LoopSummary{}, f.err
	}
	return protocol.LoopSummary{ID: "loop-1a2b3c4d", Name: "fix", Kind: "repeat", State: protocol.LoopRunning, SessionID: "sess", Iteration: 1, MaxIterations: 4}, nil
}
func (f *fakeLoops) Start(p protocol.StartLoop) (protocol.LoopSummary, error) {
	f.mu.Lock()
	f.start = p
	f.mu.Unlock()
	return f.rec("start")
}
func (f *fakeLoops) Pause(id string) (protocol.LoopSummary, error)  { return f.rec("pause " + id) }
func (f *fakeLoops) Resume(id string) (protocol.LoopSummary, error) { return f.rec("resume " + id) }
func (f *fakeLoops) Stop(id string) (protocol.LoopSummary, error)   { return f.rec("stop " + id) }
func (f *fakeLoops) Snapshot() []protocol.LoopSummary               { return f.loops }
func (f *fakeLoops) UserInput(string)                               {}

func daemonWith(t *testing.T, fl *fakeLoops) *testutil.Daemon {
	t.Helper()
	d := testutil.StartDaemonWithConfig(t, "", func(c *server.Config) {
		if fl != nil {
			c.Loops = fl
		}
	})
	t.Cleanup(d.Close)
	return d
}

// Off is inert: every subcommand gives the standard off message (exit 5)
// before any file is read, any file written or any connection tried.
func TestRunLoop_OffIsInert(t *testing.T) {
	h := home(t, "")
	writeLoopFile(t, h, "fix", fixYAML)
	for _, args := range [][]string{
		{"ls"}, {"start", "fix"}, {"pause", "loop-1a2b3c4d"}, {"resume", "loop-1a2b3c4d"},
		{"stop", "loop-1a2b3c4d"}, {"log", "loop-1a2b3c4d"}, {"new", "fresh"},
	} {
		err := RunLoop(append([]string{"-socket", "/nonexistent/rex.sock"}, args...))
		require.Equal(t, 5, exitCodeOf(t, err), "%v", args)
		require.Contains(t, err.Error(), "off: enable in Settings → Features (rex config set loops true)", "%v", args)
	}
	require.NoFileExists(t, filepath.Join(h, ".config", "rex", "loops", "fresh.yaml"))
}

func TestRunLoop_ArgumentErrors(t *testing.T) {
	home(t, loopsOn)
	for _, args := range [][]string{
		{}, {"bogus"}, {"ls", "extra"}, {"start"}, {"start", "a", "b"}, {"stop"}, {"log"}, {"new"},
		{"ls", "-cwd", "/x"}, {"start", "x", "-n", "3"}, {"stop", "not-an-id"}, {"stop", "loop-XYZ"}, {"log", "../x"},
		{"start", "Bad Name"}, {"new", "Bad Name"}, {"log", "loop-1a2b3c4d", "-n", "0"}, {"-bogus"},
	} {
		err := RunLoop(append([]string{"-socket", "/nonexistent/rex.sock"}, args...))
		require.Equal(t, 5, exitCodeOf(t, err), "%v: %v", args, err)
	}
}

func TestRunLoop_Start(t *testing.T) {
	h := home(t, loopsOn)
	writeLoopFile(t, h, "fix", fixYAML)
	fl := &fakeLoops{}
	d := daemonWith(t, fl)

	cwd := t.TempDir()
	var runErr error
	out := captureStdout(t, func() {
		runErr = RunLoop([]string{"-socket", d.Socket, "start", "fix", "-cwd", cwd, "-o", "text"})
	})
	require.NoError(t, runErr)
	require.Equal(t, protocol.StartLoop{Name: "fix", CWD: cwd}, fl.start)
	require.Contains(t, out, "started loop-1a2b3c4d fix")

	out = captureStdout(t, func() { runErr = RunLoop([]string{"-socket", d.Socket, "start", "fix", "-o", "json"}) })
	require.NoError(t, runErr)
	wd, _ := os.Getwd()
	require.Equal(t, wd, fl.start.CWD, "no -cwd and no cwd in the file: the current folder")
	var env struct {
		Cmd  string
		Data protocol.LoopSummary
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env))
	require.Equal(t, "loop-1a2b3c4d", env.Data.ID)

	out = captureStdout(t, func() { runErr = RunLoop([]string{"-socket", d.Socket, "start", "fix", "--agent"}) })
	require.NoError(t, runErr)
	require.Contains(t, out, "loop=loop-1a2b3c4d")

	// The file's own cwd is used when there is no flag.
	filecwd := t.TempDir()
	writeLoopFile(t, h, "fixcwd", strings.Replace(strings.Replace(fixYAML, "name: fix", "name: fixcwd", 1), "prompt:", "cwd: "+filecwd+"\nprompt:", 1))
	_ = captureStdout(t, func() { runErr = RunLoop([]string{"-socket", d.Socket, "start", "fixcwd"}) })
	require.NoError(t, runErr)
	require.Equal(t, filecwd, fl.start.CWD)
}

func TestRunLoop_StartRefusals(t *testing.T) {
	h := home(t, loopsOn)
	writeLoopFile(t, h, "fix", fixYAML)
	writeLoopFile(t, h, "bad", "name: bad\n")
	err := RunLoop([]string{"-socket", "/nonexistent/rex.sock", "start", "missing"})
	require.Equal(t, core.ExitSelectorNotFound, exitCodeOf(t, err))
	err = RunLoop([]string{"-socket", "/nonexistent/rex.sock", "start", "bad"})
	require.Equal(t, 5, exitCodeOf(t, err), "an invalid file fails before the daemon is asked")
	err = RunLoop([]string{"-socket", "/nonexistent/rex.sock", "start", "fix", "-cwd", "rel"})
	require.Equal(t, 5, exitCodeOf(t, err))
	err = RunLoop([]string{"-socket", "/nonexistent/rex.sock", "start", "fix", "-cwd", "/a/../b"})
	require.Equal(t, 5, exitCodeOf(t, err))
	err = RunLoop([]string{"-socket", "/nonexistent/rex.sock", "start", "fix", "-cwd", "/tmp"})
	require.Equal(t, 4, exitCodeOf(t, err), "no daemon: exit 4")
}

func TestRunLoop_PauseResumeStop(t *testing.T) {
	home(t, loopsOn)
	fl := &fakeLoops{}
	d := daemonWith(t, fl)
	for _, verb := range []string{"pause", "resume", "stop"} {
		var runErr error
		out := captureStdout(t, func() { runErr = RunLoop([]string{"-socket", d.Socket, verb, "1a2b3c4d"}) })
		require.NoError(t, runErr, verb)
		require.Contains(t, out, "loop-1a2b3c4d", verb)
	}
	require.Equal(t, []string{"pause loop-1a2b3c4d", "resume loop-1a2b3c4d", "stop loop-1a2b3c4d"}, fl.calls, "the short form expands")
}

func TestRunLoop_DaemonRefusals(t *testing.T) {
	home(t, loopsOn)
	// An old daemon.
	old := daemonWith(t, nil)
	err := RunLoop([]string{"-socket", old.Socket, "pause", "loop-1a2b3c4d"})
	require.Equal(t, core.ExitOperationRefused, exitCodeOf(t, err))
	require.Contains(t, err.Error(), "run `rex daemon restart`")

	fl := &fakeLoops{}
	d := daemonWith(t, fl)
	for _, tc := range []struct {
		err  error
		code int
		want string
	}{
		{loop.ErrOff, 5, "off: enable in Settings → Features (rex config set loops true)"},
		{&loop.Error{Kind: loop.KindBadInput, Msg: loop.MsgTooMany}, core.ExitOperationRefused, "too many loops"},
		{&loop.Error{Kind: loop.KindBadState, Msg: "loop is not paused"}, core.ExitOperationRefused, "loop is not paused"},
		{&loop.Error{Kind: loop.KindNotFound, Msg: "unknown loop"}, core.ExitOperationRefused, "unknown loop"},
	} {
		fl.err = tc.err
		err = RunLoop([]string{"-socket", d.Socket, "resume", "loop-1a2b3c4d"})
		require.Equal(t, tc.code, exitCodeOf(t, err), tc.want)
		require.Contains(t, err.Error(), tc.want)
	}
	fl.err = errors.New("boom")
	err = RunLoop([]string{"-socket", d.Socket, "stop", "loop-1a2b3c4d"})
	require.Error(t, err)
}

func TestRunLoop_LS(t *testing.T) {
	h := home(t, loopsOn)
	writeLoopFile(t, h, "fix", fixYAML)
	writeLoopFile(t, h, "bad", "name: bad\n")
	fl := &fakeLoops{loops: []protocol.LoopSummary{{
		ID: "loop-1a2b3c4d", Name: "fix", State: protocol.LoopPaused, PauseReason: protocol.PauseNeedsInput,
		Iteration: 2, MaxIterations: 4, UpdatedAt: time.Now().Add(-3 * time.Minute), Notice: "watch out",
	}}}
	d := daemonWith(t, fl)

	var runErr error
	out := captureStdout(t, func() { runErr = RunLoop([]string{"-socket", d.Socket, "ls", "-o", "text"}) })
	require.NoError(t, runErr)
	for _, want := range []string{"fix", "claude", "sonnet", "make check", "invalid:", "loop-1a2b3c4d", "paused", "needs_input", "2/4", "3m", "watch out"} {
		require.Contains(t, out, want)
	}

	out = captureStdout(t, func() { runErr = RunLoop([]string{"-socket", d.Socket, "ls", "-o", "json"}) })
	require.NoError(t, runErr)
	var env struct{ Data lsData }
	require.NoError(t, json.Unmarshal([]byte(out), &env))
	require.Equal(t, "ok", env.Data.Daemon)
	require.Len(t, env.Data.Files, 2)
	require.Len(t, env.Data.Loops, 1)
	require.NotEmpty(t, env.Data.Files[0].Error, "bad.yaml sorts first and is invalid")

	// No daemon: the files still list.
	out = captureStdout(t, func() { runErr = RunLoop([]string{"-socket", "/nonexistent/rex.sock", "ls", "-o", "json"}) })
	require.NoError(t, runErr)
	require.NoError(t, json.Unmarshal([]byte(out), &env))
	require.Equal(t, "unreachable", env.Data.Daemon)
	require.Len(t, env.Data.Files, 2)
}

func TestRunLoop_LSEmpty(t *testing.T) {
	home(t, loopsOn)
	var runErr error
	out := captureStdout(t, func() { runErr = RunLoop([]string{"-socket", "/nonexistent/rex.sock", "ls"}) })
	require.NoError(t, runErr)
	require.Contains(t, out, "no loop files")
	require.Contains(t, out, "no loops")
}

func TestRunLoop_Log(t *testing.T) {
	h := home(t, loopsOn)
	dir := filepath.Join(h, ".local", "share", "rex", "loops")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "line " + string(rune('A'+i%26)) + string(rune('0'+i/26))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "loop-1a2b3c4d.log"), []byte(strings.Join(lines, "\n")+"\n"), 0o600))

	var runErr error
	out := captureStdout(t, func() { runErr = RunLoop([]string{"log", "loop-1a2b3c4d", "-n", "3", "-o", "text"}) })
	require.NoError(t, runErr)
	require.Contains(t, out, lines[29])
	require.Contains(t, out, lines[27])
	require.NotContains(t, out, lines[26])

	out = captureStdout(t, func() { runErr = RunLoop([]string{"log", "1a2b3c4d", "-o", "json"}) })
	require.NoError(t, runErr)
	var env struct{ Data logData }
	require.NoError(t, json.Unmarshal([]byte(out), &env))
	require.Equal(t, 30, env.Data.Lines)
	require.Equal(t, "loop-1a2b3c4d", env.Data.LoopID)

	err := RunLoop([]string{"log", "loop-ffffffff"})
	require.Equal(t, core.ExitSelectorNotFound, exitCodeOf(t, err))
}

func TestRunLoop_New(t *testing.T) {
	h := home(t, loopsOn)
	var runErr error
	var edited []string
	runEditorSaved, ttySaved := runEditor, stdinIsTerminal
	t.Cleanup(func() { runEditor, stdinIsTerminal = runEditorSaved, ttySaved })
	runEditor = func(argv []string, _, _ io.Writer) error { edited = argv; return nil }
	stdinIsTerminal = func() bool { return false }
	t.Setenv("EDITOR", "myed -w")

	out := captureStdout(t, func() { runErr = RunLoop([]string{"new", "my-loop"}) })
	require.NoError(t, runErr)
	path := filepath.Join(h, ".config", "rex", "loops", "my-loop.yaml")
	require.Contains(t, out, path)
	require.Nil(t, edited, "no terminal: no editor")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(b), "# Loop workflow: my-loop")
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
		// The starter is itself a valid, loadable loop file.
		_, err = loop.LoadFile(filepath.Dir(path), "my-loop")
		require.NoError(t, err)
	}

	err = RunLoop([]string{"new", "my-loop"})
	require.Equal(t, core.ExitOperationRefused, exitCodeOf(t, err), "never overwrite")

	stdinIsTerminal = func() bool { return true }
	_ = captureStdout(t, func() { runErr = RunLoop([]string{"new", "second"}) })
	require.NoError(t, runErr)
	require.Equal(t, []string{"myed", "-w", filepath.Join(h, ".config", "rex", "loops", "second.yaml")}, edited)

	runEditor = func([]string, io.Writer, io.Writer) error { return errors.New("editor died") }
	err = RunLoop([]string{"new", "third"})
	require.ErrorContains(t, err, "editor failed")
}

func TestLoopID(t *testing.T) {
	id, err := loopID("1a2b3c4d")
	require.NoError(t, err)
	require.Equal(t, "loop-1a2b3c4d", id)
	_, err = loopID("loop-1a2b3c4d")
	require.NoError(t, err)
	for _, bad := range []string{"", "xyz", "loop-1A2B3C4D", "1a2b3c4", "../../x"} {
		_, err = loopID(bad)
		require.Error(t, err, bad)
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{5 * time.Second: "5s", 3 * time.Minute: "3m", 5 * time.Hour: "5h", 72 * time.Hour: "3d"} {
		require.Equal(t, want, age(now, now.Add(-d)))
	}
	require.Equal(t, "", age(now, time.Time{}))
}
