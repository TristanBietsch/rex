package server

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// auditLog records the order of audit and removal calls.
type auditLog struct {
	mu     sync.Mutex
	events []string
}

func (l *auditLog) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *auditLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

type fakeAudit struct {
	log     *auditLog
	prompts map[string]string
	err     error
	// deletes holds one entry per Deleted call: the session, who asked, and
	// the calls logged so far (to tell the order).
	deletes []deleteCall
}

type deleteCall struct {
	id     string
	by     *state.Requester
	before []string
}

func (f *fakeAudit) Deleted(sess *state.Session, by *state.Requester) {
	f.deletes = append(f.deletes, deleteCall{id: sess.ID, by: by, before: f.log.get()})
}

func (f *fakeAudit) Begin(sess *state.Session, prompt string) { f.prompts[sess.ID] = prompt }
func (f *fakeAudit) End(sess *state.Session) error            { f.log.add("audit:" + sess.ID); return f.err }

func newAuditServer(t *testing.T, now time.Time) (*Server, *auditLog) {
	t.Helper()
	log := &auditLog{}
	srv, err := New(Config{
		Socket:   filepath.Join(t.TempDir(), "rex.sock"),
		StateDir: t.TempDir(),
		Store:    state.NewStore(),
		Now:      func() time.Time { return now },
		Audit:    &fakeAudit{log: log, prompts: map[string]string{}},
	})
	require.NoError(t, err)
	srv.removeSession = func(_ string, s *state.Session) error {
		log.add("remove:" + s.ID)
		return nil
	}
	return srv, log
}

func addEnded(t *testing.T, srv *Server, id string, st protocol.State) *state.Session {
	t.Helper()
	sess := &state.Session{ID: id, ShortID: id, Slug: "s-" + id, State: st, StartedAt: time.Now().UTC()}
	require.NoError(t, srv.cfg.Store.Add(sess))
	return sess
}

func TestDeleteSession_AuditRecordIsWrittenBeforeTheDirIsRemoved(t *testing.T) {
	srv, log := newAuditServer(t, time.Now())
	addEnded(t, srv, "aaaa1111", protocol.StateDone)

	require.NoError(t, srv.deleteSession("aaaa1111", protocol.RemovedByUser))
	require.Equal(t, []string{"audit:aaaa1111", "remove:aaaa1111"}, log.get(), "prune/delete: record first")

	require.ErrorIs(t, srv.deleteSession("aaaa1111", protocol.RemovedByUser), errSessionNotFound)
	require.Len(t, log.get(), 2, "a missing session writes nothing")
}

func TestSweepRetired_ReapWritesTheRecordBeforeRemoval(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	srv, log := newAuditServer(t, now)
	addEnded(t, srv, "aaaa1111", protocol.StateDone)
	addEnded(t, srv, "bbbb2222", protocol.StateFailed)
	for _, id := range []string{"aaaa1111", "bbbb2222"} {
		_, err := srv.cfg.Store.SetRetire(id, now.Add(-time.Second), now.Add(-time.Hour))
		require.NoError(t, err)
	}

	srv.sweepRetired()

	ev := log.get()
	require.Len(t, ev, 4)
	for _, id := range []string{"aaaa1111", "bbbb2222"} {
		require.Less(t, indexOf(ev, "audit:"+id), indexOf(ev, "remove:"+id), "reap: record first for %s", id)
	}
}

func TestDeleteSession_NoAuditHookStillDeletes(t *testing.T) {
	srv, log := newAuditServer(t, time.Now())
	srv.cfg.Audit = nil
	addEnded(t, srv, "aaaa1111", protocol.StateDone)
	require.NoError(t, srv.deleteSession("aaaa1111", protocol.RemovedByUser))
	require.Equal(t, []string{"remove:aaaa1111"}, log.get())
}

func TestDeleteSession_AuditFailureKeepsTheSessionUntilTheNextSweep(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	srv, log := newAuditServer(t, now)
	srv.removeSession = state.RemoveSession // the real thing: the dir must stay or go
	fa := srv.cfg.Audit.(*fakeAudit)
	fa.err = errors.New("audit dir not writable")

	sess := addEnded(t, srv, "aaaa1111", protocol.StateDone)
	require.NoError(t, state.WriteMeta(srv.cfg.StateDir, sess))
	_, err := srv.cfg.Store.SetRetire("aaaa1111", now.Add(-time.Second), now.Add(-time.Hour))
	require.NoError(t, err)
	dir, err := state.SessionDir(srv.cfg.StateDir, "aaaa1111")
	require.NoError(t, err)

	srv.sweepRetired()
	require.DirExists(t, dir, "no record, no deletion")
	_, ok := srv.cfg.Store.Get("aaaa1111")
	require.True(t, ok, "the store entry stays")
	require.ErrorIs(t, srv.deleteSession("aaaa1111", protocol.RemovedByUser), errAuditFailed)

	fa.err = nil // writable again: the next sweep clears it
	srv.sweepRetired()
	require.NoDirExists(t, dir)
	_, ok = srv.cfg.Store.Get("aaaa1111")
	require.False(t, ok)
	require.Contains(t, log.get(), "audit:aaaa1111")
}

// deleteSession reads the slug under the session lock: the retire sweep
// runs it on its own goroutine while a Rename can be writing the slug.
// Caught by -race.
func TestDeleteSession_SlugReadDoesNotRaceRename(t *testing.T) {
	srv, _ := newAuditServer(t, time.Now())
	addEnded(t, srv, "bbbb2222", protocol.StateDone)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.cfg.Store.UpdateRename("bbbb2222", "renamed", "")
	}()
	_ = srv.deleteSession("bbbb2222", protocol.RemovedByUser)
	<-done
}

func TestDeleteSession_AppendsDeleteLine(t *testing.T) {
	srv, log := newAuditServer(t, time.Now())
	fa := srv.cfg.Audit.(*fakeAudit)
	addEnded(t, srv, "aaaa1111", protocol.StateDone)

	by := &state.Requester{Client: "rex-cli", PID: 4242, Exe: "/usr/bin/rex"}
	require.NoError(t, srv.deleteSessionBy("aaaa1111", protocol.RemovedByUser, by))
	require.Len(t, fa.deletes, 1)
	require.Equal(t, by, fa.deletes[0].by)
	require.Equal(t, []string{"audit:aaaa1111"}, fa.deletes[0].before, "after the run record, before the removal")
	require.Equal(t, []string{"audit:aaaa1111", "remove:aaaa1111"}, log.get())

	require.ErrorIs(t, srv.deleteSessionBy("aaaa1111", protocol.RemovedByUser, by), errSessionNotFound)
	require.Len(t, fa.deletes, 1, "a missing session writes no delete line")
}

func TestDeleteSession_FailedRecordWritesNoDeleteLine(t *testing.T) {
	srv, _ := newAuditServer(t, time.Now())
	fa := srv.cfg.Audit.(*fakeAudit)
	fa.err = errors.New("audit dir not writable")
	addEnded(t, srv, "aaaa1111", protocol.StateDone)
	require.ErrorIs(t, srv.deleteSession("aaaa1111", protocol.RemovedByUser), errAuditFailed)
	require.Empty(t, fa.deletes, "the session is kept, so it is not deleted yet")
}
