package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	alphaID = "aaaaaaaa-0000-4000-8000-000000000001"
	betaID  = "bbbbbbbb-0000-4000-8000-000000000002"
)

// namedStore is seededStore with alpha named "Fix Auth" (done) and beta
// unnamed (failed).
func namedStore(t *testing.T) *state.Store {
	t.Helper()
	store := seededStore(t)
	_, err := store.SetName(alphaID, "Fix Auth")
	require.NoError(t, err)
	return store
}

func TestIntentSetSessionName_RejectsTakenName(t *testing.T) {
	store := namedStore(t)
	sock, _, cancel := startServerWithStore(t, store)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	// Case-insensitive, whitespace collapsed: still alpha's name.
	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionName, "sn-1", protocol.SetSessionName{
		SessionID: betaID, Name: "  fix   AUTH ",
	}))
	ee := decodeError(t, readUntil(t, conn, r, errorFor("sn-1")))
	require.Equal(t, protocol.ErrCodeNameTaken, ee.Code)
	require.Equal(t, `name "Fix Auth" already in use by aaaa`, ee.Message)
	b, _ := store.Get(betaID)
	require.Empty(t, b.Name, "name unchanged on rejection")

	// The holder may change the case of its own name.
	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionName, "sn-2", protocol.SetSessionName{
		SessionID: alphaID, Name: "fix auth",
	}))
	readUntil(t, conn, r, ackFor(t, "sn-2"))
	a, _ := store.Get(alphaID)
	require.Equal(t, "fix auth", a.Name)

	// A free name goes through.
	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionName, "sn-3", protocol.SetSessionName{
		SessionID: betaID, Name: "fix auth 2",
	}))
	readUntil(t, conn, r, ackFor(t, "sn-3"))

	// A fire-and-forget refusal is still an Error, with no id.
	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionName, "", protocol.SetSessionName{
		SessionID: betaID, Name: "FIX AUTH",
	}))
	env := readUntil(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventError })
	ee = decodeError(t, env)
	require.Equal(t, protocol.ErrCodeNameTaken, ee.Code)
	require.Empty(t, ee.ID)
	require.Equal(t, "fix auth 2", b.Name)
}

func TestIntentNewSession_RejectsTakenName(t *testing.T) {
	store := namedStore(t)
	sock, dir, cancel := startServerWithStore(t, store)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	// alpha is done: it still holds its name.
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "ns-1", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "fresh", Name: "FIX AUTH", CWD: dir,
	}))
	ee := decodeError(t, readUntil(t, conn, r, errorFor("ns-1")))
	require.Equal(t, protocol.ErrCodeNameTaken, ee.Code)
	require.Equal(t, `name "Fix Auth" already in use by aaaa`, ee.Message)
	require.Len(t, store.All(), 2, "rejected spawn adds nothing")
	ents, _ := os.ReadDir(filepath.Join(dir, "sessions"))
	require.Empty(t, ents, "rejected spawn leaves no session dir")

	// A taken slug is reported first.
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "ns-2", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "alpha", Name: "Fix Auth", CWD: dir,
	}))
	ee = decodeError(t, readUntil(t, conn, r, errorFor("ns-2")))
	require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
	require.Contains(t, ee.Message, `slug "alpha"`)

	// Deleting the holder frees the name.
	require.NoError(t, w.WriteIntent(protocol.IntentDelete, "del", protocol.Delete{SessionID: alphaID}))
	readUntil(t, conn, r, ackFor(t, "del"))
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "ns-3", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "fresh", Name: "fix auth", CWD: dir,
	}))
	readUntil(t, conn, r, ackFor(t, "ns-3"))
}

// Concurrent NewSessions, one per connection, all asking for the same name:
// exactly one is created, every other gets name_taken. Run with -race.
func TestIntentNewSession_ConcurrentSameName(t *testing.T) {
	store := state.NewStore()
	sock, dir, cancel := startServerWithStore(t, store)
	defer cancel()

	const n = 8
	type conn struct {
		c net.Conn
		w *protocol.Writer
		r *protocol.Reader
	}
	conns := make([]conn, n)
	for i := range conns {
		c, err := net.Dial("unix", sock)
		require.NoError(t, err)
		defer c.Close()
		w, r := helloOnConn(t, c)
		conns[i] = conn{c, w, r}
	}

	codes := make([]string, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range conns {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cn := conns[i]
			id := fmt.Sprintf("race-%d", i)
			<-start
			if err := cn.w.WriteIntent(protocol.IntentNewSession, id, protocol.NewSession{
				ToolID: "echo", ModelID: "short", Slug: fmt.Sprintf("race-%d", i), Name: "Contested", CWD: dir,
			}); err != nil {
				codes[i] = "write: " + err.Error()
				return
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				_ = cn.c.SetReadDeadline(deadline)
				env, err := cn.r.Read()
				if err != nil {
					codes[i] = "read: " + err.Error()
					return
				}
				if env.ID != id {
					continue
				}
				if env.Type == protocol.EventAck {
					codes[i] = "ack"
					return
				}
				var ee protocol.ErrorEvent
				_ = json.Unmarshal(env.Data, &ee)
				codes[i] = ee.Code
				return
			}
			codes[i] = "timeout"
		}(i)
	}
	close(start)
	wg.Wait()

	acks := 0
	for i, code := range codes {
		if code == "ack" {
			acks++
			continue
		}
		require.Equal(t, protocol.ErrCodeNameTaken, code, "conn %d", i)
	}
	require.Equal(t, 1, acks, "codes: %v", codes)
	holders := 0
	for _, s := range store.Snapshot() {
		if s.Name == "Contested" {
			holders++
		}
	}
	require.Equal(t, 1, holders)
	require.Len(t, store.All(), 1)
}

// ackFor matches the Ack echoing reqID and fails on an Error for it.
func ackFor(t *testing.T, reqID string) func(protocol.Envelope) bool {
	return func(env protocol.Envelope) bool {
		if env.ID != reqID {
			return false
		}
		if env.Type == protocol.EventError {
			t.Fatalf("unexpected error for %s: %s", reqID, env.Data)
		}
		return env.Type == protocol.EventAck
	}
}
