package tui

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/daemonctl"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/settings"
)

// ── Setup wizard step enum ──────────────────────────────────────────────────

type setupStep int

const (
	setupStepWelcome    setupStep = iota
	setupStepDaemon               // check / start daemon
	setupStepDefaults             // tool, model, effort
	setupStepAppearance           // color_scheme, spinner, row_density
	setupStepAudio                // sound_enabled, soundset
	setupStepShell                // show profile block, offer install
	setupStepPreview              // list changes before write
	setupStepWrite                // perform writes
	setupStepDone
)

// ── Sub-step selectors for multi-field steps ────────────────────────────────

type defaultsField int

const (
	defaultsTool defaultsField = iota
	defaultsModel
	defaultsEffort
)

type appearanceField int

const (
	appearanceColorScheme appearanceField = iota
	appearanceSpinner
	appearanceRowDensity
)

type audioField int

const (
	audioEnabled audioField = iota
	audioSoundset
)

// ── Result types for async ops ───────────────────────────────────────────────

type setupDaemonCheckMsg struct{ reachable bool }
type setupDaemonStartMsg struct {
	pid int
	err error
}
type setupWriteMsg struct{ err error }

// ── Top-level model ──────────────────────────────────────────────────────────

// SetupWizardModel is the standalone Bubble Tea model for `rex setup`.
type SetupWizardModel struct {
	step    setupStep
	touched bool // any field modified — gate esc-confirm

	// terminal size
	width  int
	height int

	// step: daemon
	daemonChecked  bool
	daemonReach    bool
	daemonStarting bool
	daemonStartErr error
	daemonSocket   string

	// step: defaults
	tools         []registry.Tool
	toolIdx       int
	defaultModel  string // free text
	modelCursor   int    // cursor position in modelInput
	effortOptions []string
	effortIdx     int
	defaultsField defaultsField

	// step: appearance
	colorSchemeOptions []string
	colorSchemeIdx     int
	spinnerOptions     []string
	spinnerIdx         int
	rowDensityOptions  []string
	rowDensityIdx      int
	appearanceField    appearanceField

	// step: audio
	soundEnabled bool
	soundsetOpts []string
	soundsetIdx  int
	audioField   audioField

	// step: shell
	shellRC          string // path of target RC file
	shellBlockExists bool   // marker already present
	installShell     bool   // user wants to install

	// step: preview / write
	configPath  string
	writeItems  []writeItem
	writeResult []writeResult
	writing     bool

	// step: done
	writeErr error

	// esc-confirm overlay
	confirmCancel bool

	// existing config detected — overwrite guard
	existingConfig   bool
	overwriteGuard   bool   // true = overwrite prompt visible
	overwriteDecided bool   // true = user said yes/no
	doOverwrite      bool   // result of overwrite prompt
}

type writeItem struct {
	label string
	path  string
	kind  string // "config" | "shell"
}

type writeResult struct {
	label string
	err   error
}

