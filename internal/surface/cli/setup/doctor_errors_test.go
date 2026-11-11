package setup

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func TestCheckCLIErrors(t *testing.T) {
	t.Setenv("REX_LOG_DIR", t.TempDir())
	now := time.Now()
	r := checkCLIErrors(now)
	require.Equal(t, "cli errors", r.Name)
	require.Equal(t, checkInfo, r.Status)
	require.Equal(t, "none in 24h", r.Detail)

	opts := core.ErrLogOptions{Enabled: func() bool { return true }}
	opts.Now = func() time.Time { return now.Add(-30 * time.Hour) }
	core.RecordError([]string{"ls"}, core.NewExitError(core.ExitGeneric, "old"), opts)
	require.Equal(t, "none in 24h", checkCLIErrors(now).Detail, "older than a day does not count")

	opts.Now = func() time.Time { return now.Add(-time.Hour) }
	core.RecordError([]string{"ls"}, core.NewExitError(core.ExitGeneric, "a"), opts)
	core.RecordError([]string{"ls"}, core.NewExitError(core.ExitGeneric, "b"), opts)
	r = checkCLIErrors(now)
	require.Equal(t, checkWarn, r.Status)
	require.Equal(t, "2 in 24h · rex errors", r.Detail)
}

// The row is last, after every row other lanes append.
func TestRunAllChecksEndsWithCLIErrors(t *testing.T) {
	t.Setenv("REX_LOG_DIR", t.TempDir())
	withAgyVersion(t, func(ctx context.Context) (string, error) { return "", exec.ErrNotFound })
	checks := runAllChecks(&core.SocketFlag{})
	require.Equal(t, "cli errors", checks[len(checks)-1].Name)
	require.Equal(t, "go version", checks[len(checks)-2].Name)
}
