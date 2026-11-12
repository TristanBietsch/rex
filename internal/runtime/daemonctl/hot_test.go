package daemonctl

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestInfoCountsLiveSessions(t *testing.T) {
	sock := socketIn(t)
	start := time.Now().UTC().Truncate(time.Second)
	startFakeDaemonWith(t, sock, fakeOpts{hot: &fakeHot{started: start, build: "old", sessions: []protocol.SessionSummary{
		{ID: "a", State: protocol.StateWorking}, {ID: "b", State: protocol.StateDone}, {ID: "c", State: protocol.StateNeedsInput},
	}}}, nil)
	d, err := Info(bg(), sock)
	require.NoError(t, err)
	require.Equal(t, DaemonInfo{PID: 42, Build: "old", StartedAt: start, Live: 2}, d)
	_, err = Info(bg(), socketIn(t))
	require.ErrorIs(t, err, ErrNotRunning)
}

func TestHotRestartWaitsForTheNewImage(t *testing.T) {
	sock := socketIn(t)
	start := time.Now().UTC().Truncate(time.Second)
	startFakeDaemonWith(t, sock, fakeOpts{hot: &fakeHot{started: start, build: "old"}}, nil)
	res, err := HotRestart(bg(), sock)
	require.NoError(t, err)
	require.Equal(t, "old", res.Before.Build)
	require.Equal(t, "new", res.After.Build)
	require.True(t, res.After.StartedAt.After(res.Before.StartedAt))
}

func TestHotRestartUnavailable(t *testing.T) {
	// A daemon from before hot restart: no daemon_started_at.
	sock := socketIn(t)
	startFakeDaemon(t, sock, nil)
	_, err := HotRestart(bg(), sock)
	require.ErrorIs(t, err, ErrHotUnavailable)
	require.ErrorContains(t, err, "predates")

	// A refusal names the reason.
	sock = socketIn(t)
	startFakeDaemonWith(t, sock, fakeOpts{hot: &fakeHot{started: time.Now(), refuse: "session(s) [ab] cannot be handed over yet"}}, nil)
	_, err = HotRestart(bg(), sock)
	require.ErrorIs(t, err, ErrHotUnavailable)
	require.ErrorContains(t, err, "cannot be handed over")

	_, err = HotRestart(bg(), socketIn(t))
	require.True(t, errors.Is(err, ErrNotRunning))
}

func TestBoardsAndSwapBoards(t *testing.T) {
	sock := socketIn(t)
	want := []protocol.BoardInfo{{PID: 7, Build: "old"}}
	startFakeDaemonWith(t, sock, fakeOpts{hot: &fakeHot{started: time.Now(), boards: want}}, nil)
	got, err := Boards(bg(), sock)
	require.NoError(t, err)
	require.Equal(t, want, got)
	got, err = SwapBoards(bg(), sock, "new")
	require.NoError(t, err)
	require.Equal(t, want, got)
	_, err = Boards(bg(), socketIn(t))
	require.ErrorIs(t, err, ErrNotRunning)
}
