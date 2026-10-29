package tui

import (
	"fmt"
	"hash/fnv"
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
	Title string
	Match func(protocol.SessionSummary) bool
}

// isMarkedDone reports whether the user marked the session done (it then
// lives in Completed whatever its live state).
func isMarkedDone(s protocol.SessionSummary) bool { return !s.RetireAt.IsZero() }

// boardGroups is the canonical kanban layout. Completed includes all terminal
// states (done/failed/crashed) plus every session the user marked done,
// whatever its live state; distinct markers (●/✕/○) preserve the at-a-glance
// distinction within the column.
var boardGroups = []boardGroup{
	{"Needs input", func(s protocol.SessionSummary) bool {
		return !isMarkedDone(s) && s.State == protocol.StateNeedsInput
	}},
	{"Working", func(s protocol.SessionSummary) bool {
		return !isMarkedDone(s) && s.State == protocol.StateWorking
	}},
	{"Completed", func(s protocol.SessionSummary) bool {
		return isMarkedDone(s) || s.State == protocol.StateDone ||
			s.State == protocol.StateFailed || s.State == protocol.StateCrashed
	}},
}

// filterByGroup applies the group predicate plus the active tool filter.
func filterByGroup(sessions []protocol.SessionSummary, g boardGroup, filter string) []protocol.SessionSummary {
	out := make([]protocol.SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		if !g.Match(s) {
			continue
		}
		if filter != "all" && filter != "" && s.ToolID != filter {
			continue
		}
		out = append(out, s)
	}
	return out
}