// NewSetupWizardModel initialises the model with defaults from the settings registry.
func NewSetupWizardModel(configPath, socket string) SetupWizardModel {
	slog.Info("setup: wizard initialising", "config_path", configPath, "socket", socket)

	// tools
	reg, _ := registry.Load(toolsConfigPath())
	visible := visibleTools(reg.Tools)

	// settings options from registry
	colorSchemeOpts := enumOptions("color_scheme")
	spinnerOpts := enumOptions("spinner")
	rowDensityOpts := enumOptions("row_density")
	soundsetOpts := enumOptions("soundset")

	// pre-load existing settings to show current values as defaults
	st := settings.NewStore()
	_ = st.Load(configPath)

	colorSchemeIdx := indexIn(colorSchemeOpts, st.String("color_scheme"))
	spinnerIdx := indexIn(spinnerOpts, st.String("spinner"))
	rowDensityIdx := indexIn(rowDensityOpts, st.String("row_density"))
	soundEnabled, _ := st.Get("sound_enabled").(bool)
	soundsetIdx := indexIn(soundsetOpts, st.String("soundset"))
	if soundsetIdx < 0 {
		soundsetIdx = 0
	}

	// default model from first tool
	defaultModel := ""
	if len(visible) > 0 && len(visible[0].Models) > 0 {
		defaultModel = visible[0].Models[0].ID
	}

	// effort for first tool's first model
	effortOpts, effortIdx := effortFor(visible, 0, "")

	// shell RC
	shellRC := detectShellRC()
	shellBlockExists := hasShellBlock(shellRC)

	// check if config already has content
	existingConfig := false
	if _, err := os.Stat(configPath); err == nil {
		if b, err := os.ReadFile(configPath); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			existingConfig = true
		}
	}

	return SetupWizardModel{
		step: setupStepWelcome,

		daemonSocket: socket,

		tools:        visible,
		toolIdx:      0,
		defaultModel: defaultModel,
		effortOptions: effortOpts,
		effortIdx:    effortIdx,
		defaultsField: defaultsTool,

		colorSchemeOptions: colorSchemeOpts,
		colorSchemeIdx:     colorSchemeIdx,
		spinnerOptions:     spinnerOpts,
		spinnerIdx:         spinnerIdx,
		rowDensityOptions:  rowDensityOpts,
		rowDensityIdx:      rowDensityIdx,
		appearanceField:    appearanceColorScheme,

		soundEnabled: soundEnabled,
		soundsetOpts: soundsetOpts,
		soundsetIdx:  soundsetIdx,
		audioField:   audioEnabled,

		shellRC:          shellRC,
		shellBlockExists: shellBlockExists,
		installShell:     !shellBlockExists, // default on if not already installed

		configPath:     configPath,
		existingConfig: existingConfig,
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func enumOptions(id string) []string {
	s, ok := settings.Find(id)
	if !ok {
		return nil
	}
	return s.Options
}

func indexIn(opts []string, val string) int {
	for i, o := range opts {
		if o == val {
			return i
		}
	}
	return 0
}

func effortFor(tools []registry.Tool, idx int, current string) ([]string, int) {
	if idx >= len(tools) {
		return nil, 0
	}
	t := tools[idx]
	if len(t.Models) == 0 {
		return nil, 0
	}
	m := t.Models[0]
	if m.Effort == nil || len(m.Effort.Options) == 0 {
		return nil, 0
	}
	ei := 0
	for i, o := range m.Effort.Options {
		if o == m.Effort.Default {
			ei = i
		}
		if current != "" && o == current {
			ei = i
		}
	}
	return m.Effort.Options, ei
}

// ── Init / Update / View ─────────────────────────────────────────────────────

func (m SetupWizardModel) Init() tea.Cmd {
	return tea.WindowSize()
}

func (m SetupWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case setupDaemonCheckMsg:
		m.daemonReach = msg.reachable
		m.daemonChecked = true
		return m, nil

	case setupDaemonStartMsg:
		m.daemonStarting = false
		m.daemonStartErr = msg.err
		if msg.err == nil {
			m.daemonReach = true
			slog.Info("setup: daemon started", "pid", msg.pid)
		} else {
			slog.Error("setup: daemon start failed", "err", msg.err)
		}
		return m, nil

	case setupWriteMsg:
		m.writing = false
		m.writeErr = msg.err
		m.step = setupStepDone
		if msg.err != nil {
			slog.Error("setup: write failed", "err", msg.err)
		} else {
			slog.Info("setup: writes complete")
		}
		return m, nil
	}
	return m, nil
}

func (m SetupWizardModel) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	// esc-confirm overlay
	if m.confirmCancel {
		switch k.String() {
		case "y", "Y":
			slog.Info("setup: cancelled by user")
			return m, tea.Quit
		case "n", "N", "esc", "enter":
			m.confirmCancel = false
		}
		return m, nil
	}

	// overwrite guard overlay
	if m.overwriteGuard && !m.overwriteDecided {
		switch k.String() {
		case "y", "Y":
			m.doOverwrite = true
			m.overwriteDecided = true
		case "n", "N", "esc", "enter":
			m.doOverwrite = false
			m.overwriteDecided = true
		}
		return m, nil
	}

	// global esc — only if something touched
	if k.String() == "esc" {
		if m.touched {
			m.confirmCancel = true
		} else {
			slog.Info("setup: cancelled (nothing changed)")
			return m, tea.Quit
		}
		return m, nil
	}

	switch m.step {
	case setupStepWelcome:
		return m.keyWelcome(k)
	case setupStepDaemon:
		return m.keyDaemon(k)
	case setupStepDefaults:
		return m.keyDefaults(k)
	case setupStepAppearance:
		return m.keyAppearance(k)
	case setupStepAudio:
		return m.keyAudio(k)
	case setupStepShell:
		return m.keyShell(k)
	case setupStepPreview:
		return m.keyPreview(k)
	case setupStepWrite:
		// writing — block input
		return m, nil
	case setupStepDone:
		return m.keyDone(k)
	}
	return m, nil
}

// ── Per-step key handlers ────────────────────────────────────────────────────

func (m SetupWizardModel) keyWelcome(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter", "tab", "right":
		m.step = setupStepDaemon
		return m, m.checkDaemonCmd()
	}
	return m, nil
}

