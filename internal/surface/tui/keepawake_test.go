package tui

import (
	"errors"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests never run caffeinate: every displayAwake here has a fake
// launcher, and newDisplayAwake's is only inspected, never called.

func TestDisplayAwakeStartStopIdempotent(t *testing.T) {
	fl := &fakeLauncher{}
	d := &displayAwake{launch: fl.launch}

	d.Stop()
	require.Zero(t, fl.stops, "stop before start does nothing")

	d.Start()
	d.Start()
	require.Equal(t, 1, fl.starts, "one helper however often it is asked")
	require.True(t, d.holding())

	d.Stop()
	d.Stop()
	require.Equal(t, 1, fl.stops)
	require.False(t, d.holding())

	d.Start()
	require.Equal(t, 2, fl.starts, "starts again after a stop")
	d.Stop()
}

func TestDisplayAwakeLaunchFailure(t *testing.T) {
	fl := &fakeLauncher{err: errors.New("no caffeinate")}
	d := &displayAwake{launch: fl.launch}
	d.Start()
	require.False(t, d.holding())
	d.Stop()
	require.Zero(t, fl.stops)
}

func TestDisplayAwakeNoOps(t *testing.T) {
	var nilAwake *displayAwake
	nilAwake.Start()
	nilAwake.Stop()
	require.False(t, nilAwake.holding())

	unsupported := &displayAwake{}
	unsupported.Start()
	require.False(t, unsupported.holding(), "no launcher: nothing to hold")
	unsupported.Stop()
}

func TestNewDisplayAwakeOnlyOnMacOS(t *testing.T) {
	d := newDisplayAwake()
	require.Equal(t, runtime.GOOS == "darwin", d.launch != nil)
	require.False(t, d.holding())
}

func TestCaffeinateArgs(t *testing.T) {
	require.Equal(t, []string{"-d", "-w", "4242"}, caffeinateArgs(4242))
	require.Equal(t, "/usr/bin/caffeinate", caffeinatePath)
}
