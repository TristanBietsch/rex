package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// rex upgrade hot-restarts the daemon, asks the boards on another build
// than this rex to swap and reports each process: a board that came back on
// this rex's build is swapped, one that did not is still on the old build.
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
			return []protocol.BoardInfo{{PID: 20, Build: "rexnew"}, {PID: 30, Build: "old"}}, nil
		},
		wait: 50 * time.Millisecond, poll: 5 * time.Millisecond,
		self: "rexnew",
	}
	require.NoError(t, u.run())
	require.Equal(t, "rexnew", swapBuild, "boards follow this rex, not the daemon")
	out := f.out.String()
	require.Contains(t, out, "daemon pid 10: hot-restarted, 4 session(s) kept (build old → new)")
	require.Contains(t, out, "board pid 20: swapped to build rexnew")
	require.Contains(t, out, "board pid 30: still on build old")
	require.Empty(t, f.calls, "no cold stop or start")
}

func TestUpgrade_daemonDown(t *testing.T) {
	f := &fakeCtl{}
	u := upgrader{ctl: f.ctl(t)}
	requireExitCode(t, u.run(), 4)
}

// A daemon that cannot hot restart is cold-restarted (with consent), and
// no board means the report says so.
func TestUpgrade_coldFallbackNoBoards(t *testing.T) {
	f := &fakeCtl{up: true}
	c := f.ctl(t)
	c.yes = true
	c.hot = func(context.Context, string) (daemonctl.HotResult, error) {
		return daemonctl.HotResult{}, fmt.Errorf("%w: predates", daemonctl.ErrHotUnavailable)
	}
	c.info = func(context.Context, string) (daemonctl.DaemonInfo, error) {
		return daemonctl.DaemonInfo{PID: 11, Build: "new"}, nil
	}
	u := upgrader{
		ctl:    c,
		swap:   func(context.Context, string, string) ([]protocol.BoardInfo, error) { return nil, errors.New("no swap") },
		boards: func(context.Context, string) ([]protocol.BoardInfo, error) { return nil, nil },
		wait:   10 * time.Millisecond, poll: time.Millisecond,
	}
	require.NoError(t, u.run())
	require.Equal(t, []string{"shutdown", "start"}, f.calls)
	out := f.out.String()
	require.Contains(t, out, "daemon pid 11: cold-restarted (build new)")
	require.Contains(t, out, "no board was connected")
	require.Contains(t, f.errOut.String(), "predates")
}

// Any other hot restart error stops the upgrade.
func TestUpgrade_hotErrorStops(t *testing.T) {
	f := &fakeCtl{up: true}
	c := f.ctl(t)
	c.hot = func(context.Context, string) (daemonctl.HotResult, error) {
		return daemonctl.HotResult{}, errors.New("did not come back")
	}
	requireExitCode(t, upgrader{ctl: c}.run(), 1)
}

func TestRunUpgrade_badFlag(t *testing.T) {
	require.Error(t, RunUpgrade([]string{"-bogus"}))
}

// A hot restart drops every board. The swap is asked only once the boards
// listed before it have reconnected, or none would hear it.
func TestUpgrade_swapWaitsForBoardsToReconnect(t *testing.T) {
	f := &fakeCtl{up: true}
	c := f.ctl(t)
	c.hot = func(context.Context, string) (daemonctl.HotResult, error) {
		return daemonctl.HotResult{After: daemonctl.DaemonInfo{PID: 10, Build: "d2"}}, nil
	}
	calls, swapped := 0, false
	u := upgrader{
		ctl: c,
		boards: func(context.Context, string) ([]protocol.BoardInfo, error) {
			calls++
			switch {
			case swapped:
				return []protocol.BoardInfo{{PID: 20, Build: "r2"}}, nil
			case calls == 1 || calls >= 4: // before the restart, then back
				return []protocol.BoardInfo{{PID: 20, Build: "r1"}}, nil
			}
			return nil, nil // still redialing
		},
		swap: func(_ context.Context, _ string, build string) ([]protocol.BoardInfo, error) {
			require.GreaterOrEqual(t, calls, 4, "swap asked before the board was back")
			swapped = true
			return []protocol.BoardInfo{{PID: 20, Build: "r1"}}, nil
		},
		wait: time.Second, poll: time.Millisecond, self: "r2",
	}
	require.NoError(t, u.run())
	require.Contains(t, f.out.String(), "board pid 20: swapped to build r2")
}

func hotRefused(context.Context, string) (daemonctl.HotResult, error) {
	return daemonctl.HotResult{}, fmt.Errorf("%w: a restart is already running", daemonctl.ErrHotUnavailable)
}

func noBoards(context.Context, string) ([]protocol.BoardInfo, error) { return nil, nil }