func (m SetupWizardModel) keyDaemon(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.daemonChecked {
		return m, nil // still checking
	}
	switch k.String() {
	case "enter", "tab", "right":
		if !m.daemonReach {
			// user pressed enter on "start daemon?" — interpret as Y
			m.daemonStarting = true
			m.touched = true
			return m, startDaemonCmd(m.daemonSocket)
		}
		m.step = setupStepDefaults
	case "y", "Y":
		if !m.daemonReach {
			m.daemonStarting = true
			m.touched = true
			return m, startDaemonCmd(m.daemonSocket)
		}
	case "n", "N":
		if !m.daemonReach {
			m.step = setupStepDefaults
		}
	case "shift+tab", "left":
		m.step = setupStepWelcome
	}
	return m, nil
}

func (m SetupWizardModel) keyDefaults(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "tab", "right", "enter":
		// advance sub-field or step
		switch m.defaultsField {
		case defaultsTool:
			m.defaultsField = defaultsModel
		case defaultsModel:
			if len(m.effortOptions) > 0 {
				m.defaultsField = defaultsEffort
			} else {
				m.step = setupStepAppearance
				m.defaultsField = defaultsTool
			}
		case defaultsEffort:
			m.step = setupStepAppearance
			m.defaultsField = defaultsTool
		}
	case "shift+tab", "left":
		switch m.defaultsField {
		case defaultsTool:
			m.step = setupStepDaemon
		case defaultsModel:
			m.defaultsField = defaultsTool
		case defaultsEffort:
			m.defaultsField = defaultsModel
		}
	case "j", "down":
		m.moved()
		switch m.defaultsField {
		case defaultsTool:
			if m.toolIdx+1 < len(m.tools) {
				m.toolIdx++
				m.effortOptions, m.effortIdx = effortFor(m.tools, m.toolIdx, "")
				// reset model to first of new tool
				if len(m.tools[m.toolIdx].Models) > 0 {
					m.defaultModel = m.tools[m.toolIdx].Models[0].ID
				}
				m.modelCursor = len(m.defaultModel)
			}
		case defaultsEffort:
			if m.effortIdx+1 < len(m.effortOptions) {
				m.effortIdx++
			}
		}
	case "k", "up":
		m.moved()
		switch m.defaultsField {
		case defaultsTool:
			if m.toolIdx > 0 {
				m.toolIdx--
				m.effortOptions, m.effortIdx = effortFor(m.tools, m.toolIdx, "")
				if len(m.tools[m.toolIdx].Models) > 0 {
					m.defaultModel = m.tools[m.toolIdx].Models[0].ID
				}
				m.modelCursor = len(m.defaultModel)
			}
		case defaultsEffort:
			if m.effortIdx > 0 {
				m.effortIdx--
			}
		}
	default:
		if m.defaultsField == defaultsModel {
			m.touched = true
			switch k.Type {
			case tea.KeyBackspace:
				if m.modelCursor > 0 {
					m.defaultModel = m.defaultModel[:m.modelCursor-1] + m.defaultModel[m.modelCursor:]
					m.modelCursor--
				}
			case tea.KeyRunes:
				m.defaultModel = m.defaultModel[:m.modelCursor] + string(k.Runes) + m.defaultModel[m.modelCursor:]
				m.modelCursor += len([]rune(string(k.Runes)))
			case tea.KeySpace:
				m.defaultModel = m.defaultModel[:m.modelCursor] + " " + m.defaultModel[m.modelCursor:]
				m.modelCursor++
			}
		}
	}
	return m, nil
}

func (m SetupWizardModel) keyAppearance(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "tab", "right", "enter":
		switch m.appearanceField {
		case appearanceColorScheme:
			m.appearanceField = appearanceSpinner
		case appearanceSpinner:
			m.appearanceField = appearanceRowDensity
		case appearanceRowDensity:
			m.step = setupStepAudio
			m.appearanceField = appearanceColorScheme
		}
	case "shift+tab", "left":
		switch m.appearanceField {
		case appearanceColorScheme:
			m.step = setupStepDefaults
		case appearanceSpinner:
			m.appearanceField = appearanceColorScheme
		case appearanceRowDensity:
			m.appearanceField = appearanceSpinner
		}
	case "j", "down":
		m.moved()
		switch m.appearanceField {
		case appearanceColorScheme:
			if m.colorSchemeIdx+1 < len(m.colorSchemeOptions) {
				m.colorSchemeIdx++
			}
		case appearanceSpinner:
			if m.spinnerIdx+1 < len(m.spinnerOptions) {
				m.spinnerIdx++
			}
		case appearanceRowDensity:
			if m.rowDensityIdx+1 < len(m.rowDensityOptions) {
				m.rowDensityIdx++
			}
		}
	case "k", "up":
		m.moved()
		switch m.appearanceField {
		case appearanceColorScheme:
			if m.colorSchemeIdx > 0 {
				m.colorSchemeIdx--
			}
		case appearanceSpinner:
			if m.spinnerIdx > 0 {
				m.spinnerIdx--
			}
		case appearanceRowDensity:
			if m.rowDensityIdx > 0 {
				m.rowDensityIdx--
			}
		}
	}
	return m, nil
}

