package tui

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
)

// Step navigation on the new-agent page (R21). One step shows at a time:
//
//   - enter confirms the step and goes to the next one that applies; on the
//     task it never launches (the summary is the launch point);
//   - esc closes the innermost thing (the @ list, a filter, the picker, the
//     preview) first, then returns to the summary after a jump from it,
//     else closes the page from the step it opened on, else goes back;
//   - tab / shift+tab go to the next / previous step, 1-7 jump to one;
//   - j/k, gg/G, ctrl+d/ctrl+u move the step's list with the shared
//     navigator (nav.go).
//
// ctrl+s launches from anywhere; ctrl+o opens the import picker on the task
// and context steps; ctrl+g goes to the agent step.

// naSkipped reports whether step s does not apply: the model step for an
// agent with no models, the skills step when skills cannot be loaded (the
// feature is off, the library is empty or the agent has no skill_args).
func naSkipped(st *NewAgentState, s naStep) bool {
	switch s {
	case naStepModel:
		return len(st.selection().selectedTool().Models) == 0
	case naStepSkills:
		return !st.selection().skillsApply()
	case naStepAgent, naStepTask, naStepContext, naStepTrust, naStepSummary, naStepCount:
	}
	return false
}

// naStepFrom is the step dir (±1) away from from that applies, ok false
// when there is none.
func naStepFrom(st *NewAgentState, from naStep, dir int) (naStep, bool) {
	for s := from + naStep(dir); s >= 0 && s < naStepCount; s += naStep(dir) {
		if !naSkipped(st, s) {
			return s, true
		}
	}
	return from, false
}

// naEnterStep points the step's own cursor at its starting row: the trust
// step on the current choice, the summary on its launch row (unless it is
// being returned to), the lists inside their rows.
func naEnterStep(m Model, st *NewAgentState) {
	switch st.Step {
	case naStepTrust:
		naSyncTrustCur(m, st)
	case naStepSummary:
		if !st.ReturnToSummary {
			st.SumRow = len(naSumTargets(st))
		}
		st.ReturnToSummary = false
	case naStepContext:
		st.ItemIdx = min(max(st.ItemIdx, 0), len(st.Items))
	case naStepSkills:
		st.SkillCur = min(max(st.SkillCur, 0), max(len(st.Skills)-1, 0))
	case naStepAgent, naStepModel, naStepTask, naStepCount:
	}
}

// naGo moves to step s, dropping what belonged to the step left (the @
// list, the filter being typed).
func naGo(m Model, s naStep) {
	st := m.NewAgent
	if s != naStepAgent {
		st.OnMissing = false
		if st.Step == naStepAgent {
			st.Notice = "" // leaving the agent step, by any route, ends the open-time notice
		}
	}
	st.Step, st.Complete, st.Filtering, st.gPending = s, nil, false, false
	naEnterStep(m, st)
	playNav(m)
}

// naToSummary goes to the summary, keeping its cursor on the row jumped
// from (keepRow) or putting it on the launch row.
func naToSummary(m Model, keepRow bool) {
	st := m.NewAgent
	st.ReturnToSummary = keepRow
	naGo(m, naStepSummary)
}

// naAdvance is enter on a step: it moves on to the next step that applies.
// After a jump from the summary it goes back there instead: after an agent
// change through the model and skills steps (the ones that apply), after
// any other change at once.
func naAdvance(m Model) (Model, tea.Cmd) {
	st := m.NewAgent
	if st.Step == naStepAgent {
		if naToolsLoading(m) {
			return m, nil // the list is not in yet
		}
		st.Notice = "" // confirming the agent clears the open-time notice
	}
	next, ok := naStepFrom(st, st.Step, 1)
	if st.ReturnToSummary && (!ok || next > naStepSkills) {
		naToSummary(m, true)
		return m, nil
	}
	if ok {
		naGo(m, next)
	}
	return m, nil
}

// naTab is tab (dir 1) and shift+tab (-1): the next or previous step that
// applies, no wrap. It ends the return to the summary.
func naTab(m Model, dir int) Model {
	st := m.NewAgent
	st.ReturnToSummary = false
	if next, ok := naStepFrom(st, st.Step, dir); ok {
		naGo(m, next)
	}
	return m
}

// naJump is a number key or ctrl+g: go to step s unless it is skipped.
func naJump(m Model, s naStep) Model {
	st := m.NewAgent
	if naSkipped(st, s) {
		return m
	}
	st.ReturnToSummary = false
	naGo(m, s)
	return m
}

