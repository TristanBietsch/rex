package tui

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

	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// silentTools registers fake full-screen agents that draw one frame and then
// never draw again: they ignore ^L, SIGWINCH and every key (like an agent
// busy mid-turn), and copy what they read to input.log in their folder.
//
//   - alt draws on the alternate screen, like Claude Code fullscreen or codex.
//   - inline prints 60 numbered lines on the primary screen, then a prompt
//     line, like Claude Code without fullscreen.
const silentTools = `tools:
  - id: echo
    models:
      - id: alt
        name: "Alt-screen frame, then silent"
        args: ["stty raw -echo; printf '\\033[?1049h\\033[H\\033[2J\\033[1;31mFRAME-ONE\\033[0m \\033[38;5;208morange\\033[0m\\033[3;5H'; exec cat > input.log"]
      - id: inline
        name: "Primary-screen frame, then silent"
        args: ["stty raw -echo; for i in $(seq 1 60); do printf 'history %02d\\r\\n' $i; done; printf '\\033[7m> prompt\\033[0m'; exec cat > input.log"]
`

// startSilentSession spawns model in a temp daemon and waits until the
// daemon's screen mirror shows want (its last_line), so the agent has drawn
// before anyone attaches.
func startSilentSession(t *testing.T, model, want string) (*testutil.Daemon, protocol.SessionSummary) {
	t.Helper()
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	d := testutil.StartDaemonWithTools(t, silentTools)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	cwd := d.StateDir // not t.TempDir: on Windows its cleanup runs before d.Close, while the child still uses it
	require.NoError(t, c.NewSession(protocol.NewSession{ToolID: "echo", ModelID: model, Slug: model, CWD: cwd}))
	var sum protocol.SessionSummary
	drawn := false
	deadline := time.Now().Add(5 * time.Second)
	for !drawn {
		require.True(t, time.Now().Before(deadline), "agent never drew %q", want)
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
			if line, ok := up.Patch["last_line"].(string); ok && up.SessionID == sum.ID && strings.Contains(line, want) {
				drawn = true
			}
		}
	}
	sum.CWD = cwd
	return d, sum
}

// openFullScreenAttach opens the popup on sum as if it were a Claude Code
// session, at the PTY's own size (120x32) so the attach resize is a no-op
// and sends the agent no SIGWINCH.
func openFullScreenAttach(t *testing.T, d *testutil.Daemon, sum protocol.SessionSummary) Model {
	t.Helper()
	sum.ToolID = "claude" // full-screen path: no replay
	m := boardModel(sum)
	m.Socket = d.Socket
	m.Width, m.Height = 134, 44
	m, _ = openAttach(m, sum.ID)
	require.NotNil(t, m.Attach, "attach failed: %s", m.Err)
	require.Equal(t, 120, m.Attach.Cols)
	require.Equal(t, 32, m.Attach.Rows)
	st := m.Attach
	t.Cleanup(func() { _ = st.Client.Close() })
	return m
}

// pumpPopup applies popup output to m until done reports true or d passes.
func pumpPopup(t *testing.T, m Model, d time.Duration, done func(Model) bool) Model {
	t.Helper()
	deadline := time.Now().Add(d)
	for !done(m) {
		left := time.Until(deadline)
		if left <= 0 {
			return m
		}
		msg := nextAttachMsg(m.Attach, left)
		if msg == nil {
			return m
		}
		out, _ := m.Update(msg)
		m = out.(Model)
	}
	return m
}

func popupText(m Model) string { return m.Attach.Term.String() }

// TestAttachE2E_SilentFullScreenAgentIsNotBlank is the reported bug: opening
// a running full-screen agent that does not redraw on ^L (or is busy) left
// the popup black until the agent next drew. The popup must show the frame
// the agent already drew, with its colors and cursor.
func TestAttachE2E_SilentFullScreenAgentIsNotBlank(t *testing.T) {
	d, sum := startSilentSession(t, "alt", "FRAME-ONE")
	m := openFullScreenAttach(t, d, sum)
	m = pumpPopup(t, m, 2*time.Second, func(m Model) bool { return strings.Contains(popupText(m), "orange") })

	text := popupText(m)
	require.Contains(t, text, "FRAME-ONE orange", "popup is blank:\n%s", text)
	m.Attach.Term.Lock()
	f, o := m.Attach.Term.Cell(0, 0), m.Attach.Term.Cell(10, 0)
	cur, mode := m.Attach.Term.Cursor(), m.Attach.Term.Mode()
	m.Attach.Term.Unlock()
	require.Equal(t, 'F', f.Char)
	require.NotZero(t, f.Mode&vt10x.AttrBold)
	require.Equal(t, vt10x.LightRed, f.FG, "bold red is stored brightened")
	require.Equal(t, vt10x.Color(208), o.FG)
	require.Equal(t, [2]int{4, 2}, [2]int{cur.X, cur.Y})
	require.NotZero(t, mode&vt10x.ModeAltScreen)
}

