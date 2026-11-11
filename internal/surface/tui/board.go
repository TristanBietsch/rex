package tui

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// boardGroup defines one section of the kanban. All board grouping (rendering,
// navigation, jump-to-section) reads from boardGroups so the three views stay
// in sync. Adding a new state means updating exactly one place.
type boardGroup struct {
	// ID is the stable handle for the section ("needs", "working", "done").
	ID    string
	Title string
	// Count names the column's sessions after a number, as on the
	// screensaver ("2 working").
	Count string
	// Alert marks a column that wants the user: its title and count are
	// highlighted while it holds any session.
	Alert bool
	// Empty is the line an empty column shows.
	Empty string
	// Completed marks the column that holds finished work (it folds past
	// doneCollapseAt rows).
	Completed bool
	Match     func(protocol.SessionSummary) bool
}

// isMarkedDone reports whether the user marked the session done (it then
// lives in Done whatever its live state).
func isMarkedDone(s protocol.SessionSummary) bool { return !s.RetireAt.IsZero() }

// boardGroups is the canonical kanban layout. Done includes all terminal
// states (done/failed/crashed) plus every session the user marked done,
// whatever its live state; distinct markers (●/✕/○) preserve the at-a-glance
// distinction within the column. Working also holds queued sessions (spawned,
// agent not started yet; marker ◌): every state must land in some group, or
// the session is invisible and unreachable by keyboard.
var boardGroups = []boardGroup{
	{ID: "needs", Title: "Needs you", Count: "need you", Alert: true, Empty: "nothing waiting on you", Match: func(s protocol.SessionSummary) bool {
		return !isMarkedDone(s) && s.State == protocol.StateNeedsInput
	}},
	{ID: "working", Title: "Working", Count: "working", Empty: "nothing running", Match: func(s protocol.SessionSummary) bool {
		return !isMarkedDone(s) && (s.State == protocol.StateWorking || s.State == protocol.StateQueued)
	}},
	{ID: "done", Title: "Done", Count: "done", Empty: "nothing finished yet", Completed: true, Match: inCompleted},
}

// styleAlert highlights an Alert column's title and count while it holds
// any session. Built per call: the palette changes with color_scheme.
func styleAlert() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(colorNeeds)
}

// columnCounts counts every session (tool filter ignored) per board column,
// in boardGroups order, except failed and crashed sessions the user hasn't
// marked done: those are counted apart so a failure stands out instead of
// hiding in the done total. Each session is counted at most once, so the
// numbers add up to the stats overlay's total.
func columnCounts(sessions []protocol.SessionSummary) (cols []int, failed int) {
	cols = make([]int, len(boardGroups))
	for _, s := range sessions {
		if !isMarkedDone(s) && (s.State == protocol.StateFailed || s.State == protocol.StateCrashed) {
			failed++
			continue
		}
		for i, g := range boardGroups {
			if g.Match(s) {
				cols[i]++
				break
			}
		}
	}
	return cols, failed
}

// Board group indexes.
const (
	groupNeeds = iota
	groupWorking
	groupDone
)

// doneCollapseAt is how many Done rows show before the column folds into
// its title line (3 or :prune). Finished work stays out of the way.
const doneCollapseAt = 5

// doneCollapsed reports whether the Done column shows only its title line.
func doneCollapsed(m Model) bool {
	return !m.DoneExpanded && len(filterByGroup(m.Sessions, boardGroups[groupDone], rowFilterOf(m))) > doneCollapseAt
}

// visibleRows is group gi's rows as the board shows them: none while Done
// is collapsed or the section is hidden.
func visibleRows(m Model, gi int) []protocol.SessionSummary {
	if !rowFilterOf(m).showsSection(boardGroups[gi]) {
		return nil
	}
	if gi == groupDone && doneCollapsed(m) {
		return nil
	}
	return filterByGroup(m.Sessions, boardGroups[gi], rowFilterOf(m))
}