// naEsc is esc outside the editor's own layers: a kept model filter goes
// first; then a jump from the summary returns to it; on the step the page
// opened on (or an earlier one) the page closes, the draft kept; else it is
// one step back.
func naEsc(m Model) (Model, tea.Cmd) {
	st := m.NewAgent
	switch {
	case st.Step == naStepModel && st.Filter != "":
		st.Filter = ""
		playNav(m)
	case st.ReturnToSummary:
		naToSummary(m, true)
	case st.Step <= st.Opened:
		return closeNewAgent(m, true), nil
	default:
		if prev, ok := naStepFrom(st, st.Step, -1); ok {
			naGo(m, prev)
		}
	}
	return m, nil
}

// naTaskKey is a key on the task step, which always types: enter goes to
// the next step, ctrl+j and alt+enter add a line.
func naTaskKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	if st.Complete != nil {
		if m, cmd, ok := naCompleteKey(m, k); ok {
			return m, cmd
		}
	}
	switch k.Type {
	case tea.KeyEsc:
		return naEsc(m)
	case tea.KeyTab:
		return naTab(m, 1), nil
	case tea.KeyShiftTab:
		return naTab(m, -1), nil
	case tea.KeyCtrlO:
		return openImport(m), nil
	case tea.KeyCtrlG:
		return naJump(m, naStepAgent), nil
	case tea.KeyEnter:
		if !k.Alt {
			return naAdvance(m)
		}
	case tea.KeyRunes:
		if isRune(k, '?') && st.Task.Empty() {
			return openNewAgentHelp(m), nil
		}
	}
	m, cmd := newAgentEditKey(m, k)
	if m.NewAgent != nil {
		naSyncComplete(m.NewAgent, k)
	}
	return m, cmd
}

// naStepKey is a key on any step but the task: the keys every step shares,
// then the step's own.
func naStepKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	key := k.String()
	switch key {
	case "esc":
		return naEsc(m)
	case "q":
		return closeNewAgent(m, true), nil
	case "?":
		return openNewAgentHelp(m), nil
	case "tab":
		return naTab(m, 1), nil
	case "shift+tab":
		return naTab(m, -1), nil
	case "ctrl+g":
		return naJump(m, naStepAgent), nil
	case "1", "2", "3", "4", "5", "6", "7":
		return naJump(m, naStep(key[0]-'1')), nil
	case "ctrl+o":
		if st.Step == naStepContext {
			return openImport(m), nil
		}
		return m, nil
	}
	switch st.Step {
	case naStepAgent:
		return naAgentKey(m, k)
	case naStepModel:
		return naModelKey(m, k)
	case naStepContext:
		return naContextKey(m, k)
	case naStepSummary:
		return naSummaryKey(m, k)
	case naStepSkills:
		if key == " " {
			st.toggleSkill()
			playNav(m)
			return m, nil
		}
	case naStepTrust:
		if key == " " {
			naPickTrust(m)
			return m, nil
		}
	case naStepTask, naStepCount:
	}
	if key == "enter" {
		return naAdvance(m)
	}
	if mv := vimMotion(key, &st.gPending); mv != navNone && mv != navPending {
		naMoveList(m, mv)
	}
	return m, nil
}

// naAgentKey is a key on the agent step: enter on the not installed header
// opens Onboarding (D7), else it confirms the agent.
func naAgentKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	key := k.String()
	if key == "enter" {
		if st.OnMissing {
			return naOpenOnboarding(m)
		}
		return naAdvance(m)
	}
	if mv := vimMotion(key, &st.gPending); mv != navNone && mv != navPending {
		naMoveList(m, mv)
	}
	return m, nil
}

// naModelKey is a key on the model step: / filters, h/l (and ←/→, +/-) move
// the effort, j/k the models; enter moves on.
func naModelKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	key := k.String()
	moved := false
	switch key {
	case "enter":
		return naAdvance(m)
	case "/":
		st.Filtering, moved = true, true
	case "+", "=", "l", "right":
		moved = st.moveEffort(1)
	case "-", "_", "h", "left":
		moved = st.moveEffort(-1)
	default:
		if mv := vimMotion(key, &st.gPending); mv != navNone && mv != navPending {
			naMoveList(m, mv)
		}
	}
	if moved {
		playNav(m)
	}
	return m, nil
}

// naSummaryKey is a key on the summary: enter on the launch row launches,
// on a step row (or e) it jumps to that step, and enter there comes back.
func naSummaryKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	key := k.String()
	targets := naSumTargets(st)
	switch key {
	case "enter", "e":
		if st.SumRow >= len(targets) {
			if key == "enter" {
				return launchNewAgent(m)
			}
			return m, nil
		}
		st.ReturnToSummary = true
		naGo(m, targets[st.SumRow])
		return m, nil
	}
	if mv := vimMotion(key, &st.gPending); mv != navNone && mv != navPending {
		naMoveList(m, mv)
	}
	return m, nil
}

