//go:build !windows

package loop

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A hung check is killed at its timeout, and so is the child it spawned.
func TestRunCheck_TimeoutKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	script := "sleep 300 & echo $! > " + pidFile + "; wait"
	start := time.Now()
	r := RunCheck(context.Background(), []string{"sh", "-c", script}, t.TempDir(), 500*time.Millisecond)
	require.True(t, r.TimedOut)
	require.Less(t, time.Since(start), 10*time.Second)

	b, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) == syscall.ESRCH
	}, 5*time.Second, 50*time.Millisecond, "the check's child must die with its group")
}
