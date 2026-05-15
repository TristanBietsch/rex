package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeStep returns a stepFunc that synthesizes a successful bootStepMsg after a
// tiny tick. Used during Stage 3 to validate the splash visuals + audio without
// the real boot pipeline yet.
func fakeStep(name, desc string) stepFunc {
	return func(_ Model) tea.Cmd {
		return tea.Tick(time.Millisecond, func(time.Time) tea.Msg {
			return bootStepMsg{Name: name, Status: stepOK, Desc: desc}
		})
	}
}

func init() {
	bootSequence = []stepFunc{
		fakeStep("log.init", "~/.local/state/rex/tui.log"),
		fakeStep("paths.ensure", "config + state dirs ok"),
		fakeStep("tty.probe", "198×52 · truecolor · en_US.UTF-8"),
		fakeStep("settings.load", "~/.config/rex/config.yaml"),
		fakeStep("theme.apply", "default"),
		fakeStep("audio.init", "soundset=factorio · vol=0.80"),
		fakeStep("audio.load", "12 cues · 起動準備"),
		fakeStep("registry.load", "8 tools (5 enabled · 3 hidden)"),
		fakeStep("keymap.bind", "42 bindings"),
		fakeStep("socket.resolve", "/tmp/rex/rex.sock"),
		fakeStep("daemon", "already running (pid 0)"),
		fakeStep("client.dial", "connected · 0ms"),
		fakeStep("handshake", "接続 · rex-tui"),
		fakeStep("subscribe", "受信中 · event stream open"),
		fakeStep("snapshot.parse", "0 sessions"),
		fakeStep("state.restore", "first run"),
		fakeStep("renderer.warm", "styles cached"),
	}
}
