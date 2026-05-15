package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
)

// wizardStep names the three phases of the new-agent flow.
//
// The model selection step from the previous flow is gone — the wizard
// auto-picks the first model defined for each tool. Slug edit and confirm
// steps are also gone; the slug is derived from the task description and
// pressing enter on the describe step launches the session directly.
type wizardStep int

const (
	wizProvider wizardStep = iota
	wizEffort
	wizDescribe
)

// WizardState lives on Model when Focus == FocusWizard.
type WizardState struct {
	Step      wizardStep
	Tools     []registry.Tool
	ToolIdx   int
	EffortIdx int
	TaskText  string
}

func openWizard(m Model) (Model, tea.Cmd) {
	reg, err := registry.Load(toolsConfigPath())
	if err != nil {
		m.Err = "wizard: " + err.Error()
		return m, nil
	}
	visible := visibleTools(reg.Tools)
	if len(visible) == 0 {
		m.Err = "wizard: no tools enabled"
		return m, nil
	}
	w := &WizardState{Step: wizProvider, Tools: visible}
	presetEffortDefault(w)
	m.Wizard = w
	m.Focus = FocusWizard
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	return m, nil
}

func toolsConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rex", "tools.yaml")
}

var hiddenInWizard = map[string]bool{
	"echo": true, // internal test adapter, never user-visible
}

func visibleTools(tools []registry.Tool) []registry.Tool {
	out := make([]registry.Tool, 0, len(tools))
	for _, t := range tools {
		if hiddenInWizard[t.ID] {
			continue
		}
		if t.EnabledByDefault != nil && !*t.EnabledByDefault {
			continue
		}
		out = append(out, t)
	}
	return out
}

// firstModel returns the auto-picked model for the selected tool — the first
// one listed. tools.yaml is authored with the strongest/default model first.
func (w *WizardState) firstModel() registry.Model {
	if w.ToolIdx >= len(w.Tools) {
		return registry.Model{}
	}
	t := w.Tools[w.ToolIdx]
	if len(t.Models) == 0 {
		return registry.Model{}
	}
	return t.Models[0]
}

func (w *WizardState) currentEffort() string {
	m := w.firstModel()
	if m.Effort == nil || len(m.Effort.Options) == 0 {
		return ""
	}
	if w.EffortIdx < 0 || w.EffortIdx >= len(m.Effort.Options) {
		return ""
	}
	return m.Effort.Options[w.EffortIdx]
}

func (w *WizardState) effortApplies() bool {
	m := w.firstModel()
	return m.Effort != nil && len(m.Effort.Options) > 0
}

// presetEffortDefault snaps EffortIdx onto the model's declared default.
func presetEffortDefault(w *WizardState) {
	if !w.effortApplies() {
		w.EffortIdx = 0
		return
	}
	m := w.firstModel()
	for i, opt := range m.Effort.Options {
		if opt == m.Effort.Default {
			w.EffortIdx = i
			return
		}
	}
	w.EffortIdx = 0
}

func nextWizardStep(w *WizardState) {
	switch w.Step {
	case wizProvider:
		presetEffortDefault(w)
		if w.effortApplies() {
			w.Step = wizEffort
		} else {
			w.Step = wizDescribe
		}
	case wizEffort:
		w.Step = wizDescribe
	}
}

func prevWizardStep(w *WizardState) {
	switch w.Step {
	case wizEffort:
		w.Step = wizProvider
	case wizDescribe:
		if w.effortApplies() {
			w.Step = wizEffort
		} else {
			w.Step = wizProvider
		}
	}
}

func updateWizardKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Wizard == nil {
		return m, nil
	}
	if m.Wizard.Step == wizDescribe {
		return updateWizardDescribeStep(m, k)
	}
	switch k.String() {
	case "esc":
		return closeWizard(m), nil
	case "b":
		prevWizardStep(m.Wizard)
		playNav(m)
		return m, nil
	case "j", "down":
		return moveWizard(m, +1), nil
	case "k", "up":
		return moveWizard(m, -1), nil
	case "enter":
		nextWizardStep(m.Wizard)
		playNav(m)
		return m, nil
	}
	return m, nil
}

