package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// naRailW is the step rail's width, its 2-column indent included.
const naRailW = 16

// naPlaceholder is the empty task editor's hint.
const naPlaceholder = "describe the task. @ adds a file, paste adds context"

// naTrustView is what the rail and the trust section say about the folder:
// the rail mark and word, and the offer they come from.
type naTrustView struct {
	offer      trustOffer
	mark, word string
	ok         bool // trusted one way or another (the mark is a success)
}

func naTrustOf(m Model, st *NewAgentState) naTrustView {
	v := naTrustView{offer: trustOfferFor(m, spawnCWD(m)), mark: "?", word: "ask"}
	switch {
	case v.offer.Covered != "":
		v.mark, v.word, v.ok = "✓", "trusted", true
	case v.offer.Refused != "":
	case st.Trust == trustSession:
		v.mark, v.word, v.ok = "✓", "sess", true
	case st.Trust == trustAlways:
		v.mark, v.word, v.ok = "✓", "always", true
	}
	return v
}

// renderNewAgent is the new-agent page for a w x h layout area: the step
// rail beside the form, or under pageNarrowW a section strip over a
// one-column form. Exactly h lines, none wider than w.
func renderNewAgent(m Model, w, h int) string {
	st := m.NewAgent
	middle := styleDim.Render(protocol.FlattenDisplayText(tildePath(spawnCWD(m))))
	head := pageHeader("NEW AGENT", middle, styleDim.Render("draft"), w)
	narrow := pageNarrow(w)
	bh := pageBodyHeight(h)
	var body []string
	if st != nil {
		tv := naTrustOf(m, st)
		if narrow {
			body = naNarrowBody(m, st, tv, w, bh)
		} else {
			fw := max(w-naRailW-3-pageRightMargin, 1)
			body = joinPanes(naRail(st, tv), naForm(m, st, tv, fw, bh), naRailW, bh)
		}
	}
	section := naTask
	if st != nil {
		section = st.Section
	}
	return composePage(head, body, pageFooter(m, naHints(section, narrow), w, helpBarOn(m)), w, h)
}

// naHints is the footer for the focused section.
func naHints(section naSection, narrow bool) [][2]string {
	launch := [2]string{"ctrl+s", "launch"}
	if section == naTask {
		launch = [2]string{"enter", "launch"}
	}
	if narrow {
		return [][2]string{launch, {"tab", "next"}, {"esc", "close"}}
	}
	if section == naTask {
		return [][2]string{
			{"enter", "launch"}, {"ctrl+j", "newline"}, {"@", "file"}, {"ctrl+o", "import"},
			{"tab", "next"}, {"esc", "close"},
		}
	}
	return [][2]string{launch, {"tab", "next"}, {"1-4", "section"}, {"esc", "close"}}
}

// naMark is a section's rail mark and word, styled: ✓ done, · empty, ? ask.
func naMark(st *NewAgentState, tv naTrustView, s naSection) (mark, word string) {
	switch s {
	case naAgent:
		return styleBootReady.Render("✓"), ""
	case naTask:
		if st.Task.Empty() {
			return styleMuted.Render("·"), ""
		}
		return styleBootReady.Render("✓"), ""
	case naTrust:
		if tv.ok {
			return styleBootReady.Render(tv.mark), styleDim.Render(tv.word)
		}
		return styleBootWarn.Render(tv.mark), styleBootWarn.Render(tv.word)
	default: // naContext: items come with R16-3
		return styleMuted.Render("·"), ""
	}
}

// naRail is the step rail: STEPS, then each section with its mark (▸ on the
// focused one) and its word flush right.
func naRail(st *NewAgentState, tv naTrustView) []string {
	lines := []string{"  " + styleSectionTitle.Render("STEPS")}
	for s := range naSectionCount {
		mark, word := naMark(st, tv, s)
		name := naSectionNames[s]
		left := "  " + mark + " " + stylePrimary.Render(name)
		if s == st.Section {
			left = "  " + styleArrow.Render("▸ ") + selectedStyle(stylePrimary).Bold(true).Render(name)
		}
		if word != "" {
			gap := max(1, naRailW-1-ansi.StringWidth(left)-ansi.StringWidth(word))
			left += strings.Repeat(" ", gap) + word
		}
		lines = append(lines, left)
	}
	return lines
}

// naTitle is a form section's heading, in the accent while focused.
func naTitle(title string, focused bool) string {
	if focused {
		return styleArrow.Render(title)
	}
	return styleSectionTitle.Render(title)
}

// naAgentLine is the chosen agent: brand glyph, name, model and effort; sep
// sits between them.
func naAgentLine(st *NewAgentState, sep string) string {
	w := st.selection()
	t := w.selectedTool()
	glyph := ""
	if t.Icon != "" {
		glyph = brandStyle(t.Color).Render(protocol.FlattenDisplayText(t.Icon)) + " "
	}
	line := glyph + stylePrimary.Render(protocol.FlattenDisplayText(t.Name))
	if md := w.selectedModel(); md.ID != "" {
		line += styleMuted.Render(sep) + styleSlug.Render(protocol.FlattenDisplayText(md.ID))
	}
	if e := w.currentEffort(); e != "" {
		line += styleMuted.Render(sep) + styleDim.Render("effort ") + styleSlug.Render(protocol.FlattenDisplayText(e))
	}
	return line
}

