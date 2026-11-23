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

// ackFor reads conn until the reply to request id arrives and returns the
// session id the Ack names.
func ackFor(t *testing.T, conn interface{ SetReadDeadline(time.Time) error }, r *protocol.Reader, id string) string {
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
		id := ackFor(t, conn, r, reqID)

		var by *state.Requester
		require.Eventually(t, func() bool {
			sess, err := state.LoadMeta(e.dir, id)
			if err != nil {
				return false
			}
			by = sess.Requester()
			return by != nil
		}, 5*time.Second, 10*time.Millisecond, "meta.json carries requested_by (prompt %q)", prompt)
		require.Equal(t, os.Getpid(), by.PID)
		require.True(t, strings.HasPrefix(by.Client, "rex-cli"), by.Client)
		require.NotContains(t, by.Client, "\x1b")
		require.NotContains(t, by.Client, "\n")
		require.NotEmpty(t, by.Exe)
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
	ackFor(t, conn, r, "d1")

	require.Len(t, fa.deletes, 1)
	require.Equal(t, "rex-cli", fa.deletes[0].by.Client)
	require.Equal(t, os.Getpid(), fa.deletes[0].by.PID)
}
