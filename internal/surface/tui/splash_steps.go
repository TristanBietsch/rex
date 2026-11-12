package tui

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
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
		path, err := rexlog.Path("tui")
		if err != nil {
			return stepWarn, "logging to stderr: " + err.Error(), err
		}
		return stepOK, path, nil
	})
}

func stepPathsEnsure(_ Model) tea.Cmd {
	return emit("paths.ensure", func() (bootStatus, string, error) {
		l, err := paths.Resolve()
		if err != nil {
			return stepFail, err.Error(), err
		}
		// The config dir holds files the user edits, so an existing one is
		// left as they made it; a new one is 0700 (the XDG mode).
		if err := os.MkdirAll(l.ConfigDir, 0o700); err != nil {
			return stepFail, fmt.Sprintf("mkdir %s: %v", l.ConfigDir, err), err
		}
		// The state dir (logs, tui-state) is rex's own: created or
		// tightened to 0700.
		if err := rexlog.PrivateDir(l.LogDir); err != nil {
			return stepFail, err.Error(), err
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
		s := settings.NewStore()
		path, err := paths.ConfigFile()
		if err != nil {
			return settingsResultMsg{Store: s, Found: true, Err: err, Dur: time.Since(t0)}
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return settingsResultMsg{Store: s, Path: path, Found: false, Dur: time.Since(t0)}
		}
		err = s.Load(path)
		return settingsResultMsg{Store: s, Path: path, Found: true, Err: err, Dur: time.Since(t0)}
	}
}

// themeApplyMsg asks Update to apply the color scheme. The palette is
// global state the View reads, so it is only written on the UI goroutine,
// never from a step Cmd.
type themeApplyMsg struct {
	Scheme string // "": nothing configured, keep the default
	Dur    time.Duration
}

func stepThemeApply(m Model) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		scheme := ""
		if m.Store != nil {
			scheme, _ = m.Store.Get("theme").(string)
		}
		return themeApplyMsg{Scheme: scheme, Dur: time.Since(t0)}
	}
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
			m.Audio.SetEnabled(false)
			return stepSkip, "muted", nil
		}
		m.Audio.SetEnabled(true)
		m.Audio.SetVolume(vol)
		voice, _ := m.Store.Get("boot_voice").(bool)
		m.Audio.SetBootVoice(voice)
		slog.Info("tui: audio init", "soundset", soundset, "volume", vol, "boot_voice", voice)
		return stepOK, fmt.Sprintf("soundset=%s · vol=%.2f · voice=%t", soundset, vol, voice), nil
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
		m.Audio.SetSoundset(soundset)
		return stepOK, fmt.Sprintf("%s · 起動準備", soundset), nil
	})
}

// stepRegistryLoad validates the tool registry the wizard and daemon will
// use: the built-ins merged with the user's tools.yaml. Checking only the
// built-ins let a broken tools.yaml pass boot and fail later. A bad user
// file is a warning (the board works on built-ins and the daemon has its
// own copy); a bad built-in registry is fatal.
func stepRegistryLoad(_ Model) tea.Cmd {
	return emit("registry.load", func() (bootStatus, string, error) {
		reg, _, userErr, err := loadUserTools()
		return registryLoadStatus(reg, userErr, err)
	})
}

// registryLoadStatus maps a registry load outcome to a splash step result.
func registryLoadStatus(reg *registry.Registry, userErr, err error) (bootStatus, string, error) {
	if err != nil {
		return stepFail, err.Error(), err
	}
	if userErr != nil {
		// Validation errors don't name the file; say which one to fix.
		return stepWarn, fmt.Sprintf("tools.yaml ignored, using built-in tools: %v", userErr), userErr
	}
	if len(reg.Warnings) > 0 {
		return stepWarn, strings.Join(reg.Warnings, "; "), nil
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

// startDaemon spawns rex-daemon for the boot splash and the setup wizard.
// Tests replace it so no real daemon is spawned.
var startDaemon = daemonctl.Start

func stepDaemon(m Model) tea.Cmd {
	return emit("daemon", func() (bootStatus, string, error) {
		if daemonctl.Reachable(m.Socket) {
			return stepOK, "already running", nil
		}
		// The daemon's stderr log is owner-only (it carries session paths
		// and agent output). Without it a crash at startup leaves nothing
		// to read, so failing to open it fails the step.
		logf, err := daemonctl.OpenLog()
		if err != nil {
			return stepFail, err.Error(), err
		}
		// The child gets its own descriptor; ours is only for the spawn.
		defer logf.Close()
		res, err := startDaemon(m.Socket, logf)
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
			case protocol.StateFailed, protocol.StateCrashed:
				// Not counted: the snapshot line reports needs, work and done.
			}
		}
		desc := fmt.Sprintf("%d sessions · %d needs · %d work · %d done", len(m.Sessions), needs, work, done)
		return stepOK, desc, nil
	})
}

type stateRestoreResultMsg struct {
	State persistedState
	OK    bool
	Dur   time.Duration
}

func stepStateRestore(_ Model) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		st, ok := LoadTUIState()
		return stateRestoreResultMsg{State: st, OK: ok, Dur: time.Since(t0)}
	}
}

func short8(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

func stepRendererWarm(_ Model) tea.Cmd {
	return emit("renderer.warm", func() (bootStatus, string, error) {
		// Report only: styles are built by init/applyTheme, and a step Cmd
		// must not write them while the splash View reads them.
		return stepOK, "styles cached", nil
	})
}