func moveWizard(m Model, delta int) Model {
	if m.Wizard == nil {
		return m
	}
	moved := false
	switch m.Wizard.Step {
	case wizProvider:
		next := m.Wizard.ToolIdx + delta
		if next >= 0 && next < len(m.Wizard.Tools) {
			m.Wizard.ToolIdx = next
			moved = true
		}
	case wizEffort:
		opts := m.Wizard.firstModel().Effort.Options
		next := m.Wizard.EffortIdx + delta
		if next >= 0 && next < len(opts) {
			m.Wizard.EffortIdx = next
			moved = true
		}
	}
	if moved {
		playNav(m)
	}
	return m
}

func updateWizardDescribeStep(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		return closeWizard(m), nil
	case tea.KeyEnter:
		task := strings.TrimSpace(m.Wizard.TaskText)
		tool := m.Wizard.Tools[m.Wizard.ToolIdx]
		model := m.Wizard.firstModel()
		slug := deriveAgentSlug(tool.ID, model.ID, task, existingSlugs(m))
		cwd, _ := os.Getwd()
		cmd := wizardLaunchCmd(m.Client,
			tool.ID, model.ID, m.Wizard.currentEffort(),
			slug, task, cwd, task)
		return closeWizard(m), cmd
	case tea.KeyBackspace:
		if len(m.Wizard.TaskText) > 0 {
			m.Wizard.TaskText = m.Wizard.TaskText[:len(m.Wizard.TaskText)-1]
		}
		return m, nil
	case tea.KeyRunes:
		m.Wizard.TaskText += string(k.Runes)
		return m, nil
	case tea.KeySpace:
		m.Wizard.TaskText += " "
		return m, nil
	}
	if k.String() == "b" && m.Wizard.TaskText == "" {
		// Only treat 'b' as "back" when the input is empty — otherwise it's a character.
		prevWizardStep(m.Wizard)
		playNav(m)
		return m, nil
	}
	return m, nil
}

func closeWizard(m Model) Model {
	m.Wizard = nil
	m.Focus = FocusBoard
	if m.Audio != nil {
		m.Audio.Play(audio.EventClose)
	}
	return m
}

func playNav(m Model) {
	if m.Audio != nil {
		m.Audio.Play(audio.EventNav)
	}
}

func existingSlugs(m Model) []string {
	out := make([]string, 0, len(m.Sessions))
	for _, s := range m.Sessions {
		if s.Slug != "" {
			out = append(out, s.Slug)
		}
	}
	return out
}

