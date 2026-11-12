package core

import (
	"bytes"
	"encoding/json"
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
	require.Equal(t, "ls", recs[0].Cmd)

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
	t.Setenv("USERPROFILE", home)
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
		b, _ := json.Marshal(ErrLogRecord{V: 1, Cmd: msg})
		return string(b) + "\n"
	}
	require.NoError(t, os.WriteFile(path+".1", []byte(mk("old1")+mk("old2")), 0o600))
	require.NoError(t, os.WriteFile(path, []byte(mk("new1")+"not json\n"+mk("new2")), 0o600))
	_ = opts
	var got []string
	for _, r := range readRecords(t, path) {
		got = append(got, r.Cmd)
	}
	require.Equal(t, []string{"new2", "new1", "old2", "old1"}, got)
}
