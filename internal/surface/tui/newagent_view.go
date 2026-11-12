package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
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
	narrow := pageNarrow(w)
	bh := pageBodyHeight(h)
	if st != nil && st.Screen == naAgentScreen {
		head := pageHeader("NEW AGENT › AGENT", middle, "", w)
		foot := pageFooter(m, naAgentHints(st, narrow), w, helpBarOn(m))
		return composePage(head, naAgentBody(m, st, w, bh), foot, w, h)
	}
	head := pageHeader("NEW AGENT", middle, styleDim.Render("draft"), w)
	var body []string
	hints := naHints(&NewAgentState{Section: naTask}, narrow)
	if st != nil {
		tv := naTrustOf(m, st)
		preview := st.Screen == naPreview && st.ItemIdx < len(st.Items)
		picker := st.Screen == naImportScreen
		fw := max(w-naRailW-3-pageRightMargin, 1)
		switch {
		case picker && narrow:
			body = indentLines(naImportBody(m, st, max(w-2, 1), bh), 2)
		case picker:
			body = joinPanes(naRail(st, tv), naImportBody(m, st, fw, bh), naRailW, bh)
		case preview && narrow:
			body = indentLines(naPreviewBody(m, st, max(w-2, 1), bh), 2)
		case preview:
			body = joinPanes(naRail(st, tv), naPreviewBody(m, st, fw, bh), naRailW, bh)
		case narrow:
			body = naNarrowBody(m, st, tv, w, bh)
		default:
			body = joinPanes(naRail(st, tv), naForm(m, st, tv, fw, bh), naRailW, bh)
		}
		hints = naHints(st, narrow)
		switch {
		case picker:
			hints = naImportHints(st, narrow)
		case preview:
			hints = naPreviewHints(st, narrow)
		}
	}
	return composePage(head, body, pageFooter(m, hints, w, helpBarOn(m)), w, h)
}

// naHints is the footer for the focused section.
func naHints(st *NewAgentState, narrow bool) [][2]string {
	section := st.Section
	if len(st.Pending) > 0 {
		return naPendingHints()
	}
	if st.Renaming {
		return [][2]string{{"enter", "save"}, {"esc", "cancel"}}
	}
	if section == naContext && len(st.Items) > 0 {
		if narrow {
			return [][2]string{{"enter", "preview"}, {"x", "remove"}, {"esc", "close"}}
		}
		return [][2]string{
			{"enter", "preview"}, {"x", "remove"}, {"r", "rename"}, {"tab", "next"},
			{"ctrl+s", "launch"}, {"esc", "close"},
		}
	}
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
	default: // naContext: the number of items
		if len(st.Items) == 0 {
			return styleMuted.Render("·"), ""
		}
		return styleBootReady.Render("✓"), styleDim.Render(strconv.Itoa(len(st.Items)))
	}
}

