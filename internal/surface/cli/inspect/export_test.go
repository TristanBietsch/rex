package inspect

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/tristanbietsch/rex/internal/features/export"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// exportEnv is a state dir holding one session's synthetic transcript.
type exportEnv struct {
	home string
	root string
	sess protocol.SessionSummary
	out  *bytes.Buffer
	err  *bytes.Buffer
}

func newExportEnv(t *testing.T, tool, transcript string) *exportEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	sess := protocol.SessionSummary{
		ID: "11111111-2222-4333-8444-555555555555", ShortID: "1111", Slug: "widget", ToolID: tool,
		ModelID: "m", State: protocol.StateDone, StartedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	dir := filepath.Join(root, "sessions", sess.ID)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "transcript.log"), []byte(transcript), 0o600))
	e := &exportEnv{home: home, root: root, sess: sess, out: &bytes.Buffer{}, err: &bytes.Buffer{}}
	return e
}

// run parses args as the command line after `rex export <sel>`.
func (e *exportEnv) run(t *testing.T, env map[string]string, args ...string) error {
	t.Helper()
	getenv := func(k string) string { return env[k] }
	p, err := core.NewPrinter(core.FormatFlags{}, core.Env{Getenv: getenv, Width: 80, GOOS: runtime.GOOS}, e.out, e.err)
	require.NoError(t, err)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	a, err := parseExportArgs(append([]string{"widget"}, args...))
	if err != nil {
		return err
	}
	a.stateDir = e.root
	return doExport(a, e.sess)
}

const redrawTranscript = "\x1b[2J\x1b[Hthinking...\r\n\x1b[2J\x1b[Hdone: 42\r\n"

func TestExport_stdoutDefaultsToJSON(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	require.NoError(t, e.run(t, nil))
	var d export.Document
	require.NoError(t, json.Unmarshal(e.out.Bytes(), &d))
	require.Equal(t, 1, d.Version)
	require.Equal(t, "1111", d.Session.ShortID)
	require.Len(t, d.Frames, 2)
	require.Equal(t, "done: 42", d.Frames[1].Text)
	require.Empty(t, e.err.String())
}

func TestExport_formatFlag(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	require.NoError(t, e.run(t, nil, "-o", "md"))
	require.True(t, strings.HasPrefix(e.out.String(), "# Session export: widget\n"), e.out.String())

	e = newExportEnv(t, "claude", redrawTranscript)
	require.NoError(t, e.run(t, nil, "-o", "yaml"))
	var v map[string]any
	require.NoError(t, yaml.Unmarshal(e.out.Bytes(), &v))
	require.EqualValues(t, 1, v["version"])

	e = newExportEnv(t, "claude", redrawTranscript)
	require.NoError(t, e.run(t, nil, "-o", "txt"))
	require.Contains(t, e.out.String(), "rex session export: widget")
}

func TestExport_lineToolIsStripped(t *testing.T) {
	e := newExportEnv(t, "echo", "\x1b[32mhello\x1b[0m\r\n")
	require.NoError(t, e.run(t, nil, "-o", "txt"))
	require.Contains(t, e.out.String(), "\nhello\n")
	require.NotContains(t, e.out.String(), "\x1b")
}

func TestExport_badFormat(t *testing.T) {
	e := newExportEnv(t, "claude", "x")
	err := e.run(t, nil, "-o", "pdf")
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
}

func TestExport_argErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"no selector":    {},
		"two selectors":  {"a", "b"},
		"negative max":   {"a", "-max-frames", "-1"},
		"force no out":   {"a", "-force"},
		"empty out":      {"a", "-out", ""},
		"unknown option": {"a", "-bogus"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseExportArgs(args)
			var ec core.ExitCoder
			require.ErrorAs(t, err, &ec)
			require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
		})
	}
}

