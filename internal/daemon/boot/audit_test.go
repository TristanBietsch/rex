package boot

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/audit"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
	"github.com/tristanbietsch/rex/internal/workspace"
)

var auditNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func auditSettings(t *testing.T, kv map[string]any) *settings.Store {
	t.Helper()
	s := settings.NewStore()
	for k, v := range kv {
		require.NoError(t, s.Set(k, v))
	}
	return s
}

func auditSession(id, cwd string) *state.Session {
	return &state.Session{ID: id, ShortID: id[:4], ToolID: "claude", ModelID: "opus", Slug: "s-" + id[:4],
		CWD: cwd, State: protocol.StateWorking, StartedAt: auditNow.Add(-time.Hour)}
}

type auditRig struct {
	dir   string
	store *state.Store
	a     *Audit
}

func newAuditRig(t *testing.T, kv map[string]any, home string) *auditRig {
	t.Helper()
	dir := t.TempDir()
	store := state.NewStore()
	a := NewAudit(AuditConfig{StateDir: dir, Home: home}, store)
	a.now = func() time.Time { return auditNow }
	a.Configure(auditSettings(t, kv))
	return &auditRig{dir: dir, store: store, a: a}
}

func (r *auditRig) global() string { return filepath.Join(r.dir, paths.AuditFileName) }

func (r *auditRig) records(t *testing.T, path string) []audit.Record {
	t.Helper()
	got, errs := audit.Read([]string{path}, audit.Filter{})
	require.Empty(t, errs)
	return got
}

func TestAudit_FirstTerminalTransitionWritesOnce(t *testing.T) {
	r := newAuditRig(t, nil, "")
	s := auditSession("aaaa1111", "/w")
	r.a.Begin(s, "fix the login bug\nand the logout bug")
	require.NoError(t, r.store.Add(s))

	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone)) // same run again
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateFailed))
	r.a.Close()

	got := r.records(t, r.global())
	require.Len(t, got, 1)
	require.Equal(t, "aaaa1111", got[0].SessionID)
	require.Equal(t, "fix the login bug", got[0].PromptFirstLine)
	require.Empty(t, got[0].Prompt, "full prompt only with audit_full_prompt")
	require.Equal(t, "done", got[0].State)
	require.Empty(t, got[0].WorkspaceID)
}

func TestAudit_ResumedSessionWritesANewRecordAtItsNextEnd(t *testing.T) {
	r := newAuditRig(t, nil, "")
	s := auditSession("aaaa1111", "/w")
	require.NoError(t, r.store.Add(s))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	r.a.Close() // drain
	require.Len(t, r.records(t, r.global()), 1)

	// Resume: live again, then a second end. A fresh writer stands in for
	// the daemon's still-running one (Close ended the first).
	r.a = NewAudit(AuditConfig{StateDir: r.dir}, r.store)
	r.a.Configure(auditSettings(t, nil))
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateWorking))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateFailed))
	r.a.Close()

	got := r.records(t, r.global())
	require.Len(t, got, 2)
	require.Equal(t, "done", got[0].State)
	require.Equal(t, "failed", got[1].State)
	require.True(t, got[1].EndedAt.After(got[0].EndedAt), "a new ended_at")
}

func TestAudit_FullPromptOnlyWhenTheFlagIsOn(t *testing.T) {
	prompt := "first line\nsecret second line"
	for _, on := range []bool{false, true} {
		r := newAuditRig(t, map[string]any{"audit_full_prompt": on}, "")
		s := auditSession("aaaa1111", "/w")
		r.a.Begin(s, prompt)
		require.NoError(t, r.store.Add(s))
		require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
		r.a.Close()
		got := r.records(t, r.global())
		require.Len(t, got, 1)
		require.Equal(t, "first line", got[0].PromptFirstLine)
		if on {
			require.Equal(t, prompt, got[0].Prompt)
		} else {
			require.Empty(t, got[0].Prompt)
			raw, err := os.ReadFile(r.global())
			require.NoError(t, err)
			require.NotContains(t, string(raw), "secret", "the second line never reaches disk")
		}
	}
}

func TestAudit_FlagTurnedOffAfterSpawnDoesNotLeakThePrompt(t *testing.T) {
	r := newAuditRig(t, map[string]any{"audit_full_prompt": true}, "")
	s := auditSession("aaaa1111", "/w")
	r.a.Begin(s, "first\nsecret")
	require.NoError(t, r.store.Add(s))
	r.a.Configure(auditSettings(t, map[string]any{"audit_full_prompt": false}))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	r.a.Close()
	got := r.records(t, r.global())
	require.Len(t, got, 1)
	require.Empty(t, got[0].Prompt)
}

func TestAudit_OffWritesNothingAndKeepsNoPrompt(t *testing.T) {
	r := newAuditRig(t, map[string]any{"audit_trail": false}, t.TempDir())
	finds := 0
	r.a.find = func(string, string) (*workspace.Workspace, error) { finds++; return nil, nil }
	s := auditSession("aaaa1111", "/w")
	r.a.Begin(s, "a prompt")
	first, full := s.AuditText()
	require.Empty(t, first)
	require.Empty(t, full)
	require.NoError(t, r.store.Add(s))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	r.a.End(s)
	r.a.Close()

	require.NoFileExists(t, r.global())
	require.Zero(t, finds, "no workspace lookup while off")
	entries, err := os.ReadDir(r.dir)
	require.NoError(t, err)
	require.Empty(t, entries)
	require.False(t, s.AuditIsWritten())
}

