package tui

import (
	"strings"
	"testing"
	"time"

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