// naSumTargets are the steps the summary's rows jump to, in order: the
// steps that apply, before the summary itself. The launch row follows them.
func naSumTargets(st *NewAgentState) []naStep {
	var out []naStep
	for s := range naStepSummary {
		if !naSkipped(st, s) {
			out = append(out, s)
		}
	}
	return out
}

// naMoveList moves the current step's list cursor by mv (the shared
// navigator): the agents, the models, the skills, the context items and the
// next row, the trust choices or the summary rows.
func naMoveList(m Model, mv navMove) {
	st := m.NewAgent
	moved := false
	switch st.Step {
	case naStepAgent:
		moved = naAgentMove(st, mv)
	case naStepModel:
		idx := st.filteredIdx()
		pos := max(slices.Index(idx, st.modelIdx()), 0)
		if to := navTarget(mv, pos, len(idx), st.modelsView.H); len(idx) > 0 && (to != pos || idx[to] != st.modelIdx()) {
			st.setModel(idx[to])
			moved = true
		}
	case naStepSkills:
		if to := navTarget(mv, st.SkillCur, len(st.Skills), st.skillsView.H); to != st.SkillCur {
			st.SkillCur, moved = to, true
		}
	case naStepContext:
		if n := len(st.Items); n > 0 {
			if to := navTarget(mv, st.ItemIdx, n+1, st.itemsView.H); to != st.ItemIdx {
				st.ItemIdx, moved = to, true
			}
		}
	case naStepTrust:
		_, rows := naTrustChoices(m, st)
		if to := navTarget(mv, st.TrustCur, len(rows), len(rows)); to != st.TrustCur && len(rows) > 0 {
			st.TrustCur, moved = to, true
		}
	case naStepSummary:
		if to := navTarget(mv, st.SumRow, len(naSumTargets(st))+1, st.sumView.H); to != st.SumRow {
			st.SumRow, moved = to, true
		}
	case naStepTask, naStepCount:
	}
	if moved {
		playNav(m)
	}
}

// naAgentMove moves the agent cursor by mv: through the agents (the
// cursor's agent is the chosen one) and on to the not installed header.
func naAgentMove(st *NewAgentState, mv navMove) bool {
	n := len(st.Tools)
	if len(st.Missing) > 0 {
		n++ // the not installed header
	}
	cur := st.ToolIdx
	if st.OnMissing {
		cur = len(st.Tools)
	}
	to := navTarget(mv, cur, n, st.agentsView.H)
	if to == cur {
		return false
	}
	st.OnMissing = to >= len(st.Tools)
	if !st.OnMissing && to != st.ToolIdx {
		st.setTool(to) // back from the header onto the chosen agent keeps its model
	}
	return true
}

// naContextKey is a key on the context step: enter previews an item, or on
// the next row (or with no items) goes to the next step; x, d and delete
// remove, r renames, e edits a paste.
func naContextKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	n := len(st.Items)
	st.ItemIdx = min(max(st.ItemIdx, 0), n)
	onItem := st.ItemIdx < n
	switch {
	case k.Type == tea.KeyEnter:
		if !onItem {
			return naAdvance(m)
		}
		st.Screen, st.PrevScroll = naPreview, 0
		naLoadPreview(st)
		playNav(m)
	case onItem && (k.Type == tea.KeyDelete || isRune(k, 'x') || isRune(k, 'd')):
		removeItem(st, st.ItemIdx)
	case onItem && isRune(k, 'r'):
		naStartRename(st)
	case onItem && (k.Type == tea.KeyCtrlE || isRune(k, 'e')):
		if it := st.Items[st.ItemIdx]; it.Kind == spawnctx.Paste {
			return m, editInEditor(st.ItemIdx, it.Text)
		}
		st.Err = "! only a paste can be edited here; a file is edited in its own editor"
	default:
		if mv := vimMotion(k.String(), &st.gPending); mv != navNone && mv != navPending {
			naMoveList(m, mv)
		}
	}
	return m, nil
}

// updateNewAgentMouse is the wheel on the new-agent page: one row, as j/k,
// on the step's list (agent, models, skills, context, trust, summary); the
// task ignores it, the preview scrolls and the picker keeps its own keys.
func updateNewAgentMouse(m Model, msg tea.MouseMsg) Model {
	st := m.NewAgent
	mv := wheelMotion(msg)
	if st == nil || mv == navNone || st.Launching || st.Renaming || st.Filtering || len(st.Pending) > 0 {
		return m
	}
	m.Err, st.Err = "", ""
	st.Rev++
	st.gPending = false
	switch st.Screen {
	case naSteps:
		naMoveList(m, mv)
	case naPreview:
		naPreviewScroll(m, mv)
	case naImportScreen:
	}
	return m
}
