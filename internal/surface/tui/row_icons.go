package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// iconSlot is one cell of a row's icon strip. The order of the constants is
// the layout order; row_icon_set can remove slots but never reorder them.
type iconSlot int

const (
	slotUnseen iconSlot = iota
	slotReason
	slotLoop
	slotSubagents
	slotSpend
	slotLongRun
)

var iconSlotIDs = [...]string{"unseen", "reason", "loop", "subagents", "spend", "longrun"}

// id is the slot's name in the row_icon_set setting.
func (s iconSlot) id() string { return iconSlotIDs[s] }

// slotWidth is each slot's fixed cell width, indexed by iconSlot.
var slotWidth = [...]int{1, 8, 8, colAgents, 6, colRun}

// longRunGlyph is the long-run badge glyph (the same as retire_view's badge).
const longRunGlyph = "▴"

// Unicode and ASCII forms of the strip glyphs, by name.
var iconGlyphs = map[string][2]string{
	"unseen":    {"✦", "*"},
	"asks":      {"?", "?"},
	"review":    {"✓", "+"},
	"loop":      {loopDefault, "@"},
	"subagents": {subagentGlyph, "Y"},
	"spend":     {"$", "$"},
	"longrun":   {longRunGlyph, "^"},
}

// rowIconsOn reports whether rows render the icon strip: row_icons on and
// not the mobile layout.
func rowIconsOn(m Model) bool {
	return settings.FeatureOn(m.Store, "row_icons") && !mobileLayout(m)
}