// TestAttachE2E_SilentInlineAgentIsNotBlank: the same for an agent on the
// primary screen. The repaint redraws the visible screen in place, so none
// of it lands in the popup's scrollback as history.
func TestAttachE2E_SilentInlineAgentIsNotBlank(t *testing.T) {
	// The "> prompt" row is input chrome to termtext, so last_line stops at
	// the line above it.
	d, sum := startSilentSession(t, "inline", "history 60")
	m := openFullScreenAttach(t, d, sum)
	m = pumpPopup(t, m, 2*time.Second, func(m Model) bool { return strings.Contains(popupText(m), "> prompt") })

	lines := strings.Split(popupText(m), "\n")
	require.Equal(t, "history 30", strings.TrimSpace(lines[0]), "popup:\n%s", strings.Join(lines, "\n"))
	require.Equal(t, "history 60", strings.TrimSpace(lines[30]))
	require.Equal(t, "> prompt", strings.TrimSpace(lines[31]))
	require.Zero(t, m.Attach.sb.Len(), "the repaint must not enter the scrollback")
	m.Attach.Term.Lock()
	g := m.Attach.Term.Cell(0, 31)
	mode := m.Attach.Term.Mode()
	m.Attach.Term.Unlock()
	require.NotZero(t, g.Mode&vt10x.AttrReverse)
	require.Zero(t, mode&vt10x.ModeAltScreen)
}

// TestAttachE2E_RepaintSkipsRedrawNudge: when the daemon repaints, the
// popup must not also type ^L into the agent.
func TestAttachE2E_RepaintSkipsRedrawNudge(t *testing.T) {
	d, sum := startSilentSession(t, "alt", "FRAME-ONE")
	withAttachRepaintWait(t, time.Minute) // only the repaint and the Ack may decide
	m := openFullScreenAttach(t, d, sum)
	m = pumpPopup(t, m, 2*time.Second, func(m Model) bool { return strings.Contains(popupText(m), "orange") })
	// The Ack follows the repaint; give the pump time to see it (a nudge
	// would go out then), then type a key of our own as a marker.
	time.Sleep(300 * time.Millisecond)
	out, _ := m.Update(key("z"))
	m = out.(Model)
	require.Empty(t, m.Err)
	log := filepath.Join(sum.CWD, "input.log")
	var got []byte
	require.Eventually(t, func() bool {
		got, _ = os.ReadFile(log)
		return strings.Contains(string(got), "z")
	}, 3*time.Second, 20*time.Millisecond)
	require.Equal(t, "z", string(got), "agent received input other than the marker")
}

// withAttachRepaintWait sets the popup's fallback wait for one test.
func withAttachRepaintWait(t *testing.T, d time.Duration) {
	t.Helper()
	prev := attachRepaintWait
	attachRepaintWait = d
	t.Cleanup(func() { attachRepaintWait = prev })
}

// Output split inside a rune across messages (the daemon forwards PTY reads
// as they come) still draws the rune, and the rest of the row stays put.
func TestAttach_RuneSplitAcrossOutput(t *testing.T) {
	m := attachWithScrollback(t, 0)
	m = feedAttach(t, m, "ab\xe2\x94")
	m = feedAttach(t, m, "\x80cd")
	require.True(t, strings.HasPrefix(popupText(m), "ab─cd "), "popup row %q", strings.Split(popupText(m), "\n")[0])
}

