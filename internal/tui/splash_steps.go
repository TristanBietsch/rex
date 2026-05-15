package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/audio"
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

func stepSettingsLoad(_ Model) tea.Cmd {
	return emit("settings.load", func() (bootStatus, string, error) {
		path := settings.DefaultPath()
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return stepSkip, "no config file (defaults)", nil
		}
		s := settings.NewStore()
		if err := s.Load(path); err != nil {
			return stepWarn, fmt.Sprintf("%v (defaults)", err), err
		}
		return stepOK, path, nil
	})
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

// init keeps the stub bootSequence in place for now; Task 4.5 will swap to real Cmds.
// Stage 3's fakeStep helper is retained at the bottom for the still-stubbed steps.
func fakeStep(name, desc string) stepFunc {
	return func(_ Model) tea.Cmd {
		return tea.Tick(time.Millisecond, func(time.Time) tea.Msg {
			return bootStepMsg{Name: name, Status: stepOK, Desc: desc}
		})
	}
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
		// Remaining 7 steps still stubbed until Task 4.3:
		fakeStep("daemon", "already running (pid 0)"),
		fakeStep("client.dial", "connected · 0ms"),
		fakeStep("handshake", "接続 · rex-tui"),
		fakeStep("subscribe", "受信中 · event stream open"),
		fakeStep("snapshot.parse", "0 sessions"),
		fakeStep("state.restore", "first run"),
		fakeStep("renderer.warm", "styles cached"),
	}
}
