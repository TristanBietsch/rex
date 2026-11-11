package usage

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub user command is a POSIX shell script")
	}
}

// tapEnv is a fake HOME whose user status line is command ("" for none).
func tapEnv(t *testing.T, command string) TapOptions {
	t.Helper()
	home, cwd := fakeHome(t, false)
	if command != "" {
		body, err := jsonString(command)
		require.NoError(t, err)
		writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"statusLine":{"type":"command","command":`+body+`}}`)
	}
	return TapOptions{Session: "sess-1", Home: home, Cwd: cwd}
}

func jsonString(s string) (string, error) {
	var b bytes.Buffer
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}

const statusJSON = `{"session_id":"c1","cwd":"/work","transcript_path":"/secret/t.jsonl","model":{"id":"m1","display_name":"M One"},"cost":{"total_cost_usd":2.5},"rate_limits":{"five_hour":{"used_percentage":40,"resets_at":1900000000}}}`

func TestRunTap_PassesStdinByteForByteAndExitCode(t *testing.T) {
	skipWindows(t)
	dump := filepath.Join(t.TempDir(), "stdin.bin")
	o := tapEnv(t, "cat > '"+dump+"'; printf 'line one\\n'; echo oops >&2; exit 3")
	// Odd bytes: no trailing newline, a NUL, non-UTF-8.
	in := []byte(statusJSON + "\x00\xff tail")
	var out, errOut bytes.Buffer
	o.Stdin, o.Stdout, o.Stderr = bytes.NewReader(in), &out, &errOut
	o.Send = func(context.Context, protocol.UsageSample) error { return nil }

	code := RunTap(context.Background(), o)

	require.Equal(t, 3, code)
	require.Equal(t, "line one\n", out.String())
	require.Equal(t, "oops\n", errOut.String())
	got, err := os.ReadFile(dump)
	require.NoError(t, err)
	require.Equal(t, in, got, "the user's command gets the identical stdin bytes")
}

func TestRunTap_NoUserCommandPrintsNothingExitsZero(t *testing.T) {
	o := tapEnv(t, "")
	var out, errOut bytes.Buffer
	o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(statusJSON)), &out, &errOut
	var got []protocol.UsageSample
	o.Send = func(_ context.Context, s protocol.UsageSample) error { got = append(got, s); return nil }

	require.Zero(t, RunTap(context.Background(), o))
	require.Empty(t, out.String())
	require.Empty(t, errOut.String())
	require.Len(t, got, 1, "usage is still reported")
	require.Equal(t, "sess-1", got[0].SessionID)
}

func TestRunTap_DaemonDownStillRunsUserCommand(t *testing.T) {
	skipWindows(t)
	o := tapEnv(t, "cat >/dev/null; printf mine")
	o.Socket = filepath.Join(t.TempDir(), "nobody-listens.sock")
	var out bytes.Buffer
	o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(statusJSON)), &out, &bytes.Buffer{}

	require.Zero(t, RunTap(context.Background(), o))
	require.Equal(t, "mine", out.String())
}

func TestRunTap_SendErrorIsIgnored(t *testing.T) {
	skipWindows(t)
	o := tapEnv(t, "cat >/dev/null; printf mine")
	var out bytes.Buffer
	o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(statusJSON)), &out, &bytes.Buffer{}
	o.Send = func(context.Context, protocol.UsageSample) error { return errors.New("boom") }
	require.Zero(t, RunTap(context.Background(), o))
	require.Equal(t, "mine", out.String())
}

// A daemon that never answers must not slow Claude's render by more than
// the cap, even when the sender ignores its context.
func TestRunTap_HungDaemonAddsAtMostTheCap(t *testing.T) {
	for _, withUser := range []bool{false, true} {
		cmd := ""
		if withUser {
			skipWindows(t)
			cmd = "cat >/dev/null; printf mine"
		}
		o := tapEnv(t, cmd)
		o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(statusJSON)), &bytes.Buffer{}, &bytes.Buffer{}
		release := make(chan struct{})
		o.Send = func(context.Context, protocol.UsageSample) error { <-release; return nil } // ignores ctx
		start := time.Now()
		code := RunTap(context.Background(), o)
		elapsed := time.Since(start)
		close(release)
		require.Zero(t, code)
		require.Less(t, elapsed, 250*time.Millisecond, "withUser=%v", withUser)
	}
}

// The same bound through the real dial path, against a fake socket that
// accepts and then never reads or answers.
func TestRunTap_FakeSocketThatHangs(t *testing.T) {
	dir, err := os.MkdirTemp("", "tap")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		var conns []net.Conn
		defer func() {
			for _, c := range conns {
				_ = c.Close()
			}
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
			select {
			case <-stop:
				return
			default:
			}
		}
	}()
	t.Cleanup(func() { close(stop); _ = ln.Close(); wg.Wait() })

	o := tapEnv(t, "")
	o.Socket = sock
	o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(statusJSON)), &bytes.Buffer{}, &bytes.Buffer{}
	start := time.Now()
	require.Zero(t, RunTap(context.Background(), o))
	require.Less(t, time.Since(start), 250*time.Millisecond)
}

// Only the documented fields reach the daemon, as one JSONL intent line.
func TestRunTap_SendsOnlyTheUsageFields(t *testing.T) {
	dir, err := os.MkdirTemp("", "tap")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	lines := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 4096)
		n, _ := c.Read(buf)
		lines <- string(buf[:n])
	}()

	o := tapEnv(t, "")
	o.Socket = sock
	o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(statusJSON)), &bytes.Buffer{}, &bytes.Buffer{}
	require.Zero(t, RunTap(context.Background(), o))

	select {
	case line := <-lines:
		require.Contains(t, line, `"type":"UsageSample"`)
		require.Contains(t, line, `"session_id":"sess-1"`)
		require.Contains(t, line, `"claude_session_id":"c1"`)
		require.Contains(t, line, `"cost_usd":2.5`)
		require.Contains(t, line, `"model_id":"m1"`)
		require.NotContains(t, line, "/secret/t.jsonl")
		require.NotContains(t, line, "/work")
		require.NotContains(t, line, `"id":`, "no envelope ID: fire and forget")
	case <-time.After(2 * time.Second):
		t.Fatal("daemon never got the sample")
	}
}

func TestRunTap_NothingSentForBadSessionOrEmptySample(t *testing.T) {
	for name, tc := range map[string]struct{ session, in string }{
		"bad session":  {"a b;rm", statusJSON},
		"no session":   {"", statusJSON},
		"empty sample": {"ok", `{"cwd":"/x"}`},
		"not json":     {"ok", `plain text`},
	} {
		t.Run(name, func(t *testing.T) {
			o := tapEnv(t, "")
			o.Session = tc.session
			o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(tc.in)), &bytes.Buffer{}, &bytes.Buffer{}
			o.Send = func(context.Context, protocol.UsageSample) error { t.Fatal("sent"); return nil }
			require.Zero(t, RunTap(context.Background(), o))
		})
	}
}

func TestRunTap_UserCommandThatCannotRunExits127OrItsOwnCode(t *testing.T) {
	skipWindows(t)
	o := tapEnv(t, "definitely-not-a-command-xyz")
	o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(statusJSON)), &bytes.Buffer{}, &bytes.Buffer{}
	o.Send = func(context.Context, protocol.UsageSample) error { return nil }
	require.Equal(t, 127, RunTap(context.Background(), o), "sh reports command not found as 127")
}

func TestRunTap_FindsUserCommandThroughStatusLineCwd(t *testing.T) {
	skipWindows(t)
	home, _ := fakeHome(t, false)
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "settings.local.json"), `{"statusLine":{"type":"command","command":"cat >/dev/null; printf proj"}}`)
	var out bytes.Buffer
	in := `{"workspace":{"current_dir":"` + proj + `"},"cost":{"total_cost_usd":1}}`
	o := TapOptions{Session: "s", Home: home, Stdin: bytes.NewReader([]byte(in)), Stdout: &out, Stderr: &bytes.Buffer{},
		Send: func(context.Context, protocol.UsageSample) error { return nil }}
	require.Zero(t, RunTap(context.Background(), o))
	require.Equal(t, "proj", out.String())
}
