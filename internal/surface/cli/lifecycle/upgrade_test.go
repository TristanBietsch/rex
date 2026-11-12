package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// rex upgrade hot-restarts the daemon, asks the boards on another build to
// swap and reports each process: a board that came back on the new build
// is swapped, one that did not is still on the old build.
func TestUpgrade_reportsEachProcess(t *testing.T) {
	f := &fakeCtl{up: true}
	c := f.ctl(t)
	c.hot = func(context.Context, string) (daemonctl.HotResult, error) {
		return daemonctl.HotResult{
			Before: daemonctl.DaemonInfo{PID: 10, Build: "old"},
			After:  daemonctl.DaemonInfo{PID: 10, Build: "new", Live: 4},
		}, nil
	}
	var swapBuild string
	u := upgrader{
		ctl: c,
		swap: func(_ context.Context, _ string, build string) ([]protocol.BoardInfo, error) {
			swapBuild = build
			return []protocol.BoardInfo{{PID: 20, Build: "old"}, {PID: 30, Build: "old"}}, nil
		},
		boards: func(context.Context, string) ([]protocol.BoardInfo, error) {
			return []protocol.BoardInfo{{PID: 20, Build: "new"}, {PID: 30, Build: "old"}}, nil
		},
		wait: 50 * time.Millisecond, poll: 5 * time.Millisecond,
	}
	require.NoError(t, u.run())
	require.Equal(t, "new", swapBuild)
	out := f.out.String()
	require.Contains(t, out, "daemon pid 10: hot-restarted, 4 session(s) kept (build old → new)")
	require.Contains(t, out, "board pid 20: swapped to build new")
	require.Contains(t, out, "board pid 30: still on build old")
	require.Empty(t, f.calls, "no cold stop or start")
}

func TestUpgrade_daemonDown(t *testing.T) {
	f := &fakeCtl{}
	u := upgrader{ctl: f.ctl(t)}
	requireExitCode(t, u.run(), 4)
}
