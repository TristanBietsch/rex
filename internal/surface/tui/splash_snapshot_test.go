package tui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestSplashInitialFrame(t *testing.T) {
	m := Model{
		Focus:     FocusBoot,
		Width:     80,
		Height:    24,
		BootStart: time.Now().Add(-470 * time.Millisecond), // 0.47s elapsed
	}
	out := renderSplash(m, m.Width, m.Height)
	lines := strings.Split(out, "\n")

	require.GreaterOrEqual(t, len(lines), 4)
	require.Contains(t, lines[1], "レックス")
	require.Contains(t, lines[1], "rex runtime executive")
	require.Contains(t, lines[2], "起動中")
	require.Contains(t, lines[2], "booting")
	// No log rows yet — the third row should be blank.
	require.Equal(t, "", strings.TrimSpace(lines[3]))
}

func TestSplashMidBoot(t *testing.T) {
	m := Model{
		Focus:     FocusBoot,
		Width:     80,
		Height:    24,
		BootStart: time.Now().Add(-300 * time.Millisecond),
		BootLog: []bootLine{
			{Name: "log.init", Status: stepOK, Desc: "~/.local/state/rex/tui.log"},
			{Name: "paths.ensure", Status: stepOK, Desc: "config + state dirs ok"},
			{Name: "tty.probe", Status: stepOK, Desc: "198×52 · truecolor · en_US.UTF-8"},
		},
	}
	out := renderSplash(m, m.Width, m.Height)
	require.Contains(t, out, "[")
	require.Contains(t, out, "OK")
	require.Contains(t, out, "log.init")
	require.Contains(t, out, "paths.ensure")
	require.Contains(t, out, "tty.probe")
	// No ready footer yet (BootDone=false).
	require.NotContains(t, out, "準備完了")
}

func TestSplashAllOKShowsReady(t *testing.T) {
	m := Model{
		Focus:       FocusBoot,
		Width:       80,
		Height:      30,
		BootStart:   time.Now().Add(-1340 * time.Millisecond),
		BootDone:    true,
		BootMinDone: false, // ready footer should show even before min elapsed
		BootLog: []bootLine{
			{Name: "log.init", Status: stepOK, Desc: "ok"},
			{Name: "handshake", Status: stepOK, Desc: "接続 · rex-tui"},
			{Name: "subscribe", Status: stepOK, Desc: "受信中 · stream open"},
		},
	}
	out := renderSplash(m, m.Width, m.Height)
	require.Contains(t, out, "準備完了 ready")
	require.Contains(t, out, "接続")
	require.Contains(t, out, "受信中")
}

// daemonFailModel is the splash after the daemon step failed with err.
func daemonFailModel(err error) Model {
	return Model{
		Focus:      FocusBoot,
		Width:      100,
		Height:     30,
		BootStart:  time.Now().Add(-2 * time.Second),
		BootFailed: true,
		BootError:  err,
		BootLog: []bootLine{
			{Name: "log.init", Status: stepOK, Desc: "ok"},
			{Name: "daemon", Status: stepFail, Desc: err.Error(), Err: err},
		},
	}
}

func TestSplashFailed(t *testing.T) {
	// What daemonctl.Start returns when rex-daemon is nowhere to be found.
	notFound := fmt.Errorf("spawn rex-daemon: %w", &exec.Error{Name: "rex-daemon", Err: exec.ErrNotFound})
	m := daemonFailModel(notFound)
	out := renderSplash(m, m.Width, m.Height)
	require.Contains(t, out, "起動失敗")
	require.Contains(t, out, "FAIL")
	require.Contains(t, out, "cause:")
	require.Contains(t, out, "rex-daemon")
	require.Contains(t, out, "fix:")
	require.Contains(t, out, "ensure rex-daemon is on PATH")
	require.Contains(t, out, "press q")
}

// A daemon that started and died has a cause in its log, not a PATH
// problem.
func TestSplashFailed_DaemonExitPointsAtLog(t *testing.T) {
	for _, err := range []error{
		errors.New("rex-daemon exited during startup (exit status 1): listen /x: bind: address already in use"),
		errors.New("rex-daemon (pid 42) is running but socket /x did not come up within 2s"),
	} {
		m := daemonFailModel(err)
		out := ansi.Strip(renderSplash(m, m.Width, m.Height))
		require.Contains(t, out, "cause: "+err.Error())
		require.Contains(t, out, "fix:   see daemon.log (rex daemon logs)")
		require.NotContains(t, out, "PATH")
	}
}
