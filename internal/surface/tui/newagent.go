package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/features/skills"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The new-agent page (R16): n and :new open one full-screen page with the
// agent, a multi-line task, context and trust, instead of the popup wizard,
// while new_agent_page is on. The page lands in the task editor with the
// last launch's agent; enter there (or ctrl+s anywhere) launches. esc keeps
// the draft in memory for the next n (D4); nothing of it is written to disk.

// naSection is a section of the Compose screen, in rail order.
type naSection int

const (
	naAgent naSection = iota
	naTask
	naContext
	naTrust
	naSectionCount
)

// naSectionNames are the rail's and the narrow strip's words.
var naSectionNames = [naSectionCount]string{"Agent", "Task", "Context", "Trust"}

// naScreen is the page's screen. Compose is the only one so far; the Agent
// screen, the import picker and the preview come with later R16 tasks.
type naScreen int

const naCompose naScreen = iota

// NewAgentState lives on Model while Focus == FocusNewAgent, and as
// Model.newAgentDraft after esc.
type NewAgentState struct {
	Section naSection
	Screen  naScreen
	// Tools are the agents offered (pickerTools) and Missing the ones left
	// out because they are not installed. ToolIdx indexes Tools, ModelIdx
	// the tool's models and EffortIdx the model's effort options.
	Tools                        []registry.Tool
	Missing                      []onboardRow
	ToolIdx, ModelIdx, EffortIdx int
	// Skills is the skill library read when the page opened, SkillsOn the
	// spawn_skills feature then, SkillSel the toggled names (as the wizard).
	Skills   []skills.Skill
	SkillsOn bool
	SkillSel map[string]bool
	Task     Composer
	// Trust is the folder-trust choice (P2, wizard_trust.go).
	Trust wizTrustMode
}

// selection is the state's agent choice as a WizardState, so the page
// shares the wizard's selection helpers (selectedTool, currentEffort, …).
func (st *NewAgentState) selection() *WizardState {
	return &WizardState{
		Tools: st.Tools, ToolIdx: st.ToolIdx, ModelIdx: st.ModelIdx, EffortIdx: st.EffortIdx,
		Skills: st.Skills, SkillsOn: st.SkillsOn, SkillSel: st.SkillSel,
	}
}

// choice is the selected tool, model and effort.
func (st *NewAgentState) choice() quickChoice {
	w := st.selection()
	return quickChoice{Tool: w.selectedTool().ID, Model: w.selectedModel().ID, Effort: w.currentEffort()}
}

// choose points the selection at c: an id that is not offered falls back to
// the first tool, the tool's first model and the model's default effort.
func (st *NewAgentState) choose(c quickChoice) {
	st.ToolIdx = max(slices.IndexFunc(st.Tools, func(t registry.Tool) bool { return t.ID == c.Tool }), 0)
	w := st.selection()
	st.ModelIdx = max(slices.IndexFunc(w.selectedTool().Models, func(md registry.Model) bool { return md.ID == c.Model }), 0)
	w.ModelIdx = st.ModelIdx
	presetEffortDefault(w)
	st.EffortIdx = w.EffortIdx
	if i := slices.Index(w.effortOptions(), c.Effort); i >= 0 && c.Effort != "" {
		st.EffortIdx = i
	}
}

// offers reports whether tool id is offered.
func (st *NewAgentState) offers(id string) bool {
	return slices.ContainsFunc(st.Tools, func(t registry.Tool) bool { return t.ID == id })
}

