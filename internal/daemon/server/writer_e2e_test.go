package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/pty/ptytest"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Input while a session has no PTY writer (it is starting or exiting) is
// refused, never queued into a channel nobody drains and reported as sent.
func TestInput_RefusedWhileTheWriterIsNotReady(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	srv.writerWait = 80 * time.Millisecond
	serveTest(t, srv)
	ch := make(chan []byte, 8)
	srv.RegisterInputChannel("s1", ch) // registered before the supervisor's writer
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-tui")

	refuse := func(label string) {
		t.Helper()
		sent, err := srv.SendText("s1", "go on", func() bool { return true })
		require.ErrorContains(t, err, "session input not ready", label)
		require.False(t, sent, label)
		require.ErrorContains(t, srv.SendRaw("s1", []byte("x"), time.Second), "session input not ready", label)
		require.NoError(t, w.WriteIntent(protocol.IntentReply, "r", protocol.Reply{SessionID: "s1", Text: "ok"}))
		env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "r" })
		require.Equal(t, protocol.EventError, env.Type, label)
		require.NoError(t, w.WriteIntent(protocol.IntentSendInput, "k", protocol.SendInput{SessionID: "s1", Bytes: []byte("y")}))
		env = waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "k" })
		require.Equal(t, protocol.EventError, env.Type, label)
		var ee protocol.ErrorEvent
		require.NoError(t, json.Unmarshal(env.Data, &ee))
		require.Contains(t, ee.Message, "session input not ready", label)
		require.Empty(t, ch, label+": nothing was queued")
	}
	refuse("before the writer starts")

	registerInput(srv, "s1", ch)
	sent, err := srv.SendText("s1", "go on", func() bool { return true })
	require.NoError(t, err)
	require.True(t, sent)
	<-ch
	<-ch

	srv.UnregisterUnitWriter("s1") // the supervisor is exiting; the channel is still registered
	refuse("after the writer ends")
}

// The first input of an attach arrives right behind the session: it waits for
// the writer to come up and is then delivered in order, not refused.
func TestInput_WaitsBrieflyForAStartingSessionsWriter(t *testing.T) {
	srv, _, _ := newTestServer(t, state.NewStore())
	ch := make(chan []byte, 8)
	srv.RegisterInputChannel("s1", ch)
	go func() {
		time.Sleep(150 * time.Millisecond)
		srv.RegisterUnitWriter("s1", ptytest.ChanWriter(ch))
	}()
	_, err := srv.writeInput("s1", pty.WriteUnit{Parts: [][]byte{[]byte("held")}}, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, []byte("held"), <-ch)
}

// The whole path with a real PTY: a raw-mode cat echoes what the supervisor's
// single writer delivers, so the transcript shows the order and the timing.
func TestInput_RealSupervisorOrderAndTiming(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash and a Unix tty")
	}
	srv, sock, dir := newTestServer(t, state.NewStore())
	tools := filepath.Join(dir, "tools.yaml")
	require.NoError(t, os.WriteFile(tools, []byte(`tools:
  - id: rawcat
    name: "Raw cat"
    category: self_hosted
    command: ["bash", "-c"]
    detect: {kind: heuristic, prompt_regex: "zzz-never", idle_ms: 800}
    icon: "o"
    color: "#94A3B8"
    models:
      - {id: cat, name: "cat", args: ["stty raw -echo; echo READY; cat"]}
`), 0o600))
	reg, err := registry.Load(tools)
	require.NoError(t, err)
	srv.SetRegistry(reg)
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-tui")
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "rawcat", ModelID: "cat", Slug: "raw", CWD: dir}))
	env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "n" })
	require.Equal(t, protocol.EventAck, env.Type, string(env.Data))
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	id := ack.SessionID

	// Everything the child prints, with when it arrived.
	var mu sync.Mutex
	var out strings.Builder
	type stamp struct {
		end int
		at  time.Time
	}
	var stamps []stamp
	srv.SubscribeSessionOutput(id, func(b []byte) {
		mu.Lock()
		defer mu.Unlock()
		out.Write(b)
		stamps = append(stamps, stamp{out.Len(), time.Now()})
	})
	require.Eventually(t, func() bool { _, ok := srv.unitWriters.Load(id); return ok }, 10*time.Second, 10*time.Millisecond)
	seen := func() string { mu.Lock(); defer mu.Unlock(); return out.String() }
	require.Eventually(t, func() bool { return strings.Contains(seen(), "READY") }, 10*time.Second, 10*time.Millisecond, "the tty is raw by now")
	timeOf := func(marker string) time.Time {
		mu.Lock()
		defer mu.Unlock()
		i := strings.Index(out.String(), marker)
		require.GreaterOrEqual(t, i, 0, marker)
		for _, s := range stamps {
			if s.end > i {
				return s.at
			}
		}
		t.Fatal("no stamp")
		return time.Time{}
	}
	send := func(id, text string) {
		require.NoError(t, w.WriteIntent(protocol.IntentSendInput, id, protocol.SendInput{SessionID: ack.SessionID, Bytes: []byte(text)}))
	}

	// Rapid client keys arrive in order.
	for i, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		send("k"+string(rune('0'+i)), k)
	}
	require.Eventually(t, func() bool { return strings.Contains(seen(), "abcdefgh") }, 10*time.Second, 20*time.Millisecond, "%q", seen())

	// A Reply is its text, a pause of at least 120 ms, then Enter.
	require.NoError(t, w.WriteIntent(protocol.IntentReply, "rp", protocol.Reply{SessionID: id, Text: "HELLO"}))
	waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "rp" })
	require.Eventually(t, func() bool { return strings.Contains(seen(), "HELLO\r") }, 10*time.Second, 20*time.Millisecond, "%q", seen())
	gap := timeOf("\r").Sub(timeOf("HELLO"))
	require.GreaterOrEqual(t, gap, 100*time.Millisecond, "text, pause, Enter")

	// A loop's SendText with a client key arriving while it runs: the key
	// comes after the Enter, never between the text and the Enter.
	done := make(chan struct{})
	go func() {
		defer close(done)
		sent, err := srv.SendText(id, "LOOPTEXT", func() bool { return true })
		require.NoError(t, err)
		require.True(t, sent)
	}()
	time.Sleep(40 * time.Millisecond) // inside the 120 ms gap
	send("late", "K")
	<-done
	require.Eventually(t, func() bool { return strings.Contains(seen(), "LOOPTEXT\rK") }, 10*time.Second, 20*time.Millisecond, "%q", seen())
}
