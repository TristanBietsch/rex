package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

var errLogNow = func() time.Time { return time.Date(2026, 11, 11, 9, 12, 3, 123_000_000, time.UTC) }

func errLogOpts(t *testing.T) (ErrLogOptions, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "cli-errors.log")
	return ErrLogOptions{Path: path, Now: errLogNow, Enabled: func() bool { return true }}, path
}

func readRecords(t *testing.T, path string) []ErrLogRecord {
	t.Helper()
	recs, err := ReadErrLog(path)
	require.NoError(t, err)
	return recs
}

func TestErrLog_RecordsAndRedacts(t *testing.T) {
	opts, path := errLogOpts(t)
	SetOut(nil)
	args := []string{"new", "-tool", "claude", "-cwd", "/home/alice/secret-project", "--", "my secret prompt"}
	err := Errorf(ExitGeneric, "rex ls", "spawn failed: token=sk-abcdef1234567890: %w", errors.New("boom"))
	RecordError(args, err, opts)

	raw, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	for _, leak := range []string{"secret prompt", "alice", "secret-project", "sk-abcdef1234567890"} {
		require.NotContains(t, string(raw), leak, "the log must not hold %q", leak)
	}
	recs := readRecords(t, path)
	require.Len(t, recs, 1)
	r := recs[0]
	require.Equal(t, 1, r.V)
	require.Equal(t, "2026-11-11T09:12:03.123Z", r.Time)
	require.Equal(t, "new", r.Cmd)
	require.Equal(t, []string{"new", "-tool", "claude", "-cwd", "<redacted:26 bytes>", "--", "<redacted:16 bytes>"}, r.Args)
	require.Equal(t, ExitGeneric, r.Exit)
	require.Equal(t, "error", r.Kind)
	require.Contains(t, r.Error, "<redacted>")
	require.Equal(t, []string{"boom"}, r.Chain)
	require.Equal(t, "rex ls", r.Hint)
	require.Equal(t, Version, r.Version)
	require.NotEmpty(t, r.OS)
	require.Equal(t, os.Getpid(), r.PID)
}

func TestErrLog_GlobalFlagsBeforeTheVerb(t *testing.T) {
	opts, path := errLogOpts(t)
	RecordError([]string{"-o", "json", "rm", "fix-auth", "-force"}, NewExitError(ExitOperationRefused, "rm: nope"), opts)
	r := readRecords(t, path)[0]
	require.Equal(t, "rm", r.Cmd)
	require.Equal(t, []string{"-o", "json", "rm", "<sel:8 bytes>", "-force"}, r.Args)
	require.Equal(t, "refused", r.Kind)
}

func TestErrLog_Mode0600(t *testing.T) {
	opts, path := errLogOpts(t)
	RecordError([]string{"ls"}, NewExitError(ExitDaemonUnreachable, "no daemon"), opts)
	testperm.RequireMode(t, path, 0o600)
	testperm.RequireMode(t, filepath.Dir(path), 0o700)
	// A looser file from elsewhere is tightened.
	testperm.Loosen(t, path, 0o644)
	RecordError([]string{"ls"}, NewExitError(ExitDaemonUnreachable, "no daemon"), opts)
	testperm.RequireMode(t, path, 0o600)
}

func TestErrLog_RotatesAt1MiB(t *testing.T) {
	opts, path := errLogOpts(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("x"), errLogMaxBytes), 0o600))
	RecordError([]string{"ls"}, NewExitError(ExitGeneric, "first after rotation"), opts)

	old, err := os.ReadFile(path + ".1")
	require.NoError(t, err)
	require.Len(t, old, errLogMaxBytes, "the full log became the backup")
	recs := readRecords(t, path)
	require.Len(t, recs, 1, "garbage lines in the backup are skipped")
	require.Contains(t, recs[0].Error, "first after rotation")

	// Rotating again replaces the backup: one backup only.
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("y"), errLogMaxBytes), 0o600))
	RecordError([]string{"ls"}, NewExitError(ExitGeneric, "second"), opts)
	old, err = os.ReadFile(path + ".1")
	require.NoError(t, err)
	require.Equal(t, byte('y'), old[0])
}

func TestErrLog_OptOut(t *testing.T) {
	opts, path := errLogOpts(t)
	opts.Enabled = func() bool { return false }
	RecordError([]string{"ls"}, NewExitError(ExitGeneric, "x"), opts)
	require.NoFileExists(t, path)

	// The real checks: REX_ERROR_LOG=0, then the cli_error_log setting.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REX_LOG_DIR", filepath.Dir(path))
	require.True(t, errLogEnabled())
	t.Setenv("REX_ERROR_LOG", "0")
	require.False(t, errLogEnabled())
	t.Setenv("REX_ERROR_LOG", "")
	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("cli_error_log: false\n"), 0o600))
	require.False(t, errLogEnabled())
	require.NoError(t, os.WriteFile(cfg, []byte("cli_error_log: true\n"), 0o600))
	require.True(t, errLogEnabled())
	require.NoError(t, os.WriteFile(cfg, []byte("cli_error_log: [unparseable\n"), 0o600))
	require.True(t, errLogEnabled(), "a settings file that will not load means on")
}

