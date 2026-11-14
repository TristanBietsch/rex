package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The new-agent page's agent, model and skills steps (R21). The agent step
// lists the installed agents (the cursor's agent is the chosen one) and the
// ones that are not installed, dimmed under a header (enter there opens
// Settings → Onboarding, D7). The model step is the chosen agent's models
// with a / filter, then the effort row. The skills step is a checklist.

// naFilterMax caps the model filter, in runes.
const naFilterMax = 64

// filteredIdx is the indexes of the selected tool's models that match the
// filter: a case-insensitive substring of the id or the name.
func (st *NewAgentState) filteredIdx() []int {
	models := st.selection().selectedTool().Models
	q := strings.ToLower(strings.TrimSpace(st.Filter))
	out := make([]int, 0, len(models))
	for i, md := range models {
		if q == "" || strings.Contains(strings.ToLower(md.ID), q) || strings.Contains(strings.ToLower(md.Name), q) {
			out = append(out, i)
		}
	}
	return out
}

// filteredModels is the selected tool's models that match the filter.
func (st *NewAgentState) filteredModels() []registry.Model {
	models := st.selection().selectedTool().Models
	idx := st.filteredIdx()
	out := make([]registry.Model, len(idx))
	for i, j := range idx {
		out[i] = models[j]
	}
	return out
}

// modelIdx is ModelIdx clamped to the selected tool's models.
func (st *NewAgentState) modelIdx() int {
	return min(max(st.ModelIdx, 0), max(len(st.selection().selectedTool().Models)-1, 0))
}

// setModel selects model i of the selected tool, keeping the effort when the
// model offers it (else its default).
func (st *NewAgentState) setModel(i int) {
	c := st.choice()
	c.Model = st.selection().selectedTool().Models[i].ID
	st.choose(c)
	st.ModelIdx = i // choose finds the first of duplicate ids
}

// setTool selects tool i: its remembered model when it is the tool the page
// opened with, else its first. The filter is per tool, so it clears.
func (st *NewAgentState) setTool(i int) {
	c := quickChoice{Tool: st.Tools[i].ID}
	if st.Remembered.Tool == c.Tool {
		c.Model, c.Effort = st.Remembered.Model, st.Remembered.Effort
	}
	st.choose(c)
	st.Filter, st.Filtering = "", false
	st.SkillCur = 0
}

// refilter keeps the model cursor on the same id while it still matches,
// else moves it to the first match (none: it stays).
func (st *NewAgentState) refilter() {
	idx := st.filteredIdx()
	if len(idx) == 0 || slices.Contains(idx, st.modelIdx()) {
		return
	}
	st.setModel(idx[0])
}

// moveModel moves the model cursor within the filtered models.
func (st *NewAgentState) moveModel(delta int) bool {
	idx := st.filteredIdx()
	pos := slices.Index(idx, st.modelIdx())
	next := pos + delta
	if pos < 0 {
		next = 0
	}
	if next < 0 || next >= len(idx) || (pos >= 0 && next == pos) {
		return false
	}
	st.setModel(idx[next])
	return true
}

// moveEffort moves the effort choice along the model's options.
func (st *NewAgentState) moveEffort(delta int) bool {
	n := len(st.selection().effortOptions())
	next := st.EffortIdx + delta
	if n == 0 || next < 0 || next >= n {
		return false
	}
	st.EffortIdx = next
	return true
}

// toggleSkill turns the skill under the cursor on or off.
func (st *NewAgentState) toggleSkill() {
	w := st.selection()
	w.SkillCursor = st.SkillCur
	w.toggleSkill()
	st.SkillSel = w.SkillSel
}

