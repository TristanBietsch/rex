package client_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// sizeTools registers an agent that prints its PTY size ("rows cols") and
// waits.
const sizeTools = `tools:
  - id: echo
    models:
      - id: size
        name: "Prints its PTY size"
        args: ["stty size; sleep 20"]
`

// ansiSeq matches the CSI and OSC sequences a terminal (ConPTY) may write
// ahead of the agent's own output.
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\a]*\a`)

// ptySizeOf spawns the size agent with req's Cols/Rows and returns the
// "rows cols" line it printed.
func ptySizeOf(t *testing.T, d *testutil.Daemon, slug string, cols, rows uint16) string {
	t.Helper()
	c := dialHello(t, d.Socket)
	require.NoError(t, c.NewSession(protocol.NewSession{
		ToolID: "echo", ModelID: "size", Slug: slug, CWD: d.StateDir, Cols: cols, Rows: rows,
	}))
	id := waitSessionAdded(t, c)
	require.NoError(t, c.SubscribeReplay(id))
	var out strings.Builder
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		env, err := c.NextEvent()
		if err != nil {
			continue
		}
		if env.Type != protocol.EventSessionOutput {
			continue
		}
		var so protocol.SessionOutput
		require.NoError(t, json.Unmarshal(env.Data, &so))
		out.Write(so.Bytes)
		if line, ok := strings.CutSuffix(strings.SplitN(out.String(), "\n", 2)[0], "\r"); ok && strings.Contains(out.String(), "\n") {
			require.NoError(t, c.Delete(id))
			// ConPTY opens with mode and clear-screen sequences of its own.
			return ansiSeq.ReplaceAllString(line, "")
		}
	}
	t.Fatalf("agent never printed its size; got %q", out.String())
	return ""
}

func TestSpawnUsesRequestedSize(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and PTY children")
	}
	d := testutil.StartDaemonWithTools(t, sizeTools)
	defer d.Close()
	tests := []struct {
		name       string
		cols, rows uint16
		want       string
	}{
		{"requested", 177, 44, "44 177"},
		{"zero is default", 0, 0, "32 120"},
		{"huge is clamped", 65535, 65535, "500 1000"},
		{"tiny is clamped", 1, 1, "5 20"},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ptySizeOf(t, d, fmt.Sprintf("sz-%d", i), tc.cols, tc.rows))
		})
	}
}