func TestErrLog_SkipsHelpAndSignals(t *testing.T) {
	opts, path := errLogOpts(t)
	RecordError([]string{"ls", "-h"}, ErrHelpShown, opts)
	RecordError([]string{"status"}, NewExitError(ExitGeneric, ""), opts) // needs input
	RecordError([]string{"doctor"}, nil, opts)
	require.NoFileExists(t, path)
}

func TestErrLog_WriteFailureIsSilent(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	opts := ErrLogOptions{Path: filepath.Join(blocker, "sub", "cli-errors.log"), Now: errLogNow, Enabled: func() bool { return true }}
	var errw bytes.Buffer
	p, err := NewPrinter(FormatFlags{}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, io.Discard, &errw)
	require.NoError(t, err)
	SetOut(p)
	t.Cleanup(func() { SetOut(nil) })
	require.NotPanics(t, func() { RecordError([]string{"ls"}, NewExitError(ExitGeneric, "x"), opts) })
	require.Empty(t, errw.String(), "nothing is printed unless REX_LOG_LEVEL=debug")

	t.Setenv("REX_LOG_LEVEL", "debug")
	RecordError([]string{"ls"}, NewExitError(ExitGeneric, "x"), opts)
	require.Contains(t, errw.String(), "rex: error log:")
}

func TestErrLog_ConcurrentAppend(t *testing.T) {
	opts, path := errLogOpts(t)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RecordError([]string{"ls"}, NewExitError(ExitGeneric, fmt.Sprintf("failure %d", i)), opts)
		}()
	}
	wg.Wait()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, lines, 16)
	for _, l := range lines {
		var rec ErrLogRecord
		require.NoError(t, json.Unmarshal([]byte(l), &rec), l)
	}
}

func TestErrLog_RecordIsCappedAt4KiB(t *testing.T) {
	opts, path := errLogOpts(t)
	long := strings.Repeat("é", 5000)
	err := fmt.Errorf("%s: %w", long, fmt.Errorf("%s: %w", long, errors.New(long)))
	args := make([]string, 0, 200)
	for range 200 {
		args = append(args, "-tool", "claude")
	}
	RecordError(append([]string{"ls"}, args...), ExitError{Code: ExitGeneric, Msg: long, Err: err}, opts)
	raw, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	require.LessOrEqual(t, len(raw), errLogMaxRecord+1)
	require.Len(t, readRecords(t, path), 1, "still valid JSON after shrinking")
}

func TestErrChainWalksWrappedAndJoinedErrors(t *testing.T) {
	a, b := errors.New("a"), errors.New("b")
	chain := errChain(fmt.Errorf("top: %w", errors.Join(a, fmt.Errorf("wrapped: %w", b))))
	require.Equal(t, []string{"a wrapped: b", "a", "wrapped: b", "b"}, chain)

	deep := errors.New("end")
	for i := range 20 {
		deep = fmt.Errorf("level %d: %w", i, deep)
	}
	require.Len(t, errChain(deep), errLogMaxChain)
	require.Nil(t, errChain(errors.New("plain")))
}

func TestScrub(t *testing.T) {
	for in, want := range map[string]string{
		"key sk-abcdefgh12345678 end":              "key <redacted> end",
		"ghp_abcdefgh12345678":                     "<redacted>",
		"github_pat_11ABCDEFG0123456789":           "<redacted>",
		"AKIAABCDEFGHIJKLMNOP":                     "<redacted>",
		"xoxb-12345678-abcdefgh":                   "<redacted>",
		"Authorization: Bearer abcdefgh.ijkl-mnop": "Authorization: <redacted>",
		"password=hunter2 next":                    "password=<redacted> next",
		"api_key=abc and API-KEY=def":              "api_key=<redacted> and API-KEY=<redacted>",
		"nothing secret here, sk- is not a key":    "nothing secret here, sk- is not a key",
		"short sk-abc stays":                       "short sk-abc stays",
	} {
		require.Equal(t, want, Scrub(in), in)
	}
}

func TestErrLog_ClearAndPath(t *testing.T) {
	opts, path := errLogOpts(t)
	RecordError([]string{"ls"}, NewExitError(ExitGeneric, "x"), opts)
	require.NoError(t, os.WriteFile(path+".1", []byte("{}\n"), 0o600))
	require.NoError(t, ClearErrLog(path))
	require.NoFileExists(t, path)
	require.NoFileExists(t, path+".1")
	require.NoError(t, ClearErrLog(path), "clearing nothing is fine")
	recs, err := ReadErrLog(path)
	require.NoError(t, err)
	require.Empty(t, recs)

	t.Setenv("REX_LOG_DIR", "/x/y")
	got, err := ErrLogPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/x/y", "cli-errors.log"), got)
}

func TestErrLog_ReadsNewestFirstAcrossBackup(t *testing.T) {
	opts, path := errLogOpts(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	mk := func(msg string) string {
		b, _ := json.Marshal(ErrLogRecord{V: 1, Cmd: "ls", Error: msg})
		return string(b) + "\n"
	}
	require.NoError(t, os.WriteFile(path+".1", []byte(mk("old1")+mk("old2")), 0o600))
	require.NoError(t, os.WriteFile(path, []byte(mk("new1")+"not json\n"+mk("new2")), 0o600))
	_ = opts
	var got []string
	for _, r := range readRecords(t, path) {
		got = append(got, r.Error)
	}
	require.Equal(t, []string{"new2", "new1", "old2", "old1"}, got)
}
