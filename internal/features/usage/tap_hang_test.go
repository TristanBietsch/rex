package usage

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The bound through the real dial path against a daemon that never accepts:
// the listener's backlog is filled first, so on systems where a further
// connect blocks it really does.
func TestRunTap_DaemonThatNeverAcceptsAddsAtMostTheCap(t *testing.T) {
	dir, err := os.MkdirTemp("", "tap")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	var held []net.Conn
	t.Cleanup(func() {
		_ = ln.Close()
		for _, c := range held {
			_ = c.Close()
		}
	})
	// Fill the accept queue (never accepted) until a connect stops being instant.
	for i := 0; i < 2048; i++ {
		d := net.Dialer{Timeout: 30 * time.Millisecond}
		start := time.Now()
		c, err := d.DialContext(context.Background(), "unix", sock)
		if err != nil || time.Since(start) > 20*time.Millisecond {
			break
		}
		held = append(held, c)
	}

	o := tapEnv(t, "")
	o.Socket = sock
	o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(statusJSON)), &bytes.Buffer{}, &bytes.Buffer{}
	start := time.Now()
	require.Zero(t, RunTap(context.Background(), o))
	require.Less(t, time.Since(start), 500*time.Millisecond)
}
