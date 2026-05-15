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

type wizardStep int

const (
	wizProvider wizardStep = iota
	wizModel
	wizEffort
	wizName
	wizConfirm
)

type wizardField int

const (
	fieldSlug wizardField = iota
	fieldTitle
	fieldCWD
)

// WizardState lives on Model when Focus == FocusWizard.
type WizardState struct {
	Step      wizardStep
	Tools     []registry.Tool
	ToolIdx   int
	ModelIdx  int
	EffortIdx int
	SlugText  string
	TitleText string
	CWDText   string
	Field     wizardField
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
	cwd, _ := os.Getwd()
	m.Wizard = &WizardState{Step: wizProvider, Tools: visible, CWDText: cwd}
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

// hiddenInWizard lists tool IDs we don't expose in the new-agent wizard.
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

// currentModel returns the model selected in step 2.
func (w *WizardState) currentModel() registry.Model {
	if w.ToolIdx >= len(w.Tools) {
		return registry.Model{}
	}
	tool := w.Tools[w.ToolIdx]
	if w.ModelIdx >= len(tool.Models) {
		return registry.Model{}
	}
	return tool.Models[w.ModelIdx]
}

// currentEffort returns the chosen effort label, or "" if no effort applies.
func (w *WizardState) currentEffort() string {
	m := w.currentModel()
	if m.Effort == nil || len(m.Effort.Options) == 0 {
		return ""
	}
	if w.EffortIdx >= len(m.Effort.Options) {
		return ""
	}
	return m.Effort.Options[w.EffortIdx]
}

// effortApplies says whether step 3 is shown.
func (w *WizardState) effortApplies() bool {
	m := w.currentModel()
	return m.Effort != nil && len(m.Effort.Options) > 0
}

// defaultSlug derives a slug from the model selection.
func (w *WizardState) defaultSlug() string {
	m := w.currentModel()
	return deriveSlugFromPrompt(m.ID + "-" + fmt.Sprintf("%d", os.Getpid()))
}

func nextWizardStep(w *WizardState) {
	switch w.Step {
	case wizProvider:
		w.Step = wizModel
	case wizModel:
		// Reset effort index when re-entering the step.
		w.EffortIdx = 0
		if w.effortApplies() {
			// Pre-select the default if known.
			m := w.currentModel()
			for i, opt := range m.Effort.Options {
				if opt == m.Effort.Default {
					w.EffortIdx = i
					break
				}
			}
			w.Step = wizEffort
		} else {
			w.Step = wizName
			if w.SlugText == "" {
				w.SlugText = w.defaultSlug()
			}
		}
	case wizEffort:
		w.Step = wizName
		if w.SlugText == "" {
			w.SlugText = w.defaultSlug()
		}
	case wizName:
		w.Step = wizConfirm
	}
}

func prevWizardStep(w *WizardState) {
	switch w.Step {
	case wizModel:
		w.Step = wizProvider
	case wizEffort:
		w.Step = wizModel
	case wizName:
		if w.effortApplies() {
			w.Step = wizEffort
		} else {
			w.Step = wizModel
		}
	case wizConfirm:
		w.Step = wizName
	}
}

func updateWizardKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Wizard == nil {
		return m, nil
	}

	// Step-4 (name) has text inputs — handle differently.
	if m.Wizard.Step == wizName {
		return updateWizardNameStep(m, k)
	}

	switch k.String() {
	case "esc":
		m.Wizard = nil
		m.Focus = FocusBoard
		if m.Audio != nil {
			m.Audio.Play(audio.EventClose)
		}
		return m, nil
	case "b":
		prevWizardStep(m.Wizard)
		if m.Audio != nil {
			m.Audio.Play(audio.EventNav)
		}
		return m, nil
	case "j", "down":
		moved := false
		switch m.Wizard.Step {
		case wizProvider:
			if m.Wizard.ToolIdx+1 < len(m.Wizard.Tools) {
				m.Wizard.ToolIdx++
				m.Wizard.ModelIdx = 0
				moved = true
			}
		case wizModel:
			tool := m.Wizard.Tools[m.Wizard.ToolIdx]
			if m.Wizard.ModelIdx+1 < len(tool.Models) {
				m.Wizard.ModelIdx++
				moved = true
			}
		case wizEffort:
			opts := m.Wizard.currentModel().Effort.Options
			if m.Wizard.EffortIdx+1 < len(opts) {
				m.Wizard.EffortIdx++
				moved = true
			}
		}
		if moved && m.Audio != nil {
			m.Audio.Play(audio.EventNav)
		}
		return m, nil
	case "k", "up":
		moved := false
		switch m.Wizard.Step {
		case wizProvider:
			if m.Wizard.ToolIdx > 0 {
				m.Wizard.ToolIdx--
				m.Wizard.ModelIdx = 0
				moved = true
			}
		case wizModel:
			if m.Wizard.ModelIdx > 0 {
				m.Wizard.ModelIdx--
				moved = true
			}
		case wizEffort:
			if m.Wizard.EffortIdx > 0 {
				m.Wizard.EffortIdx--
				moved = true
			}
		}
		if moved && m.Audio != nil {
			m.Audio.Play(audio.EventNav)
		}
		return m, nil
	case "enter":
		switch m.Wizard.Step {
		case wizConfirm:
			tool := m.Wizard.Tools[m.Wizard.ToolIdx]
			model := tool.Models[m.Wizard.ModelIdx]
			slug := m.Wizard.SlugText
			if slug == "" {
				slug = m.Wizard.defaultSlug()
			}
			cwd := m.Wizard.CWDText
			if cwd == "" {
				cwd, _ = os.Getwd()
			}
			cmd := wizardLaunchCmd(m.Client, tool.ID, model.ID, m.Wizard.currentEffort(), slug, m.Wizard.TitleText, cwd)
			m.Wizard = nil
			m.Focus = FocusBoard
			if m.Audio != nil {
				m.Audio.Play(audio.EventClose)
			}
			return m, cmd
		default:
			nextWizardStep(m.Wizard)
			if m.Audio != nil {
				m.Audio.Play(audio.EventNav)
			}
			return m, nil
		}
	}
	return m, nil
}