// -no-cold never stops the daemon: a refused or impossible hot restart
// exits 6 with nothing restarted; an error after the exec stays 1.
func TestUpgrade_noCold(t *testing.T) {
	f := &fakeCtl{up: true}
	c := f.ctl(t)
	c.yes = true
	c.hot = hotRefused
	err := upgrader{ctl: c, boards: noBoards, noCold: true}.run()
	requireExitCode(t, err, 6)
	require.Contains(t, err.Error(), "already running")
	require.Empty(t, f.calls, "no cold stop or start")

	c.hot = nil // no hot restart on this platform
	requireExitCode(t, upgrader{ctl: c, boards: noBoards, noCold: true}.run(), 6)
	require.Empty(t, f.calls)

	c.hot = func(context.Context, string) (daemonctl.HotResult, error) {
		return daemonctl.HotResult{}, errors.New("did not come back")
	}
	requireExitCode(t, upgrader{ctl: c, boards: noBoards, noCold: true}.run(), 1)
	require.Empty(t, f.calls)
}

func TestRunUpgrade_coldAndNoColdContradict(t *testing.T) {
	requireExitCode(t, RunUpgrade([]string{"-cold", "-no-cold"}), 5)
}

// -if-stale leaves a daemon on the installed build alone; with every board
// on this rex's build there is nothing to do.
func TestUpgrade_ifStaleNothingToDo(t *testing.T) {
	f := &fakeCtl{up: true}
	c := f.ctl(t)
	c.hot = func(context.Context, string) (daemonctl.HotResult, error) {
		t.Fatal("a current daemon must not restart")
		return daemonctl.HotResult{}, nil
	}
	c.info = func(context.Context, string) (daemonctl.DaemonInfo, error) {
		return daemonctl.DaemonInfo{PID: 10, Build: "d1"}, nil
	}
	u := upgrader{
		ctl: c,
		boards: func(context.Context, string) ([]protocol.BoardInfo, error) {
			return []protocol.BoardInfo{{PID: 20, Build: "r1"}}, nil
		},
		swap: func(context.Context, string, string) ([]protocol.BoardInfo, error) { return nil, nil },
		wait: 10 * time.Millisecond, poll: time.Millisecond,
		self:      "r1",
		installed: func() string { return "d1" },
		ifStale:   true,
	}
	require.NoError(t, u.run())
	require.Contains(t, f.out.String(), "nothing to do: daemon on build d1, 1 board(s) on build r1")
}

// -if-stale with a current daemon still swaps a board on an older rex.
func TestUpgrade_ifStaleSwapsStaleBoards(t *testing.T) {
	f := &fakeCtl{up: true}
	c := f.ctl(t)
	c.info = func(context.Context, string) (daemonctl.DaemonInfo, error) {
		return daemonctl.DaemonInfo{PID: 10, Build: "d1"}, nil
	}
	swapped := false
	u := upgrader{
		ctl: c,
		boards: func(context.Context, string) ([]protocol.BoardInfo, error) {
			if swapped {
				return []protocol.BoardInfo{{PID: 20, Build: "r2"}}, nil
			}
			return []protocol.BoardInfo{{PID: 20, Build: "r1"}}, nil
		},
		swap: func(context.Context, string, string) ([]protocol.BoardInfo, error) {
			swapped = true
			return []protocol.BoardInfo{{PID: 20, Build: "r1"}}, nil
		},
		wait: time.Second, poll: time.Millisecond,
		self: "r2", installed: func() string { return "d1" }, ifStale: true,
	}
	require.NoError(t, u.run())
	out := f.out.String()
	require.Contains(t, out, "daemon pid 10: already on build d1")
	require.Contains(t, out, "board pid 20: swapped to build r2")
}

// An unknown installed build is never "current": the daemon restarts.
func TestUpgrade_ifStaleUnknownInstalledRestarts(t *testing.T) {
	f := &fakeCtl{up: true}
	c := f.ctl(t)
	hot := false
	c.hot = func(context.Context, string) (daemonctl.HotResult, error) {
		hot = true
		return daemonctl.HotResult{After: daemonctl.DaemonInfo{PID: 10}}, nil
	}
	c.info = func(context.Context, string) (daemonctl.DaemonInfo, error) {
		return daemonctl.DaemonInfo{PID: 10}, nil
	}
	u := upgrader{ctl: c, boards: noBoards, swap: func(context.Context, string, string) ([]protocol.BoardInfo, error) { return nil, nil },
		installed: func() string { return "" }, ifStale: true}
	require.NoError(t, u.run())
	require.True(t, hot)
}

// A daemon started from another path re-executes that path: the restart
// is reported, then the upgrade fails naming the installed build.
func TestUpgrade_verifyInstalledBuild(t *testing.T) {
	f := &fakeCtl{up: true}
	c := f.ctl(t)
	c.hot = func(context.Context, string) (daemonctl.HotResult, error) {
		return daemonctl.HotResult{Before: daemonctl.DaemonInfo{Build: "d0"}, After: daemonctl.DaemonInfo{PID: 10, Build: "d0"}}, nil
	}
	u := upgrader{ctl: c, boards: noBoards, swap: func(context.Context, string, string) ([]protocol.BoardInfo, error) { return nil, nil },
		installed: func() string { return "d1" }}
	err := u.run()
	requireExitCode(t, err, 1)
	require.Contains(t, err.Error(), "is build d1")
	require.Contains(t, f.out.String(), "daemon pid 10: hot-restarted")
}