func (m SetupWizardModel) keyAudio(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "tab", "right", "enter":
		switch m.audioField {
		case audioEnabled:
			m.audioField = audioSoundset
		case audioSoundset:
			m.step = setupStepShell
		}
	case "shift+tab", "left":
		switch m.audioField {
		case audioEnabled:
			m.step = setupStepAppearance
		case audioSoundset:
			m.audioField = audioEnabled
		}
	case "j", "down":
		m.moved()
		switch m.audioField {
		case audioEnabled:
			// toggle bool
		case audioSoundset:
			if m.soundsetIdx+1 < len(m.soundsetOpts) {
				m.soundsetIdx++
			}
		}
	case "k", "up":
		m.moved()
		switch m.audioField {
		case audioEnabled:
			// toggle bool
		case audioSoundset:
			if m.soundsetIdx > 0 {
				m.soundsetIdx--
			}
		}
	case " ":
		m.moved()
		if m.audioField == audioEnabled {
			m.soundEnabled = !m.soundEnabled
		}
	}
	return m, nil
}

func (m SetupWizardModel) keyShell(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "tab", "right", "enter":
		m.step = setupStepPreview
		m.buildPreview()
	case "shift+tab", "left":
		m.step = setupStepAudio
	case " ", "y", "Y":
		m.moved()
		m.installShell = true
	case "n", "N":
		m.moved()
		m.installShell = false
	}
	return m, nil
}

func (m *SetupWizardModel) buildPreview() {
	m.writeItems = nil
	// config always written
	m.writeItems = append(m.writeItems, writeItem{
		label: "~/.config/rex/config.yaml",
		path:  m.configPath,
		kind:  "config",
	})
	// shell block if requested
	if m.installShell && m.shellRC != "" {
		home, _ := os.UserHomeDir()
		rel := strings.TrimPrefix(m.shellRC, home)
		m.writeItems = append(m.writeItems, writeItem{
			label: "~" + rel,
			path:  m.shellRC,
			kind:  "shell",
		})
	}
}

func (m SetupWizardModel) keyPreview(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter", "y", "Y":
		// gate on overwrite
		if m.existingConfig && !m.doOverwrite && !m.overwriteDecided {
			m.overwriteGuard = true
			return m, nil
		}
		if m.overwriteDecided && !m.doOverwrite {
			// user declined — back to shell step
			m.step = setupStepShell
			m.overwriteGuard = false
			m.overwriteDecided = false
			return m, nil
		}
		m.step = setupStepWrite
		m.writing = true
		return m, m.doWriteCmd()
	case "n", "N", "shift+tab", "left":
		m.step = setupStepShell
		m.overwriteGuard = false
		m.overwriteDecided = false
	}
	return m, nil
}

func (m SetupWizardModel) keyDone(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter", "q", "esc":
		slog.Info("setup: wizard complete")
		return m, tea.Quit
	}
	return m, nil
}

func (m *SetupWizardModel) moved() {
	m.touched = true
}

// ── Async commands ────────────────────────────────────────────────────────────

func (m SetupWizardModel) checkDaemonCmd() tea.Cmd {
	socket := m.daemonSocket
	return func() tea.Msg {
		return setupDaemonCheckMsg{reachable: daemonctl.Reachable(socket)}
	}
}

func startDaemonCmd(socket string) tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		logPath := filepath.Join(home, ".local", "state", "rex", "daemon.log")
		logf, _ := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if logf != nil {
			defer logf.Close()
		}
		res, err := daemonctl.Start(socket, logf)
		if err != nil {
			return setupDaemonStartMsg{err: err}
		}
		return setupDaemonStartMsg{pid: res.PID}
	}
}

