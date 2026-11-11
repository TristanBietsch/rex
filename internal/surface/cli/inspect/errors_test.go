package inspect

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
)

func sampleErrorRecords() []core.ErrLogRecord {
	return []core.ErrLogRecord{
		{V: 1, Time: "2026-11-11T09:12:03.123Z", Cmd: "rm", Args: []string{"rm", "<sel:8 bytes>"}, Exit: 6, Kind: "refused",
			Error: "rm: stdin is not a terminal; pass -force to delete without asking", Hint: "rex rm <sel> -force"},
		{V: 1, Time: "2026-11-10T22:00:00.000Z", Cmd: "ls", Args: []string{"ls", "-o", "json"}, Exit: 4, Kind: "daemon_unreachable",
			Error: "rex-daemon is not running: dial unix /x/rex.sock: connect: no such file or directory", Hint: "rex daemon start"},
	}
}

func TestErrorsViewGolden(t *testing.T) {
	savedLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = savedLocal })
	recs := sampleErrorRecords()
	secs := goldenSections(t, func(p *core.Printer) core.Result {
		return core.Result{Cmd: "errors", Data: recs, View: errorsView(recs)}
	})
	for _, f := range []string{"text", "txt", "agent", "json"} {
		secs = append(secs, golden.Section{Name: "empty." + f, Body: emitView(t, f, 80, false, func(p *core.Printer) core.Result {
			return core.Result{Cmd: "errors", Data: nonNilRecords(nil), View: errorsView(nil)}
		})})
	}
	golden.Check(t, "testdata/golden/errors.txtar", secs...)
}

func TestFilterErrorsAndSince(t *testing.T) {
	recs := sampleErrorRecords()
	require.Len(t, filterErrors(recs, time.Time{}, 0), 2)
	require.Len(t, filterErrors(recs, time.Time{}, 1), 1)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	cut, err := parseSinceCutoff("24h", now)
	require.NoError(t, err)
	require.Len(t, filterErrors(recs, cut, 0), 2)
	cut, err = parseSinceCutoff("3h", now)
	require.NoError(t, err)
	require.Len(t, filterErrors(recs, cut, 0), 1)
	cut, err = parseSinceCutoff("2d", now)
	require.NoError(t, err)
	require.Equal(t, now.Add(-48*time.Hour), cut)
	for _, bad := range []string{"soon", "-5h", "0h", "xd"} {
		_, err = parseSinceCutoff(bad, now)
		require.Error(t, err, bad)
	}
	cut, err = parseSinceCutoff("", now)
	require.NoError(t, err)
	require.True(t, cut.IsZero())
}

// rex errors against a real log: the records written by RecordError come
// back newest first, -n and -since narrow them, and -clear needs -force
// without a terminal.
func TestRunErrors_ReadsTheLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REX_LOG_DIR", dir)
	opts := core.ErrLogOptions{Enabled: func() bool { return true }}
	opts.Now = func() time.Time { return time.Now().Add(-48 * time.Hour) }
	core.RecordError([]string{"ls"}, core.NewExitError(core.ExitDaemonUnreachable, "old failure"), opts)
	opts.Now = time.Now
	core.RecordError([]string{"rm", "7d4f"}, core.NewExitError(core.ExitOperationRefused, "new failure"), opts)

	out, err := runIn(t, "agent", func() error { return RunErrors(nil) })
	require.NoError(t, err)
	require.Regexp(t, `(?s)^error_entry time=\S+ exit=6 kind=refused cmd=rm args="rm 7d4f" msg="new failure" hint="".*error_entry .* exit=4 kind=daemon_unreachable cmd=ls`, out)
	require.Contains(t, out, "ok cmd=errors count=2\n")

	out, err = runIn(t, "agent", func() error { return RunErrors([]string{"-n", "1"}) })
	require.NoError(t, err)
	require.Contains(t, out, "ok cmd=errors count=1\n")
	out, err = runIn(t, "agent", func() error { return RunErrors([]string{"-since", "24h"}) })
	require.NoError(t, err)
	require.Contains(t, out, "ok cmd=errors count=1\n")
	out, err = runIn(t, "json", func() error { return RunErrors(nil) })
	require.NoError(t, err)
	require.Contains(t, out, `"data":[{"v":1,`)

	// -clear without a terminal and without -force: refused, nothing deleted.
	_, err = runIn(t, "txt", func() error { return RunErrors([]string{"-clear"}) })
	var ec core.ExitCoder
	require.True(t, errors.As(err, &ec))
	require.Equal(t, core.ExitOperationRefused, ec.ExitCode())
	require.FileExists(t, filepath.Join(dir, "cli-errors.log"))

	out, err = runIn(t, "agent", func() error { return RunErrors([]string{"-clear", "-force"}) })
	require.NoError(t, err)
	require.Equal(t, "ok cmd=errors cleared=true\n", out)
	require.NoFileExists(t, filepath.Join(dir, "cli-errors.log"))
	out, err = runIn(t, "agent", func() error { return RunErrors(nil) })
	require.NoError(t, err)
	require.Equal(t, "ok cmd=errors count=0\n", out)
}

func TestRunErrors_BadArgs(t *testing.T) {
	t.Setenv("REX_LOG_DIR", t.TempDir())
	for _, args := range [][]string{{"extra"}, {"-n", "-1"}, {"-since", "soon"}, {"-bogus"}, {"-o", "yaml"}} {
		var ec core.ExitCoder
		require.True(t, errors.As(RunErrors(args), &ec), "%v", args)
		require.Equal(t, core.ExitInvalidArgs, ec.ExitCode(), "%v", args)
	}
}
