package session

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// spinnerTools registers a fake full-screen agent that draws a frame once,
// then only updates a counter in place (more than the 256 KiB replay
// holds), then goes silent: it ignores ^L, SIGWINCH and every key.
const spinnerTools = `tools:
  - id: echo
    models:
      - id: spinner
        name: "Frame, then in-place updates, then silent"
        args: ["stty raw -echo; printf '\\033[?1049h\\033[H\\033[2J\\033[1;31mFRAME-ONE\\033[0m \\033[38;5;208morange\\033[0m'; i=0; while [ $i -lt 25000 ]; do i=$((i+1)); printf '\\033[5;1Htick %06d' $i; done; exec cat > /dev/null"]
`

// startSpinner spawns the spinner agent and waits until the daemon's screen
// shows its last update.
func startSpinner(t *testing.T) (*testutil.Daemon, protocol.SessionSummary) {
	t.Helper()
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	d := testutil.StartDaemonWithTools(t, spinnerTools)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.NewSession(protocol.NewSession{ToolID: "echo", ModelID: "spinner", Slug: "spin", CWD: t.TempDir()}))
	var sum protocol.SessionSummary
	deadline := time.Now().Add(15 * time.Second)
	for {
		require.True(t, time.Now().Before(deadline), "agent never finished drawing")
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		env, err := c.NextEvent()
		if err != nil {
			continue
		}
		switch env.Type {
		case protocol.EventSessionAdded:
			require.NoError(t, json.Unmarshal(env.Data, &sum))
		case protocol.EventSessionUpdated:
			var up protocol.SessionUpdated
			require.NoError(t, json.Unmarshal(env.Data, &up))
			if line, _ := up.Patch["last_line"].(string); up.SessionID == sum.ID && strings.Contains(line, "tick 025000") {
				return d, sum
			}
		}
	}
}

// TestAttach_FullScreenAgentShowsCurrentScreen: `rex attach` on a
// full-screen agent that drew long ago must show its current screen. The
// replay tail alone holds only the counter updates, and with -no-replay
// nothing at all; the agent never redraws. Output goes to a vt10x terminal
// standing in for the user's.
func TestAttach_FullScreenAgentShowsCurrentScreen(t *testing.T) {
	for _, replay := range []bool{true, false} {
		t.Run(map[bool]string{true: "replay", false: "no-replay"}[replay], func(t *testing.T) {
			d, sum := startSpinner(t)
			c, err := client.Dial(d.Socket)
			require.NoError(t, err)
			sess, err := core.ResolveSelector(c, sum.ID)
			require.NoError(t, err)
			sess.ToolID = "claude" // a full-screen agent

			screen := vt10x.New(vt10x.WithSize(120, 32)) // the PTY's size
			_, _ = screen.Write([]byte(enterAlt))
			reqID, err := subscribeAttach(c, sess, replay)
			require.NoError(t, err)
			watch := redrawFallback(c, sess, reqID)
			done := make(chan error, 1)
			go func() { done <- streamAttach(c, sess.ID, watch, screen) }()

			require.Eventually(t, func() bool {
				s := screen.String()
				return strings.Contains(s, "FRAME-ONE orange") && strings.Contains(s, "tick 025000")
			}, 5*time.Second, 20*time.Millisecond, "screen:\n%s", screen.String())
			require.False(t, watch.Nudged(), "the daemon repainted; no ^L needed")
			require.NoError(t, c.Close())
			require.NoError(t, <-done)
		})
	}
}

// legacyDaemon serves one attach connection like a daemon that predates
// repaints: Snapshot on Hello, the mode preamble on Subscribe, then the
// Subscribe's Ack only when acks is set (daemons before acks send none).
// It returns the socket and the SendInput intents it receives.
func legacyDaemon(t *testing.T, sess protocol.SessionSummary, acks bool) (string, <-chan protocol.SendInput) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexat") // short path: UDS names cap at ~104 bytes
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	inputs := make(chan protocol.SendInput, 4)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r, w := protocol.NewReader(conn), protocol.NewWriter(conn)
		for {
			env, err := r.Read()
			if err != nil {
				return
			}
			switch env.Type {
			case protocol.IntentHello:
				snap := protocol.Snapshot{Sessions: []protocol.SessionSummary{sess}}
				if acks {
					snap.DaemonVersion = "test"
				}
				_ = w.WriteEvent(protocol.EventSnapshot, env.ID, snap)
			case protocol.IntentSubscribe:
				_ = w.WriteEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{SessionID: sess.ID, Bytes: []byte("\x1b[?1049h")})
				if acks && env.ID != "" {
					_ = w.WriteEvent(protocol.EventAck, env.ID, protocol.Ack{Intent: env.Type, SessionID: sess.ID})
				}
			case protocol.IntentSendInput:
				var in protocol.SendInput
				_ = json.Unmarshal(env.Data, &in)
				inputs <- in
			}
		}
	}()
	return sock, inputs
}

// TestAttach_LegacyDaemonGetsRedrawNudge: a daemon that cannot repaint
// still gets the ^L nudge, at once on its Ack or after RepaintReplyWait
// when it predates acks.
func TestAttach_LegacyDaemonGetsRedrawNudge(t *testing.T) {
	sess := protocol.SessionSummary{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "alpha", ToolID: "codex"}
	for _, acks := range []bool{true, false} {
		t.Run(map[bool]string{true: "acks", false: "before-acks"}[acks], func(t *testing.T) {
			sock, inputs := legacyDaemon(t, sess, acks)
			c, err := client.Dial(sock)
			require.NoError(t, err)
			got, err := core.ResolveSelector(c, "alpha")
			require.NoError(t, err)
			start := time.Now()
			reqID, err := subscribeAttach(c, got, false)
			require.NoError(t, err)
			watch := redrawFallback(c, got, reqID)
			done := make(chan error, 1)
			go func() { done <- streamAttach(c, got.ID, watch, &strings.Builder{}) }()
			select {
			case in := <-inputs:
				require.Equal(t, []byte{0x0c}, in.Bytes)
				require.True(t, in.Synthetic)
				if acks {
					require.Less(t, time.Since(start), client.RepaintReplyWait, "an Ack decides at once")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no ^L nudge sent to a daemon without repaints")
			}
			require.True(t, watch.Nudged())
			require.NoError(t, c.Close())
			require.NoError(t, <-done)
		})
	}
}

// Agents that are not full-screen keep the plain replay and get no nudge.
func TestAttach_LineAgentNoRepaintNoNudge(t *testing.T) {
	sess := protocol.SessionSummary{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "alpha", ToolID: "ollama"}
	require.Nil(t, redrawFallback(nil, sess, "x"))
	sock, inputs := legacyDaemon(t, sess, true)
	c, err := client.Dial(sock)
	require.NoError(t, err)
	defer c.Close()
	got, err := core.ResolveSelector(c, "alpha")
	require.NoError(t, err)
	_, err = subscribeAttach(c, got, true)
	require.NoError(t, err)
	select {
	case in := <-inputs:
		t.Fatalf("unexpected input %q", in.Bytes)
	case <-time.After(2 * client.RepaintReplyWait):
	}
}