func (m SetupWizardModel) doWriteCmd() tea.Cmd {
	// capture values at the time the user confirms
	configPath := m.configPath
	installShell := m.installShell
	shellRC := m.shellRC

	store := settings.NewStore()

	// tool ID
	toolID := ""
	if m.toolIdx < len(m.tools) {
		toolID = m.tools[m.toolIdx].ID
	}
	modelID := strings.TrimSpace(m.defaultModel)
	effortVal := ""
	if m.effortIdx < len(m.effortOptions) {
		effortVal = m.effortOptions[m.effortIdx]
	}
	colorScheme := ""
	if m.colorSchemeIdx < len(m.colorSchemeOptions) {
		colorScheme = m.colorSchemeOptions[m.colorSchemeIdx]
	}
	spinner := ""
	if m.spinnerIdx < len(m.spinnerOptions) {
		spinner = m.spinnerOptions[m.spinnerIdx]
	}
	rowDensity := ""
	if m.rowDensityIdx < len(m.rowDensityOptions) {
		rowDensity = m.rowDensityOptions[m.rowDensityIdx]
	}
	soundEnabled := m.soundEnabled
	soundset := ""
	if m.soundsetIdx < len(m.soundsetOpts) {
		soundset = m.soundsetOpts[m.soundsetIdx]
	}

	return func() tea.Msg {
		// apply settings to store
		_ = store.Set("color_scheme", colorScheme)
		_ = store.Set("spinner", spinner)
		_ = store.Set("row_density", rowDensity)
		_ = store.Set("sound_enabled", soundEnabled)
		_ = store.Set("soundset", soundset)

		// store tool/model/effort as extension keys (not in registry, skip validation)
		// They're informational for the "done" screen; actual session args come from `rex new` flags.
		// We persist what we can via the registry keys.

		slog.Info("setup: writing config", "path", configPath,
			"tool", toolID, "model", modelID, "effort", effortVal)
		if err := store.Save(configPath); err != nil {
			return setupWriteMsg{err: err}
		}
		slog.Info("setup: config written", "path", configPath)

		if installShell && shellRC != "" {
			slog.Info("setup: installing shell block", "rc", shellRC)
			if err := installShellBlock(shellRC); err != nil {
				return setupWriteMsg{err: err}
			}
			slog.Info("setup: shell block installed", "rc", shellRC)
		} else {
			slog.Info("setup: skipping shell block install", "install", installShell, "rc", shellRC)
		}

		return setupWriteMsg{}
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m SetupWizardModel) View() string {
	if m.confirmCancel {
		return m.renderOverlay("cancel setup?", []string{
			"",
			"  You have unsaved changes.",
			"",
			"  " + styleDim.Render("[y]") + " quit without saving   " + styleDim.Render("[n / esc]") + " continue",
		})
	}
	if m.overwriteGuard && !m.overwriteDecided {
		return m.renderOverlay("overwrite existing config?", []string{
			"",
			"  " + styleBootWarn.Render("config.yaml already exists and is non-empty."),
			"",
			"  " + styleDim.Render("[y]") + " overwrite   " + styleDim.Render("[n / esc]") + " keep existing",
		})
	}

	var title string
	var body []string
	var footer string

	switch m.step {
	case setupStepWelcome:
		title, body, footer = m.renderWelcome()
	case setupStepDaemon:
		title, body, footer = m.renderDaemon()
	case setupStepDefaults:
		title, body, footer = m.renderDefaults()
	case setupStepAppearance:
		title, body, footer = m.renderAppearance()
	case setupStepAudio:
		title, body, footer = m.renderAudio()
	case setupStepShell:
		title, body, footer = m.renderShell()
	case setupStepPreview:
		title, body, footer = m.renderPreview()
	case setupStepWrite:
		title, body, footer = m.renderWriting()
	case setupStepDone:
		title, body, footer = m.renderDone()
	}

	body = padOrTrim(body, setupBodyLines)

	lines := []string{styleSlug.Render(title)}
	lines = append(lines, body...)
	lines = append(lines, "", styleDim.Render(footer))

	for i, line := range lines {
		lines[i] = padLine(line, setupWidth)
	}
	return strings.Join(lines, "\n")
}

const (
	setupWidth     = 78
	setupBodyLines = 14
)

func (m SetupWizardModel) renderOverlay(title string, body []string) string {
	body = padOrTrim(body, setupBodyLines)
	lines := []string{styleSlug.Render(title)}
	lines = append(lines, body...)
	for i, l := range lines {
		lines[i] = padLine(l, setupWidth)
	}
	return strings.Join(lines, "\n")
}

// ── Step renderers ────────────────────────────────────────────────────────────

func (m SetupWizardModel) renderWelcome() (string, []string, string) {
	body := []string{
		"",
		"  " + stylePrimary.Render("Welcome to rex."),
		"",
		"  This wizard configures your installation:",
		"  daemon, defaults, appearance, audio, and shell integration.",
		"",
		"  Nothing is written until you confirm at the end.",
	}
	return "rex setup", body, "  enter begin   esc skip"
}

func (m SetupWizardModel) renderDaemon() (string, []string, string) {
	var body []string
	body = append(body, "")

	if !m.daemonChecked {
		body = append(body, "  "+styleBootRun.Render("checking daemon …"))
		return "daemon", body, "  checking …"
	}

	if m.daemonReach {
		body = append(body, "  "+styleBootOK.Render("✓")+" daemon is reachable at "+styleDim.Render(m.daemonSocket))
	} else if m.daemonStarting {
		body = append(body, "  "+styleBootRun.Render("starting daemon …"))
	} else if m.daemonStartErr != nil {
		body = append(body, "  "+styleBootFail.Render("✗")+" failed to start: "+m.daemonStartErr.Error())
		body = append(body, "")
		body = append(body, "  You can start it manually with: "+styleSlug.Render("rex daemon"))
	} else {
		body = append(body, "  "+styleBootWarn.Render("!")+" daemon not running at "+styleDim.Render(m.daemonSocket))
		body = append(body, "")
		body = append(body, "  Start daemon now? "+styleSlug.Render("[Y/n]"))
	}

	footer := "  enter/Y start   n skip   shift+tab back   esc cancel"
	if m.daemonReach {
		footer = "  enter/tab next   shift+tab back"
	}
	return "daemon", body, footer
}

func (m SetupWizardModel) renderDefaults() (string, []string, string) {
	var body []string
	body = append(body, "")

	// tool picker
	toolLabel := styleDim.Render("default tool")
	if m.defaultsField == defaultsTool {
		toolLabel = styleSlug.Render("default tool")
	}
	body = append(body, "  "+toolLabel)
	for i, t := range m.tools {
		selected := i == m.toolIdx
		active := m.defaultsField == defaultsTool
		row := setupOption(selected && active, t.Icon, t.Name, t.Color)
		body = append(body, row)
	}
	body = append(body, "")

	// model text input
	modelLabel := styleDim.Render("default model")
	if m.defaultsField == defaultsModel {
		modelLabel = styleSlug.Render("default model")
	}
	body = append(body, "  "+modelLabel)
	modelInput := m.defaultModel
	if m.defaultsField == defaultsModel {
		modelInput = m.defaultModel + "|"
	}
	body = append(body, "  "+setupInputInline(modelInput))
	body = append(body, "")

	// effort picker (only if applicable)
	if len(m.effortOptions) > 0 {
		effortLabel := styleDim.Render("default effort")
		if m.defaultsField == defaultsEffort {
			effortLabel = styleSlug.Render("default effort")
		}
		body = append(body, "  "+effortLabel)
		for i, opt := range m.effortOptions {
			selected := i == m.effortIdx
			active := m.defaultsField == defaultsEffort
			body = append(body, setupOption(selected && active, "", opt, ""))
		}
	}

	return "defaults", body, "  j/k select   tab/enter next field   shift+tab back   esc cancel"
}

func (m SetupWizardModel) renderAppearance() (string, []string, string) {
	var body []string
	body = append(body, "")

	fields := []struct {
		id    string
		label string
		opts  []string
		idx   int
		field appearanceField
	}{
		{"color_scheme", "color scheme", m.colorSchemeOptions, m.colorSchemeIdx, appearanceColorScheme},
		{"spinner", "spinner type", m.spinnerOptions, m.spinnerIdx, appearanceSpinner},
		{"row_density", "row density", m.rowDensityOptions, m.rowDensityIdx, appearanceRowDensity},
	}

	for _, f := range fields {
		active := m.appearanceField == f.field
		label := styleDim.Render(f.label)
		if active {
			label = styleSlug.Render(f.label)
		}
		body = append(body, "  "+label)
		for i, opt := range f.opts {
			body = append(body, setupOption(i == f.idx && active, "", opt, ""))
		}
		body = append(body, "")
	}

	return "appearance", body, "  j/k select   tab/enter next field   shift+tab back   esc cancel"
}

func (m SetupWizardModel) renderAudio() (string, []string, string) {
	var body []string
	body = append(body, "")

	// sound enabled bool
	enabledLabel := styleDim.Render("sound enabled")
	if m.audioField == audioEnabled {
		enabledLabel = styleSlug.Render("sound enabled")
	}
	body = append(body, "  "+enabledLabel)
	onOff := []string{"true", "false"}
	curIdx := 1
	if m.soundEnabled {
		curIdx = 0
	}
	for i, v := range onOff {
		body = append(body, setupOption(i == curIdx && m.audioField == audioEnabled, "", v, ""))
	}
	body = append(body, "")

	// soundset picker
	soundsetLabel := styleDim.Render("soundset")
	if m.audioField == audioSoundset {
		soundsetLabel = styleSlug.Render("soundset")
	}
	body = append(body, "  "+soundsetLabel)
	for i, opt := range m.soundsetOpts {
		body = append(body, setupOption(i == m.soundsetIdx && m.audioField == audioSoundset, "", opt, ""))
	}

	return "audio", body, "  j/k select   space toggle bool   tab/enter next   shift+tab back   esc cancel"
}

func (m SetupWizardModel) renderShell() (string, []string, string) {
	var body []string
	body = append(body, "")

	block := ShellProfileBlock("bash") // show the generic block
	for _, line := range strings.Split(block, "\n") {
		body = append(body, "  "+styleMuted.Render(line))
	}
	body = append(body, "")

	if m.shellRC != "" {
		home, _ := os.UserHomeDir()
		rel := "~" + strings.TrimPrefix(m.shellRC, home)

		if m.shellBlockExists {
			body = append(body, "  "+styleBootOK.Render("✓")+" block already in "+styleDim.Render(rel))
		} else {
			check := "[ ] "
			if m.installShell {
				check = styleBootOK.Render("[x]") + " "
			}
			body = append(body, "  "+check+"install into "+styleSlug.Render(rel)+
				"  ("+styleDim.Render("y/n / space to toggle")+")")
		}
	} else {
		body = append(body, "  "+styleBootWarn.Render("!")+" could not detect shell RC — install manually")
	}

	return "shell integration", body, "  y/n/space toggle   enter/tab next   shift+tab back   esc cancel"
}

func (m SetupWizardModel) renderPreview() (string, []string, string) {
	var body []string
	body = append(body, "")
	body = append(body, "  "+stylePrimary.Render("Files to be written:"))
	body = append(body, "")
	for _, item := range m.writeItems {
		body = append(body, "  "+styleSlug.Render("·")+" "+item.label)
	}
	if len(m.writeItems) == 0 {
		body = append(body, "  "+styleDim.Render("(nothing to write)"))
	}
	body = append(body, "")
	body = append(body, "  "+styleDim.Render("Press enter to write, n/shift+tab to go back."))
	return "preview", body, "  enter/y write   n/shift+tab back   esc cancel"
}

func (m SetupWizardModel) renderWriting() (string, []string, string) {
	body := []string{
		"",
		"  " + styleBootRun.Render("writing …"),
	}
	return "writing", body, ""
}

func (m SetupWizardModel) renderDone() (string, []string, string) {
	var body []string
	body = append(body, "")

	if m.writeErr != nil {
		body = append(body, "  "+styleBootFail.Render("✗")+" error: "+m.writeErr.Error())
		return "done", body, "  enter/q exit"
	}

	// display per-item results
	for _, r := range m.writeResult {
		icon := styleBootOK.Render("✓")
		if r.err != nil {
			icon = styleBootFail.Render("✗")
		}
		body = append(body, "  "+icon+" "+r.label)
	}

	body = append(body, "")

	// first tool
	toolID := ""
	if m.toolIdx < len(m.tools) {
		toolID = m.tools[m.toolIdx].ID
	}
	modelID := strings.TrimSpace(m.defaultModel)
	if modelID == "" {
		modelID = "your-model"
	}

	nextCmd := fmt.Sprintf(`rex new "your first task" --tool %s --model %s`, toolID, modelID)
	effortVal := ""
	if m.effortIdx < len(m.effortOptions) {
		effortVal = m.effortOptions[m.effortIdx]
	}
	if effortVal != "" {
		nextCmd += " --effort " + effortVal
	}

	body = append(body, "  "+stylePrimary.Render("Next step:"))
	body = append(body, "")
	body = append(body, "  "+styleSlug.Render(nextCmd))
	body = append(body, "")
	body = append(body, "  "+styleDim.Render("Docs: https://github.com/tristanbietsch/rex"))

	return "all done", body, "  enter/q exit"
}

// ── Rendering helpers ─────────────────────────────────────────────────────────

func setupOption(selected bool, glyph, name, glyphColor string) string {
	var cursor string
	if selected {
		cursor = styleArrow.Render("▸") + " "
	} else {
		cursor = "  "
	}
	var g string
	if glyph != "" {
		if glyphColor != "" {
			g = lipgloss.NewStyle().Foreground(lipgloss.Color(glyphColor)).Render(glyph) + "  "
		} else {
			g = glyph + "  "
		}
	}
	return "  " + cursor + g + styleSlug.Render(name)
}

func setupInputInline(value string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWorking).
		Padding(0, 1).
		Width(40).
		Render(stylePrimary.Render(value))
}

