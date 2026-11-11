package usage

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func rec(id string, tokens int64, at time.Time) Record {
	return Record{SessionID: id, Tool: "claude", Model: "opus", Tokens: tokens, Estimated: true, At: at}
}

func newLedger(t *testing.T) *Ledger {
	t.Helper()
	return OpenLedger(filepath.Join(t.TempDir(), "share", "usage.jsonl"))
}

// captureWarnings routes slog to a buffer for the test.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var mu sync.Mutex
	buf := &bytes.Buffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, w: buf}, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestLedger_AppendCreatesPrivateFile(t *testing.T) {
	l := newLedger(t)
	require.NoFileExists(t, l.Path(), "opening touches nothing")
	require.NoError(t, l.Append(rec("a", 10, t0)))
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(l.Path())
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
		di, err := os.Stat(filepath.Dir(l.Path()))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), di.Mode().Perm())
	}
}

func TestLedger_AppendTightensAWiderFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	l := newLedger(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(l.Path()), 0o700))
	require.NoError(t, os.WriteFile(l.Path(), nil, 0o644))
	require.NoError(t, os.Chmod(l.Path(), 0o644))
	require.NoError(t, l.Append(rec("a", 10, t0)))
	fi, err := os.Stat(l.Path())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

// Periodic lines of one session never add up: the latest one wins.
func TestLedger_LatestPerSessionNoDoubleCount(t *testing.T) {
	l := newLedger(t)
	for i, tok := range []int64{100, 400, 900} {
		require.NoError(t, l.Append(rec("a", tok, t0.Add(time.Duration(i)*10*time.Minute))))
	}
	require.NoError(t, l.Append(rec("b", 50, t0)))
	final := rec("a", 1000, t0.Add(time.Hour))
	final.Ended = true
	require.NoError(t, l.Append(final))

	got, err := l.Latest()
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.EqualValues(t, 1000, got["a"].Tokens)
	require.True(t, got["a"].Ended)
	var total int64
	for _, r := range got {
		total += r.Tokens
	}
	require.EqualValues(t, 1050, total)
}

func TestLedger_LatestByTimeNotByLinePosition(t *testing.T) {
	l := newLedger(t)
	require.NoError(t, l.Append(rec("a", 900, t0.Add(time.Hour))))
	require.NoError(t, l.Append(rec("a", 100, t0)))
	got, err := l.Latest()
	require.NoError(t, err)
	require.EqualValues(t, 900, got["a"].Tokens)
}

func TestLedger_CorruptLinesSkippedWithOneWarning(t *testing.T) {
	l := newLedger(t)
	require.NoError(t, l.Append(rec("a", 10, t0)))
	f, err := os.OpenFile(l.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("not json at all\n{\"session_id\":\"\",\"at\":\"2026-06-01T12:00:00Z\"}\n{\"v\":1,\"session_id\":\"torn\"")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, l.Append(rec("b", 20, t0)))

	buf := captureWarnings(t)
	got, err := l.Latest()
	require.NoError(t, err)
	require.Len(t, got, 2, "the readable lines still count")
	require.Equal(t, 1, strings.Count(buf.String(), "unreadable ledger lines"), buf.String())
	require.Contains(t, buf.String(), "lines=3")
}

func TestLedger_TrimDropsOldAndCorruptKeepsTheRest(t *testing.T) {
	l := newLedger(t)
	now := t0
	require.NoError(t, l.Append(rec("old", 1, now.Add(-100*24*time.Hour))))
	require.NoError(t, l.Append(rec("edge", 2, now.Add(-89*24*time.Hour))))
	require.NoError(t, l.Append(rec("new", 3, now.Add(-time.Hour))))
	f, err := os.OpenFile(l.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, _ = f.WriteString("garbage\n")
	require.NoError(t, f.Close())

	captureWarnings(t)
	require.NoError(t, l.Trim(now.Add(-Retain)))
	got, err := l.Latest()
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Contains(t, got, "edge")
	require.Contains(t, got, "new")
	b, err := os.ReadFile(l.Path())
	require.NoError(t, err)
	require.NotContains(t, string(b), "garbage")
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(l.Path())
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "the rewritten file is private too")
	}
	entries, err := os.ReadDir(filepath.Dir(l.Path()))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temp file left behind")
}

func TestLedger_TrimOfMissingOrCleanLedgerWritesNothing(t *testing.T) {
	l := newLedger(t)
	require.NoError(t, l.Trim(t0))
	require.NoFileExists(t, l.Path())

	require.NoError(t, l.Append(rec("a", 1, t0)))
	before, err := os.Stat(l.Path())
	require.NoError(t, err)
	require.NoError(t, l.Trim(t0.Add(-time.Hour)))
	after, err := os.Stat(l.Path())
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "nothing to drop: the file is not rewritten")
}

func TestLedger_RecordHoldsNoPromptOrPath(t *testing.T) {
	l := newLedger(t)
	require.NoError(t, l.Append(rec("a", 1, t0)))
	b, err := os.ReadFile(l.Path())
	require.NoError(t, err)
	var keys []string
	for _, k := range []string{"session_id", "tool", "model", "tokens", "estimated", "at", "v"} {
		require.Contains(t, string(b), `"`+k+`"`)
		keys = append(keys, k)
	}
	require.Len(t, keys, 7)
	require.NotContains(t, string(b), "cwd")
	require.NotContains(t, string(b), "prompt")
}

var _ = context.Background