// filterText is typed text as filter text: printable runes only (no
// control, escape or bidi characters), capped at naFilterMax runes.
func filterText(cur, add string) string {
	n := utf8.RuneCountInString(cur)
	var b strings.Builder
	b.WriteString(cur)
	for _, r := range add {
		if n >= naFilterMax {
			break
		}
		if !unicode.IsPrint(r) || unicode.Is(unicode.Bidi_Control, r) {
			continue
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// naOpenOnboarding leaves the page (the draft kept) for Settings →
// Onboarding, as S opens Settings from the board, the cursor on the first
// missing agent.
func naOpenOnboarding(m Model) (Model, tea.Cmd) {
	m = closeNewAgent(m, true)
	m, cmd := openSettings(m)
	if m.Settings != nil {
		if id, ok := settingsFirstHit(m.Settings, settings.SectionOnboarding); ok {
			m.Settings.CursorID = id
		}
	}
	return m, cmd
}

// naFilterKey is a key while typing the model filter: text edits it, arrows
// move the model cursor, enter keeps it and stops typing, esc clears it.
func naFilterKey(m Model, k tea.KeyMsg) Model {
	st := m.NewAgent
	if k.Paste {
		// A paste types into the filter, flat on one line.
		st.Filter = filterText(st.Filter, strings.Join(strings.Fields(string(k.Runes)), " "))
		st.refilter()
		return m
	}
	switch k.Type {
	case tea.KeyEsc:
		st.Filter, st.Filtering = "", false
	case tea.KeyEnter:
		st.Filtering = false
	case tea.KeyBackspace:
		if r := []rune(st.Filter); len(r) > 0 {
			st.Filter = string(r[:len(r)-1])
			st.refilter()
		} else {
			st.Filtering = false
		}
	case tea.KeyCtrlU:
		st.Filter = ""
	case tea.KeyUp:
		st.moveModel(-1)
	case tea.KeyDown:
		st.moveModel(1)
	case tea.KeySpace:
		st.Filter = filterText(st.Filter, " ")
		st.refilter()
	case tea.KeyRunes:
		if !k.Alt {
			st.Filter = filterText(st.Filter, string(k.Runes))
			st.refilter()
		}
	}
	return m
}

// refreshNewAgent applies a new tool list (refreshPickers) to the open
// page: the tool, model and effort stay by id while still offered, else fall
// back as on open with a notice. A step that no longer applies gives way to
// the next one that does. With nothing left to offer the page closes, the
// draft kept.
func refreshNewAgent(m Model, offer []registry.Tool, missing []onboardRow) Model {
	if len(offer) == 0 {
		m = closeNewAgent(m, true)
		m.Notice = "the new-agent page closed: no agent is available (see Settings → Onboarding)"
		return m
	}
	st := *m.NewAgent
	c := st.choice()
	var sumStep naStep = -1 // the step the summary cursor is on, if any
	if targets := naSumTargets(&st); st.SumRow < len(targets) {
		sumStep = targets[st.SumRow]
	}
	st.Tools, st.Missing = offer, missing
	st.choose(c)
	if st.choice().Tool != c.Tool {
		st.Filter, st.Filtering = "", false
		if c.Tool != "" {
			st.Notice = unavailableNotice(c.Tool, st.choice().Tool)
		}
	}
	if len(missing) == 0 {
		st.OnMissing = false
	}
	st.SkillCur = min(st.SkillCur, max(len(st.Skills)-1, 0))
	if naSkipped(&st, st.Step) {
		st.Step, _ = naStepFrom(&st, st.Step, 1)
		st.Complete, st.Filtering = nil, false
	}
	targets := naSumTargets(&st)
	switch {
	case sumStep < 0:
		st.SumRow = len(targets) // it was on the launch row
	case slices.Contains(targets, sumStep):
		st.SumRow = slices.Index(targets, sumStep)
	default:
		st.SumRow = len(targets) - 1 // its step went away: the last step row, never launch
	}
	m.NewAgent = &st
	return m
}

// ── Rendering ──────────────────────────────────────────────────────────────

// naMarkCol is the cursor column of a list row: ▸ on the cursor, else two
// spaces.
func naMarkCol(on bool) string {
	if on {
		return selectCursor()
	}
	return "  "
}

// naAgentBody is the agent step's list fw wide in at most rows rows: each
// offered agent (brand glyph and name, ▸ on the chosen one), then the
// missing ones dimmed under "not installed". While the cursor is on that
// header the chosen agent keeps a ✓. While the daemon's tool list is on its
// way it is the spinner and words instead.
func naAgentBody(m Model, st *NewAgentState, fw, rows int) []string {
	if naToolsLoading(m) {
		return []string{ansi.Truncate(styleArrow.Render(naSpinner(m))+styleDim.Render(" checking which agents are installed…"), max(fw, 1), "…")}
	}
	var list []string
	cur := st.ToolIdx
	for i, t := range st.Tools {
		mark := naMarkCol(i == st.ToolIdx && !st.OnMissing)
		if i == st.ToolIdx && st.OnMissing {
			mark = styleBootReady.Render("✓") + " "
		}
		glyph := ""
		if t.Icon != "" {
			glyph = brandStyle(t.Color).Render(protocol.FlattenDisplayText(t.Icon)) + " "
		}
		left := mark + glyph
		name := ansi.Truncate(protocol.FlattenDisplayText(t.Name), max(fw-ansi.StringWidth(left), 1), "…")
		style := stylePrimary
		if i == st.ToolIdx {
			style = selectedStyle(stylePrimary).Bold(true)
		}
		list = append(list, left+style.Render(name))
	}
	if len(st.Missing) > 0 {
		list = append(list, "", naMarkCol(st.OnMissing)+styleDim.Render("not installed"))
		if st.OnMissing {
			cur = len(list) - 1
		}
		for _, r := range st.Missing {
			left := "  " + styleMuted.Render("· ")
			name := ansi.Truncate(protocol.FlattenDisplayText(r.Tool.Name), max(fw-ansi.StringWidth(left), 1), "…")
			list = append(list, left+styleMuted.Render(name))
		}
	}
	out := st.agentsView.window("agents", list, cur, max(rows, 1), "  ")
	for i, l := range out {
		out[i] = ansi.Truncate(l, fw, "…")
	}
	return out
}

// naFilterHead is the model step's first row: the filter as typed
// ("> son█") with how many models match, or a dim "/ filter" with the count.
func naFilterHead(m Model, st *NewAgentState, fw int) string {
	n := len(st.selection().selectedTool().Models)
	count := fmt.Sprintf("%d models", n)
	if n == 1 {
		count = "1 model"
	}
	var head string
	if st.Filter != "" || st.Filtering {
		cur := ""
		if st.Filtering {
			cur = cursorBlock(m)
		}
		head = styleArrow.Render("> ") + styleSlug.Render(protocol.FlattenDisplayText(st.Filter)) + cur
		if st.Filter != "" {
			count = fmt.Sprintf("%d · %d shown", n, len(st.filteredIdx()))
		}
	} else {
		head = styleKey.Render("/") + styleDim.Render(" filter")
	}
	right := styleDim.Render(count)
	if gap := fw - ansi.StringWidth(head) - ansi.StringWidth(right); gap >= 2 {
		head += strings.Repeat(" ", gap) + right
	}
	return head
}

// naModelRows is the filtered models in rows rows: ▸ on the chosen one, the
// id, the name when it says more, and "detected" for a model the tool
// listed itself.
func naModelRows(st *NewAgentState, rows int) []string {
	models := st.selection().selectedTool().Models
	idx := st.filteredIdx()
	if len(idx) == 0 {
		msg := "no model for this agent"
		if st.Filter != "" {
			msg = "no model matches " + strconvQuote(st.Filter)
		}
		return []string{"  " + styleMuted.Render(msg)}
	}
	idW := 0
	for _, i := range idx {
		idW = max(idW, min(ansi.StringWidth(protocol.FlattenDisplayText(models[i].ID)), 28))
	}
	cur := st.modelIdx()
	pos := 0
	out := make([]string, len(idx))
	for n, i := range idx {
		md := models[i]
		id := ansi.Truncate(protocol.FlattenDisplayText(md.ID), 28, "…")
		style := styleSlug
		if i == cur {
			pos = n
			style = selectedStyle(styleSlug).Bold(true)
		}
		row := naMarkCol(i == cur) + style.Render(id) + strings.Repeat(" ", idW-ansi.StringWidth(id))
		if name := protocol.FlattenDisplayText(md.Name); name != "" && !strings.EqualFold(name, md.ID) {
			row += "  " + styleDim.Render(name)
		}
		if md.Detected {
			row += "  " + styleDim.Render("detected")
		}
		out[n] = row
	}
	return st.modelsView.window(st.selection().selectedTool().ID+"/"+st.Filter, out, pos, rows, "  ")
}

// strconvQuote quotes the filter for a message, flattened for display.
func strconvQuote(s string) string { return "“" + protocol.FlattenDisplayText(s) + "”" }

// naEffortLine is the effort row: "effort" and the model's options, the
// chosen one bracketed and bold (not color only). Empty for a model with
// no effort.
func naEffortLine(st *NewAgentState) string {
	opts := st.selection().effortOptions()
	if len(opts) == 0 {
		return ""
	}
	parts := make([]string, len(opts))
	for i, o := range opts {
		o = protocol.FlattenDisplayText(o)
		if i == st.EffortIdx {
			parts[i] = styleBold.Render("[" + o + "]")
		} else {
			parts[i] = styleDim.Render(o)
		}
	}
	return styleDim.Render("effort  ") + strings.Join(parts, "  ")
}

// naModelBody is the model step's body fw wide in at most rows rows: the
// filter row, the models (as many as there are, within what the effort row
// leaves), a blank and the effort row.
func naModelBody(m Model, st *NewAgentState, fw, rows int) []string {
	lines := []string{naFilterHead(m, st, fw)}
	effort := naEffortLine(st)
	reserved := 1
	if effort != "" {
		reserved += 2
	}
	area := max(1, min(max(len(st.filteredIdx()), 1), rows-reserved))
	lines = append(lines, naModelRows(st, area)...)
	if effort != "" {
		lines = append(lines, "", effort)
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, fw, "…")
	}
	return lines
}

// naSkillLines is the skills checklist in at most rows rows: ▸ on the
// cursor, [x] on the chosen ones, then the description.
func naSkillLines(st *NewAgentState, rows int) []string {
	nameW := 0
	for _, s := range st.Skills {
		nameW = max(nameW, min(ansi.StringWidth(s.Name), 24))
	}
	out := make([]string, len(st.Skills))
	for i, s := range st.Skills {
		box := "[ ]"
		if st.SkillSel[s.Name] {
			box = "[x]"
		}
		name := truncate(protocol.FlattenDisplayText(s.Name), 24)
		row := naMarkCol(i == st.SkillCur) + box + " " + styleSlug.Render(name) +
			strings.Repeat(" ", max(nameW-ansi.StringWidth(name), 0))
		if s.Description != "" {
			row += "  " + styleDim.Render(protocol.FlattenDisplayText(s.Description))
		}
		out[i] = row
	}
	return st.skillsView.window("skills", out, st.SkillCur, rows, "  ")
}
