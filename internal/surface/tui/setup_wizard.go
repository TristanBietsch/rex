package tui

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ── Setup wizard step enum ──────────────────────────────────────────────────

type setupStep int

const (
	setupStepWelcome    setupStep = iota
	setupStepDaemon               // check / start daemon
	setupStepDefaults             // tool, model, effort
	setupStepAppearance           // theme, spinner, row_density
	setupStepAudio                // sound_enabled, soundset
	setupStepTrust                // auto_trust_dirs: opt in, default no
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
	appearanceTheme appearanceField = iota
	appearanceSpinner
	appearanceRowDensity
)

type audioField int

const (
	audioEnabled audioField = iota
	audioSoundset
)

type trustField int

const (
	trustAnswer trustField = iota // no / yes
	trustFolder                   // the folders, as text
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
	// toolsErr is why ~/.config/rex/tools.yaml couldn't be used; tools then
	// holds the built-in registry and the welcome and defaults steps say so.
	toolsErr      error
	tools         []registry.Tool
	toolIdx       int
	defaultModel  string // free text
	modelCursor   int    // cursor position in modelInput
	effortOptions []string
	effortIdx     int
	defaultsField defaultsField

	// step: appearance
	themes            *theme.Catalog // the user's theme files
	themeOptions      []string
	themeIdx          int
	spinnerOptions    []string
	spinnerIdx        int
	rowDensityOptions []string
	rowDensityIdx     int
	appearanceField   appearanceField

	// step: audio
	soundEnabled bool
	soundsetOpts []string
	soundsetIdx  int
	audioField   audioField

	// step: trust
	trustYes   bool   // write trustDir to auto_trust_dirs; default no
	trustDir   string // folders as typed (~/ allowed), list-separated
	trustErr   error  // why trustDir was refused; shown under the field
	trustField trustField

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
	overwriteGuard   bool // true = overwrite prompt visible
	overwriteDecided bool // true = user said yes/no
	doOverwrite      bool // result of overwrite prompt
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

	// tools: a broken user tools.yaml falls back to the built-ins (shown to
	// the user via toolsErr) instead of a nil registry.
	var all []registry.Tool
	reg, toolsPath, toolsErr, err := loadUserTools()
	if toolsErr != nil {
		slog.Warn("setup: tools.yaml unusable; offering built-in tools", "path", toolsPath, "err", toolsErr)
	}
	if err != nil {
		slog.Error("setup: built-in tool registry failed to load", "err", err)
	} else {
		all = reg.Tools
	}

	// settings options from registry
	themes := theme.LoadDir(themesDir())
	themeOpts := append(enumOptions("theme"), themes.UserNames()...)
	spinnerOpts := enumOptions("spinner")
	rowDensityOpts := enumOptions("row_density")
	soundsetOpts := enumOptions("soundset")

	// pre-load existing settings to show current values as defaults
	st := settings.NewStore()
	_ = st.Load(configPath)

	visible := setupTools(all, st)

	// A configured theme that is not on offer (its file was skipped or
	// removed) stays selected, so finishing the wizard does not reset it.
	if cur := st.String("theme"); !slices.Contains(themeOpts, cur) {
		themeOpts = append(themeOpts, cur)
	}
	themeIdx := indexIn(themeOpts, st.String("theme"))
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

	// auto-trust: keep an existing opt-in. Otherwise the answer is no and
	// the folder field holds the old value (to fix) or a suggestion.
	trustDir, trustYes := st.String("auto_trust_dirs"), false
	if v, err := settings.CheckAutoTrustDirs(trustDir); err == nil {
		trustDir, trustYes = v, true
	} else if strings.TrimSpace(trustDir) == "" {
		cwd, _ := os.Getwd()
		trustDir = settings.SuggestTrustDir(cwd)
	}

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

		toolsErr:      toolsErr,
		tools:         visible,
		toolIdx:       0,
		defaultModel:  defaultModel,
		effortOptions: effortOpts,
		effortIdx:     effortIdx,
		defaultsField: defaultsTool,

		themes:            themes,
		themeOptions:      themeOpts,
		themeIdx:          themeIdx,
		spinnerOptions:    spinnerOpts,
		spinnerIdx:        spinnerIdx,
		rowDensityOptions: rowDensityOpts,
		rowDensityIdx:     rowDensityIdx,
		appearanceField:   appearanceTheme,

		soundEnabled: soundEnabled,
		soundsetOpts: soundsetOpts,
		soundsetIdx:  soundsetIdx,
		audioField:   audioEnabled,

		trustYes: trustYes,
		trustDir: trustDir,

		shellRC:          shellRC,
		shellBlockExists: shellBlockExists,
		installShell:     !shellBlockExists, // default on if not already installed

		configPath:     configPath,
		existingConfig: existingConfig,
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// setupTools is the tools `rex setup` offers as the default: the enabled
// ones, except that the extra agents (qwen, cursor, grok) show only while
// extra_agents is on and their binary is on PATH (there is no daemon yet to
// ask; this process looks itself).
func setupTools(all []registry.Tool, st *settings.Store) []registry.Tool {
	extra := settings.FeatureOn(st, "extra_agents")
	var out []registry.Tool
	for _, t := range visibleTools(all) {
		if slices.Contains(registry.ExtraAgentIDs, t.ID) && (!extra || !registry.Installed(t)) {
			continue
		}
		out = append(out, t)
	}
	return out
}

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
	case setupStepTrust:
		return m.keyTrust(k)
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
		case defaultsModel:
			// Free-text field: no list to move through.
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
		case defaultsModel:
			// Free-text field: no list to move through.
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
		case appearanceTheme:
			m.appearanceField = appearanceSpinner
		case appearanceSpinner:
			m.appearanceField = appearanceRowDensity
		case appearanceRowDensity:
			m.step = setupStepAudio
			m.appearanceField = appearanceTheme
		}
	case "shift+tab", "left":
		switch m.appearanceField {
		case appearanceTheme:
			m.step = setupStepDefaults
		case appearanceSpinner:
			m.appearanceField = appearanceTheme
		case appearanceRowDensity:
			m.appearanceField = appearanceSpinner
		}
	case "j", "down":
		m.moved()
		switch m.appearanceField {
		case appearanceTheme:
			if m.themeIdx+1 < len(m.themeOptions) {
				m.themeIdx++
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
		case appearanceTheme:
			if m.themeIdx > 0 {
				m.themeIdx--
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
			m.step = setupStepTrust
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

func (m SetupWizardModel) keyTrust(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.trustField == trustFolder {
		return m.keyTrustFolder(k)
	}
	switch k.String() {
	case "tab", "right", "enter":
		if m.trustYes {
			m.trustField = trustFolder
		} else {
			m.step = setupStepShell
		}
	case "shift+tab", "left":
		m.step = setupStepAudio
	case "j", "down", "k", "up", " ":
		m.moved()
		m.trustYes = !m.trustYes
	case "y", "Y":
		m.moved()
		m.trustYes = true
	case "n", "N":
		m.moved()
		m.trustYes = false
	}
	m.trustErr = nil
	return m, nil
}

// keyTrustFolder edits the folder text. Every printable key is text here
// (paths hold j, k, y and n), so only enter/tab, shift+tab and ctrl+u act.
func (m SetupWizardModel) keyTrustFolder(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEnter, tea.KeyTab:
		v, err := settings.CheckAutoTrustDirs(m.trustDir)
		if err != nil {
			m.trustErr = err
			return m, nil
		}
		m.trustDir, m.trustErr = v, nil
		m.step = setupStepShell
		return m, nil
	case tea.KeyShiftTab:
		m.trustField, m.trustErr = trustAnswer, nil
		return m, nil
	case tea.KeyBackspace:
		if r := []rune(m.trustDir); len(r) > 0 {
			m.trustDir = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		m.trustDir = ""
	case tea.KeySpace:
		m.trustDir += " "
	case tea.KeyRunes:
		// Drop control characters a paste can carry (newlines, escapes).
		m.trustDir += strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, string(k.Runes))
	default:
		return m, nil
	}
	m.moved()
	m.trustErr = nil
	return m, nil
}

// autoTrustValue is what setup writes to auto_trust_dirs: the folders when
// the user said yes and they pass the daemon's checks, else "" (off).
func (m SetupWizardModel) autoTrustValue() string {
	if !m.trustYes {
		return ""
	}
	v, err := settings.CheckAutoTrustDirs(m.trustDir)
	if err != nil {
		return ""
	}
	return v
}

// withAutoTrust starts the trust step at yes with folders filled in, for
// `rex setup -auto-trust` without -yes. The user still confirms.
func (m SetupWizardModel) withAutoTrust(folders string) SetupWizardModel {
	if folders != "" {
		m.trustYes, m.trustDir = true, folders
	}
	return m
}

func (m SetupWizardModel) keyShell(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "tab", "right", "enter":
		m.step = setupStepPreview
		m.buildPreview()
	case "shift+tab", "left":
		m.step = setupStepTrust
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
		m.writeItems = append(m.writeItems, writeItem{
			label: tildePath(m.shellRC),
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

// startDaemonCmd starts rex-daemon on socket with its stderr in the
// owner-only daemon.log (daemonctl.OpenLog). A log that cannot be opened
// is reported rather than starting a daemon whose crash output is lost.
func startDaemonCmd(socket string) tea.Cmd {
	return func() tea.Msg {
		logf, err := daemonctl.OpenLog()
		if err != nil {
			return setupDaemonStartMsg{err: err}
		}
		// The child gets its own descriptor; ours is only for the spawn.
		defer logf.Close()
		res, err := startDaemon(socket, logf)
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
	themeName := ""
	if m.themeIdx < len(m.themeOptions) {
		themeName = m.themeOptions[m.themeIdx]
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
	autoTrust := m.autoTrustValue()
	daemonSocket := m.daemonSocket
	soundset := ""
	if m.soundsetIdx < len(m.soundsetOpts) {
		soundset = m.soundsetOpts[m.soundsetIdx]
	}

	tools := m.tools

	return func() tea.Msg {
		// The model field is free text: refuse one the tool does not have
		// before anything is written.
		var spawn registry.SpawnChoice
		if toolID != "" {
			var err error
			if spawn, err = wizardSpawnChoice(tools, toolID, modelID, effortVal); err != nil {
				return setupWriteMsg{err: err}
			}
		}
		// Save is sparse: it writes only the keys set or reset here and keeps
		// the rest of an existing config.yaml. A value the wizard cannot
		// produce is reset (key deleted) rather than left at what the file
		// holds, so an old setting never outlives the answer given here.
		setOrReset := func(id string, v any) {
			if err := store.Set(id, v); err != nil {
				_ = store.Reset(id)
			}
		}
		setOrReset("theme", themeName)
		setOrReset("spinner", spinner)
		setOrReset("row_density", rowDensity)
		setOrReset("sound_enabled", soundEnabled)
		setOrReset("soundset", soundset)
		// "No, always ask" must delete a previously saved folder list, or
		// rex would keep auto-trusting after the user said no.
		if autoTrust != "" {
			setOrReset("auto_trust_dirs", autoTrust)
		} else {
			_ = store.Reset("auto_trust_dirs")
		}

		if toolID != "" {
			setOrReset("default_spawn_tool", spawn.Tool)
			setOrReset("default_spawn_model", spawn.Model)
			setOrReset("default_spawn_effort", spawn.Effort)
		}

		slog.Info("setup: writing config", "path", configPath,
			"tool", toolID, "model", modelID, "effort", effortVal)
		if err := store.Save(configPath); err != nil {
			return setupWriteMsg{err: err}
		}
		slog.Info("setup: config written", "path", configPath)
		// The wizard may have granted or revoked folder trust.
		reloadDaemonForTrust(nil, daemonSocket, "auto_trust_dirs")

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
	case setupStepTrust:
		title, body, footer = m.renderTrust()
	case setupStepShell:
		title, body, footer = m.renderShell()
	case setupStepPreview:
		title, body, footer = m.renderPreview()
	case setupStepWrite:
		title, body, footer = m.renderWriting()
	case setupStepDone:
		title, body, footer = m.renderDone()
	}

	height := setupBodyLines
	if m.step == setupStepPreview {
		// The preview grows rather than cut what the user is confirming.
		height = max(height, len(body))
	}
	body = padOrTrim(body, height)

	lines := []string{styleSlug.Render(title)}
	if m.step == setupStepWelcome || m.step == setupStepDefaults {
		// Above the fixed-height body so the steps' own lines aren't clipped.
		lines = append(lines, m.toolsWarningLines()...)
	}
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

// toolsWarningLines explains a tools.yaml fallback: a headline plus the load
// error wrapped to the popup (at most three lines) so the user can fix the
// file. Nil when tools.yaml loaded fine.
func (m SetupWizardModel) toolsWarningLines() []string {
	if m.toolsErr == nil {
		return nil
	}
	out := []string{"  " + styleBootWarn.Render("!") + " tools.yaml ignored — using the built-in tools"}
	// YAML errors can quote the file's content: sanitize before wrapping.
	wrapped := lipgloss.NewStyle().Width(setupWidth - 4).Render(protocol.FlattenDisplayText(m.toolsErr.Error()))
	for i, l := range strings.Split(wrapped, "\n") {
		if i == 3 {
			break
		}
		out = append(out, "    "+styleDim.Render(l))
	}
	return out
}

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
		"  daemon, defaults, appearance, audio, folder trust, and shell.",
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
		{"theme", "theme", m.themeOptions, m.themeIdx, appearanceTheme},
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

func (m SetupWizardModel) renderTrust() (string, []string, string) {
	answerActive := m.trustField == trustAnswer
	folderActive := m.trustField == trustFolder
	body := []string{
		"",
		"  Claude Code and Antigravity ask \"do you trust this folder?\" when they",
		"  open one. rex can say yes in the folder below and every folder inside it.",
		"  " + styleBootWarn.Render("Only pick folders whose code you trust."),
		"",
		"  " + setupLabel("skip that question?", answerActive),
		setupOption(!m.trustYes, "", "no, always ask", ""),
		setupOption(m.trustYes, "", "yes, in this folder", ""),
		"",
		"  " + setupLabel("folder", folderActive),
	}
	// The folder can come from config.yaml or a folder name: strip escapes.
	shown := protocol.FlattenDisplayText(m.trustDir)
	if !m.trustYes {
		if shown == "" {
			shown = "(none)"
		}
		body = append(body, "  "+styleDim.Render(shown))
		return "folder trust", body, "  j/k or y/n choose   enter next   shift+tab back   esc cancel"
	}

	if folderActive {
		shown += "|"
	}
	for _, l := range strings.Split(setupInput(shown, setupWidth-12), "\n") {
		body = append(body, "  "+l)
	}
	switch {
	case m.trustErr != nil:
		body = append(body, "  "+styleBootFail.Render("✗")+" "+protocol.FlattenDisplayText(m.trustErr.Error()))
	case folderActive:
		body = append(body, "  "+styleDim.Render(
			"separate folders with "+string(filepath.ListSeparator)+"   ~/ is your home folder"))
	}
	if folderActive {
		return "folder trust", body, "  type to edit   ctrl+u clear   enter next   shift+tab back   esc cancel"
	}
	return "folder trust", body, "  j/k or y/n choose   enter edit folder   shift+tab back   esc cancel"
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
		rel := tildePath(m.shellRC)

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
		if item.kind == "config" {
			body = append(body, m.previewTrustLines()...)
		}
	}
	if len(m.writeItems) == 0 {
		body = append(body, "  "+styleDim.Render("(nothing to write)"))
	}
	body = append(body, "")
	body = append(body, "  "+styleDim.Render("Press enter to write, n/shift+tab to go back."))
	return "preview", body, "  enter/y write   n/shift+tab back   esc cancel"
}

// previewTrustLines shows what auto_trust_dirs will hold in full: one
// folder per line, long ones wrapped, so nothing the user confirms is cut.
func (m SetupWizardModel) previewTrustLines() []string {
	const indent = "      "
	label := "auto-trust: "
	v := m.autoTrustValue()
	if v == "" {
		return []string{indent + styleDim.Render(label) + "off (agents ask before trusting a folder)"}
	}
	pad := indent + strings.Repeat(" ", len(label))
	var out []string
	for _, dir := range filepath.SplitList(v) {
		wrapped := ansi.Hardwrap(protocol.FlattenDisplayText(dir), setupWidth-len(pad), true)
		for _, l := range strings.Split(wrapped, "\n") {
			if len(out) == 0 {
				out = append(out, indent+styleDim.Render(label)+l)
			} else {
				out = append(out, pad+l)
			}
		}
	}
	return out
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
		cursor = styleArrow.Render(selectGlyph()) + " "
	} else {
		cursor = "  "
	}
	var g string
	if glyph != "" {
		if glyphColor != "" {
			g = brandStyle(glyphColor).Render(glyph) + "  "
		} else {
			g = glyph + "  "
		}
	}
	return "  " + cursor + g + styleSlug.Render(name)
}

func setupInputInline(value string) string { return setupInput(value, 40) }

// setupInput is a one-line bordered text field width cells wide. A longer
// value keeps its end (where the user types) and drops its start.
func setupInput(value string, width int) string {
	if room := width - 2; ansi.StringWidth(value) > room {
		value = ansi.TruncateLeft(value, ansi.StringWidth(value)-room+1, "…")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(0, 1).
		Width(width).
		Render(stylePrimary.Render(value))
}

// setupLabel is a field label: bright while the field has focus.
func setupLabel(text string, active bool) string {
	if active {
		return styleSlug.Render(text)
	}
	return styleDim.Render(text)
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

// tildePath shows p with the home directory abbreviated to "~", or as is
// when p is outside it or the home directory is unknown.
func tildePath(p string) string {
	home, err := paths.Home()
	if err != nil {
		return p
	}
	rel, err := filepath.Rel(home, p)
	switch {
	case err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
		return p
	case rel == ".":
		return "~"
	default:
		return "~" + string(filepath.Separator) + rel
	}
}

// DetectShellRC returns the path to the user's shell RC file based on $SHELL.
// Exported for use by internal/surface/cli/setup.go's preview renderer.
func DetectShellRC() string { return detectShellRC() }

// detectShellRC is the internal implementation. It returns "" when the
// home directory is unknown: a relative ".zshrc" would edit a file in the
// working directory.
func detectShellRC() string {
	home, err := paths.Home()
	if err != nil {
		return ""
	}
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
// Exported for use by internal/surface/cli/setup.go's preview renderer.
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
// Content outside the markers and the file's permission bits are preserved.
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

	// A new profile's directory (e.g. ~/.config/fish) gets the XDG 0700.
	if err := os.MkdirAll(filepath.Dir(rcPath), 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// The profile is the user's file: keep its mode across the rewrite. A
	// profile rex creates is owner-only; the shell reads it as its owner.
	mode := os.FileMode(0o600)
	if info, err := os.Stat(rcPath); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := rcPath + ".rextmp"
	if err := os.WriteFile(tmp, []byte(newContent), 0o600); err != nil { //nolint:gosec // G703: rcPath is the user's own shell profile under $HOME, chosen from $SHELL.
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod tmp: %w", err)
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

// ── Entry point (called from internal/surface/cli/setup.go) ──────────────────────────

// RunSetupWizard launches the setup wizard as a standalone Bubble Tea program.
// autoTrust, when set, pre-fills the folder trust step (rex setup -auto-trust).
func RunSetupWizard(configPath, socket, autoTrust string) error {
	slog.Info("setup: launching TUI wizard", "config_path", configPath, "socket", socket)
	model := NewSetupWizardModel(configPath, socket).withAutoTrust(autoTrust)
	// Draw the wizard in the user's own theme, not the built-in default.
	applyThemeFrom(model.themes, model.themeOptions[model.themeIdx])
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		slog.Error("setup: wizard error", "err", err)
	}
	return err
}

// wizardSpawnChoice resolves the wizard's tool and free-text model against
// the registry. The effort list the wizard shows belongs to the tool's first
// model, so an effort the typed model does not offer falls back to that
// model's default instead of failing.
func wizardSpawnChoice(tools []registry.Tool, toolID, modelID, effort string) (registry.SpawnChoice, error) {
	reg := &registry.Registry{Tools: tools}
	choice, _, err := reg.ResolveSpawn(registry.SpawnChoice{}, toolID, modelID, "")
	if err != nil {
		return registry.SpawnChoice{}, err
	}
	if effort != "" {
		if c, _, err := reg.ResolveSpawn(choice, "", "", effort); err == nil {
			choice = c
		}
	}
	return choice, nil
}

// ResolveSpawnDefaults checks the tool, model and effort given to rex setup
// against the registry, on top of the spawn defaults already in the config
// at configPath. An empty tool, model and effort changes nothing. The error
// names the valid ids.
func ResolveSpawnDefaults(configPath, toolID, modelID, effortVal string) (registry.SpawnChoice, bool, error) {
	if toolID == "" && modelID == "" && effortVal == "" {
		return registry.SpawnChoice{}, false, nil
	}
	store := settings.NewStore()
	if err := store.Load(configPath); err != nil {
		return registry.SpawnChoice{}, false, err
	}
	return resolveSpawnDefaults(store, toolID, modelID, effortVal)
}

func resolveSpawnDefaults(store *settings.Store, toolID, modelID, effortVal string) (registry.SpawnChoice, bool, error) {
	if toolID == "" && modelID == "" && effortVal == "" {
		return registry.SpawnChoice{}, false, nil
	}
	reg, err := loadToolsStrict()
	if err != nil {
		return registry.SpawnChoice{}, false, fmt.Errorf("load tools: %w", err)
	}
	cur := registry.SpawnChoice{
		Tool:   store.String("default_spawn_tool"),
		Model:  store.String("default_spawn_model"),
		Effort: store.String("default_spawn_effort"),
	}
	return reg.ResolveSpawn(cur, toolID, modelID, effortVal)
}

// DefaultsReport says what ApplyDefaults did, for the caller to render.
type DefaultsReport struct {
	// ConfigPath is the config.yaml that was written.
	ConfigPath string
	// RCPath is the shell profile the block went into, "" when none was
	// touched; RCState is "installed", "present" (already there), "failed"
	// or "skipped" (-no-shell, or no profile found).
	RCPath, RCState string
	// RCError is why the block could not be installed (RCState "failed").
	RCError string
	// AutoTrust is the auto_trust_dirs value written, "" when unchanged.
	AutoTrust string
	// NextCommand is the command to run after setup.
	NextCommand string
}

// ApplyDefaultsNonInteractive is ApplyDefaults for callers that do not
// render the report.
func ApplyDefaultsNonInteractive(configPath, socket string, toolID, modelID, effortVal, autoTrust string, noShell bool) error {
	_, err := ApplyDefaults(configPath, socket, toolID, modelID, effortVal, autoTrust, noShell)
	return err
}

// ApplyDefaults writes the config without prompting. Used by -yes.
// A non-empty autoTrust is written to auto_trust_dirs after the same checks
// the daemon applies; empty leaves the current value alone. It prints
// nothing: the caller renders the report.
func ApplyDefaults(configPath, socket string, toolID, modelID, effortVal, autoTrust string, noShell bool) (DefaultsReport, error) {
	rep := DefaultsReport{ConfigPath: configPath, RCState: "skipped"}
	slog.Info("setup: non-interactive start",
		"config_path", configPath, "socket", socket,
		"tool", toolID, "model", modelID, "effort", effortVal, "auto_trust", autoTrust)

	store := settings.NewStore()
	// Load existing so we don't wipe unrelated keys
	_ = store.Load(configPath)

	spawn, spawnChanged, err := resolveSpawnDefaults(store, toolID, modelID, effortVal)
	if err != nil {
		return rep, err
	}

	if autoTrust != "" {
		v, err := settings.CheckAutoTrustDirs(autoTrust)
		if err != nil {
			return rep, fmt.Errorf("auto-trust: %w", err)
		}
		if err := store.Set("auto_trust_dirs", v); err != nil {
			return rep, fmt.Errorf("auto-trust: %w", err)
		}
		rep.AutoTrust = v
	}

	if spawnChanged {
		for id, v := range map[string]string{
			"default_spawn_tool":   spawn.Tool,
			"default_spawn_model":  spawn.Model,
			"default_spawn_effort": spawn.Effort,
		} {
			if err := store.Set(id, v); err != nil {
				return rep, fmt.Errorf("%s: %w", id, err)
			}
		}
	}

	// Check if config already exists and is non-empty — non-interactive treats
	// an existing non-empty config as a no-op unless explicit overwrite is implied
	// by calling with --force (checked by the caller before invoking this func).

	slog.Info("setup: writing config", "path", configPath)
	if err := store.Save(configPath); err != nil {
		return rep, fmt.Errorf("save config: %w", err)
	}
	slog.Info("setup: config written", "path", configPath)
	if autoTrust != "" {
		reloadDaemonForTrust(nil, socket, "auto_trust_dirs")
	}

	if !noShell {
		rcPath := detectShellRC()
		if rcPath != "" {
			rep.RCPath = rcPath
			if hasShellBlock(rcPath) {
				rep.RCState = "present"
				slog.Info("setup: shell block already installed, skipping", "rc", rcPath)
			} else {
				slog.Info("setup: installing shell block", "rc", rcPath)
				if err := installShellBlock(rcPath); err != nil {
					// non-fatal: warn and continue
					slog.Error("setup: shell block install failed", "rc", rcPath, "err", err)
					rep.RCState, rep.RCError = "failed", err.Error()
				} else {
					rep.RCState = "installed"
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
	rep.NextCommand = NextCommand(toolDisplay, modelDisplay, effortVal)
	return rep, nil
}

// NextCommand is the command setup suggests running next.
func NextCommand(tool, model, effort string) string {
	next := fmt.Sprintf(`rex new -tool %s -model %s -- "your first task"`, tool, model)
	if effort != "" {
		next = fmt.Sprintf(`rex new -tool %s -model %s -effort %s -- "your first task"`, tool, model, effort)
	}
	return next
}

// styleArrow is declared in prompt.go and shared across the tui package.