// resolveSection maps a user reference to a boardGroups index: an ID, a
// unique ID prefix, the title with spaces ignored, or a 1-based number.
func resolveSection(ref string) (int, error) {
	r := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(ref), " ", ""))
	if n, err := strconv.Atoi(r); err == nil {
		if n >= 1 && n <= len(boardGroups) {
			return n - 1, nil
		}
	} else if r != "" {
		prefix, hits := 0, 0
		for i, g := range boardGroups {
			if r == g.ID || r == strings.ToLower(strings.ReplaceAll(g.Title, " ", "")) {
				return i, nil
			}
			if strings.HasPrefix(g.ID, r) {
				prefix, hits = i, hits+1
			}
		}
		if hits == 1 {
			return prefix, nil
		}
	}
	ids := make([]string, len(boardGroups))
	for i, g := range boardGroups {
		ids[i] = g.ID
	}
	return 0, fmt.Errorf("unknown section %q: use %s or 1-%d", ref, strings.Join(ids, ", "), len(boardGroups))
}

// inCompleted reports whether a session sits in the Done column.
func inCompleted(s protocol.SessionSummary) bool {
	return isMarkedDone(s) || s.State == protocol.StateDone ||
		s.State == protocol.StateFailed || s.State == protocol.StateCrashed
}

