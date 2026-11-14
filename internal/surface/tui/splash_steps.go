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
	"github.com/tristanbietsch/rex/internal/catalog/theme"
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
	// Lua is what config.lua produced (zero while lua_config is off).
	Lua settings.LayerReport
}

func stepSettingsLoad(_ Model) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		path, err := paths.ConfigFile()
		if err != nil {
			return settingsResultMsg{Store: settings.NewStore(), Found: true, Err: err, Dur: time.Since(t0)}
		}
		_, statErr := os.Stat(path)
		s, rep, err := settings.LoadLayered(path)
		return settingsResultMsg{Store: s, Path: path, Found: !os.IsNotExist(statErr), Err: err, Lua: rep, Dur: time.Since(t0)}
	}
}

// stepLuaConfig is the boot row for config.lua, from what stepSettingsLoad
// read: SKIP when lua_config is off or there is no file, WARN with the
// (flattened) reason when it failed or refused something, else OK.
func stepLuaConfig(m Model) tea.Cmd {
	return emit("config.lua", func() (bootStatus, string, error) {
		rep := m.luaLayer
		switch {
		case m.Store != nil && !settings.LuaConfigOn(m.Store):
			return stepSkip, "off (Settings → Features)", nil
		case !rep.Exists:
			return stepSkip, "no config.lua", nil
		case rep.Err != nil:
			return stepWarn, luaNotice(rep), rep.Err
		}
		desc := fmt.Sprintf("%d setting(s), %d theme(s)", len(rep.Values), len(rep.Themes))
		if n := len(rep.Warnings); n > 0 {
			return stepWarn, configNotice(desc, rep.Warnings[0], ""), nil
		}
		if n := len(rep.ThemeIssues); n > 0 {
			return stepWarn, configNotice(desc, rep.ThemeIssues[0].String(), ""), nil
		}
		return stepOK, desc, nil
	})
}

// themesLoadedMsg carries the user's theme files, read on a step Cmd (file
// I/O stays off the UI goroutine) and installed by Update before the theme is
// applied.
type themesLoadedMsg struct {
	Themes *theme.Catalog
	// Stamp is the state of config.yaml and the theme files as of the read,
	// the baseline for live reload.
	Stamp string
	Dur   time.Duration
}

func stepThemesLoad(m Model) tea.Cmd {
	return func() tea.Msg {
		t0 := time.Now()
		dir := themesDir()
		return themesLoadedMsg{
			Themes: themesWithLua(dir, m.luaLayer),
			Stamp:  configStamp(m.StorePath, dir, m.Store == nil || settings.LuaConfigOn(m.Store)),
			Dur:    time.Since(t0),
		}
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
		vol := m.Store.Display("master_volume")
		if !enabled || soundset == "off" {
			m.Audio.SetEnabled(false)
			return stepSkip, "muted", nil
		}
		m.Audio.SetEnabled(true)
		m.Audio.SetVolume(settings.VolumeGain(m.Store.Get("master_volume")))
		voice, _ := m.Store.Get("boot_voice").(bool)
		m.Audio.SetBootVoice(voice)
		slog.Info("tui: audio init", "soundset", soundset, "volume", vol, "boot_voice", voice)
		return stepOK, fmt.Sprintf("soundset=%s · vol=%s · voice=%t", soundset, vol, voice), nil
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
		stepLuaConfig,
		stepThemesLoad,
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

func stepStateRestore(m Model) tea.Cmd {
	if st := m.swapIn; st != nil {
		// A swap carries the view itself; tui-state.json stays for a
		// normal start.
		hidden := append([]string{}, st.Hidden...)
		ps := persistedState{Selection: st.Selection, Filter: st.Filter, Search: st.Search, HiddenSections: &hidden}
		return func() tea.Msg { return stateRestoreResultMsg{State: ps, OK: true} }
	}
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