// legacyAttachDaemon serves one attach connection like a daemon that
// predates repaints: the mode preamble on Subscribe, then its Ack only when
// acks is set (daemons before acks send none). It returns the socket and
// every intent it reads.
func legacyAttachDaemon(t *testing.T, sessionID string, acks bool) (string, <-chan protocol.Envelope) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexla") // short path: UDS names cap at ~104 bytes
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "d.sock"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	intents := make(chan protocol.Envelope, 8)
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
			intents <- env
			if env.Type != protocol.IntentSubscribe {
				continue
			}
			_ = w.WriteEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{SessionID: sessionID, Bytes: []byte("\x1b[?1049h")})
			if acks && env.ID != "" {
				_ = w.WriteEvent(protocol.EventAck, env.ID, protocol.Ack{Intent: env.Type, SessionID: sessionID})
			}
		}
	}()
	return ln.Addr().String(), intents
}

func nextIntent(ch <-chan protocol.Envelope, d time.Duration) (protocol.Envelope, bool) {
	select {
	case env := <-ch:
		return env, true
	case <-time.After(d):
		return protocol.Envelope{}, false
	}
}

// TestAttach_LegacyDaemonGetsRedrawNudge: against a daemon that cannot
// repaint, the popup still nudges a full-screen agent with ^L: on the
// Subscribe's Ack (the fallback wait is a minute, so only the Ack can
// trigger it), or after the wait from a daemon without acks.
func TestAttach_LegacyDaemonGetsRedrawNudge(t *testing.T) {
	for _, acks := range []bool{true, false} {
		t.Run(map[bool]string{true: "acks", false: "before-acks"}[acks], func(t *testing.T) {
			if acks {
				withAttachRepaintWait(t, time.Minute)
			} else {
				withAttachRepaintWait(t, 50*time.Millisecond)
			}
			sock, intents := legacyAttachDaemon(t, "s1", acks)
			m := boardModel(protocol.SessionSummary{ID: "s1", Slug: "a", ToolID: "codex"})
			m.Socket = sock
			m, _ = openAttach(m, "s1")
			require.NotNil(t, m.Attach, m.Err)
			st := m.Attach
			t.Cleanup(func() { _ = st.Client.Close() })

			resize, ok := nextIntent(intents, 2*time.Second)
			require.True(t, ok)
			require.Equal(t, protocol.IntentResize, resize.Type, "resize first, so a repaint has the popup's size")
			sub, ok := nextIntent(intents, 2*time.Second)
			require.True(t, ok)
			require.Equal(t, protocol.IntentSubscribe, sub.Type)
			var p protocol.Subscribe
			require.NoError(t, json.Unmarshal(sub.Data, &p))
			require.Equal(t, protocol.Subscribe{SessionID: "s1", ModeSync: true, Repaint: true}, p)
			require.NotEmpty(t, sub.ID)

			in, ok := nextIntent(intents, 5*time.Second)
			require.True(t, ok, "no ^L nudge")
			require.Equal(t, protocol.IntentSendInput, in.Type)
			var si protocol.SendInput
			require.NoError(t, json.Unmarshal(in.Data, &si))
			require.Equal(t, []byte{0x0c}, si.Bytes)
			require.True(t, si.Synthetic)

			m = pumpPopup(t, m, 2*time.Second, func(m Model) bool { return !m.Attach.onPrimaryScreen() })
			require.False(t, m.Attach.onPrimaryScreen(), "the mode-sync preamble still applies")
		})
	}
}

// Replayed (line-oriented) agents keep the replay and get no repaint or nudge.
func TestAttach_ReplayedAgentNoRepaint(t *testing.T) {
	sock, intents := legacyAttachDaemon(t, "s1", true)
	m := boardModel(protocol.SessionSummary{ID: "s1", Slug: "a", ToolID: "ollama"})
	m.Socket = sock
	m, _ = openAttach(m, "s1")
	require.NotNil(t, m.Attach, m.Err)
	st := m.Attach
	t.Cleanup(func() { _ = st.Client.Close() })
	_, ok := nextIntent(intents, 2*time.Second) // Resize
	require.True(t, ok)
	sub, ok := nextIntent(intents, 2*time.Second)
	require.True(t, ok)
	var p protocol.Subscribe
	require.NoError(t, json.Unmarshal(sub.Data, &p))
	require.Equal(t, protocol.Subscribe{SessionID: "s1", Replay: true, ModeSync: true}, p)
	if env, ok := nextIntent(intents, 2*client.RepaintReplyWait); ok {
		t.Fatalf("unexpected intent %s %s", env.Type, env.Data)
	}
}
