package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testenv"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestMain(m *testing.M) { testenv.Run(m) }

var t0 = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func rec(id string, ended time.Time, mut ...func(*Record)) Record {
	r := Record{
		TS: ended, SessionID: id, ShortID: id[:4], Name: "fix-" + id[:4], Tool: "claude", Model: "opus",
		State: "done", StartedAt: ended.Add(-time.Hour), EndedAt: ended, DurationS: 3600,
	}
	for _, m := range mut {
		m(&r)
	}
	return r
}

func TestAppend_PrivateFileAndDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "audit.jsonl")
	require.NoError(t, Append(path, rec("aaaa1111", t0)))
	require.NoError(t, Append(path, rec("bbbb2222", t0.Add(time.Minute))))

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
		fi, err = os.Stat(filepath.Dir(path))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
	}
	got, errs := Read([]string{path}, Filter{})
	require.Empty(t, errs)
	require.Len(t, got, 2)
}

func TestAppend_TightensWideExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modes")
	}
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0o644)) //nolint:gosec // the point: a too-wide file must be tightened
	require.NoError(t, Append(path, rec("aaaa1111", t0)))
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestAppend_RefusesSymlinkAndDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	require.NoError(t, os.WriteFile(victim, []byte("keep\n"), 0o600))
	link := filepath.Join(dir, "audit.jsonl")
	require.NoError(t, os.Symlink(victim, link))

	err := Append(link, rec("aaaa1111", t0))
	require.ErrorIs(t, err, ErrNotRegular)
	b, _ := os.ReadFile(victim)
	require.Equal(t, "keep\n", string(b), "nothing written through the link")

	require.ErrorIs(t, Append(dir, rec("aaaa1111", t0)), ErrNotRegular)

	_, errs := Read([]string{link}, Filter{})
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], ErrNotRegular)
}

func TestAppend_RejectsIncompleteRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	require.Error(t, Append(path, Record{}))
	require.Error(t, Append(path, Record{SessionID: "x", State: "done"}))
	require.NoFileExists(t, path)
}

func TestAppend_StartsNewLineAfterTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	require.NoError(t, Append(path, rec("aaaa1111", t0)))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"session_id":"torn","ended_`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, Append(path, rec("bbbb2222", t0.Add(time.Minute))))

	got, errs := Read([]string{path}, Filter{})
	require.Empty(t, errs)
	require.Len(t, got, 2, "the torn line is skipped, the new record is whole")
	require.Equal(t, "bbbb2222", got[1].SessionID)
}

func TestAppend_ConcurrentRecordsStayWholeLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("s%03d", i) + "xx"
			require.NoError(t, Append(path, rec(id, t0.Add(time.Duration(i)*time.Second))))
		}()
	}
	wg.Wait()
	got, _ := Read([]string{path}, Filter{})
	require.Len(t, got, 30)
}

func TestRead_TornLinesAndGarbageSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	good := mustLine(t, rec("aaaa1111", t0))
	content := "not json\n" + "\n" + `{"session_id":"x"}` + "\n" + good + `{"session_id":"y","ended_at":"2026-`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	got, errs := Read([]string{path}, Filter{})
	require.Empty(t, errs)
	require.Len(t, got, 1)
	require.Equal(t, "aaaa1111", got[0].SessionID)
}

func TestRead_DedupesBySessionAndEndedAtAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.jsonl"), filepath.Join(dir, "b.jsonl")
	r1 := rec("aaaa1111", t0)
	r2 := rec("aaaa1111", t0.Add(time.Hour)) // a resumed run: new ended_at, kept
	require.NoError(t, Append(a, r1))
	require.NoError(t, Append(a, r1)) // a synced duplicate
	require.NoError(t, Append(b, r1)) // the other machine's copy
	require.NoError(t, Append(b, r2))
	got, errs := Read([]string{a, b}, Filter{})
	require.Empty(t, errs)
	require.Len(t, got, 2)
	require.True(t, got[0].EndedAt.Before(got[1].EndedAt), "oldest first")
}

func TestRead_MissingFileIsEmptyAndOthersStillRead(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jsonl")
	require.NoError(t, Append(a, rec("aaaa1111", t0)))
	got, errs := Read([]string{filepath.Join(dir, "missing.jsonl"), a}, Filter{})
	require.Empty(t, errs)
	require.Len(t, got, 1)
}

