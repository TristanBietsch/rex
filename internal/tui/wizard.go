package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
	return m, nil
}

func toolsConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rex", "tools.yaml")
}

func visibleTools(tools []registry.Tool) []registry.Tool {
	out := make([]registry.Tool, 0, len(tools))
	for _, t := range tools {
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
		return m, nil
	case "b":
		prevWizardStep(m.Wizard)
		return m, nil
	case "j", "down":
		switch m.Wizard.Step {
		case wizProvider:
			if m.Wizard.ToolIdx+1 < len(m.Wizard.Tools) {
				m.Wizard.ToolIdx++
				m.Wizard.ModelIdx = 0
			}
		case wizModel:
			tool := m.Wizard.Tools[m.Wizard.ToolIdx]
			if m.Wizard.ModelIdx+1 < len(tool.Models) {
				m.Wizard.ModelIdx++
			}
		case wizEffort:
			opts := m.Wizard.currentModel().Effort.Options
			if m.Wizard.EffortIdx+1 < len(opts) {
				m.Wizard.EffortIdx++
			}
		}
		return m, nil
	case "k", "up":
		switch m.Wizard.Step {
		case wizProvider:
			if m.Wizard.ToolIdx > 0 {
				m.Wizard.ToolIdx--
				m.Wizard.ModelIdx = 0
			}
		case wizModel:
			if m.Wizard.ModelIdx > 0 {
				m.Wizard.ModelIdx--
			}
		case wizEffort:
			if m.Wizard.EffortIdx > 0 {
				m.Wizard.EffortIdx--
			}
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
			return m, cmd
		default:
			nextWizardStep(m.Wizard)
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
		return m, nil
	case tea.KeyTab:
		m.Wizard.Field = (m.Wizard.Field + 1) % 3
		return m, nil
	case tea.KeyShiftTab:
		m.Wizard.Field = (m.Wizard.Field + 2) % 3
		return m, nil
	case tea.KeyEnter:
		nextWizardStep(m.Wizard)
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

func renderWizard(m Model) string {
	if m.Wizard == nil {
		return ""
	}
	totalSteps := 5
	stepNum := int(m.Wizard.Step) + 1
	var b strings.Builder
	b.WriteString(styleHeaderMeta.Render(fmt.Sprintf("step %d / %d — ", stepNum, totalSteps)))
	switch m.Wizard.Step {
	case wizProvider:
		b.WriteString(styleHeaderApp.Render("choose a provider\n\n"))
		for i, t := range m.Wizard.Tools {
			cursor := "  "
			if i == m.Wizard.ToolIdx {
				cursor = styleArrow.Render("▸ ")
			}
			b.WriteString(cursor + styleSlug.Render(t.Name) + "  " + styleDim.Render("("+t.Category+")") + "\n")
		}
	case wizModel:
		tool := m.Wizard.Tools[m.Wizard.ToolIdx]
		b.WriteString(styleHeaderApp.Render("choose a model — " + tool.Name + "\n\n"))
		for i, mm := range tool.Models {
			cursor := "  "
			if i == m.Wizard.ModelIdx {
				cursor = styleArrow.Render("▸ ")
			}
			b.WriteString(cursor + styleSlug.Render(mm.Name) + "\n")
		}
	case wizEffort:
		m2 := m.Wizard.currentModel()
		b.WriteString(styleHeaderApp.Render("reasoning effort — " + m2.Name + "\n\n"))
		for i, opt := range m2.Effort.Options {
			cursor := "  "
			if i == m.Wizard.EffortIdx {
				cursor = styleArrow.Render("▸ ")
			}
			b.WriteString(cursor + styleSlug.Render(opt) + "\n")
		}
	case wizName:
		b.WriteString(styleHeaderApp.Render("name your agent\n\n"))
		b.WriteString(renderNameField("slug", m.Wizard.SlugText, m.Wizard.Field == fieldSlug))
		b.WriteString(renderNameField("title", m.Wizard.TitleText, m.Wizard.Field == fieldTitle))
		b.WriteString(renderNameField("working dir", m.Wizard.CWDText, m.Wizard.Field == fieldCWD))
	case wizConfirm:
		tool := m.Wizard.Tools[m.Wizard.ToolIdx]
		model := tool.Models[m.Wizard.ModelIdx]
		b.WriteString(styleHeaderApp.Render("confirm and launch\n\n"))
		b.WriteString(styleSlug.Render(tool.Name) + " · " + styleSlug.Render(model.Name))
		if eff := m.Wizard.currentEffort(); eff != "" {
			b.WriteString("  " + styleDim.Render("effort: ") + styleSlug.Render(eff))
		}
		b.WriteString("\n")
		b.WriteString(styleDim.Render(m.Wizard.CWDText) + "  ·  slug: " + styleSlug.Render(m.Wizard.SlugText) + "\n\n")
		b.WriteString(styleArrow.Render("▸ ") + styleHeaderApp.Render("[ launch ]") + "\n")
	}
	b.WriteString("\n")
	if m.Wizard.Step == wizName {
		b.WriteString(styleDim.Render("tab cycles fields · enter next · b back · esc cancel"))
	} else {
		b.WriteString(styleDim.Render("j/k select · enter next · b back · esc cancel"))
	}
	return b.String()
}

func renderNameField(label, value string, focused bool) string {
	lbl := styleDim.Render(fmt.Sprintf("%-12s ", label))
	val := value
	if focused {
		val = val + " "
		val = styleSelected.Render(val)
	} else {
		val = styleSlug.Render(val)
	}
	return lbl + val + "\n"
}