func TestExport_outInfersFormatFromExtension(t *testing.T) {
	for ext, check := range map[string]func(string){
		"json": func(s string) { require.True(t, json.Valid([]byte(s))) },
		"yaml": func(s string) { require.True(t, strings.HasPrefix(s, "version: 1\n")) },
		"yml":  func(s string) { require.True(t, strings.HasPrefix(s, "version: 1\n")) },
		"md":   func(s string) { require.True(t, strings.HasPrefix(s, "# Session export")) },
		"txt":  func(s string) { require.True(t, strings.HasPrefix(s, "rex session export")) },
		"log":  func(s string) { require.True(t, json.Valid([]byte(s)), "unknown extension: json") },
	} {
		t.Run(ext, func(t *testing.T) {
			e := newExportEnv(t, "claude", redrawTranscript)
			path := filepath.Join(t.TempDir(), "out."+ext)
			require.NoError(t, e.run(t, nil, "-out", path))
			b, err := os.ReadFile(path)
			require.NoError(t, err)
			check(string(b))
		})
	}
}

func TestExport_dashOWinsOverExtension(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	path := filepath.Join(t.TempDir(), "out.txt")
	require.NoError(t, e.run(t, nil, "-o", "md", "-out", path))
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(b), "# Session export"))
}

func TestExport_outRefusesToOverwriteWithoutForce(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	path := filepath.Join(t.TempDir(), "out.json")
	require.NoError(t, os.WriteFile(path, []byte("precious"), 0o600))

	err := e.run(t, nil, "-out", path)
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitOperationRefused, ec.ExitCode())
	require.Contains(t, err.Error(), "--force")
	b, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	require.Equal(t, "precious", string(b))

	require.NoError(t, e.run(t, nil, "-out", path, "-force"))
	b, rerr = os.ReadFile(path)
	require.NoError(t, rerr)
	require.True(t, json.Valid(b))
}

func TestExport_outFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	e := newExportEnv(t, "claude", redrawTranscript)
	path := filepath.Join(t.TempDir(), "out.md")
	require.NoError(t, e.run(t, nil, "-out", path))
	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
}

func TestExport_outReportsThePath(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	path := filepath.Join(t.TempDir(), "out.json")
	require.NoError(t, e.run(t, nil, "-out", path))
	require.Contains(t, e.out.String(), path)
	require.NotContains(t, e.out.String(), `"frames"`, "the document goes to the file, not stdout")
}

func TestExport_agentModeReportsOneRecord(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	path := filepath.Join(t.TempDir(), "out.json")
	require.NoError(t, e.run(t, map[string]string{"REX_AGENT": "1"}, "-out", path))
	lines := strings.Split(strings.TrimSpace(e.out.String()), "\n")
	require.Len(t, lines, 2, e.out.String())
	require.True(t, strings.HasPrefix(lines[0], "export "), lines[0])
	require.Contains(t, lines[0], "path=")
	require.Contains(t, lines[0], "frames=2")
	require.True(t, strings.HasPrefix(lines[1], "ok "), lines[1])
}

func TestExport_agentModeStdoutIsStillTheDocument(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	require.NoError(t, e.run(t, map[string]string{"REX_AGENT": "1"}))
	require.True(t, json.Valid(e.out.Bytes()))
}

func TestExport_maxFrames(t *testing.T) {
	e := newExportEnv(t, "claude", "a\x1b[2J\x1b[Hb\x1b[2J\x1b[Hc")
	require.NoError(t, e.run(t, nil, "-max-frames", "1"))
	var d export.Document
	require.NoError(t, json.Unmarshal(e.out.Bytes(), &d))
	require.Len(t, d.Frames, 1)
	require.Equal(t, "c", d.Frames[0].Text)
	require.Equal(t, 2, d.FramesDropped)
}

func TestExport_missingTranscript(t *testing.T) {
	e := newExportEnv(t, "claude", "x")
	require.NoError(t, os.Remove(filepath.Join(e.root, "sessions", e.sess.ID, "transcript.log")))
	err := e.run(t, nil)
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitGeneric, ec.ExitCode())
	require.Contains(t, err.Error(), "no transcript")
}

func TestExportFailure_tooLargeSuggestsMaxFrames(t *testing.T) {
	err := exportFailure(export.ErrOutputTooLarge)
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitOperationRefused, ec.ExitCode())
	require.Contains(t, err.Error(), "--max-frames")
}

func TestExport_offIsInert(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	cfg := filepath.Join(e.home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("session_export: false\n"), 0o600))

	path := filepath.Join(t.TempDir(), "out.json")
	err := RunExport([]string{"-socket", "/nonexistent/rex.sock", "-out", path, "widget"})
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode(), "off: exit 5, before any daemon contact")
	require.Contains(t, err.Error(), "off: enable in Settings → Features (rex config set session_export true)")
	require.NoFileExists(t, path)
	require.Empty(t, e.out.String())
}