// taskTitle is the task's first non-blank line, trimmed: the card title and
// the slug's source.
func taskTitle(task string) string {
	for line := range strings.SplitSeq(task, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// request builds the NewSession the page stands for, as the wizard's
// request does: the whole task is the prompt, its first line the title and
// the slug's source, made unique against existing.
func (st *NewAgentState) request(cwd string, existing []string) protocol.NewSession {
	w := st.selection()
	task := strings.TrimSpace(st.Task.String())
	title := taskTitle(task)
	tool, model := w.selectedTool(), w.selectedModel()
	return protocol.NewSession{
		ToolID:        tool.ID,
		ModelID:       model.ID,
		Effort:        w.currentEffort(),
		Slug:          deriveAgentSlug(tool.ID, model.ID, title, existing),
		Title:         title,
		CWD:           cwd,
		InitialPrompt: task,
		Skills:        w.chosenSkills(),
	}
}

// openNew is n and :new: the new-agent page while new_agent_page is on,
// else the wizard.
func openNew(m Model) (Model, tea.Cmd) {
	if m.featureOn("new_agent_page") {
		return openNewAgent(m)
	}
	return openWizard(m)
}

// openNewAgent opens the page on the agents pickerTools offers, restoring
// the draft left with esc. The agent is the draft's, else the last launch
// (D3), else the quick-spawn defaults; one no longer offered falls back
// with a notice.
func openNewAgent(m Model) (Model, tea.Cmd) {
	reg, err := loadToolsStrict()
	if err != nil {
		m.Err = "new agent: " + err.Error()
		return m, nil
	}
	visible, missing := pickerTools(m, reg)
	if len(visible) == 0 {
		m.Err = "new agent: no tools enabled"
		if len(missing) > 0 {
			m.Err = "new agent: no agent is installed (see Settings → Onboarding)"
		}
		return m, nil
	}
	st := &NewAgentState{Section: naTask, Screen: naCompose, Tools: visible, Missing: missing}
	if m.featureOn("spawn_skills") {
		st.SkillsOn = true
		st.Skills = loadWizardSkills()
	}
	want := m.lastSpawn
	if d := m.newAgentDraft; d != nil {
		st.Section, st.Task, st.Trust, st.SkillSel = d.Section, d.Task, d.Trust, d.SkillSel
		c := d.choice()
		want = &c
	}
	m.Notice = ""
	switch {
	case want != nil && st.offers(want.Tool):
		st.choose(*want)
	default:
		c, notice := quickChoice{}, ""
		if m.Store != nil {
			c, notice = quickChoiceOf(m)
		}
		st.choose(c)
		if want != nil && want.Tool != "" {
			notice = unavailableNotice(want.Tool, st.choice().Tool)
		}
		m.Notice = notice
	}
	m.NewAgent = st
	m.newAgentDraft = nil
	m.Focus = FocusNewAgent
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	return m, nil
}

// closeNewAgent leaves the page for the board; keepDraft keeps the page's
// state for the next n.
func closeNewAgent(m Model, keepDraft bool) Model {
	if keepDraft {
		m.newAgentDraft = m.NewAgent
	} else {
		m.newAgentDraft = nil
	}
	m.NewAgent = nil
	m.Focus = FocusBoard
	if m.Audio != nil {
		m.Audio.Play(audio.EventClose)
	}
	return m
}

// newAgentTrustRequest applies the page's trust choice to req (P2's
// wizardTrustRequest rules).
func newAgentTrustRequest(m Model, cwd string, req protocol.NewSession) (Model, protocol.NewSession, bool) {
	return trustRequest(m, m.NewAgent.Trust, cwd, req)
}

// launchNewAgent sends the page's NewSession as the wizard does, remembers
// the agent for the next page, and closes the page.
func launchNewAgent(m Model) (Model, tea.Cmd) {
	st := m.NewAgent
	cwd := spawnCWD(m)
	req := st.request(cwd, existingSlugs(m))
	var ok bool
	if m, req, ok = newAgentTrustRequest(m, cwd, req); !ok {
		return m, nil
	}
	req.Cols, req.Rows = spawnSizeOf(m)
	m.PendingSelectSlug = req.Slug
	m.PendingSpawn = pendingSpawn{Skills: req.Skills}
	c := st.choice()
	m.lastSpawn = &c
	return closeNewAgent(m, false), wizardLaunchCmd(m.Client, req)
}

// updateNewAgentKey routes a key on the page. A paste only ever inserts;
// enter launches only as a real key in the task editor.
func updateNewAgentKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	if st == nil {
		return m, nil
	}
	if !m.featureOn("new_agent_page") {
		// Turned off while open: back to the board, the draft kept.
		return closeNewAgent(m, true), nil
	}
	m.Err = "" // an error stays until the next key
	if k.Paste {
		st.Section = naTask
		st.Task.Insert(string(k.Runes))
		return m, nil
	}
	if next, mode, ok := trustKeyToggle(m, k, st.Trust); ok {
		next.NewAgent.Trust = mode
		return next, nil
	}
	switch k.Type {
	case tea.KeyEsc:
		return closeNewAgent(m, true), nil
	case tea.KeyTab:
		st.Section = (st.Section + 1) % naSectionCount
		playNav(m)
		return m, nil
	case tea.KeyShiftTab:
		st.Section = (st.Section + naSectionCount - 1) % naSectionCount
		playNav(m)
		return m, nil
	case tea.KeyCtrlS:
		return launchNewAgent(m)
	case tea.KeyCtrlG:
		return m, nil // the Agent screen comes with R16-2
	}
	if st.Section == naTask {
		return newAgentEditKey(m, k)
	}
	if k.Type == tea.KeyRunes && len(k.Runes) == 1 && k.Runes[0] >= '1' && k.Runes[0] <= '4' {
		st.Section = naSection(k.Runes[0] - '1')
		playNav(m)
	}
	return m, nil
}

// newAgentEditKey is a key in the task editor.
func newAgentEditKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	c := &st.Task
	switch k.Type {
	case tea.KeyEnter:
		if k.Alt {
			c.Newline()
			return m, nil
		}
		return launchNewAgent(m)
	case tea.KeyCtrlJ:
		c.Newline()
	case tea.KeyBackspace:
		if k.Alt {
			c.DeleteWord()
		} else {
			c.Backspace()
		}
	case tea.KeyCtrlW:
		c.DeleteWord()
	case tea.KeyCtrlU:
		if c.Empty() {
			// An empty editor: ctrl+u drops the whole draft (D4).
			c.Clear()
			st.Trust = trustAsk
			st.SkillSel = nil
			m.newAgentDraft = nil
			return m, nil
		}
		c.ClearLine()
	case tea.KeyLeft:
		c.Move(0, -1)
	case tea.KeyRight:
		c.Move(0, 1)
	case tea.KeyUp:
		c.Move(-1, 0)
	case tea.KeyDown:
		c.Move(1, 0)
	case tea.KeyHome:
		c.Home()
	case tea.KeyEnd:
		c.End()
	case tea.KeySpace:
		c.Insert(" ")
	case tea.KeyRunes:
		if !k.Alt {
			c.Insert(string(k.Runes))
		}
	}
	return m, nil
}
