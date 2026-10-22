package tui

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// wheelEchoTools registers a fake agent that behaves like Claude Code's
// fullscreen mode as far as the mouse goes (alt screen + button tracking +
// SGR reports), then echoes every byte it reads as "x%02x".
const wheelEchoTools = `tools:
  - id: echo
    models:
      - id: wheelecho
        name: "Mouse-tracking hex echo"
        args: ["stty raw -echo; printf '\\033[?1049h\\033[?1000h\\033[?1006h'; LC_ALL=C; while IFS= read -r -s -n1 -d '' c; do printf 'x%02x' \"'$c\"; done"]
`

func hexEcho(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		sb.WriteString("x" + hex.EncodeToString([]byte{c}))
	}
	return sb.String()
}

// nextAttachMsg runs the popup's listen command once; nil on timeout.
func nextAttachMsg(st *AttachState, timeout time.Duration) tea.Msg {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- listenAttach(st)() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

// TestAttachE2E_WheelReachesTrackingAgent drives the real attach popup
// against a temp daemon: the wheel over a mouse-tracking agent must arrive
// at the child's PTY as an SGR report, byte for byte.
func TestAttachE2E_WheelReachesTrackingAgent(t *testing.T) {
	m, sum := startEchoAttach(t)

	// Feed output until the emulator sees the agent's mouse tracking (the
	// mode-sync preamble or the replayed escapes, whichever lands first).
	deadline := time.Now().Add(5 * time.Second)
	for {
		msg := nextAttachMsg(m.Attach, time.Until(deadline))
		require.NotNil(t, msg, "agent never enabled mouse tracking")
		out, _ := m.Update(msg)
		m = out.(Model)
		m.Attach.Term.Lock()
		mode := m.Attach.Term.Mode()
		m.Attach.Term.Unlock()
		if mode&vt10x.ModeMouseSgr != 0 && mode&vt10x.ModeMouseButton != 0 {
			break
		}
	}

	ox, oy := attachInnerOrigin(m)
	wheel := tea.MouseMsg{X: ox + 4, Y: oy + 2, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}
	m, cmd := updateMouse(m, wheel)
	require.NotNil(t, cmd)
	m, _ = updateMouse(m, wheel)
	m, _ = flushAttachWheel(m, attachWheelFlushMsg{SessionID: sum.ID, Seq: m.Attach.wheelSeq})
	require.Empty(t, m.Err)

	waitEcho(t, m, []byte(strings.Repeat("\x1b[<64;5;3M", 2)))
}

// TestAttachE2E_HeldRunesReachAgent: a held 'q' or ':' that isn't followed
// by its twin still reaches the agent, in order; "qq" detaches.
func TestAttachE2E_HeldRunesReachAgent(t *testing.T) {
	m, _ := startEchoAttach(t)
	for _, k := range []string{"q", "x", ":", "q", "y"} {
		out, _ := m.Update(key(k))
		m = out.(Model)
	}
	require.Empty(t, m.Err)
	require.Zero(t, m.Attach.held)
	waitEcho(t, m, []byte("qx:qy"))

	for _, k := range []string{"q", "q"} {
		out, _ := m.Update(key(k))
		m = out.(Model)
	}
	require.Nil(t, m.Attach, "qq detaches")
	require.Equal(t, FocusBoard, m.Focus)
}

// startEchoAttach spawns the hex-echo agent in a temp daemon and opens the
// attach popup on it.
func startEchoAttach(t *testing.T) (Model, protocol.SessionSummary) {
	t.Helper()
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	d := testutil.StartDaemonWithTools(t, wheelEchoTools)
	t.Cleanup(d.Close)

	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.NewSession(protocol.NewSession{ToolID: "echo", ModelID: "wheelecho", Slug: "wheel", CWD: d.StateDir}))
	var sum protocol.SessionSummary
	require.Eventually(t, func() bool {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		env, err := c.NextEvent()
		if err != nil || env.Type != protocol.EventSessionAdded {
			return false
		}
		return json.Unmarshal(env.Data, &sum) == nil
	}, 5*time.Second, 10*time.Millisecond)

	m := boardModel(sum)
	m.Socket = d.Socket
	m.Width, m.Height = 120, 40
	m, _ = openAttach(m, sum.ID)
	require.NotNil(t, m.Attach, "attach failed: %s", m.Err)
	st := m.Attach
	t.Cleanup(func() { _ = st.Client.Close() }) // a qq detach may have closed it already
	return m, sum
}

// waitEcho reads popup output until the agent has echoed b.
func waitEcho(t *testing.T, m Model, b []byte) {
	t.Helper()
	want := hexEcho(b)
	var seen strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(seen.String(), want) {
		msg := nextAttachMsg(m.Attach, time.Until(deadline))
		require.NotNil(t, msg, "agent echoed %q, want it to contain %q", seen.String(), want)
		if o, ok := msg.(AttachOutputMsg); ok {
			seen.Write(o.Bytes)
		}
	}
}
