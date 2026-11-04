package setup

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
)

func TestStopAndWait_notRunning(t *testing.T) {
	d := &fakeDaemon{stopErr: daemonctl.ErrNotRunning}
	require.NoError(t, d.control().stopAndWait())
	require.Equal(t, 1, d.stopCount())
}

// FU-11: the stop request goes to the daemon on the socket this control
// probes, as a Shutdown intent, never to a process found by name.
func TestStopAndWait_stopsTheDaemonOnItsSocket(t *testing.T) {
	d := &fakeDaemon{up: true}
	require.NoError(t, d.control().stopAndWait())
	require.Equal(t, []string{"fake.sock"}, d.stoppedSockets)
}

// The socket can keep answering for a moment after the stop request while
// the daemon persists state and exits. stopAndWait must wait for that.
func TestStopAndWait_waitsForSocketToClose(t *testing.T) {
	d := &fakeDaemon{up: true, downAfter: 3}
	require.NoError(t, d.control().stopAndWait())
	require.False(t, d.up)
	require.GreaterOrEqual(t, d.probes, 4, "returned before the socket stopped answering")
}

func TestStopAndWait_timesOut(t *testing.T) {
	d := &fakeDaemon{up: true, stubborn: true}
	err := d.control().stopAndWait()
	require.ErrorIs(t, err, errDaemonStillRunning)
	require.Contains(t, err.Error(), "fake.sock")
}

func TestStopAndWait_reportsStopError(t *testing.T) {
	boom := errors.New("send Shutdown to rex-daemon: broken pipe")
	d := &fakeDaemon{up: true, stopErr: boom}
	err := d.control().stopAndWait()
	require.ErrorIs(t, err, errDaemonStillRunning)
	require.ErrorIs(t, err, boom)
}

// A failed stop request is fine if the daemon is gone anyway (it exited on
// its own between the probe and the request).
func TestStopAndWait_stopErrorButGone(t *testing.T) {
	d := &fakeDaemon{stopErr: errors.New("connection reset by peer")}
	require.NoError(t, d.control().stopAndWait())
}
