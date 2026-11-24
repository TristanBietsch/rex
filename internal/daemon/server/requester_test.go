package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ackSessionID reads conn until the reply to request id arrives and returns the
// session id the Ack names.
func ackSessionID(t *testing.T, conn interface{ SetReadDeadline(time.Time) error }, r *protocol.Reader, id string) string {
	t.Helper()
	for {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		env, err := r.Read()
		require.NoError(t, err)
		if env.ID != id {
			continue
		}
		require.Equal(t, protocol.EventAck, env.Type)
		var a protocol.Ack
		require.NoError(t, json.Unmarshal(env.Data, &a))
		return a.SessionID
	}
}

func TestSpawn_StoresRequesterInMeta(t *testing.T) {
	e := startAckServer(t)
	conn := dialTest(t, e.sock)
	w, r, _ := helloAs(t, conn, "rex-cli\x1b[31m\nforged")

	for i, prompt := range []string{"do the thing", ""} {
		slug := []string{"with-prompt", "no-prompt"}[i]
		reqID := "n-" + slug
		require.NoError(t, w.WriteIntent(protocol.IntentNewSession, reqID,
			protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: slug, CWD: e.dir, InitialPrompt: prompt}))
		id := ackSessionID(t, conn, r, reqID)

		var by *state.Requester
		require.Eventually(t, func() bool {
			sess, err := state.LoadMeta(e.dir, id)
			if err != nil {
				return false
			}
			by = sess.Requester()
			return by != nil
		}, 5*time.Second, 10*time.Millisecond, "meta.json carries requested_by (prompt %q)", prompt)
		require.Equal(t, wantPeerPID(), by.PID)
		require.True(t, strings.HasPrefix(by.Client, "rex-cli"), by.Client)
		require.NotContains(t, by.Client, "\x1b")
		require.NotContains(t, by.Client, "\n")
		if peerCredSupported {
			require.NotEmpty(t, by.Exe)
		}
		live, ok := e.srv.cfg.Store.Get(id)
		require.True(t, ok)
		require.Equal(t, by, live.Requester())
	}
}

func TestDelete_PassesTheRequesterToTheAuditHook(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	log := &auditLog{}
	fa := &fakeAudit{log: log, prompts: map[string]string{}}
	srv.cfg.Audit = fa
	srv.removeSession = func(string, *state.Session) error { return nil }
	require.NoError(t, srv.cfg.Store.Add(&state.Session{ID: "aaaa1111", ShortID: "aaaa", Slug: "s", State: protocol.StateDone, StartedAt: time.Now().UTC()}))
	serveTest(t, srv)

	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-cli")
	require.NoError(t, w.WriteIntent(protocol.IntentDelete, "d1", protocol.Delete{SessionID: "aaaa1111"}))
	ackSessionID(t, conn, r, "d1")

	require.Len(t, fa.deletes, 1)
	require.Equal(t, "rex-cli", fa.deletes[0].by.Client)
	require.Equal(t, wantPeerPID(), fa.deletes[0].by.PID)
}

// seedExited adds an exited resumable session started by "original".
func seedExited(t *testing.T, e *retireEnv, id string) *state.Session {
	t.Helper()
	sess := exited(func() string { return e.dir }, id, protocol.StateDone, "slow", testResumeID)
	sess.SetRequestedBy(&state.Requester{Client: "original", PID: 11, Exe: "/bin/original"})
	require.NoError(t, e.store.Add(sess))
	require.NoError(t, state.WriteMeta(e.dir, sess))
	return sess
}

func TestResume_ByAClientRecordsThatClient(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	sess := seedExited(t, e, "a")
	conn := dialTest(t, e.sock)
	w, r, _ := helloAs(t, conn, "resumer")

	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Empty(t, code, msg)
	by := sess.Requester()
	require.NotNil(t, by)
	require.Equal(t, "resumer", by.Client, "the client that asked for this run")
	require.Equal(t, wantPeerPID(), by.PID)
	onDisk, err := state.LoadMeta(e.dir, "a")
	require.NoError(t, err)
	require.Equal(t, by, onDisk.Requester())
}

func TestResume_WithoutClientKeepsRequester(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	sess := seedExited(t, e, "a")

	require.NoError(t, e.srv.EnsureLive(t.Context(), "a")) // rex resumes it itself, as a loop does
	require.Equal(t, &state.Requester{Client: "original", PID: 11, Exe: "/bin/original"}, sess.Requester())
	onDisk, err := state.LoadMeta(e.dir, "a")
	require.NoError(t, err)
	require.Equal(t, "original", onDisk.Requester().Client)
}

// wantPeerPID is the requester pid the daemon records for this test process:
// its own pid where peer credentials exist, 0 where peerPID is a documented
// no-op (Windows).
func wantPeerPID() int {
	if !peerCredSupported {
		return 0
	}
	return os.Getpid()
}
