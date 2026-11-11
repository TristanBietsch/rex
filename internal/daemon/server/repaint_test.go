package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// repaintTools: frame draws once on the alternate screen with mouse
// tracking on, then stays silent; count prints 1..4000 in paced bursts,
// wrapping and scrolling, then END.
const repaintTools = `tools:
  - id: echo
    models:
      - id: frame
        name: "Alt-screen frame"
        args: ["stty raw -echo; printf '\\033[?1049h\\033[?1000h\\033[H\\033[1;32mHELLO\\033[0m\\033[2;3H'; exec cat >/dev/null"]
      - id: count
        name: "Counter"
        args: ["stty raw -echo; i=0; while [ $i -lt 4000 ]; do i=$((i+1)); printf '%d ' $i; if [ $((i % 200)) -eq 0 ]; then sleep 0.02; fi; done; printf END; exec cat >/dev/null"]
`

// startToolsServer runs a server whose registry adds toolsYAML.
func startToolsServer(t *testing.T, toolsYAML string, seed ...*state.Session) *retireEnv {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-rp")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	toolsPath := filepath.Join(dir, "tools.yaml")
	require.NoError(t, os.WriteFile(toolsPath, []byte(toolsYAML), 0o600))
	reg, err := registry.Load(toolsPath)
	require.NoError(t, err)
	store := state.NewStore()
	for _, s := range seed {
		require.NoError(t, state.WriteMeta(dir, s))
		require.NoError(t, store.Add(s))
	}
	sock := filepath.Join(dir, "rex.sock")
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: store})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(served) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(10 * time.Second):
			t.Error("Serve did not return")
		}
	})
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", sock)
		if err == nil {
			c.Close()
		}
		return err == nil
	}, 2*time.Second, 20*time.Millisecond)
	return &retireEnv{srv: srv, store: store, sock: sock, dir: dir}
}

// spawn starts model and returns its session ID.
func (e *retireEnv) spawn(t *testing.T, model string) string {
	t.Helper()
	conn, w, r := e.dial(t)
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: model, Slug: model, CWD: e.dir,
	}))
	ack := waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.ID == "n" })
	require.Equal(t, protocol.EventAck, ack.Type, string(ack.Data))
	var a protocol.Ack
	require.NoError(t, json.Unmarshal(ack.Data, &a))
	return a.SessionID
}

// mirror returns the daemon's screen mirror of id, rebuilt from a repaint.
func (e *retireEnv) mirror(t *testing.T, id string) vt10x.Terminal {
	t.Helper()
	var b []byte
	e.srv.withOutputHeld(id, func(repaint func() []byte) { b = repaint() })
	term := vt10x.New(vt10x.WithSize(120, 32))
	_, err := term.Write(b)
	require.NoError(t, err)
	return term
}

// nextEvent reads the next session output or reply, skipping the board
// events a connection gets after Hello.
func nextEvent(t *testing.T, conn net.Conn, r *protocol.Reader) protocol.Envelope {
	t.Helper()
	for {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		env, err := r.Read()
		require.NoError(t, err)
		switch env.Type {
		case protocol.EventSessionOutput, protocol.EventAck, protocol.EventError:
			return env
		}
	}
}

func outputOf(t *testing.T, env protocol.Envelope) protocol.SessionOutput {
	t.Helper()
	require.Equal(t, protocol.EventSessionOutput, env.Type, string(env.Data))
	var so protocol.SessionOutput
	require.NoError(t, json.Unmarshal(env.Data, &so))
	return so
}

func startFrame(t *testing.T) (*retireEnv, string) {
	t.Helper()
	e := startToolsServer(t, repaintTools)
	id := e.spawn(t, "frame")
	require.Eventually(t, func() bool { return strings.Contains(e.mirror(t, id).String(), "HELLO") },
		5*time.Second, 20*time.Millisecond)
	return e, id
}

func TestSubscribe_RepaintSendsCurrentScreen(t *testing.T) {
	e, id := startFrame(t)
	conn, w, r := e.dial(t)
	require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "s1", protocol.Subscribe{SessionID: id, ModeSync: true, Repaint: true}))

	so := outputOf(t, nextEvent(t, conn, r))
	require.True(t, so.Repaint)
	require.Equal(t, id, so.SessionID)
	term := vt10x.New(vt10x.WithSize(120, 32))
	_, err := term.Write(so.Bytes)
	require.NoError(t, err)
	require.Contains(t, term.String(), "HELLO")
	term.Lock()
	g, cur, mode := term.Cell(0, 0), term.Cursor(), term.Mode()
	term.Unlock()
	require.NotZero(t, g.Mode&vt10x.AttrBold)
	require.Equal(t, vt10x.LightGreen, g.FG)
	require.Equal(t, [2]int{2, 1}, [2]int{cur.X, cur.Y})
	require.NotZero(t, mode&vt10x.ModeAltScreen, "the repaint carries the modes")
	require.NotZero(t, mode&vt10x.ModeMouseButton)

	ack := nextEvent(t, conn, r)
	require.Equal(t, protocol.EventAck, ack.Type, "the repaint replaces the mode-sync preamble, then the Ack")
	require.Equal(t, "s1", ack.ID)
}