func updateWizardNameStep(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.Wizard = nil
		m.Focus = FocusBoard
		if m.Audio != nil {
			m.Audio.Play(audio.EventClose)
		}
		return m, nil
	case tea.KeyTab:
		m.Wizard.Field = (m.Wizard.Field + 1) % 3
		if m.Audio != nil {
			m.Audio.Play(audio.EventNav)
		}
		return m, nil
	case tea.KeyShiftTab:
		m.Wizard.Field = (m.Wizard.Field + 2) % 3
		if m.Audio != nil {
			m.Audio.Play(audio.EventNav)
		}
		return m, nil
	case tea.KeyEnter:
		nextWizardStep(m.Wizard)
		if m.Audio != nil {
			m.Audio.Play(audio.EventNav)
		}
		return m, nil
	case tea.KeyBackspace:
		s := m.Wizard.fieldValue()
		if len(s) > 0 {
			m.Wizard.setFieldValue(s[:len(s)-1])
		}
		return m, nil
	case tea.KeyRunes:
		m.Wizard.setFieldValue(m.Wizard.fieldValue() + string(k.Runes))
		return m, nil
	case tea.KeySpace:
		m.Wizard.setFieldValue(m.Wizard.fieldValue() + " ")
		return m, nil
	}
	switch k.String() {
	case "b":
		prevWizardStep(m.Wizard)
		if m.Audio != nil {
			m.Audio.Play(audio.EventNav)
		}
		return m, nil
	}
	return m, nil
}

func (w *WizardState) fieldValue() string {
	switch w.Field {
	case fieldSlug:
		return w.SlugText
	case fieldTitle:
		return w.TitleText
	case fieldCWD:
		return w.CWDText
	}
	return ""
}

func (w *WizardState) setFieldValue(v string) {
	switch w.Field {
	case fieldSlug:
		w.SlugText = v
	case fieldTitle:
		w.TitleText = v
	case fieldCWD:
		w.CWDText = v
	}
}