// iconSlots are the strip's slots in layout order: the row_icon_set ids
// (unknown ones ignored), minus the loop slot with loops off and the spend
// slot with usage off.
func iconSlots(m Model) []iconSlot {
	raw := "unseen,reason,loop,subagents,spend,longrun"
	if m.Store != nil {
		if v, ok := m.Store.Get("row_icon_set").(string); ok {
			raw = v
		}
	}
	want := map[string]bool{}
	for _, id := range strings.Split(raw, ",") {
		want[strings.ToLower(strings.TrimSpace(id))] = true
	}
	var out []iconSlot
	for s := slotUnseen; s <= slotLongRun; s++ {
		if !want[s.id()] {
			continue
		}
		if s == slotLoop && !settings.FeatureOn(m.Store, "loops") {
			continue
		}
		if s == slotSpend && !settings.FeatureOn(m.Store, "usage") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// iconGlyph is the strip glyph for name, in its ASCII form with ascii_glyphs
// on. An unknown name is "".
func iconGlyph(m Model, name string) string {
	g, ok := iconGlyphs[name]
	if !ok {
		return ""
	}
	if m.Store != nil {
		if on, _ := m.Store.Get("ascii_glyphs").(bool); on {
			return g[1]
		}
	}
	return g[0]
}

// seenOn reports whether the TUI tracks unseen rows: the row_icons feature.
// Off, nothing is stamped, written or saved.
func seenOn(m Model) bool { return m.featureOn("row_icons") }

// sectionOf is the id of the board column s sits in ("" if none), by the
// board's own grouping.
func sectionOf(m Model, s protocol.SessionSummary) string {
	ctx := groupCtx{now: m.now(), waiting: m.featureOn("waiting_section")}
	for _, g := range boardGroups {
		if g.Match(s, ctx) {
			return g.ID
		}
	}
	return ""
}

// finishedSection reports whether id is a column whose rows can be unseen.
func finishedSection(id string) bool { return id == "needs" || id == "done" }

// stampEntered records that s entered Ready or Done now, when its section
// changed from before. A no-op with row_icons off.
func stampEntered(m *Model, before string, s protocol.SessionSummary) {
	if !seenOn(*m) || isMarkedDone(s) {
		return
	}
	if after := sectionOf(*m, s); after != before && finishedSection(after) {
		if m.EnteredAt == nil {
			m.EnteredAt = map[string]time.Time{}
		}
		m.EnteredAt[s.ID] = m.now()
	}
}

// markSeen records that you looked at session id now (attach, cc).
func markSeen(m *Model, id string) {
	if !seenOn(*m) {
		return
	}
	if m.SeenAt == nil {
		m.SeenAt = map[string]time.Time{}
	}
	m.SeenAt[id] = m.now()
}

// adoptSeen runs on a snapshot, given each session's section before it
// (nil on the first). The first snapshot marks every session without a
// saved seen time as seen now, so a restart never floods the board. Later
// snapshots (a reconnect) stamp rows that moved into Ready or Done.
func adoptSeen(m *Model, before map[string]string) {
	if !seenOn(*m) {
		return
	}
	if !m.seenInit {
		m.seenInit = true
		for _, s := range m.Sessions {
			if _, ok := m.SeenAt[s.ID]; !ok {
				markSeen(m, s.ID)
			}
		}
		return
	}
	for _, s := range m.Sessions {
		stampEntered(m, before[s.ID], s)
	}
}

// adoptSessionsSeen installs a snapshot's sessions and runs adoptSeen on
// them, recording each session's section before the swap once seenInit is
// set. Boot, reconnect and the Snapshot event all go through it.
func adoptSessionsSeen(m Model, sessions []protocol.SessionSummary) Model {
	var before map[string]string
	if seenOn(m) && m.seenInit {
		before = make(map[string]string, len(m.Sessions))
		for _, s := range m.Sessions {
			before[s.ID] = sectionOf(m, s)
		}
	}
	m = adoptSessions(m, sessions)
	adoptSeen(&m, before)
	return m
}

// pruneSeen is SeenAt limited to the live sessions, for saving. Nil with row
// icons off or nothing to save.
func pruneSeen(m Model) map[string]time.Time {
	if !seenOn(m) || len(m.SeenAt) == 0 {
		return nil
	}
	out := map[string]time.Time{}
	for _, s := range m.Sessions {
		if t, ok := m.SeenAt[s.ID]; ok {
			out[s.ID] = t
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isUnseen reports whether the row shows the unseen mark: a Ready or Done row
// (not marked done) that entered that section after you last looked at it.
func isUnseen(m Model, s protocol.SessionSummary) bool {
	if !seenOn(m) || isMarkedDone(s) || !finishedSection(sectionOf(m, s)) {
		return false
	}
	entered, ok := m.EnteredAt[s.ID]
	return ok && entered.After(m.SeenAt[s.ID])
}

func asciiGlyphs(m Model) bool {
	if m.Store == nil {
		return false
	}
	on, _ := m.Store.Get("ascii_glyphs").(bool)
	return on
}

// reasonText is the why slot's text and style: the reason a Ready row needs
// you, or the tool a Waiting row waits on. Blank for everything else.
func reasonText(m Model, s protocol.SessionSummary) (string, lipgloss.Style) {
	if isMarkedDone(s) {
		return "", styleDim
	}
	if s.State == protocol.StateNeedsInput {
		switch s.WaitReason {
		case protocol.WaitInput:
			return iconGlyph(m, "asks") + " asks", styleNeeds
		case protocol.WaitReview:
			return iconGlyph(m, "review") + " review", styleDone
		case "":
			if q := strings.TrimSpace(protocol.FlattenDisplayText(s.LastLine)); strings.HasSuffix(q, "?") {
				return iconGlyph(m, "asks") + " asks", styleNeeds
			}
		}
		return "", styleDim
	}
	if kind := waitingKindOf(m, s); kind != "" && isWaiting(s, m.now()) {
		label := map[string]string{
			protocol.WaitShell: "sh", protocol.WaitWeb: "web", protocol.WaitSubagent: "sub",
		}[kind]
		if label == "" {
			label = kind
		}
		return label + " " + durationAgoAt(m.now(), s.WaitingSince), styleWorking
	}
	return "", styleDim
}

// spendText formats a session's spend in at most 5 cells: $0.42, $12.3,
// $123, $1.2k (capped at $999k).
func spendText(usd float64) string {
	usd = max(usd, 0)
	if v := math.Round(usd*100) / 100; v < 10 {
		return fmt.Sprintf("$%.2f", v)
	}
	if v := math.Round(usd*10) / 10; v < 100 {
		return fmt.Sprintf("$%.1f", v)
	}
	if v := math.Round(usd); v < 1000 {
		return fmt.Sprintf("$%.0f", v)
	}
	if usd >= 999500 {
		return "$999k"
	}
	return fmt.Sprintf("$%.1fk", math.Round(usd/100)/10)
}

// spendStyle is dim, then warmer at $5 and $20.
func spendStyle(usd float64) lipgloss.Style {
	switch {
	case usd >= 20:
		return styleLongRun[0]
	case usd >= 5:
		return styleLongRun[1]
	}
	return styleDim
}

// loopSlotText is the loop slot's text: "⟳ 3/8" while running, else the
// loop's state word. "" for a non-loop session.
func loopSlotText(m Model, s protocol.SessionSummary) string {
	if s.LoopID == "" {
		return ""
	}
	g := iconGlyph(m, "loop")
	l, ok := loopByID(m, s.LoopID)
	if !ok {
		return g + " " + loopIter(s.LoopIter, 0)
	}
	if l.State == protocol.LoopRunning {
		return g + " " + loopIter(l.Iteration, l.MaxIterations)
	}
	word := string(l.State)
	if l.State == protocol.LoopStopped {
		word = "stop"
	}
	return g + " " + word
}

// slotCell renders one strip cell, exactly w cells wide. Styled.
func slotCell(m Model, s protocol.SessionSummary, slot iconSlot, w int, selected bool) string {
	if w <= 0 {
		return ""
	}
	text, style := "", styleDim
	align := lipgloss.Left
	switch slot {
	case slotUnseen:
		if isUnseen(m, s) {
			text, style = iconGlyph(m, "unseen"), styleAccent
		}
	case slotReason:
		text, style = reasonText(m, s)
		if w < 3 && text != "" {
			// Glyph only; a Waiting row's marker already says why.
			if s.State == protocol.StateNeedsInput {
				text = string([]rune(text)[0])
			} else {
				text = ""
			}
		}
	case slotLoop:
		if text = loopSlotText(m, s); text != "" {
			style = styleWorking
			if l, ok := loopByID(m, s.LoopID); ok {
				style = loopStyle(l)
			}
		}
	case slotSubagents:
		c := subagentCell(s, w, isMarkedDone(s) && !selected) // as renderRow: muted when marked done
		if asciiGlyphs(m) {
			c = strings.Replace(c, subagentGlyph, iconGlyph(m, "subagents"), 1)
		}
		return c
	case slotSpend:
		if s.CostUSD != nil {
			text, style = spendText(*s.CostUSD), spendStyle(*s.CostUSD)
		}
		align = lipgloss.Right
	case slotLongRun:
		c := longRunCell(m.now(), s, w)
		if asciiGlyphs(m) {
			c = strings.Replace(c, longRunGlyph, iconGlyph(m, "longrun"), 1)
		}
		return c
	}
	if selected {
		style = selectedStyle(style)
	}
	return style.Width(w).Align(align).Render(ansi.Truncate(text, w, "…"))
}
