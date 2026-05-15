package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/daemonctl"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/rexlog"
	"github.com/tristanbietsch/rex/internal/settings"
)

// emit wraps a synchronous step in a tea.Cmd.
func emit(name string, fn func() (bootStatus, string, error)) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		status, desc, err := fn()
		return bootStepMsg{
			Name:   name,
			Status: status,
			Desc:   desc,
			Dur:    time.Since(t0),
			Err:    err,
		}
	}
}

func stepLogInit(_ Model) tea.Cmd {
	return emit("log.init", func() (bootStatus, string, error) {
		rexlog.Init("tui")
		home, _ := os.UserHomeDir()
		return stepOK, filepath.Join(home, ".local/state/rex/tui.log"), nil
	})
}

func stepPathsEnsure(_ Model) tea.Cmd {
	return emit("paths.ensure", func() (bootStatus, string, error) {
		home, _ := os.UserHomeDir()
		paths := []string{
			filepath.Join(home, ".config", "rex"),
			filepath.Join(home, ".local", "state", "rex"),
		}
		for _, p := range paths {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return stepFail, fmt.Sprintf("mkdir %s: %v", p, err), err
			}
		}
		return stepOK, "config + state dirs ok", nil
	})
}

func stepTTYProbe(m Model) tea.Cmd {
	return emit("tty.probe", func() (bootStatus, string, error) {
		lc := os.Getenv("LC_ALL")
		if lc == "" {
			lc = os.Getenv("LANG")
		}
		if lc == "" {
			lc = "C"
		}
		color := os.Getenv("COLORTERM")
		if color == "" {
			color = "256-color"
		}
		if !strings.Contains(strings.ToLower(lc), "utf-8") && !strings.Contains(strings.ToLower(lc), "utf8") {
			return stepWarn, fmt.Sprintf("%dx%d · %s · %s (ascii fallback)", m.Width, m.Height, color, lc), nil
		}
		return stepOK, fmt.Sprintf("%dx%d · %s · %s", m.Width, m.Height, color, lc), nil
	})
}

type settingsResultMsg struct {
	Store *settings.Store
	Path  string
	Err   error
	Found bool
	Dur   time.Duration
}

func stepSettingsLoad(_ Model) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		path := settings.DefaultPath()
		s := settings.NewStore()
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return settingsResultMsg{Store: s, Path: path, Found: false, Dur: time.Since(t0)}
		}
		err := s.Load(path)
		return settingsResultMsg{Store: s, Path: path, Found: true, Err: err, Dur: time.Since(t0)}
	}
}

func stepThemeApply(m Model) tea.Cmd {
	return emit("theme.apply", func() (bootStatus, string, error) {
		if m.Store == nil {
			return stepSkip, "default", nil
		}
		scheme, _ := m.Store.Get("color_scheme").(string)
		if scheme == "" {
			return stepSkip, "default", nil
		}
		applyTheme(scheme)
		return stepOK, scheme, nil
	})
}

func stepAudioInit(m Model) tea.Cmd {
	return emit("audio.init", func() (bootStatus, string, error) {
		if m.Store == nil || m.Audio == nil {
			return stepSkip, "no audio configured", nil
		}
		enabled, _ := m.Store.Get("sound_enabled").(bool)
		soundset, _ := m.Store.Get("soundset").(string)
		vol, _ := m.Store.Get("master_volume").(float64)
		if !enabled || soundset == "off" {
			return stepSkip, "muted", nil
		}
		return stepOK, fmt.Sprintf("soundset=%s · vol=%.2f", soundset, vol), nil
	})
}

func stepAudioLoad(m Model) tea.Cmd {
	return emit("audio.load", func() (bootStatus, string, error) {
		if m.Store == nil || m.Audio == nil {
			return stepSkip, "muted", nil
		}
		soundset, _ := m.Store.Get("soundset").(string)
		if soundset == "" || soundset == "off" {
			return stepSkip, "muted", nil
		}
		// Re-apply soundset on the live player.
		if p, ok := m.Audio.(*audio.Player); ok {
			p.SetSoundset(soundset)
		}
		return stepOK, fmt.Sprintf("%s · 起動準備", soundset), nil
	})
}

func stepRegistryLoad(_ Model) tea.Cmd {
	return emit("registry.load", func() (bootStatus, string, error) {
		reg, err := registry.Load("")
		if err != nil {
			return stepFail, err.Error(), err
		}
		enabled, hidden := 0, 0
		for _, t := range reg.Tools {
			if t.EnabledByDefault != nil && *t.EnabledByDefault {
				enabled++
			} else {
				hidden++
			}
		}
		desc := fmt.Sprintf("%d tools (%d enabled · %d hidden)", len(reg.Tools), enabled, hidden)
		return stepOK, desc, nil
	})
}

