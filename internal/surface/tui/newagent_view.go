package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// naPlaceholder is the empty task editor's hint.
const naPlaceholder = "describe the task. @ adds a file, paste adds context"

// naKeyW is the transcript's key column: "summary" and two spaces.
const naKeyW = 9

// naIndentFor is the body's left indent: 6 columns, 4 under pageNarrowW.
// A list keeps its cursor column inside it (two cells), so its text lines
// up with the rest of the body.
func naIndentFor(w int) int {
	if pageNarrow(w) {
		return 4
	}
	return 6
}

// renderNewAgent is the new-agent page for a w x h layout area: the header
// with the step count, the transcript and the current step's body, the
// footer's keys. One layout at every width. Exactly h lines, none wider
// than w.
func renderNewAgent(m Model, w, h int) string {
	st := m.NewAgent
	middle := styleDim.Render(protocol.FlattenDisplayText(tildePath(spawnCWD(m))))
	if st == nil {
		return composePage(naHeader("NEW AGENT", middle, "", w), nil, pageFooter(m, nil, w, helpBarOn(m)), w, h)
	}
	right := "step " + strconv.Itoa(int(st.Step)+1) + "/" + strconv.Itoa(int(naStepCount))
	if st.Launching {
		right = "starting…"
	}
	head := naHeader("NEW AGENT", middle, styleDim.Render(right), w)
	body := naBodyLines(m, st, w, pageBodyHeight(h))
	foot := pageFooter(m, naHints(m, st, pageNarrow(w)), w, helpBarOn(m))
	return composePage(head, body, foot, w, h)
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

// naEscWord is what esc does on the current step, for the footer, in
// naEsc's order.
func naEscWord(st *NewAgentState) string {
	switch {
	case st.Step == naStepModel && st.Filter != "":
		return "clear"
	case st.ReturnToSummary:
		return "summary"
	case st.Step == st.Opened:
		return "close"
	}
	if _, ok := naStepFrom(st, st.Step, -1); !ok {
		return "close"
	}
	return "back"
}

// naHints is the footer for the screen and step: the keys of what has the
// keyboard now, then the step's own and the navigation ones.
func naHints(m Model, st *NewAgentState, narrow bool) [][2]string {
	esc := [2]string{"esc", naEscWord(st)}
	help := [2]string{"?", "help"}
	switch {
	case st.Launching:
		return [][2]string{{"esc", "close (keeps the draft)"}}
	case len(st.Pending) > 0:
		return naPendingHints()
	case st.Renaming:
		return [][2]string{{"enter", "save"}, {"esc", "cancel"}}
	case st.Screen == naImportScreen:
		return naImportHints(st, narrow)
	case st.Screen == naPreview:
		return naPreviewHints(st, narrow)
	case st.Filtering:
		return [][2]string{{"type", "filter"}, {"↑/↓", "model"}, {"enter", "keep"}, {"esc", "clear"}}
	}
	var hints [][2]string
	switch st.Step {
	case naStepAgent:
		if st.OnMissing {
			hints = [][2]string{{"enter", "onboarding"}, {"k", "agents"}}
		} else {
			hints = [][2]string{{"j/k", "agent"}, {"enter", "next"}}
		}
	case naStepModel:
		hints = [][2]string{{"j/k", "model"}, {"/", "filter"}}
		if len(st.selection().effortOptions()) > 0 {
			hints = append(hints, [2]string{"h/l", "effort"})
		}
		hints = append(hints, [2]string{"enter", "next"})
	case naStepSkills:
		hints = [][2]string{{"space", "toggle"}, {"enter", "next"}}
	case naStepTask:
		if narrow {
			return [][2]string{{"enter", "next"}, {"ctrl+j", "newline"}, esc}
		}
		return [][2]string{{"enter", "next"}, {"ctrl+j", "newline"}, {"@", "file"}, {"ctrl+o", "import"}, {"tab", "next"}, esc}
	case naStepContext:
		if len(st.Items) > 0 {
			hints = [][2]string{{"enter", "preview"}, {"tab", "next"}, {"x", "remove"}, {"ctrl+o", "import"}}
		} else {
			hints = [][2]string{{"enter", "next"}, {"ctrl+o", "import"}}
		}
	case naStepTrust:
		hints = [][2]string{{"j/k", "move"}, {"space", "pick"}, {"enter", "next"}}
	case naStepSummary:
		hints = [][2]string{{"enter", "launch"}, {"j/k", "row"}, {"e", "edit row"}}
	case naStepCount:
	}
	if narrow {
		// Esc and ? come before the step's extras, so the cut never drops them.
		return append([][2]string{hints[0], esc, help}, hints[1:]...)[:min(len(hints)+2, 4)]
	}
	return append(hints, esc, help)
}

// naMarkDone styles a transcript line's mark: ✓ done, – skipped.
func naMarkDone(skipped bool) string {
	if skipped {
		return styleMuted.Render("–")
	}
	return styleBootReady.Render("✓")
}

// naAgentValue is the chosen agent: brand glyph and name, or the spinner
// words while the daemon's tool list is on its way.
func naAgentValue(m Model, st *NewAgentState) string {
	if naToolsLoading(m) {
		return styleArrow.Render(naSpinner(m)) + styleDim.Render(" checking which agents are installed…")
	}
	t := st.selection().selectedTool()
	glyph := ""
	if t.Icon != "" {
		glyph = brandStyle(t.Color).Render(protocol.FlattenDisplayText(t.Icon)) + " "
	}
	return glyph + stylePrimary.Render(protocol.FlattenDisplayText(t.Name))
}

// naModelValue is the chosen model and, when it has one, its effort.
func naModelValue(st *NewAgentState) string {
	w := st.selection()
	out := styleSlug.Render(protocol.FlattenDisplayText(w.selectedModel().ID))
	if e := w.currentEffort(); e != "" {
		out += styleMuted.Render(" · ") + styleDim.Render("effort ") + styleSlug.Render(protocol.FlattenDisplayText(e))
	}
	return out
}

// naSkillsValue is the chosen skills, comma separated, or "none".
func naSkillsValue(st *NewAgentState) string {
	names := st.selection().chosenSkills()
	if len(names) == 0 {
		return styleDim.Render("none")
	}
	flat := make([]string, len(names))
	for i, n := range names {
		flat[i] = protocol.FlattenDisplayText(n)
	}
	return styleSlug.Render(strings.Join(flat, ", "))
}

// naTaskValue is the task's first line and how many more it has.
func naTaskValue(st *NewAgentState) string {
	task := strings.TrimSpace(st.Task.String())
	if task == "" {
		return styleDim.Render("empty")
	}
	out := stylePrimary.Render(protocol.FlattenDisplayText(taskTitle(task)))
	if n := strings.Count(task, "\n"); n > 0 {
		out += styleDim.Render(" (+" + naLines(n) + ")")
	}
	return out
}

// naContextValue is the context's count and size, or "none".
func naContextValue(st *NewAgentState) string {
	if len(st.Items) == 0 {
		return styleDim.Render("none")
	}
	return stylePrimary.Render(naItemsTotal(st.Items))
}

// naDoneLine is a finished step as a transcript line: "✓ key  value", or
// a dim "– key  not available" for a step that did not apply.
func naDoneLine(m Model, st *NewAgentState, s naStep) string {
	key := padRight(naStepNames[s], naKeyW)
	if naSkipped(st, s) {
		return "  " + naMarkDone(true) + " " + styleDim.Render(key+"not available")
	}
	var val string
	switch s {
	case naStepAgent:
		val = naAgentValue(m, st)
	case naStepModel:
		val = naModelValue(st)
	case naStepSkills:
		val = naSkillsValue(st)
	case naStepTask:
		val = naTaskValue(st)
	case naStepContext:
		val = naContextValue(st)
	case naStepTrust:
		val = stylePrimary.Render(naTrustDone(m, st))
	case naStepSummary, naStepCount:
	}
	return "  " + naMarkDone(false) + " " + styleDim.Render(key) + val
}

// naStepAsk is the current step's question.
var naStepAsk = [naStepCount]string{
	"pick an agent", "pick a model and effort", "pick any (optional)", "what should it do?",
	"add files or paste (optional)", "trust this folder?", "review, then launch",
}

// naCurrentLine is the current step's "› key  question" line; on the
// context step the total sits flush right.
func naCurrentLine(st *NewAgentState, w int) string {
	line := "  " + styleArrow.Render("›") + " " + selectedStyle(stylePrimary).Bold(true).Render(padRight(naStepNames[st.Step], naKeyW)) +
		styleDim.Render(naStepAsk[st.Step])
	if st.Step == naStepContext && len(st.Items) > 0 {
		total := styleDim.Render(naItemsTotal(st.Items))
		if gap := w - pageRightMargin - ansi.StringWidth(line) - ansi.StringWidth(total); gap >= 2 {
			line += strings.Repeat(" ", gap) + total
		}
	}
	return line
}

// naNotice is the open-time notice (a remembered agent that is gone) and,
// on the context step, a draft whose files are from another folder
// (naStaleRoot), as warning lines, or nil.
func naNotice(st *NewAgentState) []string {
	var out []string
	if st.Notice != "" {
		out = append(out, styleBootWarn.Render("! ")+styleDim.Render(protocol.FlattenDisplayText(st.Notice)))
	}
	if st.Step == naStepContext && naStaleRoot(st) {
		out = append(out, styleBootWarn.Render("! ")+styleDim.Render("files from "+
			protocol.FlattenDisplayText(tildePath(st.ItemsRoot))+", not this folder: x removes them"))
	}
	return out
}

// naUnder is what sits right under the current line: the notices, the
// launch's progress and the page's errors (the summary shows its errors
// under the launch row instead), every line indented to the body and at most
// limit of them: notices give way first, then the errors are cut short with
// …, so the launch's progress and the first error always show.
func naUnder(m Model, st *NewAgentState, w, limit int) []string {
	ind := naIndentFor(w)
	fw := max(w-ind-pageRightMargin, 1)
	notices := naNotice(st)
	var launching, errs []string
	if st.Launching {
		launching = []string{naStartingLine(m)}
	}
	if st.Step != naStepSummary || st.Screen != naSteps {
		errs = naErrLines(st, fw)
	}
	for len(notices)+len(launching)+len(errs) > limit && len(notices) > 0 {
		notices = notices[1:]
	}
	if room := limit - len(launching); len(errs) > room {
		room = max(room, 1)
		errs = errs[:room]
		errs[room-1] = ansi.Truncate(errs[room-1], max(fw-1, 1), "") + "…"
	}
	under := append(append(notices, launching...), errs...)
	return indentLines(under, ind)
}

// naTranscript is the finished steps as lines. A step the page is past
// shows; the ones not reached yet do not. The summary replaces the
// transcript with its own review, so it has none.
func naTranscript(m Model, st *NewAgentState) []string {
	if st.Step == naStepSummary && st.Screen == naSteps {
		return nil
	}
	var out []string
	for s := range st.Step {
		out = append(out, naDoneLine(m, st, s))
	}
	return out
}

// naTop is everything above the body: the transcript, the current line and
// what sits under it. On a screen too short for all of it the oldest
// transcript lines give way, so the current line always shows.
func naTop(m Model, st *NewAgentState, w, bh int) []string {
	need := 1 // body rows kept under the top: one, four for the trust step
	if st.Step == naStepTrust && st.Screen == naSteps {
		need = 4
	}
	limit := max(bh-2-need, 0) // the current line and a blank sit with the body
	if st.Step == naStepSummary && st.Screen == naSteps {
		limit = naSummaryUnderLimit(st, w, bh)
	}
	under := naUnder(m, st, w, limit)
	tr := naTranscript(m, st)
	if keep := max(bh-2-need-len(under), 0); len(tr) > keep {
		tr = tr[len(tr)-keep:]
	}
	top := make([]string, 0, len(tr)+1+len(under))
	top = append(top, tr...)
	top = append(top, naCurrentLine(st, w))
	top = append(top, under...)
	for i, l := range top {
		top[i] = ansi.Truncate(l, max(w-pageRightMargin, 1), "…")
	}
	return top
}

// naBodyRows is the rows the body has under the top lines and a blank, on
// the page as drawn at the model's size.
func naBodyRows(m Model, st *NewAgentState) int {
	w, h := helpSize(m)
	bh := pageBodyHeight(h)
	return max(bh-len(naTop(m, st, w, bh))-1, 1)
}

// naBodyLines is the page body for a w wide, bh tall area: the top, a blank
// and the step's body (or the picker's, or the preview's) in the rows
// left. The summary is its own layout.
func naBodyLines(m Model, st *NewAgentState, w, bh int) []string {
	if st.Step == naStepSummary && st.Screen == naSteps {
		return naSummaryLines(m, st, w, bh)
	}
	top := naTop(m, st, w, bh)
	rows := max(bh-len(top)-1, 1)
	ind := naIndentFor(w)
	lw := max(w-(ind-2)-pageRightMargin, 1) // a list's width: its cursor column starts 2 in
	fw := max(w-ind-pageRightMargin, 1)
	lines := make([]string, 0, bh)
	lines = append(lines, top...)
	lines = append(lines, "")
	switch {
	case st.Screen == naImportScreen:
		lines = append(lines, indentLines(naImportBody(m, st, lw, rows), ind-2)...)
	case st.Screen == naPreview && st.ItemIdx < len(st.Items):
		lines = append(lines, indentLines(naPreviewBody(m, st, lw, rows), ind-2)...)
	default:
		lines = append(lines, naStepBody(m, st, fw, lw, rows, ind)...)
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return lines
}

// naStepBody is the current step's body: fw wide for text and boxes, lw for
// lists (their cursor column sits ind-2 in), at most rows rows, every line
// indented.
func naStepBody(m Model, st *NewAgentState, fw, lw, rows, ind int) []string {
	list := func(lines []string) []string { return indentLines(lines, ind-2) }
	switch st.Step {
	case naStepAgent:
		return list(naAgentBody(m, st, lw, rows))
	case naStepModel:
		return list(naModelBody(m, st, lw, rows))
	case naStepSkills:
		return list(naSkillLines(st, rows))
	case naStepTask:
		return indentLines(naTaskBody(m, st, fw, rows), ind)
	case naStepContext:
		return list(naContextBody(m, st, lw, rows))
	case naStepTrust:
		return list(naTrustBody(m, st, lw, rows))
	case naStepSummary, naStepCount:
	}
	return nil
}

// naTaskBody is the task editor box fw wide, rows high with the @ list
// under it (at most what is left after a one-row box), as L1 says: the box
// takes every row the completion does not.
func naTaskBody(m Model, st *NewAgentState, fw, rows int) []string {
	comp := naCompleteLines(m, st, fw)
	if keep := max(rows-3, 0); len(comp) > keep { // two box edges and one text row
		comp = comp[:keep]
	}
	text := max(rows-2-len(comp), 1)
	return append(naEditorBox(m, st, fw, text), comp...)
}

// naEditorBox is the task editor in a box w wide with rows text rows: the
// placeholder while empty, the focus border and the cursor always (the task
// step types).
func naEditorBox(m Model, st *NewAgentState, w, rows int) []string {
	w = max(w, 5)
	inner := w - 4
	cur := cursorBlock(m)
	text := st.Task.View(inner, rows, true, cur)
	if st.Task.Empty() {
		hint := styleMuted.Render(ansi.Truncate(naPlaceholder, inner, "…"))
		if rows > 1 {
			text[1] = hint
		} else {
			text[0] = ansi.Truncate(cur+hint, inner, "…")
		}
	}
	border := styleBorderFocus
	out := []string{border.Render("┌" + strings.Repeat("─", w-2) + "┐")}
	for _, l := range text {
		out = append(out, border.Render("│ ")+padLine(l, inner)+border.Render(" │"))
	}
	return append(out, border.Render("└"+strings.Repeat("─", w-2)+"┘"))
}

// naContextBody is the context step's list lw wide in at most rows rows:
// the items (windowed on the cursor) and a "next ›" row last, or the empty
// state naming the three ways to add context.
func naContextBody(m Model, st *NewAgentState, lw, rows int) []string {
	if len(st.Items) == 0 {
		out := []string{}
		if st.Indexing {
			out = append(out, "  "+naLoadingRow(m, st, lw-2), "")
		}
		return append(out,
			"  "+styleMuted.Render("nothing added yet"),
			"  "+styleKey.Render("ctrl+o")+styleDim.Render("  import files and folders"),
			"  "+styleKey.Render("@")+styleDim.Render("       in the task: add a file"),
			"  "+styleKey.Render("paste")+styleDim.Render("   in the task: add text"),
		)
	}
	items := make([]string, 0, len(st.Items)+1)
	for i := range st.Items {
		items = append(items, naItemRow(m, st, i, lw, true))
	}
	next := naMarkCol(st.ItemIdx >= len(st.Items)) + stylePrimary.Render("next ›")
	return st.itemsView.window("items", append(items, next), st.ItemIdx, max(rows, 1), "  ")
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
		left = selectCursor()
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

// naToolsLoading reports whether the daemon's tool list is still on its way:
// no Snapshot yet. A Snapshot without one is an old daemon, and every tool
// counts as installed (pickerTools), so the agent shows.
func naToolsLoading(m Model) bool { return !m.toolsKnown && !m.toolsSnap }