func TestSubscribe_ReplayThenRepaint(t *testing.T) {
	e, id := startFrame(t)
	conn, w, r := e.dial(t)
	require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "s2", protocol.Subscribe{SessionID: id, Replay: true, Repaint: true}))
	replay := outputOf(t, nextEvent(t, conn, r))
	require.False(t, replay.Repaint)
	require.Contains(t, string(replay.Bytes), "HELLO")
	require.True(t, outputOf(t, nextEvent(t, conn, r)).Repaint)
	require.Equal(t, protocol.EventAck, nextEvent(t, conn, r).Type)
}

// TestSubscribe_OldClientUnchanged: a client that does not ask for a
// repaint gets what it always got, with no repaint key on the wire.
func TestSubscribe_OldClientUnchanged(t *testing.T) {
	e, id := startFrame(t)
	conn, w, r := e.dial(t)
	require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "s3", protocol.Subscribe{SessionID: id, ModeSync: true}))
	env := nextEvent(t, conn, r)
	require.NotContains(t, string(env.Data), "repaint")
	require.Equal(t, []byte("\x1b[?1049h\x1b[?1000h"), outputOf(t, env).Bytes)
	require.Equal(t, protocol.EventAck, nextEvent(t, conn, r).Type)

	conn2, w2, r2 := e.dial(t)
	require.NoError(t, w2.WriteIntent(protocol.IntentSubscribe, "s4", protocol.Subscribe{SessionID: id}))
	require.Equal(t, protocol.EventAck, nextEvent(t, conn2, r2).Type, "a plain Subscribe sends nothing first")

	// The JSON an old client sends has no repaint field at all.
	conn3, _, r3 := e.dial(t)
	_, err := fmt.Fprintf(conn3, `{"v":1,"kind":"Intent","type":"Subscribe","id":"s5","data":{"session_id":%q,"mode_sync":true}}`+"\n", id)
	require.NoError(t, err)
	require.False(t, outputOf(t, nextEvent(t, conn3, r3)).Repaint)
}

// A session that is not running has no screen: nothing to repaint.
func TestSubscribe_RepaintNotRunning(t *testing.T) {
	e := startToolsServer(t, repaintTools, doneSession("old"))
	conn, w, r := e.dial(t)
	require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "s6", protocol.Subscribe{SessionID: "old", Repaint: true}))
	ack := nextEvent(t, conn, r)
	require.Equal(t, protocol.EventAck, ack.Type, string(ack.Data))
}

// TestSubscribe_SeamlessUnderLoad subscribes while the agent is writing:
// what each client gets (replay or repaint, then live output) must add up
// to the agent's output with no chunk lost or doubled.
func TestSubscribe_SeamlessUnderLoad(t *testing.T) {
	e := startToolsServer(t, repaintTools)
	id := e.spawn(t, "count")
	type sub struct {
		replay bool
		raw    bytes.Buffer
		term   vt10x.Terminal
		done   chan error
	}
	// read collects s's output until END. Each client is read from the
	// start: the daemon drops one that falls 1024 lines behind.
	read := func(s *sub, conn net.Conn, r *protocol.Reader) {
		deadline := time.Now().Add(10 * time.Second)
		for !bytes.HasSuffix(s.raw.Bytes(), []byte("END")) {
			if err := conn.SetReadDeadline(deadline); err != nil {
				s.done <- err
				return
			}
			env, err := r.Read()
			if err != nil {
				s.done <- fmt.Errorf("after %d bytes: %w", s.raw.Len(), err)
				return
			}
			if env.Type != protocol.EventSessionOutput {
				continue
			}
			var so protocol.SessionOutput
			if err := json.Unmarshal(env.Data, &so); err != nil {
				s.done <- err
				return
			}
			s.raw.Write(so.Bytes)
			_, _ = s.term.Write(so.Bytes)
		}
		s.done <- nil
	}
	var subs []*sub
	for i := 0; i < 8; i++ {
		conn, w, r := e.dial(t)
		s := &sub{replay: i%2 == 1, term: vt10x.New(vt10x.WithSize(120, 32)), done: make(chan error, 1)}
		go read(s, conn, r)
		require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: id, Replay: s.replay, Repaint: !s.replay}))
		subs = append(subs, s)
		time.Sleep(40 * time.Millisecond)
	}
	for i, s := range subs {
		require.NoError(t, <-s.done, "client %d", i)
	}
	transcript, err := state.TranscriptTail(e.dir, id, transcriptReplayMax)
	require.NoError(t, err)
	require.True(t, bytes.HasSuffix(transcript, []byte("END")))
	require.Less(t, len(transcript), transcriptReplayMax, "the replay must cover the whole run")
	want := vt10x.New(vt10x.WithSize(120, 32))
	_, err = want.Write(transcript)
	require.NoError(t, err)
	for i, s := range subs {
		if s.replay {
			require.Equal(t, string(transcript), s.raw.String(), "client %d: replay plus live output", i)
		}
		require.Equal(t, want.String(), s.term.String(), "client %d screen", i)
		want.Lock()
		s.term.Lock()
		wc, gc := want.Cursor(), s.term.Cursor()
		s.term.Unlock()
		want.Unlock()
		require.Equal(t, [2]int{wc.X, wc.Y}, [2]int{gc.X, gc.Y}, "client %d cursor", i)
	}
}
