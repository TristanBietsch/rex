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

// The new-agent page's Agent screen (R16 mockup C): the agents on the left,
// the selected agent's models (with a / filter), effort and skills on the
// right. ctrl+g or enter on the Compose agent row opens it; enter is done and
// esc goes back (clearing an active filter first). Agents that are not
// installed show dimmed under "not installed" and cannot be chosen; enter on
// that header opens Settings → Onboarding (D7).

// naAgentPane is the focused pane of the Agent screen.
type naAgentPane int

const (
	naPaneAgents naAgentPane = iota
	naPaneModels
	naPaneSkills
	naPaneCount
)

// naPaneNames are the narrow strip's words.
var naPaneNames = [naPaneCount]string{"Agents", "Models", "Skills"}

// naFilterMax caps the model filter, in runes.
const naFilterMax = 64

// naSkillRows is the most skill rows the wide screen shows at once.
const naSkillRows = 6

// openAgentScreen switches the page to the Agent screen, agents focused.
func openAgentScreen(m Model) Model {
	st := m.NewAgent
	st.Screen = naAgentScreen
	st.Pane = naPaneAgents
	st.OnMissing = false
	st.Filter, st.Filtering = "", false
	playNav(m)
	return m
}

// closeAgentScreen goes back to Compose.
func closeAgentScreen(m Model) Model {
	st := m.NewAgent
	st.Screen = naCompose
	st.OnMissing = false
	st.Filter, st.Filtering = "", false
	playNav(m)
	return m
}

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
	if st.Pane == naPaneSkills && !st.selection().skillsApply() {
		st.Pane = naPaneAgents
	}
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

// nextPane is the pane after (delta 1) or before (-1) the focused one; the
// skills pane only while the selected agent can load skills.
func (st *NewAgentState) nextPane(delta int) naAgentPane {
	p := st.Pane
	for range naPaneCount {
		p = (p + naAgentPane(delta) + naPaneCount) % naPaneCount
		if p != naPaneSkills || st.selection().skillsApply() {
			return p
		}
	}
	return st.Pane
}