func wizardLaunchCmd(c *client.Client, toolID, modelID, effort, slug, title, cwd, initialPrompt string) tea.Cmd {
	return func() tea.Msg {
		if slug == "" {
			slug = "session"
		}
		err := c.NewSession(protocol.NewSession{
			ToolID:        toolID,
			ModelID:       modelID,
			Effort:        effort,
			Slug:          slug,
			Title:         title,
			CWD:           cwd,
			InitialPrompt: initialPrompt,
		})
		if err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

// ── Slug schema ────────────────────────────────────────────────────────────
//
// Format: <toolShort>.<modelShort>.<taskKebab>
//
//	cc.opus.fix-auth-bug
//	cx.gpt-5-codex.migrate-billing
//	gm.2-5-pro.refactor-auth
//	ol.llama3-1.write-readme
//
// Empty task → <toolShort>.<modelShort>.<hash4>, e.g. cc.opus.a3f2
// Collision → <slug>-2, <slug>-3, … against the client's known session set.

var toolShortcodes = map[string]string{
	"claude":   "cc",
	"codex":    "cx",
	"gemini":   "gm",
	"ollama":   "ol",
	"grok":     "gk",
	"deepseek": "ds",
	"kimi":     "km",
	"echo":     "ec",
}

func toolShort(id string) string {
	if s, ok := toolShortcodes[id]; ok {
		return s
	}
	if len(id) >= 2 {
		return id[:2]
	}
	return id
}

// modelShort canonicalizes a model ID into a slug-safe fragment by lowercasing,
// turning dots into dashes, and dropping anything else.
func modelShort(id string) string {
	s := strings.ToLower(id)
	s = strings.ReplaceAll(s, ".", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

// kebabTask normalizes free-form task text into kebab-case, truncated to ~32
// chars on a word boundary.
func kebabTask(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 32 {
		cut := out[:32]
		if i := strings.LastIndexByte(cut, '-'); i > 16 {
			cut = cut[:i]
		}
		out = cut
	}
	return out
}

// hash4 returns 4 hex characters from an FNV-1a hash of s. Used as a fallback
// task fragment when the user gave no description.
func hash4(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%04x", h&0xffff)
}

// deriveAgentSlug builds the namespaced slug and disambiguates against the
// given set of existing slugs by appending -2, -3, … on collision.
func deriveAgentSlug(toolID, modelID, task string, existing []string) string {
	base := toolShort(toolID)
	if ms := modelShort(modelID); ms != "" {
		base += "." + ms
	}
	taskFrag := kebabTask(task)
	if taskFrag == "" {
		taskFrag = hash4(fmt.Sprintf("%s|%d", modelID, os.Getpid()))
	}
	candidate := base + "." + taskFrag
	if !inSlice(existing, candidate) {
		return candidate
	}
	for n := 2; n < 1000; n++ {
		c := fmt.Sprintf("%s-%d", candidate, n)
		if !inSlice(existing, c) {
			return c
		}
	}
	return candidate
}

func inSlice(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ── Rendering ──────────────────────────────────────────────────────────────

// Wizard inner content dimensions: fixed so every step renders at the same size.
const (
	wizardWidth     = 78
	wizardBodyLines = 12
	wizardInputCols = 60
)

func renderWizard(m Model) string {
	if m.Wizard == nil {
		return ""
	}

	var (
		title string
		body  []string
	)

	switch m.Wizard.Step {
	case wizProvider:
		title = styleSlug.Render("which provider?")
		body = append(body, "")
		for i, t := range m.Wizard.Tools {
			body = append(body, wizOption(i == m.Wizard.ToolIdx, t.Icon, t.Name, t.Color))
		}
	case wizEffort:
		title = styleSlug.Render("reasoning effort?")
		body = append(body, "")
		opts := m.Wizard.firstModel().Effort.Options
		for i, opt := range opts {
			body = append(body, wizOption(i == m.Wizard.EffortIdx, "", opt, ""))
		}
	case wizDescribe:
		title = styleSlug.Render("describe the task")
		body = append(body, "")
		body = append(body, wizInputBoxLines(m.Wizard.TaskText, m)...)
	}

	// Pad / truncate to a fixed line budget so the popup stays the same size.
	body = padOrTrim(body, wizardBodyLines)

	footer := wizardFooter(m.Wizard.Step)

	lines := []string{title}
	lines = append(lines, body...)
	lines = append(lines, "", footer)

	for i, line := range lines {
		lines[i] = padLine(line, wizardWidth)
	}
	return strings.Join(lines, "\n")
}

func wizardFooter(step wizardStep) string {
	switch step {
	case wizProvider:
		return styleDim.Render("  j/k select   enter next   esc cancel")
	case wizEffort:
		return styleDim.Render("  j/k select   enter next   b back   esc cancel")
	case wizDescribe:
		return styleDim.Render("  enter launch   b back (when empty)   esc cancel")
	}
	return ""
}

func padOrTrim(body []string, n int) []string {
	if len(body) < n {
		for len(body) < n {
			body = append(body, "")
		}
		return body
	}
	if len(body) > n {
		return body[:n]
	}
	return body
}

// wizOption renders a single selectable row: cursor + optional brand glyph + name.
func wizOption(selected bool, glyph, name, glyphColor string) string {
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

// wizInputBoxLines renders the task input box as a slice of lines (so it
// composes cleanly into the fixed-line body).
func wizInputBoxLines(value string, m Model) []string {
	content := styleSlug.Render(value) + cursorBlock(m)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWorking).
		Padding(0, 2).
		Width(wizardInputCols).
		Render(content)
	out := strings.Split(box, "\n")
	for i := range out {
		out[i] = "  " + out[i]
	}
	return out
}