func TestAudit_WorkspaceSessionGoesToTheWorkspaceFile(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "dev", "proj")
	cwd := filepath.Join(root, "src")
	require.NoError(t, os.MkdirAll(cwd, 0o755))
	w, err := workspace.Init(root, "proj", nil)
	require.NoError(t, err)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	r := newAuditRig(t, map[string]any{"workspaces": true}, home)
	s := auditSession("aaaa1111", cwd)
	require.NoError(t, r.store.Add(s))
	require.NoError(t, r.store.Add(auditSession("bbbb2222", filepath.Join(home, "other"))))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	require.NoError(t, r.store.Transition("bbbb2222", protocol.StateDone))
	r.a.Close()

	wsFile := filepath.Join(root, ".rex", "audit.jsonl")
	got := r.records(t, wsFile)
	require.Len(t, got, 1)
	require.Equal(t, w.ID, got[0].WorkspaceID)
	require.Equal(t, "aaaa1111", got[0].SessionID)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(wsFile)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}
	glob := r.records(t, r.global())
	require.Len(t, glob, 1)
	require.Equal(t, "bbbb2222", glob[0].SessionID, "a session outside any workspace is global")
}

func TestAudit_WorkspacesFlagOffMeansNoDiscovery(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "proj")
	require.NoError(t, os.MkdirAll(root, 0o755))
	_, err := workspace.Init(root, "proj", nil)
	require.NoError(t, err)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	r := newAuditRig(t, nil, home) // workspaces defaults to off
	finds := 0
	r.a.find = func(string, string) (*workspace.Workspace, error) { finds++; return nil, nil }
	require.NoError(t, r.store.Add(auditSession("aaaa1111", root)))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	r.a.Close()

	require.Zero(t, finds)
	require.NoFileExists(t, filepath.Join(root, ".rex", "audit.jsonl"))
	require.Len(t, r.records(t, r.global()), 1)
}

func TestAudit_UnwritableWorkspaceFileFallsBackToGlobal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	home := t.TempDir()
	root := filepath.Join(home, "proj")
	require.NoError(t, os.MkdirAll(root, 0o755))
	_, err := workspace.Init(root, "proj", nil)
	require.NoError(t, err)
	// A symlinked audit.jsonl is refused, never followed.
	victim := filepath.Join(home, "victim")
	require.NoError(t, os.WriteFile(victim, []byte("keep\n"), 0o600))
	require.NoError(t, os.Symlink(victim, filepath.Join(root, ".rex", "audit.jsonl")))

	r := newAuditRig(t, map[string]any{"workspaces": true}, home)
	require.NoError(t, r.store.Add(auditSession("aaaa1111", root)))
	require.NoError(t, r.store.Add(auditSession("bbbb2222", root)))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	require.NoError(t, r.store.Transition("bbbb2222", protocol.StateDone))
	r.a.Close()

	b, _ := os.ReadFile(victim)
	require.Equal(t, "keep\n", string(b))
	got := r.records(t, r.global())
	require.Len(t, got, 2, "the records are not lost")
	w, err := workspace.Find(root, home)
	require.NoError(t, err)
	require.Equal(t, w.ID, got[0].WorkspaceID)
}

func TestAudit_WriteFailureIsRetriedAtTeardown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	r := newAuditRig(t, nil, "")
	victim := filepath.Join(r.dir, "victim")
	require.NoError(t, os.WriteFile(victim, nil, 0o600))
	require.NoError(t, os.Symlink(victim, r.global()))
	s := auditSession("aaaa1111", "/w")
	require.NoError(t, r.store.Add(s))
	require.NoError(t, r.store.Transition("aaaa1111", protocol.StateDone))
	r.a.Close()
	require.False(t, s.AuditIsWritten(), "a failed write does not count")

	require.NoError(t, os.Remove(r.global()))
	r.a.End(s) // the teardown path
	require.True(t, s.AuditIsWritten())
	require.Len(t, r.records(t, r.global()), 1)
}

func TestAudit_PersistCalledAfterAWrite(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore()
	var persisted []string
	a := NewAudit(AuditConfig{StateDir: dir, Persist: func(s *state.Session) { persisted = append(persisted, s.ID) }}, store)
	a.Configure(auditSettings(t, nil))
	s := auditSession("aaaa1111", "/w")
	require.NoError(t, store.Add(s))
	require.NoError(t, store.Transition("aaaa1111", protocol.StateDone))
	a.Close()
	a.End(s) // already written: nothing more
	require.Equal(t, []string{"aaaa1111"}, persisted)
}

func TestAudit_WarnOnceLogsEachKeyOnce(t *testing.T) {
	a := NewAudit(AuditConfig{StateDir: t.TempDir()}, state.NewStore())
	defer a.Close()
	require.NotPanics(t, func() {
		for range 3 {
			a.warnOnce("k", os.ErrPermission)
			a.warnOnce("k2", audit.ErrNotRegular)
		}
	})
	n := 0
	a.warned.Range(func(_, _ any) bool { n++; return true })
	require.Equal(t, 2, n)
	require.True(t, strings.Contains(a.global, paths.AuditFileName))
}
