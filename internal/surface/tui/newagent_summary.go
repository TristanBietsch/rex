package tui

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The summary step (R21): the last step, a review of everything the launch
// sends. Each step has a row (j/k move, enter or e jumps to the step, and
// enter there comes back here); enter on the launch row launches. Context
// rows show under the context line when they fit; the equivalent `rex new`
// line is display only.

// naSumSubMax is the most context sub-rows the summary lists.
const naSumSubMax = 6

// naPromptSize is the assembled prompt's size, or why it cannot be built.
// Assembling runs only when the page changed since the last time (Rev).
func naPromptSize(st *NewAgentState) (int, error) {
	if !st.sumOK || st.sumRev != st.Rev {
		prompt, err := spawnctx.Assemble(strings.TrimSpace(st.Task.String()), st.Items, spawnctx.DefaultCaps)
		st.sumRev, st.sumOK, st.sumBytes, st.sumErr = st.Rev, true, len(prompt), err
	}
	return st.sumBytes, st.sumErr
}

// naPromptCell is the summary's prompt row: the size against the agent's
// cap, in the warning style (and "! prompt") when over it, or the failure.
func naPromptCell(st *NewAgentState) (label, value string) {
	size, err := naPromptSize(st)
	if err != nil {
		return styleStateFailed.Render("✗ prompt"), styleStateFailed.Render(protocol.FlattenDisplayText(err.Error()))
	}
	limit := naPromptCap(st)
	if size > limit {
		return styleBootWarn.Render("! prompt"), styleBootWarn.Render(naBytes(size)) + styleDim.Render(" of "+naBytes(limit))
	}
	return styleDim.Render("prompt"), stylePrimary.Render(naBytes(size)) + styleDim.Render(" of "+naBytes(limit))
}