func stepKeymapBind(_ Model) tea.Cmd {
	return emit("keymap.bind", func() (bootStatus, string, error) {
		return stepOK, "global + board + wizard bindings ready", nil
	})
}

func stepSocketResolve(m Model) tea.Cmd {
	return emit("socket.resolve", func() (bootStatus, string, error) {
		if m.Socket == "" {
			return stepFail, "socket path empty", fmt.Errorf("socket path empty")
		}
		return stepOK, m.Socket, nil
	})
}

func init() {
	bootSequence = []stepFunc{
		stepLogInit,
		stepPathsEnsure,
		stepTTYProbe,
		stepSettingsLoad,
		stepThemeApply,
		stepAudioInit,
		stepAudioLoad,
		stepRegistryLoad,
		stepKeymapBind,
		stepSocketResolve,
		stepDaemon,
		stepClientDial,
		stepHandshake,
		stepSubscribe,
		stepSnapshotParse,
		stepStateRestore,
		stepRendererWarm,
	}
}

func stepDaemon(m Model) tea.Cmd {
	return emit("daemon", func() (bootStatus, string, error) {
		if daemonctl.Reachable(m.Socket) {
			return stepOK, "already running", nil
		}
		home, _ := os.UserHomeDir()
		logPath := filepath.Join(home, ".local", "state", "rex", "daemon.log")
		logf, _ := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		res, err := daemonctl.Start(m.Socket, logf)
		if err != nil {
			return stepFail, err.Error(), err
		}
		return stepOK, fmt.Sprintf("started · pid %d · %s", res.PID, res.Elapsed.Truncate(time.Millisecond)), nil
	})
}

// dialResultMsg carries the dialed client back into the model so later steps can use it.
type dialResultMsg struct {
	C   *client.Client
	Err error
	Dur time.Duration
}

func stepClientDial(m Model) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		c, err := client.Dial(m.Socket)
		return dialResultMsg{C: c, Err: err, Dur: time.Since(t0)}
	}
}

// snapshotResultMsg carries the snapshot back so snapshot.parse can compute counts.
type snapshotResultMsg struct {
	Snap *protocol.Snapshot
	Err  error
	Dur  time.Duration
}

func stepHandshake(m Model) tea.Cmd {
	return func() tea.Msg {
		if m.Client == nil {
			return bootStepMsg{Name: "handshake", Status: stepFail, Err: fmt.Errorf("client not dialed")}
		}
		t0 := time.Now()
		snap, err := m.Client.Hello("rex-tui")
		return snapshotResultMsg{Snap: snap, Err: err, Dur: time.Since(t0)}
	}
}

func stepSubscribe(m Model) tea.Cmd {
	return emit("subscribe", func() (bootStatus, string, error) {
		if m.Client == nil {
			return stepFail, "client not dialed", fmt.Errorf("client not dialed")
		}
		if err := m.Client.Subscribe(""); err != nil {
			return stepFail, err.Error(), err
		}
		return stepOK, "受信中 · event stream open", nil
	})
}

func stepSnapshotParse(m Model) tea.Cmd {
	return emit("snapshot.parse", func() (bootStatus, string, error) {
		if len(m.Sessions) == 0 {
			return stepSkip, "no sessions yet", nil
		}
		needs, work, done := 0, 0, 0
		for _, s := range m.Sessions {
			switch s.State {
			case protocol.StateNeedsInput:
				needs++
			case protocol.StateWorking, protocol.StateQueued:
				work++
			case protocol.StateDone:
				done++
			}
		}
		desc := fmt.Sprintf("%d sessions · %d needs · %d work · %d done", len(m.Sessions), needs, work, done)
		return stepOK, desc, nil
	})
}

func stepStateRestore(_ Model) tea.Cmd {
	return emit("state.restore", func() (bootStatus, string, error) {
		sel, filt, ok := LoadTUIState()
		if !ok {
			return stepSkip, "first run", nil
		}
		return stepOK, fmt.Sprintf("selected=%s · filter=%s", short8(sel), filt), nil
	})
}

func short8(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

func stepRendererWarm(_ Model) tea.Cmd {
	return emit("renderer.warm", func() (bootStatus, string, error) {
		rebuildStyles()
		return stepOK, "styles cached", nil
	})
}
