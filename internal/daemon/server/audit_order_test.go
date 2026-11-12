package server

import (
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
}

func (f *fakeAudit) Begin(sess *state.Session, prompt string) { f.prompts[sess.ID] = prompt }
func (f *fakeAudit) End(sess *state.Session)                  { f.log.add("audit:" + sess.ID) }

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
