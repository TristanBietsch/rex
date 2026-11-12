//go:build !windows

package registry

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The command runs in a neutral folder, not the daemon's working directory.
func TestDetectModels_NeutralDir(t *testing.T) {
	dir := t.TempDir()
	tool := stubTool(t, dir, "stub-pwd", `pwd -P`, ModelsCmd{Args: []string{"m"}})
	models, err := DetectModels(context.Background(), tool)
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(os.TempDir())
	require.NoError(t, err)
	require.Equal(t, []string{want}, ids(models))
}

// A timeout kills the whole process group: a grandchild that inherited the
// pipes must not outlive the command.
func TestDetectModels_TimeoutKillsGrandchild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "pid")
	tool := stubTool(t, dir, "stub-gc", "[ \"$1\" = --warm ] && exit 0\nsleep 60 &\necho $! > '"+pidFile+"'\nwait", ModelsCmd{Args: []string{"m"}, TimeoutMs: 2000})
	require.NoError(t, exec.Command(filepath.Join(dir, "stub-gc"), "--warm").Run()) // a fresh script's first run can be slow
	_, err := DetectModels(context.Background(), tool)
	require.ErrorContains(t, err, "timed out")
	raw, rerr := os.ReadFile(pidFile) //nolint:gosec // test path under t.TempDir()
	require.NoError(t, rerr)
	pid, perr := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, perr)
	require.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) != nil // gone
	}, 3*time.Second, 50*time.Millisecond, "grandchild %d still running", pid)
}