// filterByGroup applies the group predicate plus the active tool filter.
func filterByGroup(sessions []protocol.SessionSummary, g boardGroup, f rowFilter) []protocol.SessionSummary {
	out := make([]protocol.SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		if !g.Match(s) || !f.keep(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// partitionSessions buckets sessions into board columns in one pass (O(n) vs 3× scan).
func partitionSessions(sessions []protocol.SessionSummary, f rowFilter) [][]protocol.SessionSummary {
	out := make([][]protocol.SessionSummary, len(boardGroups))
	for i := range out {
		out[i] = make([]protocol.SessionSummary, 0, len(sessions)/len(boardGroups)+1)
	}
	for _, s := range sessions {
		if !f.keep(s) {
			continue
		}
		for i, g := range boardGroups {
			if g.Match(s) {
				out[i] = append(out[i], s)
				break
			}
		}
	}
	return out
}

// fleetPalette is a small set of distinct lipgloss colors that work on both
// dark and light terminals. The color for a fleet name is selected by:
//
//	fnv32(name) % len(fleetPalette)
//
// This ensures the same name always maps to the same color.
var fleetPalette = []lipgloss.Color{
	"#5B8DEF", // blue
	"#E5B341", // amber
	"#4ADE80", // green
	"#EF4444", // red
	"#A78BFA", // violet
	"#22D3EE", // cyan
	"#FB923C", // orange
	"#F472B6", // pink
	"#84CC16", // lime
	"#14B8A6", // teal
}

// fleetColor returns a stable, deterministic color for the given fleet name.
func fleetColor(name string) lipgloss.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	idx := int(h.Sum32()) % len(fleetPalette)
	return fleetPalette[idx]
}

// spinnerFrameSets is the full catalog of spinner glyph sets. Selected at
// render time via the `spinner` setting.
var spinnerFrameSets = map[string][]string{
	"braille":    {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	"ascii_line": {"|", "/", "-", "\\"},
	"moon":       {"◐", "◓", "◑", "◒"},
	"pulse":      {"·", "•", "●", "•"},
	"blocks":     {"░", "▒", "▓", "█", "▓", "▒"},
}

// spinnerFramesFor returns the active spinner frame set, honoring reduce_motion.
func spinnerFramesFor(m Model) []string {
	if m.Store != nil {
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			return []string{"*"}
		}
		if id, _ := m.Store.Get("spinner").(string); id != "" {
			if frames, ok := spinnerFrameSets[id]; ok {
				return frames
			}
		}
	}
	return spinnerFrameSets["braille"]
}

// now is the render clock; tests pin it via Model.Clock.
func (m Model) now() time.Time {
	if m.Clock != nil {
		return m.Clock()
	}
	return time.Now()
}

func (m Model) hasWorkingSessions() bool {
	for _, s := range m.Sessions {
		if s.State == protocol.StateWorking {
			return true
		}
	}
	return false
}

// slotKind tags one line of the unscrolled board.
type slotKind int

const (
	slotGap slotKind = iota
	slotTitle
	slotNone
	slotRow
)

// boardSlot is one line of the unscrolled board. renderBoard, scrolling
// (selectedBoardLine) and mouse hit-testing all walk the same slots, so their
// line math can't drift apart (e.g. row_density gaps).
type boardSlot struct {
	kind  slotKind
	group int
	sess  protocol.SessionSummary
}

func boardSlots(m Model) []boardSlot {
	gapBetween := densityGap(m)
	collapsed := doneCollapsed(m)
	f := rowFilterOf(m)
	var slots []boardSlot
	for i := range boardGroups {
		if !f.showsSection(boardGroups[i]) {
			continue
		}
		if len(slots) > 0 { // a gap before every shown section but the first
			for j := 0; j < gapBetween; j++ {
				slots = append(slots, boardSlot{kind: slotGap, group: i})
			}
		}
		slots = append(slots, boardSlot{kind: slotTitle, group: i})
		if i == groupDone && collapsed {
			continue // the title line says how many are hidden
		}
		rows := visibleRows(m, i)
		if len(rows) == 0 {
			slots = append(slots, boardSlot{kind: slotNone, group: i})
			continue
		}
		for _, s := range rows {
			slots = append(slots, boardSlot{kind: slotRow, group: i, sess: s})
		}
	}
	return slots
}

// groupTitle renders a column title: NAME, its count, and for a long Done
// column how to show, hide or clear it. It is cut and padded to width: a
// wrapped title would push every row below it off the layout math.
func groupTitle(m Model, gi, width int) string {
	return padLine(groupTitleText(m, gi), width)
}

func groupTitleText(m Model, gi int) string {
	n := len(filterByGroup(m.Sessions, boardGroups[gi], rowFilterOf(m)))
	title := styleSectionTitle.Render(strings.ToUpper(boardGroups[gi].Title))
	if boardGroups[gi].Alert && n > 0 {
		title = styleAlert().Render(strings.ToUpper(boardGroups[gi].Title))
	}
	out := "  " + title + styleDim.Render(fmt.Sprintf("  %d", n))
	if gi != groupDone || n <= doneCollapseAt {
		return out
	}
	sep := styleMuted.Render("  ·  ")
	verb := " hide"
	if doneCollapsed(m) {
		out += styleDim.Render(" hidden")
		verb = " show"
	}
	return out + sep + styleHeaderApp.Render("3") + styleDim.Render(verb) + sep +
		styleHeaderApp.Render(":prune") + styleDim.Render(" clear")
}

// clampScroll bounds offset to [0, total-height] so the board never scrolls
// past its last line.
func clampScroll(offset, total, height int) int {
	if max := total - height; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// renderBoard renders the three sections sized to fit `width` x `height`.
// Long boards scroll: m.ScrollOffset skips that many lines from the top.
func renderBoard(m Model, width, height int) string {
	slots := boardSlots(m)
	off := clampScroll(m.ScrollOffset, len(slots), height)
	if height <= 0 {
		height = len(slots) - off
	}
	end := off + height
	if end > len(slots) {
		end = len(slots)
	}
	cols := layoutSlots(slots, m.now(), width, m.Detailed)
	// An empty column's text starts where row content does.
	noneIndent := strings.Repeat(" ", rowIndent+colMarker+colGap)
	lines := make([]string, 0, height)
	for _, sl := range slots[off:end] {
		switch sl.kind {
		case slotGap:
			lines = append(lines, "")
		case slotTitle:
			lines = append(lines, groupTitle(m, sl.group, width))
		case slotNone:
			lines = append(lines, noneIndent+styleMuted.Render(boardGroups[sl.group].Empty))
		case slotRow:
			lines = append(lines, renderRow(m, sl.sess, cols))
		}
	}

	for len(lines) < height {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// Row grid. Left to right:
//
//	indent marker  [id]  name  text  [model·effort  tokens]  subagents  run-badge  time
//
// The bracketed columns only show in the detailed view (v). indent, marker,
// id and time are fixed. name, model and tokens are sized to
// the widest value among the rendered rows (capped), the long-run
// badge column only exists while some row has such a badge, and the description takes
// whatever is left. Every row is exactly the board width.
const (
	colMarker = 1
	colID     = 4
	colSlug   = 22 // name column cap
	colModel  = 24 // model · effort column cap
	colTokens = 8  // "99.9M tk"
	colRun    = 4  // long-running badge: "▴42m"
	colAgents = 3  // running subagents: subagentGlyph + count, "⋔12"
	colTime   = 4  // "59m", "23h", "12d"
	colSpan   = 13 // start→end: "Jan 2→Jan 2" at most
	colGap    = 2
	rowIndent = 3 // " " + arrow/space + " "

	descMin       = 20 // desc width kept before info columns start shrinking
	descMinNarrow = 8  // desc floor once every info column is gone
	modelMin      = 12 // model column floor before it is dropped
	slugMin       = 12 // name column floor while desc is above descMinNarrow
)

// rowCols holds the per-frame column widths. 0 hides id, model, tokens,
// span, agents and run (no cell, no gap).
type rowCols struct {
	id, slug, desc, model, tokens, span, agents, run int
}

// used is the row width of everything except the description cell.
func (c rowCols) used() int {
	w := rowIndent + colMarker + colGap + c.slug + colGap + colGap + colTime
	for _, x := range []int{c.id, c.model, c.tokens, c.span, c.agents, c.run} {
		if x > 0 {
			w += colGap + x
		}
	}
	return w
}

// boardLayout sizes the columns for every row the board renders (all
// sections, not just the scrolled window, so scrolling never shifts columns).
func boardLayout(m Model, width int) rowCols {
	return layoutSlots(boardSlots(m), m.now(), width, m.Detailed)
}

func layoutSlots(slots []boardSlot, now time.Time, width int, detailed bool) rowCols {
	rows := make([]protocol.SessionSummary, 0, len(slots))
	for _, sl := range slots {
		if sl.kind == slotRow {
			rows = append(rows, sl.sess)
		}
	}
	return layoutRows(rows, now, width, detailed)
}

// layoutRows sizes columns to content, then degrades for narrow widths in a
// fixed order until the description gets descMin: drop the start→end span,
// drop tokens, shrink model
// (to modelMin), shrink name (to slugMin), drop model, drop subagents, drop
// long-run badge. Below that
// the description shrinks to descMinNarrow, then the name gives way.
func layoutRows(rows []protocol.SessionSummary, now time.Time, width int, detailed bool) rowCols {
	var c rowCols
	if detailed {
		c.id = colID
	}
	for _, s := range rows {
		c.slug = max(c.slug, ansi.StringWidth(displayName(s)))
		c.model = max(c.model, ansi.StringWidth(modelEffortLabel(s)))
		if s.Tokens > 0 {
			c.tokens = max(c.tokens, ansi.StringWidth(formatTokens(s.Tokens)))
		}
		if longRunTier(now, s) >= 0 {
			c.run = colRun
		}
		if s.Subagents > 0 {
			c.agents = colAgents
		}
		if detailed {
			c.span = max(c.span, ansi.StringWidth(spanText(now, s)))
		}
	}
	c.slug = min(c.slug, colSlug)
	c.model = min(c.model, colModel)
	c.tokens = min(c.tokens, colTokens)
	c.span = min(c.span, colSpan)
	if !detailed {
		c.model, c.tokens = 0, 0
	}

	// shrink narrows *x by what the description is short of descMin, but not
	// below floor (a column already under floor is left alone).
	shrink := func(x *int, floor int) {
		if short := descMin - (width - c.used()); short > 0 && *x > floor {
			*x = max(*x-short, floor)
		}
	}
	steps := []func(){
		func() { c.span = 0 }, // the least glanceable column goes first
		func() { c.tokens = 0 },
		func() { shrink(&c.model, modelMin) },
		func() { shrink(&c.slug, slugMin) },
		func() { c.model = 0 },
		func() { c.agents = 0 },
		func() { c.run = 0 },
	}
	for _, step := range steps {
		if width-c.used() >= descMin {
			break
		}
		step()
	}
	if short := descMinNarrow - (width - c.used()); short > 0 {
		c.slug = max(c.slug-short, 0)
	}
	c.desc = max(width-c.used(), 0)
	return c
}

// cell renders s truncated and padded to exactly w columns. It is the board
// row's choke point for session strings: truncate sanitizes s, so a cell is
// always one line of printable text.
func cell(style lipgloss.Style, s string, w int) string {
	return style.Width(w).Render(truncate(s, w))
}

func renderRow(m Model, s protocol.SessionSummary, c rowCols) string {
	selected := m.SelectedID == s.ID

	// Marker is the only indicator for the selected row — no bg fill,
	// avoids the chunky highlight artifacts.
	var markerPrefix string
	if selected {
		markerPrefix = styleArrow.Render("▸")
	} else if s.Fleet != "" {
		// Fleet indicator: tinted left border using the fleet's stable color.
		markerPrefix = lipgloss.NewStyle().Foreground(fleetColor(s.Fleet)).Render("▍")
	} else {
		markerPrefix = " "
	}

	now := m.now()
	retiring := !s.RetireAt.IsZero()
	dim := lipgloss.NewStyle().Foreground(colorFgDim)
	state := s.State
	if retiring {
		state = protocol.StateDone // marked done reads green whatever the live state
	}
	marker := stateMarkerCellFor(m, state, m.SpinnerTick, false)
	// A marked-done row recedes: muted name and description.
	slugStyle := lipgloss.NewStyle().Foreground(colorFgPrimary).Bold(true)
	if retiring && !selected {
		slugStyle = dim
	}
	slug := cell(slugStyle, displayName(s), c.slug)
	descColor := colorFgDim
	if selected {
		descColor = colorFgPrimary
	} else if retiring {
		descColor = colorFgMuted
	}
	descSource := rowText(s)
	if m.Detailed {
		descSource = rowDescription(s)
		if anim, ok := m.DescAnim[s.ID]; ok && anim.Active(now) {
			descSource = renderAnimFrame(anim, c.desc, now)
		}
	} else if !retiring && s.State == protocol.StateNeedsInput && !selected {
		descColor = colorFgPrimary // what needs you reads first
	}
	desc := cell(lipgloss.NewStyle().Foreground(descColor), descSource, c.desc)

	gap := strings.Repeat(" ", colGap)
	indent := " " + markerPrefix + " " // 3 chars total: marker arrow indent

	var b strings.Builder
	b.WriteString(indent + marker + gap)
	if c.id > 0 {
		b.WriteString(cell(lipgloss.NewStyle().Foreground(colorFgMuted), s.ShortID, c.id) + gap)
	}
	b.WriteString(slug + gap + desc)
	if c.model > 0 {
		b.WriteString(gap + cell(dim, modelEffortLabel(s), c.model))
	}
	if c.tokens > 0 {
		tk := ""
		if s.Tokens > 0 {
			tk = formatTokens(s.Tokens)
		}
		b.WriteString(gap + cell(dim.Align(lipgloss.Right), tk, c.tokens))
	}
	if c.span > 0 {
		b.WriteString(gap + cell(dim, spanText(now, s), c.span))
	}
	if c.agents > 0 {
		b.WriteString(gap + subagentCell(s, c.agents, retiring && !selected))
	}
	if c.run > 0 {
		b.WriteString(gap + longRunCell(now, s, c.run))
	}
	b.WriteString(gap + cell(dim.Align(lipgloss.Right), durationAgoAt(now, s.LastEventAt), colTime))
	row := b.String()

	if blinkEnabled(m) {
		if until, ok := m.BlinkUntil[s.ID]; ok && time.Now().Before(until) {
			if time.Now().UnixMilli()/200%2 == 0 {
				row = lipgloss.NewStyle().Foreground(colorDone).Render(row)
			}
		}
	}
	return row
}

// subagentGlyph marks the subagent count (a fork: work split in parallel).
const subagentGlyph = "⋔"

// subagentCell renders how many subagents the session is running, e.g.
// "⋔3", or blank. Counts above 99 show as 99.
func subagentCell(s protocol.SessionSummary, w int, dim bool) string {
	text := ""
	if s.Subagents > 0 {
		text = fmt.Sprintf("%s%d", subagentGlyph, min(s.Subagents, 99))
	}
	style := lipgloss.NewStyle().Foreground(colorWorking)
	if dim {
		style = lipgloss.NewStyle().Foreground(colorFgMuted)
	}
	return style.Width(w).Render(text)
}

// longRunCell renders the long-run badge column. The badge is rex-made and
// already styled by tier, so it skips cell()'s sanitizer, which would strip
// its color.
func longRunCell(now time.Time, s protocol.SessionSummary, w int) string {
	return lipgloss.NewStyle().Width(w).Render(ansi.Truncate(longRunBadge(now, s), w, "…"))
}

// densityGap returns the number of blank lines to insert between section
// groups based on the row_density setting.
func densityGap(m Model) int {
	if m.Store == nil {
		return 1
	}
	switch v, _ := m.Store.Get("row_density").(string); v {
	case "compact":
		return 0
	case "roomy":
		return 2
	default:
		return 1
	}
}

// blinkEnabled returns true when "done" rows should flash green. Off when the
// blinking_enabled setting is false or reduce_motion is on.
func blinkEnabled(m Model) bool {
	if m.Store == nil {
		return true
	}
	if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
		return false
	}
	b, ok := m.Store.Get("blinking_enabled").(bool)
	if !ok {
		return true
	}
	return b
}

// queuedGlyph marks a session that is spawned but whose agent hasn't
// started yet. It sits in the Working column, so it must read as "not
// running yet" next to the spinner without looking like crashed (○).
const queuedGlyph = "◌"

// stateMarkerCellFor renders the marker glyph for a state, sourcing the
// spinner from the live store on m.
func stateMarkerCellFor(m Model, st protocol.State, tick int, selected bool) string {
	style := lipgloss.NewStyle().Bold(true).Width(colMarker)
	if selected {
		style = style.Background(colorBgElev)
	}
	switch st {
	case protocol.StateWorking:
		frames := spinnerFramesFor(m)
		return style.Foreground(colorWorking).Render(frames[tick%len(frames)])
	case protocol.StateQueued:
		return style.Foreground(colorWorking).Render(queuedGlyph)
	case protocol.StateNeedsInput:
		return style.Foreground(colorNeeds).Render("◆")
	case protocol.StateDone:
		return style.Foreground(colorDone).Render("●")
	case protocol.StateFailed:
		return style.Foreground(colorFailed).Render("✕")
	case protocol.StateCrashed:
		return style.Foreground(colorCrashed).Render("○")
	default:
		return style.Render(" ")
	}
}

// modelLabel is "model · effort · tokens" for one-line contexts (attach
// header). The board splits tokens into their own column.
func modelLabel(s protocol.SessionSummary) string {
	base := modelEffortLabel(s)
	if s.Tokens > 0 {
		base = base + " · " + formatTokens(s.Tokens)
	}
	return base
}

// modelEffortLabel is the board's model column: "model · effort".
func modelEffortLabel(s protocol.SessionSummary) string {
	if s.Effort != "" {
		return s.ModelID + " · " + s.Effort
	}
	return s.ModelID
}

// formatTokens humanizes a token count; at most 8 columns ("99.9M tk").
// < 1000   → "<N> tk"
// < 99.95K → "<N.N>K tk"
// < 999.5K → "<N>K tk"
// < 99.95M → "<N.N>M tk"
// else     → "<N>M tk"
func formatTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d tk", n)
	case n < 99_950:
		return fmt.Sprintf("%.1fK tk", float64(n)/1e3)
	case n < 999_500:
		return fmt.Sprintf("%dK tk", (n+500)/1000)
	case n < 99_950_000:
		return fmt.Sprintf("%.1fM tk", float64(n)/1e6)
	default:
		return fmt.Sprintf("%dM tk", (n+500_000)/1_000_000)
	}
}

// truncate cuts s to at most n terminal columns (wide runes count double),
// ending in "…" when cut. Counting runes instead of columns let CJK / emoji
// text overflow its cell, and lipgloss then wrapped the row onto two lines.
// s is untrusted text, so it is sanitized first
// (protocol.FlattenDisplayText): a newline would wrap the row and an escape
// sequence would reach the terminal. Never pass lipgloss-styled text; its
// styling would be stripped.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = protocol.FlattenDisplayText(s)
	if ansi.StringWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

func durationAgo(t time.Time) string {
	return durationAgoAt(time.Now(), t)
}

// durationAgoAt renders now-t in one unit ("7s", "4m", "2h", "9d"), "-" when
// t is unset. A future t (clock skew) reads as "0s".
func durationAgoAt(now, t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := max(now.Sub(t), 0)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

// rowDescription is the desc column text: the AI summary once one exists,
// else the task the session was started with. Raw last_line is never shown —
// for TUI agents it is spinner frames and key hints, not activity. The
// result is sanitized (see protocol.FlattenDisplayText) so column sizing
// measures what renders.
func rowDescription(s protocol.SessionSummary) string {
	if s.Description != "" {
		return protocol.FlattenDisplayText(s.Description)
	}
	return protocol.FlattenDisplayText(s.Title)
}

// rowText is the simple view's text column: the task you gave the session.
// A session waiting on you shows its screen's last line instead when that
// line is a question; otherwise last_line is usually the agent's own status
// text ("Checking for updates"), which says nothing about what it needs.
func rowText(s protocol.SessionSummary) string {
	if s.State == protocol.StateNeedsInput && !isMarkedDone(s) {
		if q := strings.TrimSpace(protocol.FlattenDisplayText(s.LastLine)); strings.HasSuffix(q, "?") {
			return q
		}
	}
	if t := protocol.FlattenDisplayText(s.Title); t != "" {
		return t
	}
	return rowDescription(s)
}