// naEditorBox is the task editor in a box w wide with rows text rows: the
// placeholder while empty, the cursor while focused.
func naEditorBox(m Model, st *NewAgentState, w, rows int, focused bool) []string {
	w = max(w, 5)
	inner := w - 4
	border := styleBorderFg
	if focused {
		border = styleBorderFocus
	}
	cur := ""
	if focused {
		cur = cursorBlock(m)
	}
	text := st.Task.View(inner, rows, focused, cur)
	if st.Task.Empty() {
		hint := styleMuted.Render(ansi.Truncate(naPlaceholder, inner, "…"))
		if rows > 1 {
			text[1] = hint
		} else {
			text[0] = ansi.Truncate(cur+hint, inner, "…")
		}
	}
	out := []string{border.Render("┌" + strings.Repeat("─", w-2) + "┐")}
	for _, l := range text {
		out = append(out, border.Render("│ ")+padLine(l, inner)+border.Render(" │"))
	}
	return append(out, border.Render("└"+strings.Repeat("─", w-2)+"┘"))
}

// naTrustLines is the trust section: the folder's state in words, then the
// keys and the folder ctrl+a saves.
func naTrustLines(tv naTrustView, st *NewAgentState) []string {
	o := tv.offer
	switch {
	case o.Covered != "":
		return []string{styleDim.Render("trusted (" + trustSettingID + ")")}
	case o.Refused != "":
		return []string{styleBootWarn.Render(protocol.FlattenDisplayText(o.Refused))}
	}
	var state string
	switch st.Trust {
	case trustSession:
		state = styleBootReady.Render("trusted for this session only")
	case trustAlways:
		state = styleBootReady.Render("always trusted") +
			styleDim.Render(": "+protocol.FlattenDisplayText(o.AlwaysEntry)+" is saved to "+trustSettingID+" at launch")
	default:
		state = styleBootWarn.Render("folder not trusted") + styleDim.Render(": the agent will ask")
	}
	keys := styleKey.Render("ctrl+t") + styleDim.Render(" this session only") + styleMuted.Render(" · ")
	if o.AlwaysDir == "" {
		keys += styleKey.Render("ctrl+a") + styleDim.Render(": only folders inside your home can be saved")
	} else {
		keys += styleKey.Render("ctrl+a") + styleDim.Render(" always ("+protocol.FlattenDisplayText(o.AlwaysEntry)+")")
	}
	return []string{state, keys}
}

// naNotice is the open-time notice (a remembered agent that is gone), as a
// warning line, or nil.
func naNotice(m Model) []string {
	if m.Notice == "" {
		return nil
	}
	return []string{styleBootWarn.Render("! ") + styleDim.Render(protocol.FlattenDisplayText(m.Notice))}
}

// naForm is the Compose form fw wide for a body bh rows tall (mockup A):
// agent, task editor, context and trust, each under its heading.
func naForm(m Model, st *NewAgentState, tv naTrustView, fw, bh int) []string {
	notice := naNotice(m)
	agent := naAgentLine(st, "  ·  ")
	change := styleKey.Render("ctrl+g") + styleDim.Render(" change")
	if gap := fw - ansi.StringWidth(agent) - ansi.StringWidth(change); gap >= 2 {
		agent += strings.Repeat(" ", gap) + change
	}
	// Twelve rows are fixed (headings, agent, box edges, context, trust and
	// two blanks); four stay free at the bottom as in mockup A.
	rows := max(1, min(10, bh-16-len(notice)))
	trust := naTrustLines(tv, st)

	lines := []string{naTitle("AGENT", st.Section == naAgent), agent}
	lines = append(lines, notice...)
	lines = append(lines, "", naTitle("TASK", st.Section == naTask))
	lines = append(lines, naEditorBox(m, st, fw, rows, st.Section == naTask)...)
	lines = append(lines,
		naTitle("CONTEXT", st.Section == naContext),
		"  "+styleMuted.Render("none yet: type @path, paste, or ctrl+o to import"),
		"",
		naTitle("TRUST", st.Section == naTrust),
	)
	lines = append(lines, indentLines(trust, 2)...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, fw, "…")
	}
	return lines
}

// naStrip is the narrow page's section strip: ‹ Agent · [Task] · … ›, the
// focused section bracketed and bold.
func naStrip(st *NewAgentState) string {
	parts := make([]string, naSectionCount)
	for s := range naSectionCount {
		if s == st.Section {
			parts[s] = styleBold.Render("[" + naSectionNames[s] + "]")
		} else {
			parts[s] = styleDim.Render(naSectionNames[s])
		}
	}
	return "  " + styleArrow.Render("‹ ") + strings.Join(parts, styleMuted.Render(" · ")) + styleArrow.Render(" ›")
}

// naNarrowBody is the one-column page under pageNarrowW (mockup G).
func naNarrowBody(m Model, st *NewAgentState, tv naTrustView, w, bh int) []string {
	notice := naNotice(m)
	rows := max(1, min(8, bh-8-len(notice)))
	trust := styleDim.Render("trust: ") + styleBootWarn.Render(tv.word)
	if tv.ok {
		trust = styleDim.Render("trust: ") + styleBootReady.Render(tv.word)
	}
	lines := []string{naStrip(st), "  " + naAgentLine(st, " · ")}
	lines = append(lines, indentLines(notice, 2)...)
	lines = append(lines, indentLines(naEditorBox(m, st, w-2, rows, st.Section == naTask), 2)...)
	lines = append(lines,
		"  "+styleDim.Render("context: none yet"),
		"  "+trust,
	)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return lines
}