func TestExport_globalFormatFlagIsRefused(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	p, err := core.NewPrinter(core.FormatFlags{Output: "md"}, core.Env{Getenv: func(string) string { return "" }, Width: 80, GOOS: runtime.GOOS}, e.out, io.Discard)
	require.NoError(t, err)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	err = RunExport([]string{"-socket", "/nonexistent/rex.sock", "widget"})
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
	require.Contains(t, err.Error(), "after the command")
}

func TestRunExport_daemonDown(t *testing.T) {
	newExportEnv(t, "claude", "x")
	err := RunExport([]string{"-socket", "/nonexistent/rex.sock", "widget"})
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitDaemonUnreachable, ec.ExitCode())
}

func TestRunExport_endToEnd(t *testing.T) {
	// The agent's working folder. Its removal runs last and waits for the
	// deleted agent to exit: Windows cannot remove a running child's cwd.
	cwd, err := os.MkdirTemp("", "rex-export-cwd")
	require.NoError(t, err)
	t.Cleanup(func() {
		deadline := time.Now().Add(10 * time.Second)
		for os.RemoveAll(cwd) != nil && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
	})
	d := testutil.StartDaemon(t)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("export-test")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{ToolID: "echo", ModelID: "prompt", Slug: "exporty", CWD: cwd})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	t.Cleanup(func() { _, _ = c.Do(ctx, protocol.IntentDelete, protocol.Delete{SessionID: ack.SessionID}) })

	// The session is still running while it is exported.
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(filepath.Join(d.StateDir, "sessions", ack.SessionID, "transcript.log"))
		return err == nil && strings.Contains(string(b), "awaiting input:")
	}, 15*time.Second, 50*time.Millisecond)

	var runErr error
	out := captureStdout(t, func() {
		runErr = RunExport([]string{"-socket", d.Socket, "-state-dir", d.StateDir, "exporty", "-o", "txt"})
	})
	require.NoError(t, runErr)
	require.Contains(t, out, "tool: echo")
	require.Contains(t, out, "awaiting input:")
}

const agentLogFixture = `{"type":"user","timestamp":"2026-01-02T03:04:06Z","message":{"role":"user","content":"fix the widget"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"On it."},{"type":"tool_use","name":"Edit","input":{"file_path":"/work/example/widget.go"}}]}}
`

func TestExport_fromAgentLog(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	e.sess.ResumeID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	dir := filepath.Join(e.home, ".claude", "projects", "-work-example")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, e.sess.ResumeID+".jsonl"), []byte(agentLogFixture), 0o600))

	require.NoError(t, e.run(t, nil, "--from-agent-log"))
	var d export.Document
	require.NoError(t, json.Unmarshal(e.out.Bytes(), &d))
	require.Equal(t, export.SourceAgentLog, d.Source)
	require.Empty(t, d.Frames)
	require.Len(t, d.Turns, 2)
	require.Equal(t, "On it.\n[tool] Edit /work/example/widget.go", d.Turns[1].Text)
	require.Empty(t, e.err.String())
}

func TestExport_fromAgentLogMissingFallsBackWithNotice(t *testing.T) {
	e := newExportEnv(t, "claude", redrawTranscript)
	e.sess.ResumeID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	require.NoError(t, e.run(t, nil, "--from-agent-log"))
	var d export.Document
	require.NoError(t, json.Unmarshal(e.out.Bytes(), &d))
	require.Equal(t, export.SourceFrames, d.Source)
	require.Len(t, d.Frames, 2)
	require.Contains(t, d.Notice, "showing frames")
	require.Contains(t, e.err.String(), "rex: export: agent log")
}

func TestExportFailure_mapping(t *testing.T) {
	for err, code := range map[error]int{
		export.ErrTooLarge:     core.ExitOperationRefused,
		export.ErrSymlink:      core.ExitOperationRefused,
		export.ErrExists:       core.ExitOperationRefused,
		export.ErrNoTranscript: core.ExitGeneric,
		io.ErrUnexpectedEOF:    core.ExitGeneric,
	} {
		var ec core.ExitCoder
		require.ErrorAs(t, exportFailure(err), &ec)
		require.Equal(t, code, ec.ExitCode(), err.Error())
	}
}