// naRail is the step rail: STEPS, then each section with its mark (▸ on the
// focused one) and its word flush right. Every row fits naRailW whole, so no
// word is ever cut (1-column indent: " ✓ Trust" + "trusted" is 16).
func naRail(st *NewAgentState, tv naTrustView) []string {
	lines := []string{" " + styleSectionTitle.Render("STEPS")}
	for s := range naSectionCount {
		mark, word := naMark(st, tv, s)
		name := naSectionNames[s]
		left := " " + mark + " " + stylePrimary.Render(name)
		if s == st.Section {
			left = " " + styleArrow.Render("▸ ") + selectedStyle(stylePrimary).Bold(true).Render(name)
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
func naNotice(st *NewAgentState) []string {
	if st.Notice == "" {
		return nil
	}
	return []string{styleBootWarn.Render("! ") + styleDim.Render(protocol.FlattenDisplayText(st.Notice))}
}

// naForm is the Compose form fw wide for a body bh rows tall (mockups A
// and B): agent, task editor, context list and trust, each under its
// heading, and once there is a task or context the launch summary on the
// last row. The editor gives up rows to the list first, down to one; a list
// longer than what is left scrolls with its cursor.
func naForm(m Model, st *NewAgentState, tv naTrustView, fw, bh int) []string {
	notice := naNotice(st)
	errs := naErrLines(st)
	agent := naAgentLine(st, "  ·  ")
	change := styleKey.Render("ctrl+g") + styleDim.Render(" change")
	if gap := fw - ansi.StringWidth(agent) - ansi.StringWidth(change); gap >= 2 {
		agent += strings.Repeat(" ", gap) + change
	}
	summary := !st.Task.Empty() || len(st.Items) > 0
	// Eleven rows are fixed (headings, agent, box edges, trust and two
	// blanks). Four stay free at the bottom as in mockup A; with a summary,
	// a blank and the summary row.
	free := 4
	if summary {
		free = 2
	}
	comp := naCompleteLines(m, st, fw)
	budget := bh - 11 - free - len(notice) - len(errs) - len(comp)
	listRows := min(max(len(st.Items), 1), max(1, budget-1))
	rows := max(1, min(10, budget-listRows))
	trust := naTrustLines(tv, st)

	lines := []string{naTitle("AGENT", st.Section == naAgent), agent}
	lines = append(lines, notice...)
	lines = append(lines, "", naTitle("TASK", st.Section == naTask))
	lines = append(lines, naEditorBox(m, st, fw, rows, st.Section == naTask)...)
	lines = append(lines, comp...)
	lines = append(lines, naContextLines(m, st, fw, listRows)...)
	lines = append(lines, errs...)
	lines = append(lines, "", naTitle("TRUST", st.Section == naTrust))
	lines = append(lines, indentLines(trust, 2)...)
	if summary && len(lines) < bh {
		for len(lines) < bh-1 {
			lines = append(lines, "")
		}
		lines = append(lines, naLaunchSummary(m, st))
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, fw, "…")
	}
	return lines
}

// naContextLines is the CONTEXT heading (with the count and size flush
// right) over the item list, at most rows rows, windowed on the cursor; or
// the empty state.
func naContextLines(m Model, st *NewAgentState, fw, rows int) []string {
	focused := st.Section == naContext
	head := naTitle("CONTEXT", focused)
	if len(st.Items) == 0 {
		return []string{head, "  " + styleMuted.Render("none yet: type @path, paste, or ctrl+o to import")}
	}
	total := styleDim.Render(naItemsTotal(st.Items))
	if gap := fw - ansi.StringWidth(head) - ansi.StringWidth(total); gap >= 2 {
		head += strings.Repeat(" ", gap) + total
	}
	rows = max(rows, 1)
	start := 0
	if len(st.Items) > rows {
		start = min(max(st.ItemIdx-rows/2, 0), len(st.Items)-rows)
	}
	lines := []string{head}
	for i := start; i < min(start+rows, len(st.Items)); i++ {
		lines = append(lines, naItemRow(m, st, i, fw, focused))
	}
	return lines
}

// naItemsTotal is "N items · X KB": the count and the summed sizes.
func naItemsTotal(items []spawnctx.Item) string {
	n := 0
	for _, it := range items {
		n += it.Bytes
	}
	word := " items · "
	if len(items) == 1 {
		word = " item · "
	}
	return strconv.Itoa(len(items)) + word + naBytes(n)
}

// naItemRow is one context row (mockup B): the cursor, the kind as a word,
// the name, a paste's first line quoted, and its size flush right.
func naItemRow(m Model, st *NewAgentState, i, fw int, focused bool) string {
	it := st.Items[i]
	cur := focused && i == st.ItemIdx
	left := "  "
	label := protocol.FlattenDisplayText(it.Name)
	if it.Kind == spawnctx.Dir && !strings.HasSuffix(label, "/") {
		label += "/"
	}
	name := stylePrimary.Render(label)
	if cur {
		left = styleArrow.Render("▸ ")
		name = selectedStyle(stylePrimary).Bold(true).Render(label)
	}
	if cur && st.Renaming {
		name = naRenameField(m, st)
	}
	left += styleMuted.Render(padRight(it.Kind.String(), 6)) + " " + name
	var meta string
	switch it.Kind {
	case spawnctx.Paste:
		meta = strconv.Itoa(spawnctx.Lines(it.Text)) + " lines " + naBytes(it.Bytes)
	case spawnctx.File:
		meta = naBytes(it.Bytes)
	case spawnctx.Dir:
		meta = strconv.Itoa(it.Files) + " files"
	case spawnctx.URL:
	}
	metaW := ansi.StringWidth(meta)
	if it.Kind == spawnctx.Paste && (!cur || !st.Renaming) {
		if room := fw - ansi.StringWidth(left) - metaW - 6; room >= 6 {
			left += "  " + styleDim.Render(`"`+ansi.Truncate(naFirstLine(it.Text), room, "…")+`"`)
		}
	}
	if gap := fw - ansi.StringWidth(left) - metaW; meta != "" && gap >= 2 {
		left += strings.Repeat(" ", gap) + styleDim.Render(meta)
	}
	return left
}

// padRight pads s with spaces to n cells.
func padRight(s string, n int) string {
	return s + strings.Repeat(" ", max(0, n-ansi.StringWidth(s)))
}

// naFirstLine is a paste's first non-blank line, flattened for a row.
func naFirstLine(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(line) != "" {
			return protocol.FlattenDisplayText(strings.TrimSpace(strings.ReplaceAll(line, "\t", " ")))
		}
	}
	return ""
}

// naRenameField is the name being typed: bracketed, with the cursor.
func naRenameField(m Model, st *NewAgentState) string {
	return styleArrow.Render("[") + stylePrimary.Render(protocol.FlattenDisplayText(string(st.RenameBuf))) +
		cursorBlock(m) + styleArrow.Render("]")
}

// naErrLines is the page's error: "✗ " and words in the failure style (a
// "! " warning in the warning style), a hint line dimmed under it; and
// first, the confirm a denylisted path waits for. Nil for none.
func naErrLines(st *NewAgentState) []string {
	var out []string
	if len(st.Pending) > 0 {
		out = append(out, naPendingLine(st))
	}
	if st.Err == "" {
		return out
	}
	parts := strings.Split(st.Err, "\n")
	style := styleStateFailed
	if strings.HasPrefix(parts[0], "!") {
		style = styleBootWarn
	}
	out = append(out, style.Render(protocol.FlattenDisplayText(parts[0])))
	for _, p := range parts[1:] {
		out = append(out, "  "+styleDim.Render(protocol.FlattenDisplayText(p)))
	}
	return out
}

// naPromptCap is the prompt limit the summary measures against: the
// daemon's 96 KB, or 16 KB for an agent that takes its prompt as a PTY
// paste.
func naPromptCap(st *NewAgentState) int {
	if st.selection().selectedTool().PromptPaste != nil {
		return spawnctx.PTYPasteCap
	}
	return spawnctx.DefaultCaps.Prompt
}

// naLaunchSummary is the last body row (mockup B): the assembled prompt's
// size against the cap and the slug the launch would take. Assembling runs
// only when the page changed since the last time (Rev).
func naLaunchSummary(m Model, st *NewAgentState) string {
	if !st.sumOK || st.sumRev != st.Rev {
		prompt, err := spawnctx.Assemble(strings.TrimSpace(st.Task.String()), st.Items, spawnctx.DefaultCaps)
		st.sumRev, st.sumOK, st.sumBytes, st.sumErr = st.Rev, true, len(prompt), err
	}
	w := st.selection()
	tool, model := w.selectedTool(), w.selectedModel()
	slug := deriveAgentSlug(tool.ID, model.ID, taskTitle(st.Task.String()), existingSlugs(m))
	tail := styleDim.Render(" · slug ") + styleSlug.Render(protocol.FlattenDisplayText(slug))
	if st.sumErr != nil {
		return styleStateFailed.Render("✗ prompt: "+protocol.FlattenDisplayText(st.sumErr.Error())) + tail
	}
	limit := naPromptCap(st)
	size := stylePrimary.Render(naBytes(st.sumBytes))
	lead := styleDim.Render("prompt ")
	if st.sumBytes > limit {
		lead = styleBootWarn.Render("! prompt ")
		size = styleBootWarn.Render(naBytes(st.sumBytes))
	}
	return lead + size + styleDim.Render(" of "+naBytes(limit)) + tail
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
	notice := naNotice(st)
	comp := naCompleteLines(m, st, max(w-2, 1))
	rows := max(1, min(8, bh-8-len(notice)-len(naErrLines(st))-len(comp)))
	trust := styleDim.Render("trust: ") + styleBootWarn.Render(tv.word)
	if tv.ok {
		trust = styleDim.Render("trust: ") + styleBootReady.Render(tv.word)
	}
	lines := []string{naStrip(st), "  " + naAgentLine(st, " · ")}
	lines = append(lines, indentLines(notice, 2)...)
	if st.Section == naContext && len(st.Items) > 0 {
		lines = append(lines, indentLines(naContextLines(m, st, max(w-2, 1), rows+2), 2)...)
	} else {
		lines = append(lines, indentLines(naEditorBox(m, st, w-2, rows, st.Section == naTask), 2)...)
		lines = append(lines, indentLines(comp, 2)...)
		ctx := styleDim.Render("context: none yet")
		if n := len(st.Items); n > 0 {
			ctx = styleDim.Render(strings.Replace(naItemsTotal(st.Items), "item", "context item", 1)) +
				styleMuted.Render("  (tab to view)")
		}
		lines = append(lines, "  "+ctx)
	}
	lines = append(lines, indentLines(naErrLines(st), 2)...)
	lines = append(lines, "  "+trust)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return lines
}