func wizardLaunchCmd(c *client.Client, toolID, modelID, effort, slug, title, cwd string) tea.Cmd {
	return func() tea.Msg {
		if slug == "" {
			slug = "session"
		}
		err := c.NewSession(protocol.NewSession{
			ToolID:  toolID,
			ModelID: modelID,
			Effort:  effort,
			Slug:    slug,
			Title:   title,
			CWD:     cwd,
		})
		if err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

// Wizard inner content dimensions: fixed so every step renders at the same size
// and feels roomy enough for the largest step (model list under Claude).
const (
	wizardWidth      = 78
	wizardBodyLines  = 12 // option/field area between title and footer
	wizardFooterPush = 1  // blank line before footer
)

// renderWizard returns the wizard popup body (no border — overlay wraps it).
// Every step pads its body to the same number of lines so the popup keeps a
// constant footprint as the user steps through it.
func renderWizard(m Model) string {
	if m.Wizard == nil {
		return ""
	}
	totalSteps := 5
	stepNum := int(m.Wizard.Step) + 1

	titleLine := func(rest string) string {
		return styleHeaderMeta.Render(fmt.Sprintf("step %d / %d — ", stepNum, totalSteps)) +
			styleHeaderApp.Render(rest)
	}

	var (
		title string
		body  []string
	)

	switch m.Wizard.Step {
	case wizProvider:
		title = titleLine("choose a provider")
		for i, t := range m.Wizard.Tools {
			body = append(body, wizOption(i == m.Wizard.ToolIdx, t.Name, "", t.Category, wizardWidth))
		}
	case wizModel:
		tool := m.Wizard.Tools[m.Wizard.ToolIdx]
		title = titleLine("choose a model — " + tool.Name)
		for i, mm := range tool.Models {
			body = append(body, wizOption(i == m.Wizard.ModelIdx, mm.Name, "", "", wizardWidth))
		}
	case wizEffort:
		m2 := m.Wizard.currentModel()
		title = titleLine("reasoning effort — " + m2.Name)
		for i, opt := range m2.Effort.Options {
			body = append(body, wizOption(i == m.Wizard.EffortIdx, opt, "", "", wizardWidth))
		}
	case wizName:
		title = titleLine("name your agent")
		body = append(body, wizField("slug", m.Wizard.SlugText, m.Wizard.Field == fieldSlug, wizardWidth))
		body = append(body, wizField("title", m.Wizard.TitleText, m.Wizard.Field == fieldTitle, wizardWidth))
		body = append(body, wizField("working dir", m.Wizard.CWDText, m.Wizard.Field == fieldCWD, wizardWidth))
	case wizConfirm:
		tool := m.Wizard.Tools[m.Wizard.ToolIdx]
		model := tool.Models[m.Wizard.ModelIdx]
		title = titleLine("confirm and launch")
		summary := styleSlug.Render(tool.Name) + styleDim.Render(" · ") + styleSlug.Render(model.Name)
		if eff := m.Wizard.currentEffort(); eff != "" {
			summary += styleDim.Render(" · effort: ") + styleSlug.Render(eff)
		}
		body = append(body, "  "+summary)
		body = append(body, "  "+styleDim.Render(m.Wizard.CWDText)+styleDim.Render("  ·  slug: ")+styleSlug.Render(m.Wizard.SlugText))
		body = append(body, "")
		body = append(body, "  "+styleArrow.Render("▸")+" "+styleSlug.Render("[ launch ]"))
	}

	// Pad / truncate body to a fixed number of lines so the popup stays a constant size.
	if len(body) < wizardBodyLines {
		for len(body) < wizardBodyLines {
			body = append(body, "")
		}
	} else if len(body) > wizardBodyLines {
		body = body[:wizardBodyLines]
	}

	var footer string
	if m.Wizard.Step == wizName {
		footer = styleDim.Render("tab cycles fields · enter next · b back · esc cancel")
	} else {
		footer = styleDim.Render("j/k select · enter next · b back · esc cancel")
	}

	lines := []string{title, ""}
	lines = append(lines, body...)
	for i := 0; i < wizardFooterPush; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, footer)

	// Pad every line to the same width so the bordered popup keeps a constant
	// rectangular footprint across steps regardless of content length.
	for i, line := range lines {
		lines[i] = padLine(line, wizardWidth)
	}
	return strings.Join(lines, "\n")
}

// wizOption renders a single selectable row in the wizard.
// Columns: cursor(2) + name(left-flex) + right tag (auto).
func wizOption(selected bool, name, hint, tag string, width int) string {
	var cursor string
	if selected {
		cursor = styleArrow.Render("▸") + " "
	} else {
		cursor = "  "
	}
	left := cursor + styleSlug.Render(name)
	if hint != "" {
		left += "  " + styleDim.Render(hint)
	}
	if tag == "" {
		return left
	}
	rightTxt := styleDim.Render(tag)
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(rightTxt)
	gap := width - leftW - rightW
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + rightTxt
}

// wizField renders a name-step field row.
func wizField(label, value string, focused bool, width int) string {
	cursor := "  "
	if focused {
		cursor = styleArrow.Render("▸") + " "
	}
	lbl := lipgloss.NewStyle().Foreground(colorFgDim).Render(label)
	// Pad label to 14 cells using plain spaces (no styling on padding).
	lblPad := 14 - lipgloss.Width(lbl)
	if lblPad < 1 {
		lblPad = 1
	}
	var val string
	switch {
	case value == "" && focused:
		val = cursorBlock(Model{SpinnerTick: 0})
	case value == "":
		val = styleMuted.Render("(empty)")
	default:
		val = styleSlug.Render(value)
	}
	return cursor + lbl + strings.Repeat(" ", lblPad) + val
}
