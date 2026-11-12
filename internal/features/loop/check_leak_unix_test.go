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

// A check that exits 0 but leaves a child holding its output is a pass, not
// an error, and the leftover child is killed.
func TestRunCheck_LeakedChildStillPasses(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	script := "sleep 300 & echo $! > " + pidFile + "; echo done; exit 0"
	r := RunCheck(context.Background(), []string{"sh", "-c", script}, t.TempDir(), time.Minute)
	require.NoError(t, r.Err)
	require.True(t, r.Passed(), "exit 0 is a pass")
	require.True(t, r.Leaked)
	require.Contains(t, string(r.Output), "done")

	b, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, 5*time.Second, 50*time.Millisecond)
}
