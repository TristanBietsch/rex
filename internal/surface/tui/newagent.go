package tui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/features/skills"
	"github.com/tristanbietsch/rex/internal/features/spawnctx"
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

// naScreen is the page's screen: Compose, the Agent screen (agents,
// models, effort and skills; newagent_agent.go), one context item's
// preview (newagent_preview.go), or the import picker
// (newagent_import.go).
type naScreen int

const (
	naCompose naScreen = iota
	naAgentScreen
	naPreview
	naImportScreen
)

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
	// Notice is the open-time notice (a remembered agent that is gone).
	Notice string
	// Trust is the folder-trust choice (P2, wizard_trust.go).
	Trust wizTrustMode
	// The Agent screen: the focused pane, whether the agent cursor is on the
	// not installed header, the model filter and whether keys type into it,
	// and the skills cursor. Remembered is the agent the page opened with:
	// going back to its tool starts on its model.
	Pane       naAgentPane
	OnMissing  bool
	Filter     string
	Filtering  bool
	SkillCur   int
	Remembered quickChoice
	// Items is the context (newagent_context.go), in prompt order; ItemIdx
	// is the list cursor and PasteSeq the last paste number given, so a
	// removed paste's number is never reused while its placeholder may
	// still be in the task.
	Items    []spawnctx.Item
	ItemIdx  int
	PasteSeq int
	// Err is the page's error, "✗ " and words (a second line is a hint). It
	// stays until the next key.
	Err string
	// Launching is true from the launch until the daemon answers
	// (newagent_launch.go); launchGen is that launch. ItemsRoot is the
	// folder the file and folder items were added in: a launch from any
	// other folder is refused.
	Launching bool
	launchGen int64
	ItemsRoot string
	// Renaming is true while a new name for item ItemIdx is typed into
	// RenameBuf. PrevScroll is the preview's first line.
	Renaming   bool
	RenameBuf  []rune
	PrevScroll int
	// PrevLines and PrevMore are a file or folder preview, read when the
	// preview opens; PrevErr is why it could not be read.
	PrevLines []string
	PrevMore  int
	PrevErr   string
	// The workspace index (newagent_import.go): Root is the folder walked
	// (spawnCWD when the page opened), Index the walk's result once it is
	// in, Indexing true until then, IndexErr a failed walk. indexCount is
	// the walk's live count, indexGen tells this page's walk from an older
	// one, indexCancel stops it when the page closes.
	Root        string
	Index       spawnctx.Index
	Indexing    bool
	IndexErr    error
	indexCount  *atomic.Int64
	indexGen    int64
	indexCancel context.CancelFunc
	// Import is the open picker's state, Complete the open @ completion,
	// Pending the denylisted paths waiting for y (D9).
	Import   *naImport
	Complete *naComplete
	Pending  []naPending
	// rowsKey and rowsCache keep the picker's rows until the query, the
	// folder or the index changes; sizes the rows' file sizes.
	rowsKey   string
	rowsCache []string
	sizes     map[string]string
	// Rev counts the page's changes: every key and editor result bumps it.
	// The launch summary's assembled size is cached at sumRev.
	Rev      int
	sumRev   int
	sumOK    bool
	sumBytes int
	sumErr   error
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

// request builds the NewSession the page stands for: the prompt is the
// task with its context (spawnctx.Assemble), the task's first line the
// title and the slug's source, made unique against existing. Every file
// and folder is checked again against cwd, and the prompt against the
// chosen agent's limit (16 KB for a prompt_paste agent, else 96 KB). A
// context the prompt cannot carry is an error in the page's words, and
// nothing launches.
func (st *NewAgentState) request(cwd string, existing []string) (protocol.NewSession, error) {
	w := st.selection()
	task := strings.TrimSpace(st.Task.String())
	if err := st.naCheckLaunch(cwd); err != nil {
		return protocol.NewSession{}, err
	}
	caps := spawnctx.DefaultCaps
	caps.Prompt = 0 // checked below, in KB
	prompt, err := spawnctx.Assemble(task, st.Items, caps)
	if err != nil {
		return protocol.NewSession{}, naAssembleErr(err)
	}
	tool, model := w.selectedTool(), w.selectedModel()
	if limit := naPromptCap(st); len(prompt) > limit {
		name := ""
		if limit != spawnctx.DefaultCaps.Prompt {
			name = protocol.FlattenDisplayText(tool.Name)
		}
		return protocol.NewSession{}, naPromptErr(len(prompt), limit, name)
	}
	// A plain paste (no bracketed-paste markers) reaches a line REPL such
	// as ollama's as typed input: every newline is Enter, so a multi-line
	// prompt would go as several messages.
	if pp := tool.PromptPaste; pp != nil && pp.Plain && strings.Contains(prompt, "\n") {
		return protocol.NewSession{}, naErr{msg: protocol.FlattenDisplayText(tool.Name) +
			" takes the prompt as typed lines, so each line break would send a message. " +
			"Write a one-line task without context, or choose another agent (ctrl+g)."}
	}
	title := taskTitle(task)
	return protocol.NewSession{
		ToolID:         tool.ID,
		ModelID:        model.ID,
		Effort:         w.currentEffort(),
		Slug:           deriveAgentSlug(tool.ID, model.ID, title, existing),
		Title:          title,
		CWD:            cwd,
		InitialPrompt:  prompt,
		Skills:         w.chosenSkills(),
		ContextSummary: spawnctx.Summary(st.Items),
	}, nil
}