func TestRead_OversizedLineSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	huge := `{"session_id":"huge","state":"done","ended_at":"2026-11-10T12:00:00Z","description":"` + strings.Repeat("x", maxLineBytes+10) + `"}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(huge+mustLine(t, rec("aaaa1111", t0))), 0o600))
	got, errs := Read([]string{path}, Filter{})
	require.Empty(t, errs)
	require.Len(t, got, 1)
	require.Equal(t, "aaaa1111", got[0].SessionID)
}

func TestRead_LongButAllowedLineParses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	r := rec("aaaa1111", t0, func(r *Record) { r.Prompt = strings.Repeat("p", 200<<10) })
	require.NoError(t, Append(path, r))
	got, _ := Read([]string{path}, Filter{})
	require.Len(t, got, 1)
	require.Len(t, got[0].Prompt, 200<<10)
}

func TestRead_Filters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	rs := []Record{
		rec("aaaa1111", t0.Add(-10*24*time.Hour)),
		rec("bbbb2222", t0.Add(-time.Hour), func(r *Record) { r.Tool = "codex"; r.State = "failed" }),
		rec("cccc3333", t0, func(r *Record) { r.WorkspaceID = "7f3b89a2" }),
	}
	for _, r := range rs {
		require.NoError(t, Append(path, r))
	}
	ids := func(f Filter) []string {
		got, _ := Read([]string{path}, f)
		var out []string
		for _, r := range got {
			out = append(out, r.SessionID)
		}
		return out
	}
	require.Equal(t, []string{"aaaa1111", "bbbb2222", "cccc3333"}, ids(Filter{}))
	require.Equal(t, []string{"bbbb2222", "cccc3333"}, ids(Filter{Since: t0.Add(-24 * time.Hour)}))
	require.Equal(t, []string{"bbbb2222"}, ids(Filter{Tool: "codex"}))
	require.Equal(t, []string{"bbbb2222"}, ids(Filter{State: "failed"}))
	require.Equal(t, []string{"cccc3333"}, ids(Filter{Workspace: "7f3b89a2"}))
	require.Empty(t, ids(Filter{Tool: "codex", State: "done"}))
}

func TestFromSummary(t *testing.T) {
	sum := protocol.SessionSummary{
		ID: "aaaa1111-0000-4000-8000-000000000000", ShortID: "aaaa", ToolID: "claude", ModelID: "opus",
		Slug: "fix-auth", State: protocol.StateDone, StartedAt: t0.Add(-90 * time.Second), EndedAt: t0,
		ResumeID: "r1", Description: "did the \x1b[31mthing",
	}
	long := strings.Repeat("é", 300)
	r := FromSummary(sum, "7f3b89a2", "\x1b[2Jfirst line\nsecond line", "first line\nsecond line", false, t0.Add(time.Second))
	require.Equal(t, "fix-auth", r.Name, "slug when no name")
	require.Equal(t, "first line", r.PromptFirstLine)
	require.Empty(t, r.Prompt, "full prompt only when opted in")
	require.Equal(t, int64(90), r.DurationS)
	require.Equal(t, t0, r.EndedAt)
	require.Equal(t, "did the thing", r.Description)
	require.Equal(t, "7f3b89a2", r.WorkspaceID)

	r = FromSummary(sum, "", long, "a\nb", true, t0)
	require.Len(t, []rune(r.PromptFirstLine), MaxFirstLineRunes)
	require.Equal(t, "a\nb", r.Prompt)

	// No end time: now stands in, and a negative span is zero.
	sum.EndedAt = time.Time{}
	sum.Name = "Named"
	r = FromSummary(sum, "", "", "", true, t0)
	require.Equal(t, t0, r.EndedAt)
	require.Equal(t, "Named", r.Name)
	require.Empty(t, r.Prompt)
	sum.StartedAt = t0.Add(time.Hour)
	require.Zero(t, FromSummary(sum, "", "", "", false, t0).DurationS)
}

func TestTrimPrompt(t *testing.T) {
	require.Equal(t, "ok", TrimPrompt("ok"))
	require.Len(t, []rune(TrimPrompt(strings.Repeat("é", MaxPromptRunes+5))), MaxPromptRunes)
	require.True(t, strings.Contains(TrimPrompt("a\xffb"), "a"))
}

func TestParseSince(t *testing.T) {
	now := t0
	for in, want := range map[string]time.Time{
		"":           {},
		"7d":         now.AddDate(0, 0, -7),
		"0d":         now,
		"12h":        now.Add(-12 * time.Hour),
		"90m":        now.Add(-90 * time.Minute),
		"2026-11-01": time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	} {
		got, err := ParseSince(in, now)
		require.NoError(t, err, in)
		require.True(t, got.Equal(want), "%q: %v != %v", in, got, want)
	}
	got, err := ParseSince("2026-11-01T10:00:00Z", now)
	require.NoError(t, err)
	require.Equal(t, 10, got.Hour())
	for _, bad := range []string{"x", "7x", "-3d", "d", "1.5d", "-1h"} {
		_, err := ParseSince(bad, now)
		require.Errorf(t, err, "%q", bad)
	}
}

func mustLine(t *testing.T, r Record) string {
	t.Helper()
	b, err := marshalLine(r)
	require.NoError(t, err)
	return string(b)
}

func TestFromSummary_LiveSessionAtTeardownIsRecordedAsFailed(t *testing.T) {
	for _, st := range []protocol.State{protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput} {
		sum := protocol.SessionSummary{ID: "aaaa1111-0", ShortID: "aaaa", ToolID: "claude", State: st, StartedAt: t0.Add(-time.Minute)}
		r := FromSummary(sum, "", "", "", false, t0)
		require.Equal(t, "failed", r.State, st)
		require.Equal(t, DeletedWhileRunning, r.Description)
		require.Equal(t, t0, r.EndedAt)
	}
	sum := protocol.SessionSummary{ID: "aaaa1111-0", ToolID: "claude", State: protocol.StateWorking, Description: "fixing it", StartedAt: t0}
	require.Equal(t, "fixing it (deleted while running)", FromSummary(sum, "", "", "", false, t0).Description)
	sum.State = protocol.StateCrashed
	r := FromSummary(sum, "", "", "", false, t0)
	require.Equal(t, "crashed", r.State)
	require.Equal(t, "fixing it", r.Description)
}