// partitionSessions buckets sessions into board columns in one pass (O(n) vs 3× scan).
func partitionSessions(sessions []protocol.SessionSummary, filter string) [][]protocol.SessionSummary {
	out := make([][]protocol.SessionSummary, len(boardGroups))
	for i := range out {
		out[i] = make([]protocol.SessionSummary, 0, len(sessions)/len(boardGroups)+1)
	}
	for _, s := range sessions {
		if filter != "all" && filter != "" && s.ToolID != filter {
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
	partitions := partitionSessions(m.Sessions, m.Filter)
	var slots []boardSlot
	for i := range boardGroups {
		if i > 0 {
			for j := 0; j < gapBetween; j++ {
				slots = append(slots, boardSlot{kind: slotGap, group: i})
			}
		}
		slots = append(slots, boardSlot{kind: slotTitle, group: i})
		if len(partitions[i]) == 0 {
			slots = append(slots, boardSlot{kind: slotNone, group: i})
			continue
		}
		for _, s := range partitions[i] {
			slots = append(slots, boardSlot{kind: slotRow, group: i, sess: s})
		}
	}
	return slots
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
	cols := layoutSlots(slots, m.now(), width)
	// "(none)" starts where row content does: the ID column.
	noneIndent := strings.Repeat(" ", rowIndent+colMarker+colGap)
	lines := make([]string, 0, height)
	for _, sl := range slots[off:end] {
		switch sl.kind {
		case slotGap:
			lines = append(lines, "")
		case slotTitle:
			lines = append(lines, "  "+styleSectionTitle.Render(boardGroups[sl.group].Title))
		case slotNone:
			lines = append(lines, noneIndent+styleMuted.Render("(none)"))
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
//	indent marker  id  name  description  model·effort  tokens  run-badge  time
//
// indent, marker, id and time are fixed. name, model and tokens are sized to
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
	colTime   = 4  // "59m", "23h", "12d"
	colGap    = 2
	rowIndent = 3 // " " + arrow/space + " "

	descMin       = 20 // desc width kept before info columns start shrinking
	descMinNarrow = 8  // desc floor once every info column is gone
	modelMin      = 12 // model column floor before it is dropped
	slugMin       = 12 // name column floor while desc is above descMinNarrow
)

// rowCols holds the per-frame column widths. 0 hides model, tokens and run
// (no cell, no gap).
type rowCols struct {
	slug, desc, model, tokens, run int
}

// used is the row width of everything except the description cell.
func (c rowCols) used() int {
	w := rowIndent + colMarker + colGap + colID + colGap + c.slug + colGap + colGap + colTime
	for _, x := range []int{c.model, c.tokens, c.run} {
		if x > 0 {
			w += colGap + x
		}
	}
	return w
}

// layoutSlots sizes the columns for every row in slots: renderBoard passes
// all sections, not just the scrolled window, so scrolling never shifts
// columns.
func layoutSlots(slots []boardSlot, now time.Time, width int) rowCols {
	rows := make([]protocol.SessionSummary, 0, len(slots))
	for _, sl := range slots {
		if sl.kind == slotRow {
			rows = append(rows, sl.sess)
		}
	}
	return layoutRows(rows, now, width)
}

// layoutRows sizes columns to content, then degrades for narrow widths in a
// fixed order until the description gets descMin: drop tokens, shrink model
// (to modelMin), shrink name (to slugMin), drop model, drop long-run
// badge. Below that
// the description shrinks to descMinNarrow, then the name gives way.
func layoutRows(rows []protocol.SessionSummary, now time.Time, width int) rowCols {
	var c rowCols
	for _, s := range rows {
		c.slug = max(c.slug, ansi.StringWidth(displayName(s)))
		c.model = max(c.model, ansi.StringWidth(modelEffortLabel(s)))
		if s.Tokens > 0 {
			c.tokens = max(c.tokens, ansi.StringWidth(formatTokens(s.Tokens)))
		}
		if longRunTier(now, s) >= 0 {
			c.run = colRun
		}
	}
	c.slug = min(c.slug, colSlug)
	c.model = min(c.model, colModel)
	c.tokens = min(c.tokens, colTokens)

	// shrink narrows *x by what the description is short of descMin, but not
	// below floor (a column already under floor is left alone).
	shrink := func(x *int, floor int) {
		if short := descMin - (width - c.used()); short > 0 && *x > floor {
			*x = max(*x-short, floor)
		}
	}
	steps := []func(){
		func() { c.tokens = 0 },
		func() { shrink(&c.model, modelMin) },
		func() { shrink(&c.slug, slugMin) },
		func() { c.model = 0 },
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

// cell renders untrusted text s as one row, truncated and padded to exactly
// w columns.
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
	id := cell(lipgloss.NewStyle().Foreground(colorFgMuted), s.ShortID, colID)
	// A marked-done row recedes: muted name and description.
	slugStyle := lipgloss.NewStyle().Foreground(colorFgPrimary).Bold(true)
	if retiring && !selected {
		slugStyle = dim
	}
	if doneFlashOn(m, s.ID, now) {
		slugStyle = slugStyle.Foreground(colorDone)
	}
	slug := cell(slugStyle, displayName(s), c.slug)
	descColor := colorFgDim
	if selected {
		descColor = colorFgPrimary
	} else if retiring {
		descColor = colorFgMuted
	}
	descStyle := lipgloss.NewStyle().Foreground(descColor)
	var desc string
	if anim, ok := m.DescAnim[s.ID]; ok && anim.Active(now) {
		// Frames are sanitized and exactly c.desc wide already; cell would
		// collapse the wipe effect's space padding.
		desc = descStyle.Render(renderAnimFrame(anim, c.desc, now))
	} else {
		desc = cell(descStyle, rowDescription(s), c.desc)
	}

	gap := strings.Repeat(" ", colGap)
	indent := " " + markerPrefix + " " // 3 chars total: marker arrow indent

	var b strings.Builder
	b.WriteString(indent + marker + gap + id + gap + slug + gap + desc)
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
	if c.run > 0 {
		// The badge is rex's own styled text: cell would strip its escapes.
		b.WriteString(gap + padLine(longRunBadge(now, s), c.run))
	}
	b.WriteString(gap + cell(dim.Align(lipgloss.Right), durationAgoAt(now, s.LastEventAt), colTime))
	return b.String()
}

// doneFlashOn reports whether a just-finished session's name is in the lit
// phase of its done flash (200 ms on, 200 ms off until BlinkUntil). It used
// to wrap the whole styled row in a green foreground, which the cells' own
// colors overrode, so the flash never showed.
func doneFlashOn(m Model, id string, now time.Time) bool {
	until, ok := m.BlinkUntil[id]
	return ok && now.Before(until) && blinkEnabled(m) && now.UnixMilli()/200%2 == 0
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
	case protocol.StateNeedsInput:
		return style.Foreground(colorNeeds).Render("◆")
	case protocol.StateDone:
		return style.Foreground(colorDone).Render("●")
	case protocol.StateFailed:
		return style.Foreground(colorFailed).Render("✕")
	case protocol.StateCrashed:
		return style.Foreground(colorCrashed).Render("○")
	}
	return style.Render(" ")
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

// truncate makes untrusted text s one terminal row (protocol.SingleLine: a
// newline in a multi-line prompt spilled its board row over several screen
// lines; an escape could repaint the screen) and cuts it to at most n columns
// (wide runes count double), ending in "…" when cut. Counting runes instead
// of columns let CJK / emoji text overflow its cell, and lipgloss then
// wrapped the row onto two lines.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = protocol.SingleLine(s)
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
// for TUI agents it is spinner frames and key hints, not activity.
func rowDescription(s protocol.SessionSummary) string {
	if s.Description != "" {
		return s.Description
	}
	return s.Title
}