// styleArrow is already defined in wizard.go but not exported — replicate the
// inline usage instead.  (Both files are in the same package so we just use it.)

// ── Shell block logic ─────────────────────────────────────────────────────────

const (
	shellBlockBegin = "# BEGIN REX"
	shellBlockEnd   = "# END REX"
)

// ShellProfileBlock returns the profile block text for the given shell.
// shell should be "bash" or "zsh".
func ShellProfileBlock(shell string) string {
	completionLine := `source <(rex completion bash 2>/dev/null) || true`
	if shell == "zsh" {
		completionLine = `source <(rex completion zsh 2>/dev/null) || true`
	}
	return strings.Join([]string{
		shellBlockBegin,
		`export PATH="$HOME/.local/bin:$PATH"`,
		completionLine,
		shellBlockEnd,
	}, "\n")
}

// DetectShellRC returns the path to the user's shell RC file based on $SHELL.
// Exported for use by internal/cli/setup.go's preview renderer.
func DetectShellRC() string { return detectShellRC() }

// detectShellRC is the internal implementation.
func detectShellRC() string {
	home, _ := os.UserHomeDir()
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		return filepath.Join(home, ".bashrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	default:
		// try .zshrc then .bashrc
		for _, name := range []string{".zshrc", ".bashrc", ".profile"} {
			p := filepath.Join(home, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return ""
	}
}

// HasShellBlock returns true if the RC file already contains the BEGIN REX marker.
// Exported for use by internal/cli/setup.go's preview renderer.
func HasShellBlock(rcPath string) bool { return hasShellBlock(rcPath) }

// hasShellBlock is the internal implementation.
func hasShellBlock(rcPath string) bool {
	if rcPath == "" {
		return false
	}
	b, err := os.ReadFile(rcPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), shellBlockBegin)
}

// installShellBlock installs (or rewrites in-place) the REX shell block in rcPath.
// Idempotent: if the block already exists, it is replaced with the current block.
// Content outside the markers is preserved.
func installShellBlock(rcPath string) error {
	shell := filepath.Base(os.Getenv("SHELL"))
	block := ShellProfileBlock(shell)

	existing, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", rcPath, err)
	}

	content := string(existing)
	newContent := rewriteShellBlock(content, block)
	if newContent == content {
		slog.Info("setup: shell block unchanged, skipping write", "rc", rcPath)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(rcPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := rcPath + ".rextmp"
	if err := os.WriteFile(tmp, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, rcPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// rewriteShellBlock replaces the BEGIN/END REX section in content with block,
// or appends block if no section exists. Returns the new content.
func rewriteShellBlock(content, block string) string {
	begin := strings.Index(content, shellBlockBegin)
	end := strings.Index(content, shellBlockEnd)

	if begin >= 0 && end > begin {
		// replace existing block (inclusive of both markers)
		endIdx := end + len(shellBlockEnd)
		// consume trailing newline if present
		if endIdx < len(content) && content[endIdx] == '\n' {
			endIdx++
		}
		return content[:begin] + block + "\n" + content[endIdx:]
	}

	// append
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + "\n" + block + "\n"
}

// ── Entry point (called from internal/cli/setup.go) ──────────────────────────

// RunSetupWizard launches the setup wizard as a standalone Bubble Tea program.
func RunSetupWizard(configPath, socket string) error {
	slog.Info("setup: launching TUI wizard", "config_path", configPath, "socket", socket)
	model := NewSetupWizardModel(configPath, socket)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		slog.Error("setup: wizard error", "err", err)
	}
	return err
}

// ApplyDefaultsNonInteractive writes the config without prompting. Used by --yes.
func ApplyDefaultsNonInteractive(configPath, socket string, toolID, modelID, effortVal string, noShell bool) error {
	slog.Info("setup: non-interactive start",
		"config_path", configPath, "socket", socket,
		"tool", toolID, "model", modelID, "effort", effortVal)

	store := settings.NewStore()
	// Load existing so we don't wipe unrelated keys
	_ = store.Load(configPath)

	// Check if config already exists and is non-empty — non-interactive treats
	// an existing non-empty config as a no-op unless explicit overwrite is implied
	// by calling with --force (checked by the caller before invoking this func).

	slog.Info("setup: writing config", "path", configPath)
	if err := store.Save(configPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	slog.Info("setup: config written", "path", configPath)

	if !noShell {
		rcPath := detectShellRC()
		if rcPath != "" {
			if hasShellBlock(rcPath) {
				slog.Info("setup: shell block already installed, skipping", "rc", rcPath)
			} else {
				slog.Info("setup: installing shell block", "rc", rcPath)
				if err := installShellBlock(rcPath); err != nil {
					// non-fatal: warn and continue
					slog.Error("setup: shell block install failed", "rc", rcPath, "err", err)
					fmt.Fprintf(os.Stderr, "warning: could not install shell block into %s: %v\n", rcPath, err)
				} else {
					slog.Info("setup: shell block installed", "rc", rcPath)
				}
			}
		}
	} else {
		slog.Info("setup: skipping shell integration (--no-shell)")
	}

	slog.Info("setup: non-interactive complete")

	toolDisplay := toolID
	if toolDisplay == "" {
		toolDisplay = "claude"
	}
	modelDisplay := modelID
	if modelDisplay == "" {
		modelDisplay = "opus"
	}
	nextCmd := fmt.Sprintf(`rex new "your first task" --tool %s --model %s`, toolDisplay, modelDisplay)
	if effortVal != "" {
		nextCmd += " --effort " + effortVal
	}
	fmt.Printf("setup complete. Next: %s\n", nextCmd)
	fmt.Println("Docs: https://github.com/tristanbietsch/rex")
	return nil
}

// styleArrow is declared in prompt.go and shared across the tui package.
