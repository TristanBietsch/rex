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
	head := naHeader("NEW AGENT", middle, styleDim.Render("draft"), w)
	var body []string
	hints := naHints(&NewAgentState{Section: naTask, Editing: true}, narrow)
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

// naHeader is pageHeader, but under pageNarrowW the folder gives way so
// the right word still fits (mockup G: "draft" stays).
func naHeader(title, middle, right string, w int) []string {
	if !pageNarrow(w) {
		return pageHeader(title, middle, right, w)
	}
	head := pageHeader(title, "", "", w)
	rightW := ansi.StringWidth(right)
	left := strings.TrimRight(ansi.Strip(head[1]), " ")
	if rightW == 0 || ansi.StringWidth(left)+1+rightW+pageRightMargin > w {
		return head
	}
	row := ansi.Truncate(head[1], w-pageRightMargin-rightW, "") + right
	head[1] = padLine(row, w)
	return head
}

// naHints is the footer for the mode and the focused section: the editor's
// keys while typing, else the section's own keys and the navigation ones.
func naHints(st *NewAgentState, narrow bool) [][2]string {
	switch {
	case st.Launching:
		return [][2]string{{"esc", "close (keeps the draft)"}}
	case len(st.Pending) > 0:
		return naPendingHints()
	case st.Renaming:
		return [][2]string{{"enter", "save"}, {"esc", "cancel"}}
	case st.Editing && narrow:
		return [][2]string{{"enter", "launch"}, {"esc", "navigate"}, {"tab", "next"}}
	case st.Editing:
		return [][2]string{
			{"enter", "launch"}, {"ctrl+j", "newline"}, {"@", "file"}, {"ctrl+o", "import"},
			{"esc", "navigate"}, {"tab", "next"},
		}
	case st.OnRail && !narrow:
		return [][2]string{{"j/k", "step"}, {"l", "open"}, {"ctrl+s", "launch"}, {"esc", "close"}}
	}
	var own [][2]string
	switch st.Section {
	case naAgent:
		own = [][2]string{{"enter", "change"}}
	case naTask:
		own = [][2]string{{"i", "edit"}}
	case naContext:
		if len(st.Items) > 0 {
			own = [][2]string{{"enter", "preview"}, {"x", "remove"}, {"r", "rename"}, {"e", "edit"}}
		} else {
			own = [][2]string{{"ctrl+o", "import"}}
		}
	case naTrust:
		own = [][2]string{{"enter", "trust"}}
	case naLaunchRow:
		own = [][2]string{{"enter", "launch"}}
	case naSectionCount:
	}
	if narrow {
		return append(own[:min(len(own), 2):min(len(own), 2)], [2]string{"j/k", "move"}, [2]string{"esc", "close"})
	}
	own = append(own, [2]string{"j/k", "move"}, [2]string{"h", "steps"})
	if st.Section != naLaunchRow {
		own = append(own, [2]string{"ctrl+s", "launch"})
	}
	return append(own, [2]string{"esc", "close"})
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
	case naLaunchRow:
		if st.Task.Empty() && len(st.Items) == 0 {
			return styleMuted.Render("·"), ""
		}
		return styleArrow.Render("›"), ""
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
	lines := []string{" " + naTitle("STEPS", st.OnRail)}
	for s := range naSectionCount {
		mark, word := naMark(st, tv, s)
		name := naSectionNames[s]
		left := " " + mark + " " + stylePrimary.Render(name)
		switch {
		case s == st.Section && st.OnRail:
			left = " " + styleArrow.Render("▸ ") + selectedStyle(stylePrimary).Bold(true).Render(name)
		case s == st.Section:
			left = " " + styleArrow.Render("▸ "+name)
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
// sits between them. The narrow line (mockup G) drops the word "effort".
func naAgentLine(st *NewAgentState, sep string, narrow bool) string {
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
		if !narrow {
			line += styleMuted.Render(sep) + styleDim.Render("effort ")
		} else {
			line += styleMuted.Render(sep)
		}
		line += styleSlug.Render(protocol.FlattenDisplayText(e))
	}
	return line
}

// naEditorBox is the task editor in a box w wide with rows text rows: the
// placeholder while empty, the focus border while the task is the focused
// step, the cursor only while typing (Editing).
func naEditorBox(m Model, st *NewAgentState, w, rows int) []string {
	w = max(w, 5)
	inner := w - 4
	focused := st.Section == naTask && !st.OnRail
	border := styleBorderFg
	if focused {
		border = styleBorderFocus
	}
	cur := ""
	if st.Editing {
		cur = cursorBlock(m)
	}
	text := st.Task.View(inner, rows, st.Editing, cur)
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

// naNotice is the open-time notice (a remembered agent that is gone) and a
// draft whose files are from another folder (naStaleRoot), as warning lines,
// or nil.
func naNotice(st *NewAgentState) []string {
	var out []string
	if st.Notice != "" {
		out = append(out, styleBootWarn.Render("! ")+styleDim.Render(protocol.FlattenDisplayText(st.Notice)))
	}
	if naStaleRoot(st) {
		out = append(out, styleBootWarn.Render("! ")+styleDim.Render("files from "+
			protocol.FlattenDisplayText(tildePath(st.ItemsRoot))+", not this folder: x removes them"))
	}
	return out
}

// naForm is the Compose form fw wide for a body bh rows tall (mockups A
// and B): agent, task editor, context list and trust, each under its
// heading, and once there is a task or context the launch summary on the
// last row. The editor gives up rows to the list first, down to one; a list
// longer than what is left scrolls with its cursor.
func naForm(m Model, st *NewAgentState, tv naTrustView, fw, bh int) []string {
	notice := naNotice(st)
	errs := naErrLines(st, fw)
	agent := naAgentRow(m, st, "  ·  ", false, fw)
	change := styleKey.Render("ctrl+g") + styleDim.Render(" change")
	if naToolsLoading(m) {
		change = ""
	}
	if gap := fw - ansi.StringWidth(agent) - ansi.StringWidth(change); gap >= 2 {
		agent += strings.Repeat(" ", gap) + change
	}
	// Eleven rows are fixed (headings, agent, box edges, two trust rows and
	// two blanks); a blank and the launch row stay at the bottom. The
	// focused trust section shows every key with its exact folder (P2's
	// lines), so it takes more.
	free := 2
	trust := indentLines(naTrustLines(tv, st), 2)
	if st.Section == naTrust {
		trust = trustLines(m, st.Trust, spawnCWD(m), fw)
	}
	comp := naCompleteLines(m, st, fw)
	budget := bh - 11 - free - len(notice) - len(errs) - len(comp) - (len(trust) - 2)
	listRows := min(max(len(st.Items), 1), max(1, budget-1))
	rows := max(1, min(10, budget-listRows))

	on := func(s naSection) bool { return st.Section == s && !st.OnRail }
	lines := []string{naTitle("AGENT", on(naAgent)), agent}
	lines = append(lines, notice...)
	lines = append(lines, "", naTitle("TASK", on(naTask)))
	lines = append(lines, naEditorBox(m, st, fw, rows)...)
	lines = append(lines, comp...)
	lines = append(lines, naContextLines(m, st, fw, listRows)...)
	lines = append(lines, errs...)
	lines = append(lines, "", naTitle("TRUST", on(naTrust)))
	lines = append(lines, trust...)
	if len(lines) < bh {
		for len(lines) < bh-1 {
			lines = append(lines, "")
		}
		lines = append(lines, naLaunchLine(m, st))
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
	focused := st.Section == naContext && !st.OnRail
	head := naTitle("CONTEXT", focused)
	if len(st.Items) == 0 && st.Indexing {
		return []string{head, "  " + naLoadingRow(m, st, fw-2)} // mockup F
	}
	if len(st.Items) == 0 {
		return []string{head, "  " + styleMuted.Render("none yet: type @path, paste, or ctrl+o to import")}
	}
	total := styleDim.Render(naItemsTotal(st.Items))
	if gap := fw - ansi.StringWidth(head) - ansi.StringWidth(total); gap >= 2 {
		head += strings.Repeat(" ", gap) + total
	}
	items := make([]string, len(st.Items))
	for i := range st.Items {
		items[i] = naItemRow(m, st, i, fw, focused)
	}
	return append([]string{head}, st.itemsView.window("items", items, st.ItemIdx, max(rows, 1), "  ")...)
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
		meta = naLines(spawnctx.Lines(it.Text)) + " " + naBytes(it.Bytes)
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

// naErrLines is the page's error w cells wide: "✗ " and words in the
// failure style (a "! " warning in the warning style), a hint line dimmed
// under it, each wrapped at word boundaries so no word of it is cut; and
// first, the confirm a denylisted path waits for. Nil for none.
func naErrLines(st *NewAgentState, w int) []string {
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
	for i, l := range naWrap(protocol.FlattenDisplayText(parts[0]), w) {
		if i > 0 {
			l = "  " + l // under the words, past the mark
		}
		out = append(out, style.Render(l))
	}
	for _, p := range parts[1:] {
		for _, l := range naWrap(protocol.FlattenDisplayText(p), w-2) {
			out = append(out, "  "+styleDim.Render(l))
		}
	}
	return out
}

// naWrap wraps plain text at word boundaries to w cells (a word longer
// than w is broken); continuation lines get 2 cells less, for their indent.
func naWrap(s string, w int) []string {
	w = max(w, 8)
	first, rest, _ := strings.Cut(ansi.Wrap(s, w, ""), "\n")
	out := []string{first}
	if rest != "" {
		out = append(out, strings.Split(ansi.Wrap(strings.ReplaceAll(rest, "\n", " "), w-2, ""), "\n")...)
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

// naLaunchLine is the launch row: "▸ launch" while it is the focused step
// (enter launches), then the summary once there is a task or context, or
// the launch's progress.
func naLaunchLine(m Model, st *NewAgentState) string {
	if st.Launching {
		return naStartingLine(m)
	}
	head := "  " + styleDim.Render("launch")
	if st.Section == naLaunchRow && !st.OnRail {
		head = styleArrow.Render("▸ ") + selectedStyle(stylePrimary).Bold(true).Render("launch")
	}
	if st.Task.Empty() && len(st.Items) == 0 {
		return head + styleMuted.Render("  describe the task first")
	}
	return head + styleMuted.Render("  ·  ") + naLaunchSummary(m, st)
}

// naStrip is the narrow page's section strip: ‹ Agent · [Task] · Context 2
// · Trust ›, the focused section bracketed and bold, the context counted.
func naStrip(st *NewAgentState) string {
	parts := make([]string, naSectionCount)
	for s := range naSectionCount {
		name := naSectionNames[s]
		if s == naContext && len(st.Items) > 0 {
			name += " " + strconv.Itoa(len(st.Items))
		}
		if s == st.Section {
			parts[s] = styleBold.Render("[" + name + "]")
		} else {
			parts[s] = styleDim.Render(name)
		}
	}
	return "  " + styleArrow.Render("‹ ") + strings.Join(parts, styleMuted.Render(" · ")) + styleArrow.Render(" ›")
}

// naNarrowBody is the one-column page under pageNarrowW (mockup G).
func naNarrowBody(m Model, st *NewAgentState, tv naTrustView, w, bh int) []string {
	notice := naNotice(st)
	comp := naCompleteLines(m, st, max(w-2, 1))
	trust := []string{"  " + styleDim.Render("trust: ") + styleBootWarn.Render(tv.word)}
	if tv.ok {
		trust = []string{"  " + styleDim.Render("trust: ") + styleBootReady.Render(tv.word)}
	}
	if st.Section == naTrust {
		trust = trustLines(m, st.Trust, spawnCWD(m), w)
	}
	trust = append(trust, "  "+naLaunchLine(m, st))
	rows := max(1, min(8, bh-7-len(trust)-len(notice)-len(naErrLines(st, w-2))-len(comp)))
	lines := []string{naStrip(st), "  " + naAgentRow(m, st, " · ", true, w-2)}
	lines = append(lines, indentLines(notice, 2)...)
	if st.Section == naContext && len(st.Items) > 0 {
		lines = append(lines, indentLines(naContextLines(m, st, max(w-2, 1), rows+2), 2)...)
	} else {
		lines = append(lines, indentLines(naEditorBox(m, st, w-2, rows), 2)...)
		lines = append(lines, indentLines(comp, 2)...)
		ctx := styleDim.Render("context: none yet")
		if n := len(st.Items); n > 0 {
			ctx = styleDim.Render(strings.Replace(naItemsTotal(st.Items), "item", "context item", 1)) +
				styleMuted.Render("  (j to view)")
		}
		lines = append(lines, "  "+ctx)
	}
	lines = append(lines, indentLines(naErrLines(st, w-2), 2)...)
	lines = append(lines, trust...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return lines
}

// naToolsLoading reports whether the daemon's tool list is still on its way:
// no Snapshot yet. A Snapshot without one is an old daemon, and every tool
// counts as installed (pickerTools), so the agent shows.
func naToolsLoading(m Model) bool { return !m.toolsKnown && !m.toolsSnap }

// naAgentRow is the AGENT section's row: the chosen agent, or while the
// daemon's tool list is unknown the spinner and words of mockup F.
func naAgentRow(m Model, st *NewAgentState, sep string, narrow bool, w int) string {
	if naToolsLoading(m) {
		return ansi.Truncate(styleArrow.Render(naSpinner(m))+styleDim.Render(" checking which agents are installed…"), max(w, 1), "…")
	}
	return naAgentLine(st, sep, narrow)
}