// openNew is n and :new: the new-agent page while new_agent_page is on,
// else the wizard.
func openNew(m Model) (Model, tea.Cmd) {
	if m.featureOn("new_agent_page") {
		return openNewAgent(m)
	}
	m, cmd := openWizard(m)
	return naSeedWizard(m), cmd
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
	st.ItemsRoot = spawnCWD(m)
	if d := m.newAgentDraft; d != nil {
		st.Section, st.Task, st.Trust, st.SkillSel = d.Section, d.Task, d.Trust, d.SkillSel
		st.Items, st.ItemIdx, st.PasteSeq = d.Items, d.ItemIdx, d.PasteSeq
		st.Launching, st.launchGen = d.Launching, d.launchGen
		if naHasPaths(d.Items) {
			st.ItemsRoot = d.ItemsRoot
		}
		c := d.choice()
		want = &c
	}
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
		st.Notice = notice
	}
	st.Remembered = st.choice()
	m.NewAgent = st
	m.newAgentDraft = nil
	m.Focus = FocusNewAgent
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	return m, startIndex(st, spawnCWD(m))
}

// closeNewAgent leaves the page for the board; keepDraft keeps the page's
// state for the next n.
func closeNewAgent(m Model, keepDraft bool) Model {
	if m.NewAgent != nil {
		stopIndex(m.NewAgent)
	}
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

// openNewAgentHelp is ? on the page: help on the new-agent group alone;
// esc or ? there comes back to the page as it was.
func openNewAgentHelp(m Model) Model {
	m = openHelp(m)
	m.Help.Only = groupNewAgent
	return m
}

// naHasPaths reports whether items hold a file or a folder.
func naHasPaths(items []spawnctx.Item) bool {
	return slices.ContainsFunc(items, func(it spawnctx.Item) bool {
		return it.Kind == spawnctx.File || it.Kind == spawnctx.Dir
	})
}

// naStaleRoot reports a draft whose files and folders were added in
// another folder than the one this page indexes (a draft reopened after
// the workspace changed): they cannot launch here, and new paths cannot
// join them.
func naStaleRoot(st *NewAgentState) bool {
	return naHasPaths(st.Items) && st.ItemsRoot != "" && st.Root != "" &&
		filepath.Clean(st.ItemsRoot) != filepath.Clean(st.Root)
}

// naItemsRootOf is the folder the file and folder items are relative to:
// ItemsRoot, else the indexed folder.
func naItemsRootOf(st *NewAgentState) string {
	if st.ItemsRoot != "" {
		return st.ItemsRoot
	}
	return st.Root
}

// naResetItemsRoot points ItemsRoot back at the indexed folder once no file
// or folder item is left, so items added next belong to it.
func naResetItemsRoot(st *NewAgentState) {
	if !naHasPaths(st.Items) {
		st.ItemsRoot = st.Root
	}
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
	if st.Launching {
		return naLaunchingKey(m, k)
	}
	m.Err, st.Err = "", "" // an error stays until the next key
	st.Rev++
	switch {
	case len(st.Pending) > 0:
		return naPendingKey(m, k)
	case st.Screen == naImportScreen:
		return naImportKey(m, k)
	case st.Screen == naAgentScreen:
		return updateNewAgentAgentKey(m, k)
	case st.Renaming:
		return naRenameKey(m, k)
	case st.Screen == naPreview:
		return naPreviewKey(m, k)
	}
	if k.Paste {
		st.Section, st.Complete = naTask, nil
		if err := addPaste(st, string(k.Runes)); err != nil {
			st.Err = naFail(err)
		}
		return m, nil
	}
	if next, mode, ok := trustKeyToggle(m, k, st.Trust); ok {
		next.NewAgent.Trust = mode
		if next.Err != "" {
			next.NewAgent.Err = "✗ " + next.Err // the page shows it, not the board
			next.Err = ""
		}
		return next, nil
	}
	if st.Complete != nil && st.Section == naTask {
		if m, cmd, ok := naCompleteKey(m, k); ok {
			return m, cmd
		}
	}
	if st.Section != naTask && isRune(k, '?') {
		return openNewAgentHelp(m), nil
	}
	switch k.Type {
	case tea.KeyEsc:
		return closeNewAgent(m, true), nil
	case tea.KeyCtrlO:
		return openImport(m), nil
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
		return openAgentScreen(m), nil
	}
	if st.Section == naTask {
		m, cmd := newAgentEditKey(m, k)
		if m.NewAgent != nil {
			naSyncComplete(m.NewAgent, k)
		}
		return m, cmd
	}
	if st.Section == naContext {
		if m, cmd, ok := naContextKey(m, k); ok {
			return m, cmd
		}
	}
	if st.Section == naAgent && k.Type == tea.KeyEnter {
		return openAgentScreen(m), nil
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
	case tea.KeyCtrlE:
		return m, editInEditor(-1, c.String())
	case tea.KeyCtrlU:
		if c.Empty() {
			// An empty editor: ctrl+u drops the whole draft (D4).
			c.Clear()
			st.Trust = trustAsk
			st.SkillSel = nil
			st.Items, st.ItemIdx, st.PasteSeq = nil, 0, 0
			naResetItemsRoot(st)
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
