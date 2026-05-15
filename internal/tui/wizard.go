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
	wizConfirm
)

// WizardState lives on Model when Focus == FocusWizard.
type WizardState struct {
	Step     wizardStep
	Tools    []registry.Tool
	ToolIdx  int
	ModelIdx int
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
	m.Wizard = &WizardState{Step: wizProvider, Tools: visible}
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

func updateWizardKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Wizard == nil {
		return m, nil
	}
	switch k.String() {
	case "esc":
		m.Wizard = nil
		m.Focus = FocusBoard
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
		}
		return m, nil
	case "b":
		if m.Wizard.Step > wizProvider {
			m.Wizard.Step--
		}
		return m, nil
	case "enter":
		switch m.Wizard.Step {
		case wizProvider, wizModel:
			m.Wizard.Step++
			return m, nil
		case wizConfirm:
			tool := m.Wizard.Tools[m.Wizard.ToolIdx]
			model := tool.Models[m.Wizard.ModelIdx]
			cwd, _ := os.Getwd()
			cmd := wizardLaunchCmd(m.Client, tool.ID, model.ID, cwd, model.ID)
			m.Wizard = nil
			m.Focus = FocusBoard
			return m, cmd
		}
	}
	return m, nil
}

func wizardLaunchCmd(c *client.Client, toolID, modelID, cwd, slugHint string) tea.Cmd {
	return func() tea.Msg {
		slug := deriveSlugFromPrompt(slugHint + "-" + fmt.Sprintf("%d", os.Getpid()))
		if slug == "" {
			slug = "session"
		}
		err := c.NewSession(protocol.NewSession{
			ToolID:  toolID,
			ModelID: modelID,
			Slug:    slug,
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
	var b strings.Builder
	b.WriteString(styleHeaderMeta.Render(fmt.Sprintf("step %d / 3 — ", int(m.Wizard.Step)+1)))
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
	case wizConfirm:
		tool := m.Wizard.Tools[m.Wizard.ToolIdx]
		model := tool.Models[m.Wizard.ModelIdx]
		b.WriteString(styleHeaderApp.Render("confirm and launch\n\n"))
		b.WriteString(styleSlug.Render(tool.Name) + " · " + styleSlug.Render(model.Name) + "\n\n")
		b.WriteString(styleArrow.Render("▸ ") + styleHeaderApp.Render("[ launch ]") + "\n")
	}
	b.WriteString("\n" + styleDim.Render("j/k select · enter next · b back · esc cancel"))
	return b.String()
}
