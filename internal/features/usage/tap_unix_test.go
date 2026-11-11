//go:build !windows

package usage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A signalled tap (Claude cancels the status line) kills the user's command
// and its children, and returns promptly.
func TestRunTap_CancelKillsTheUserCommandsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	o := tapEnv(t, "sleep 60 & echo $! > '"+pidFile+"'; wait")
	o.Stdin, o.Stdout, o.Stderr = bytes.NewReader([]byte(statusJSON)), &bytes.Buffer{}, &bytes.Buffer{}
	o.Send = func(context.Context, protocol.UsageSample) error { return nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- RunTap(ctx, o) }()

	var pid int
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(b)))
		pid = n
		return err == nil && n > 0
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the tap did not return after cancel")
	}
	require.Eventually(t, func() bool { return syscall.Kill(pid, 0) != nil }, 3*time.Second, 20*time.Millisecond,
		"the user command's child is still running")
}