// moveAgent moves the agent cursor: through the offered agents, then onto
// the not installed header, never onto a missing agent.
func (st *NewAgentState) moveAgent(delta int) bool {
	switch {
	case st.OnMissing && delta < 0:
		st.OnMissing = false
		return true
	case st.OnMissing:
		return false
	}
	next := st.ToolIdx + delta
	switch {
	case next >= 0 && next < len(st.Tools):
		st.setTool(next)
		return true
	case next >= len(st.Tools) && len(st.Missing) > 0:
		st.OnMissing = true
		return true
	}
	return false
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

// moveSkill moves the skills cursor.
func (st *NewAgentState) moveSkill(delta int) bool {
	next := st.SkillCur + delta
	if next < 0 || next >= len(st.Skills) {
		return false
	}
	st.SkillCur = next
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

// updateNewAgentAgentKey routes a key on the Agent screen.
func updateNewAgentAgentKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	if st.Filtering {
		return naFilterKey(m, k), nil
	}
	if k.Paste {
		return m, nil // nothing here takes text outside the filter
	}
	if next, mode, ok := trustKeyToggle(m, k, st.Trust); ok {
		next.NewAgent.Trust = mode
		return next, nil
	}
	moved := false
	switch k.String() {
	case "esc":
		if st.Filter != "" {
			st.Filter = ""
			playNav(m)
			return m, nil
		}
		return closeAgentScreen(m), nil
	case "ctrl+g":
		return closeAgentScreen(m), nil
	case "ctrl+s":
		return launchNewAgent(m)
	case "enter":
		if st.OnMissing {
			return naOpenOnboarding(m)
		}
		return closeAgentScreen(m), nil
	case "tab":
		st.Pane, moved = st.nextPane(1), true
	case "shift+tab":
		st.Pane, moved = st.nextPane(-1), true
	case "/":
		st.Pane, st.Filtering, st.OnMissing = naPaneModels, true, false
		moved = true
	case "h", "left":
		moved = st.moveEffort(-1)
	case "l", "right":
		moved = st.moveEffort(1)
	case " ":
		if st.Pane == naPaneSkills && st.selection().skillsApply() {
			st.toggleSkill()
			moved = true
		}
	case "j", "down", "k", "up":
		delta := 1
		if s := k.String(); s == "k" || s == "up" {
			delta = -1
		}
		switch st.Pane {
		case naPaneAgents:
			moved = st.moveAgent(delta)
		case naPaneModels:
			moved = st.moveModel(delta)
		case naPaneSkills:
			moved = st.moveSkill(delta)
		default:
		}
	}
	if moved {
		playNav(m)
	}
	return m, nil
}

// naFilterKey is a key while typing the model filter: text edits it, arrows
// move the model cursor, enter keeps it, esc clears it.
func naFilterKey(m Model, k tea.KeyMsg) Model {
	st := m.NewAgent
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
// back as on open. With nothing left to offer the page closes, the draft
// kept.
func refreshNewAgent(m Model, offer []registry.Tool, missing []onboardRow) Model {
	if len(offer) == 0 {
		m = closeNewAgent(m, true)
		m.Notice = "the new-agent page closed: no agent is available (see Settings → Onboarding)"
		return m
	}
	st := *m.NewAgent
	c := st.choice()
	st.Tools, st.Missing = offer, missing
	st.choose(c)
	if st.choice().Tool != c.Tool {
		st.Filter, st.Filtering = "", false
	}
	if len(missing) == 0 {
		st.OnMissing = false
	}
	if st.Pane == naPaneSkills && !st.selection().skillsApply() {
		st.Pane = naPaneAgents
	}
	st.SkillCur = min(st.SkillCur, max(len(st.Skills)-1, 0))
	m.NewAgent = &st
	return m
}

// ── Rendering ──────────────────────────────────────────────────────────────

// naAgentBody is the Agent screen's body for a w x bh area: the agents
// beside the models, effort and skills, or under pageNarrowW one pane at a
// time under a strip.
func naAgentBody(m Model, st *NewAgentState, w, bh int) []string {
	if pageNarrow(w) {
		return naAgentNarrow(m, st, w, bh)
	}
	fw := max(w-naRailW-3-pageRightMargin, 1)
	return joinPanes(naAgentsPane(st, naRailW, bh, 1), naAgentRight(m, st, fw, bh), naRailW, bh)
}

// naAgentHints is the Agent screen's footer.
func naAgentHints(st *NewAgentState, narrow bool) [][2]string {
	switch {
	case st.Filtering:
		return [][2]string{{"type", "filter"}, {"↑/↓", "model"}, {"enter", "keep"}, {"esc", "clear"}}
	case st.OnMissing:
		return [][2]string{{"enter", "onboarding"}, {"k", "agents"}, {"esc", "back"}}
	case narrow:
		return [][2]string{{"j/k", "move"}, {"tab", "pane"}, {"enter", "done"}}
	}
	noun := map[naAgentPane]string{naPaneAgents: "agent", naPaneModels: "model", naPaneSkills: "skill"}[st.Pane]
	next := strings.ToLower(naPaneNames[st.nextPane(1)])
	hints := [][2]string{{"j/k", noun}, {"tab", next}, {"/", "filter"}, {"h/l", "effort"}}
	if st.selection().skillsApply() {
		hints = append(hints, [2]string{"space", "skill"})
	}
	return append(hints, [2]string{"enter", "done"})
}

// naMarkCol is the cursor column of a list row: ▸ on the cursor, else two
// spaces.
func naMarkCol(on bool) string {
	if on {
		return styleArrow.Render(selectGlyph()) + " "
	}
	return "  "
}

// naAgentsPane is the agents list w wide and at most h rows: AGENTS, each
// offered agent (brand glyph and name, ▸ on the chosen one), then the
// missing ones dimmed under "not installed". While the cursor is on that
// header the chosen agent keeps a ✓. indent is the left margin.
func naAgentsPane(st *NewAgentState, w, h, indent int) []string {
	pad := strings.Repeat(" ", indent)
	title := pad + naTitle("AGENTS", st.Pane == naPaneAgents)
	var rows []string
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
		left := pad + mark + glyph
		name := ansi.Truncate(protocol.FlattenDisplayText(t.Name), max(w-ansi.StringWidth(left), 1), "…")
		style := stylePrimary
		if i == st.ToolIdx {
			style = selectedStyle(stylePrimary).Bold(true)
		}
		rows = append(rows, left+style.Render(name))
	}
	if len(st.Missing) > 0 {
		rows = append(rows, "", pad+naMarkCol(st.OnMissing)+styleDim.Render("not installed"))
		if st.OnMissing {
			cur = len(rows) - 1
		}
		for _, r := range st.Missing {
			left := pad + "  " + styleMuted.Render("· ")
			name := ansi.Truncate(protocol.FlattenDisplayText(r.Tool.Name), max(w-ansi.StringWidth(left), 1), "…")
			rows = append(rows, left+styleMuted.Render(name))
		}
	}
	out := append([]string{title}, windowLines(rows, cur, max(h-1, 1))...)
	for i, l := range out {
		out[i] = ansi.Truncate(l, w, "…")
	}
	return out
}

// naModelsHead is the MODELS heading: the tool's name, then "/ filter" and
// the count flush right when they fit.
func naModelsHead(st *NewAgentState, fw int) string {
	t := st.selection().selectedTool()
	head := naTitle("MODELS", st.Pane == naPaneModels) + "  " + stylePrimary.Render(protocol.FlattenDisplayText(t.Name))
	n := len(t.Models)
	count := fmt.Sprintf("%d models", n)
	if n == 1 {
		count = "1 model"
	}
	if st.Filter != "" {
		count = fmt.Sprintf("%d · %d shown", n, len(st.filteredIdx()))
	}
	right := styleKey.Render("/") + styleDim.Render(" filter   "+count)
	if gap := fw - ansi.StringWidth(head) - ansi.StringWidth(right); gap >= 2 {
		return head + strings.Repeat(" ", gap) + right
	}
	return head
}

// naFilterLine is the filter as typed ("> son█"), or nil when there is none.
func naFilterLine(m Model, st *NewAgentState) []string {
	if st.Filter == "" && !st.Filtering {
		return nil
	}
	cur := ""
	if st.Filtering {
		cur = cursorBlock(m)
	}
	return []string{styleArrow.Render("> ") + styleSlug.Render(protocol.FlattenDisplayText(st.Filter)) + cur}
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
	return windowLines(out, pos, rows)
}

// strconvQuote quotes the filter for a message, flattened for display.
func strconvQuote(s string) string { return "“" + protocol.FlattenDisplayText(s) + "”" }

// naEffortLine is EFFORT and the model's options in a row, the chosen one
// bracketed and bold (not color only).
func naEffortLine(st *NewAgentState) string {
	head := naTitle("EFFORT", false) + "  "
	w := st.selection()
	opts := w.effortOptions()
	if len(opts) == 0 {
		return head + styleDim.Render("(none for this model)")
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
	return head + strings.Join(parts, "  ")
}

// naSkillLines is the skills checklist in at most rows rows: ▸ on the cursor
// while the pane is focused, [x] on the chosen ones, then the description.
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
		row := naMarkCol(st.Pane == naPaneSkills && i == st.SkillCur) + box + " " + styleSlug.Render(name) +
			strings.Repeat(" ", max(nameW-ansi.StringWidth(name), 0))
		if s.Description != "" {
			row += "  " + styleDim.Render(protocol.FlattenDisplayText(s.Description))
		}
		out[i] = row
	}
	return windowLines(out, st.SkillCur, rows)
}