// naShellSafe matches a word a shell takes as it is.
var naShellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./][A-Za-z0-9_@%+=:,./-]*$`)

// naShellQuote is s as one shell word: left alone when it is plain (and does
// not start with a dash, which a command would read as a flag), else in
// single quotes.
func naShellQuote(s string) string {
	if naShellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// naRexNewLine is the `rex new` call that stands for the page started in
// cwd, for w cells: the flags rex new has (-tool -model -effort -skill
// -trust -cwd) and the task's first line, cut with … to fit. An empty tool or
// model id leaves its flag out. Context and an "always" trust have no flag: a
// comment, kept whole, says what is left out. Display only, never run.
func naRexNewLine(st *NewAgentState, cwd string, w int) string {
	sel := st.selection()
	var parts []string
	add := func(flag, val string) {
		if val != "" {
			parts = append(parts, flag, naShellQuote(protocol.FlattenDisplayText(val)))
		}
	}
	parts = append(parts, "$ rex new")
	add("-tool", sel.selectedTool().ID)
	add("-model", sel.selectedModel().ID)
	add("-effort", sel.currentEffort())
	for _, s := range sel.chosenSkills() {
		add("-skill", s)
	}
	if st.Trust == trustSession {
		parts = append(parts, "-trust")
	}
	add("-cwd", naShowDir(cwd))
	var extra []string
	if len(st.Items) > 0 {
		extra = append(extra, "context")
	}
	if st.Trust == trustAlways {
		extra = append(extra, "always trust")
	}
	tail := ""
	if len(extra) > 0 {
		tail = "  # + " + strings.Join(extra, " + ")
	}
	head := strings.Join(parts, " ")
	room := max(w-ansi.StringWidth(tail), 1) // the comment is reserved first
	if title := taskTitle(st.Task.String()); title != "" {
		title = protocol.FlattenDisplayText(title)
		q := naShellQuote(title)
		for ansi.StringWidth(head)+1+ansi.StringWidth(q) > room && title != "" {
			r := []rune(strings.TrimSuffix(title, "…"))
			if len(r) == 0 {
				break
			}
			title = string(r[:len(r)-1]) + "…"
			q = naShellQuote(title)
		}
		head += " " + q
	}
	return ansi.Truncate(head, room, "…") + tail
}

// naSumRow is one summary row's lines: the cursor column, the key cell
// (already styled and padded) and the value; extra lines (a task's line
// count) go under the value.
func naSumRow(mark, keyCell, value string, more ...string) []string {
	out := []string{mark + keyCell + value}
	for _, l := range more {
		out = append(out, strings.Repeat(" ", 2+naKeyW)+l)
	}
	return out
}

// naSumKey is a step row's key cell: dim, or bold under the cursor.
func naSumKey(key string, cur bool) string {
	if cur {
		return selectedStyle(stylePrimary).Bold(true).Render(padRight(key, naKeyW))
	}
	return styleDim.Render(padRight(key, naKeyW))
}

// naSumTaskCell is the task row's value (its first line, cut to room) and
// the line count under it.
func naSumTaskCell(st *NewAgentState, room int) (string, []string) {
	task := strings.TrimSpace(st.Task.String())
	if task == "" {
		return styleDim.Render("empty"), nil
	}
	first := ansi.Truncate(protocol.FlattenDisplayText(taskTitle(task)), max(room, 1), "…")
	var more []string
	if n := strings.Count(task, "\n"); n > 0 {
		more = []string{styleDim.Render("(+" + naLines(n) + ")")}
	}
	return stylePrimary.Render(first), more
}

// naSumValue is a step row's value for the summary.
func naSumValue(m Model, st *NewAgentState, s naStep, room int) (string, []string) {
	switch s {
	case naStepAgent:
		return naAgentValue(m, st), nil
	case naStepModel:
		return naModelValue(st), nil
	case naStepSkills:
		return naSkillsValue(st), nil
	case naStepTask:
		return naSumTaskCell(st, room)
	case naStepContext:
		return naContextValue(st), nil
	case naStepTrust:
		return stylePrimary.Render(naTrustSummary(m, st)), naTrustFolder(m, st, room)
	case naStepSummary, naStepCount:
	}
	return "", nil
}

// naSummaryRows is the review's lines for w cells (every line starts at
// the cursor column): a row for each step (the ones that do not apply
// dimmed and out of the cursor's way), the context items under the context
// row when sub is set, then the prompt size and the slug; and the line
// index of the row under the cursor.
func naSummaryRows(m Model, st *NewAgentState, w int, sub bool) (lines []string, curLine int) {
	targets := naSumTargets(st)
	room := max(w-2-naKeyW, 8)
	rowOf := map[naStep]int{}
	for i, s := range targets {
		rowOf[s] = i
	}
	for s := range naStepSummary {
		i, ok := rowOf[s]
		if !ok {
			lines = append(lines, naSumRow(naMarkCol(false), naSumKey(naStepNames[s], false), styleDim.Render("not available"))...)
			continue
		}
		cur := st.SumRow == i
		if cur {
			curLine = len(lines)
		}
		val, more := naSumValue(m, st, s, room)
		lines = append(lines, naSumRow(naMarkCol(cur), naSumKey(naStepNames[s], cur), val, more...)...)
		if s == naStepContext && sub {
			pad := strings.Repeat(" ", 2+naKeyW)
			for j := range st.Items[:min(len(st.Items), naSumSubMax)] {
				lines = append(lines, pad+naItemRow(m, st, j, room, false))
			}
			if n := len(st.Items) - naSumSubMax; n > 0 {
				lines = append(lines, pad+"  "+styleDim.Render("… +"+strconv.Itoa(n)+" more"))
			}
		}
	}
	label, value := naPromptCell(st)
	lines = append(lines, naSumRow("  ", padRightStyled(label, naKeyW), value)...)
	w0 := st.selection()
	slug := deriveAgentSlug(w0.selectedTool().ID, w0.selectedModel().ID, taskTitle(st.Task.String()), existingSlugs(m))
	lines = append(lines, naSumRow("  ", styleDim.Render(padRight("slug", naKeyW)), styleSlug.Render(protocol.FlattenDisplayText(slug)))...)
	return lines, curLine
}

// padRightStyled pads an already styled s with spaces to n cells.
func padRightStyled(s string, n int) string {
	return s + strings.Repeat(" ", max(0, n-ansi.StringWidth(s)))
}

// naSummaryErrs is the page's errors under the launch row, at most what a
// bh tall body can spare (the last line ends in … when more were cut), and
// never fewer than one.
func naSummaryErrs(st *NewAgentState, w, bh int) []string {
	errs := naErrLines(st, max(w-naIndentFor(w)-pageRightMargin, 1))
	if keep := max(bh-5, 1); len(errs) > keep {
		errs = errs[:keep]
		errs[keep-1] = ansi.Truncate(errs[keep-1], max(w-naIndentFor(w)-pageRightMargin-1, 1), "") + "…"
	}
	return errs
}

// naSummaryUnderLimit is how many notice and progress lines may sit under
// the summary's current line: what the errors and the launch row leave.
func naSummaryUnderLimit(st *NewAgentState, w, bh int) int {
	return max(bh-5-len(naSummaryErrs(st, w, bh)), 0)
}

// naSummaryLines is the summary's page body for a w wide, bh tall area: the
// current line, the review (windowed on the cursor, collapsed to fit), the
// `rex new` line when there is room, and the launch row and the page's
// errors pinned under it.
func naSummaryLines(m Model, st *NewAgentState, w, bh int) []string {
	ind := naIndentFor(w)
	lw := max(w-(ind-2)-pageRightMargin, 1)
	top := naTop(m, st, w, bh)
	onLaunch := st.SumRow >= len(naSumTargets(st))
	launch := naMarkCol(onLaunch) + styleDim.Render("launch")
	if onLaunch {
		launch = naMarkCol(true) + selectedStyle(stylePrimary).Bold(true).Render("launch")
	}
	pinned := append([]string{"", launch}, indentLines(naSummaryErrs(st, w, bh), 2)...)
	area := max(bh-len(top)-1-len(pinned), 0)

	rows, cur := naSummaryRows(m, st, lw, !pageNarrow(w))
	cli := []string{"", "  " + styleDim.Render(naRexNewLine(st, spawnCWD(m), max(lw-2, 8)))}
	if len(rows)+len(cli) <= area {
		rows = append(rows, cli...)
	} else if len(rows) > area {
		rows, cur = naSummaryRows(m, st, lw, false) // collapse the context items
	}
	if onLaunch {
		cur = len(rows) - 1
	}
	if area > 0 {
		rows = st.sumView.window("sum", rows, cur, area, "  ")
	} else {
		rows = nil
	}
	lines := make([]string, 0, len(top)+1+len(rows)+len(pinned))
	lines = append(lines, top...)
	lines = append(lines, "")
	lines = append(lines, indentLines(append(rows, pinned...), ind-2)...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return lines
}
