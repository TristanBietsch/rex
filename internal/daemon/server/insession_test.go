package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Environment of the in-session helper process (see TestMain).
const (
	inSessionHelperEnv = "REX_TEST_IN_SESSION_SOCKET"
	inSessionIntentEnv = "REX_TEST_IN_SESSION_INTENT"
)

// runInSessionHelper dials sock, sends one intent of type intent with an
// ID and an empty payload, and prints the reply as "<event type> <error
// code>". It returns the process exit code.
func runInSessionHelper(sock, intent string) int {
	conn, err := net.Dial("unix", sock) //nolint:gosec // G704: the test daemon's socket, named by the parent test.
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		return 2
	}
	defer conn.Close()
	w := protocol.NewWriter(conn)
	if err := w.WriteIntent(intent, "req-1", struct{}{}); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		return 2
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	env, err := protocol.NewReader(conn).Read()
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		return 2
	}
	code := "-"
	if env.Type == protocol.EventError {
		var e protocol.ErrorEvent
		if err := json.Unmarshal(env.Data, &e); err == nil {
			code = e.Code
		}
	}
	fmt.Println(env.Type, code)
	return 0
}

// sendFromChild runs the test binary as a child process (a descendant of
// the in-process daemon) that sends intent to sock, and returns its reply
// as "<event type> <error code>".
func sendFromChild(t *testing.T, sock, intent string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // G204: this test binary.
	cmd.Env = append(os.Environ(), inSessionHelperEnv+"="+sock, inSessionIntentEnv+"="+intent)
	out, err := cmd.Output()
	require.NoError(t, err, "helper failed: %s", out)
	return strings.TrimSpace(string(out))
}

func TestInSessionPeer_ControlIntentsRefused(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("peer pids are read on linux and darwin only")
	}
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)

	// Every intent the daemon serves, so widening inSessionAllowed (or
	// adding an intent) cannot slip past this test.
	intents := make([]string, 0, len(defaultHandlers()))
	for intent := range defaultHandlers() {
		if intent != protocol.IntentUsageSample {
			intents = append(intents, intent)
		}
	}
	slices.Sort(intents)
	require.Contains(t, intents, protocol.IntentSendInput)
	for _, intent := range intents {
		t.Run(intent, func(t *testing.T) {
			require.Equal(t, protocol.EventError+" "+protocol.ErrCodeInSession, sendFromChild(t, sock, intent))
		})
	}

	// The daemon is still up and serves a client outside the session.
	conn := dialTest(t, sock)
	helloOnConn(t, conn)
}

func TestInSessionPeer_UsageSampleAllowed(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("peer pids are read on linux and darwin only")
	}
	srv, sock, _ := newTestServer(t, state.NewStore())
	srv.cfg.OnUsageSample = func(protocol.UsageSample) {}
	serveTest(t, srv)

	require.Equal(t, protocol.EventAck+" -", sendFromChild(t, sock, protocol.IntentUsageSample))
}