// naAgentRight is the right pane fw wide and bh tall: models (filter line,
// list), effort, and skills when the agent can load them. The model list
// is as tall as the filtered models (one row at least) within the room
// left.
func naAgentRight(m Model, st *NewAgentState, fw, bh int) []string {
	filter := naFilterLine(m, st)
	var skills []string
	if st.selection().skillsApply() {
		skills = append([]string{"", naTitle("SKILLS", st.Pane == naPaneSkills) + "  " + styleDim.Render("optional")},
			naSkillLines(st, min(len(st.Skills), naSkillRows))...)
	}
	fixed := 1 + len(filter) + 2 + len(skills) // heading, blank, effort
	area := max(1, min(len(st.filteredIdx()), bh-fixed))
	lines := []string{naModelsHead(st, fw)}
	lines = append(lines, filter...)
	lines = append(lines, padOrTrim(naModelRows(st, area), area)...)
	lines = append(lines, "", naEffortLine(st))
	lines = append(lines, skills...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, fw, "…")
	}
	return lines
}

// naAgentStrip is the narrow screen's pane strip: ‹ Agents · [Models] · … ›.
func naAgentStrip(st *NewAgentState) string {
	parts := make([]string, naPaneCount)
	for p := range naPaneCount {
		if p == st.Pane {
			parts[p] = styleBold.Render("[" + naPaneNames[p] + "]")
		} else {
			parts[p] = styleDim.Render(naPaneNames[p])
		}
	}
	return "  " + styleArrow.Render("‹ ") + strings.Join(parts, styleMuted.Render(" · ")) + styleArrow.Render(" ›")
}

// naAgentNarrow is the one-column Agent screen: the strip, then the focused
// pane (the models pane carries the effort line).
func naAgentNarrow(m Model, st *NewAgentState, w, bh int) []string {
	lines := []string{naAgentStrip(st)}
	rest := max(bh-1, 1)
	inner := max(w-2-pageRightMargin, 1)
	switch st.Pane {
	case naPaneAgents:
		lines = append(lines, naAgentsPane(st, w, rest, 2)...)
	case naPaneModels:
		filter := naFilterLine(m, st)
		area := max(1, rest-1-len(filter)-2)
		body := []string{naModelsHead(st, inner)}
		body = append(body, filter...)
		body = append(body, padOrTrim(naModelRows(st, area), max(1, min(area, len(st.filteredIdx()))))...)
		body = append(body, "", naEffortLine(st))
		lines = append(lines, indentLines(body, 2)...)
	case naPaneSkills:
		body := []string{"no skills for this agent"}
		if st.selection().skillsApply() {
			body = naSkillLines(st, max(rest, 1))
		}
		lines = append(lines, indentLines(body, 2)...)
	default:
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return lines
}
