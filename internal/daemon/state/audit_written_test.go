package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestAuditOnce_WritesOnceAndRetriesAfterFailure(t *testing.T) {
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateDone}
	calls := 0
	boom := errors.New("disk full")

	wrote, err := sess.AuditOnce(func() error { calls++; return boom })
	require.ErrorIs(t, err, boom)
	require.False(t, wrote)
	require.False(t, sess.AuditIsWritten(), "a failed write does not count")

	wrote, err = sess.AuditOnce(func() error { calls++; return nil })
	require.NoError(t, err)
	require.True(t, wrote)
	require.True(t, sess.AuditIsWritten())

	wrote, err = sess.AuditOnce(func() error { calls++; return nil })
	require.NoError(t, err)
	require.False(t, wrote)
	require.Equal(t, 2, calls)
}

func TestAuditOnce_ConcurrentCallersWriteOnce(t *testing.T) {
	sess := &Session{ID: "id1", ShortID: "id1"}
	var mu sync.Mutex
	n := 0
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = sess.AuditOnce(func() error {
				time.Sleep(time.Millisecond)
				mu.Lock()
				n++
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()
	require.Equal(t, 1, n)
}

func TestAuditWritten_PersistedInMetaButNotOnTheWire(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateDone, StartedAt: time.Now().UTC()}
	sess.SetExit(protocol.StateDone, nil)
	_, err := sess.AuditOnce(func() error { return nil })
	require.NoError(t, err)
	require.NoError(t, WriteMeta(dir, sess))

	raw, err := os.ReadFile(filepath.Join(dir, "sessions", "id1", "meta.json"))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"audit_written": true`)

	got, err := LoadMeta(dir, "id1")
	require.NoError(t, err)
	require.True(t, got.AuditWritten, "a restart remembers the record was written")

	wire, err := json.Marshal(sess.Summary())
	require.NoError(t, err)
	require.NotContains(t, string(wire), "audit")
}

func TestAuditWritten_ClearsWhenTheSessionGoesLiveAgain(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateWorking, StartedAt: time.Now().UTC()}
	require.NoError(t, s.Add(sess))
	require.NoError(t, s.Transition("id1", protocol.StateDone))
	_, err := sess.AuditOnce(func() error { return nil })
	require.NoError(t, err)
	require.True(t, sess.AuditIsWritten())

	require.NoError(t, s.Transition("id1", protocol.StateDone))
	require.True(t, sess.AuditIsWritten(), "a second terminal transition is the same run")

	require.NoError(t, s.Transition("id1", protocol.StateWorking))
	require.False(t, sess.AuditIsWritten(), "a resumed run gets its own record")
}

func TestAuditText_MemoryOnly(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateDone}
	sess.SetAuditText("first", "first\nsecond")
	first, full := sess.AuditText()
	require.Equal(t, "first", first)
	require.Equal(t, "first\nsecond", full)

	require.NoError(t, WriteMeta(dir, sess))
	raw, err := os.ReadFile(filepath.Join(dir, "sessions", "id1", "meta.json"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "second")
	require.NotContains(t, string(raw), "first")
}

func TestReopen_StartsANewAuditRunAndForgetsThePrompt(t *testing.T) {
	dir := t.TempDir()
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateWorking, StartedAt: time.Now().UTC()}
	require.NoError(t, s.Add(sess))
	sess.SetAuditText("first", "first\nsecret")
	require.NoError(t, s.Transition("id1", protocol.StateDone))
	_, err := sess.AuditOnce(func() error { return nil })
	require.NoError(t, err)
	require.NoError(t, WriteMeta(dir, sess))

	require.NoError(t, s.Reopen("id1"))
	require.False(t, sess.AuditIsWritten(), "a resumed run is audited at its next end")
	first, full := sess.AuditText()
	require.Empty(t, first)
	require.Empty(t, full)

	// And it stays cleared across a restart once meta is written again.
	require.NoError(t, WriteMeta(dir, sess))
	got, err := LoadMeta(dir, "id1")
	require.NoError(t, err)
	require.False(t, got.AuditWritten)
}
