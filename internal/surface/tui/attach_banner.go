package tui

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The attach banner is one row of rex chrome between the popup's title bar
// and the rule above the agent's screen: what the session is doing, where,
// and for how long. It sits above the rule so the agent cannot draw into it,
// and its row never forwards mouse input.

// Banner modes (the attach_banner setting).
const (
	bannerMarquee = "marquee"
	bannerStatic  = "static"
	bannerOff     = "off"
)

const (
	// bannerMinAgentRows is how many rows the agent must keep for the banner
	// to show; on shorter terminals the banner gives way.
	bannerMinAgentRows = 12
	// bannerMetaMinCols is the narrowest popup that still shows the meta.
	bannerMetaMinCols = 60
	// bannerStaleAfter is the description age past which "updated" is muted.
	bannerStaleAfter = 2 * time.Minute
	bannerLead       = "↳ "

	// bannerPause is how long a marquee rests at its start before it
	// scrolls; bannerStep is the time per cell of scroll (also the tick).
	bannerPause = 2 * time.Second
	bannerStep  = 120 * time.Millisecond
	// bannerGap separates the end of a scrolling text from its next copy.
	bannerGap = "   ·   "
	// bannerSep joins the pieces of the one-line marquee text.
	bannerSep = "  ·  "
)

// bannerItem is one piece of the banner's meta: where the session is, how
// long it has been in its state, how fresh its description is.
type bannerItem struct {
	text  string
	muted bool
}

// bannerModeOf reads attach_banner; unset or unknown means the default.
func bannerModeOf(m Model) string {
	if m.Store != nil {
		if v, _ := m.Store.Get("attach_banner").(string); v == bannerStatic || v == bannerOff || v == bannerMarquee {
			return v
		}
	}
	return bannerMarquee
}

// attachBannerRows is the banner rows (0 or 1) the popup shows now. It is the
// only input to the agent's size besides the terminal: openAttach and
// resizeAttach both call it.
func attachBannerRows(m Model) int {
	return attachBannerRowsFor(m, attachChrome{sheet: mobileLayout(m)})
}

// attachBannerRowsFor is attachBannerRows for a frame: the banner needs room
// for the agent's minimum rows beside it.
func attachBannerRowsFor(m Model, c attachChrome) int {
	if bannerModeOf(m) == bannerOff || m.bannerHidden {
		return 0
	}
	if _, rows := c.withBanner(0).innerSize(m.Width, m.Height); rows < bannerMinAgentRows+1 {
		return 0
	}
	return 1
}

// shortCWD abbreviates a working directory for one row: "~" for the home
// directory, otherwise the last two path elements ("~/x" when home-relative
// and shallow). The result is sanitized.
func shortCWD(p string) string {
	p = protocol.FlattenDisplayText(p)
	if p == "" {
		return ""
	}
	tp := tildePath(p)
	if tp == "~" {
		return tp
	}
	elems := strings.Split(strings.Trim(tp, string(filepath.Separator)), string(filepath.Separator))
	if len(elems) <= 2 {
		return tp
	}
	return strings.Join(elems[len(elems)-2:], "/")
}

// bannerParts splits the banner into its lead (the description, else the
// task, else the question the session waits on) and its meta. ended says the
// popup's agent has exited even if the board has not caught up. Every piece
// is flattened: the description is a model paraphrase of agent output, the
// question is raw screen text, title and cwd are user or agent supplied.
func bannerParts(m Model, sum protocol.SessionSummary, ended bool) (lead string, meta []bannerItem) {
	switch {
	case sum.Description != "":
		lead = protocol.FlattenDisplayText(sum.Description)
	case sum.Title != "":
		lead = "task: " + protocol.FlattenDisplayText(sum.Title)
	default:
		// Not rowQuestion: the banner shows the question whatever the
		// state, and only after the summary and the task.
		lead = lastLineQuestion(sum)
	}
	lead = strings.TrimSpace(lead)

	now := m.now()
	if cwd := shortCWD(sum.CWD); cwd != "" {
		meta = append(meta, bannerItem{text: cwd})
	}
	switch {
	case ended || sum.State == protocol.StateDone || sum.State == protocol.StateFailed || sum.State == protocol.StateCrashed:
		at := sum.EndedAt
		if at.IsZero() {
			at = sum.LastEventAt
		}
		if !at.IsZero() {
			meta = append(meta, bannerItem{text: "ended " + at.Local().Format("15:04")})
		}
	case sum.State == protocol.StateWorking && !sum.WorkingSince.IsZero():
		meta = append(meta, bannerItem{text: "working " + durationAgoAt(now, sum.WorkingSince)})
	case sum.State == protocol.StateNeedsInput && !sum.LastEventAt.IsZero():
		meta = append(meta, bannerItem{text: "waiting " + durationAgoAt(now, sum.LastEventAt)})
	}
	if sum.Description != "" && !sum.DescriptionAt.IsZero() {
		meta = append(meta, bannerItem{
			text:  fmt.Sprintf("updated %s ago", durationAgoAt(now, sum.DescriptionAt)),
			muted: now.Sub(sum.DescriptionAt) > bannerStaleAfter,
		})
	}
	return lead, append(meta, bannerElsewhere(m, sum.ID)...)
}

// bannerElsewhere is what the other sessions need, when the overview is on:
// counts from the same buckets as the header's overview line, without the
// attached session, then the daemon's headline when it has one. The headline
// is untrusted model text: flattened like every other piece.
func bannerElsewhere(m Model, self string) []bannerItem {
	if !overviewOn(m) {
		return nil
	}
	var others []protocol.SessionSummary
	for _, s := range scopedSessions(m) {
		if s.ID != self {
			others = append(others, s)
		}
	}
	var items []bannerItem
	if len(others) > 0 {
		g := overview.Group(others)
		var parts []string
		if n := len(g[overview.Review]); n > 0 {
			parts = append(parts, fmt.Sprintf("%d to review", n))
		}
		if n := len(g[overview.NeedsYou]); n > 0 {
			parts = append(parts, fmt.Sprintf("%d ready", n))
		}
		if n := len(g[overview.Failed]); n > 0 {
			parts = append(parts, fmt.Sprintf("%d failed", n))
		}
		if len(parts) == 0 {
			parts = []string{"quiet"}
		}
		items = append(items, bannerItem{text: "elsewhere: " + strings.Join(parts, " · ")})
	}
	if o := m.Overview; o != nil && o.Enabled {
		if h := strings.TrimSpace(protocol.FlattenDisplayText(overviewHeadline(m))); h != "" {
			items = append(items, bannerItem{text: "» " + h})
		}
	}
	return items
}

// bannerMotion reports whether a long banner scrolls: marquee mode and no
// reduce_motion. Otherwise it is cut at a word boundary like static.
func bannerMotion(m Model) bool {
	if bannerModeOf(m) != bannerMarquee {
		return false
	}
	if m.Store != nil {
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			return false
		}
	}
	return true
}

// marqueeText is the banner as one plain string for scrolling (without the
// fixed arrow): the lead, the task when the lead is the description, then the
// meta. Plain on purpose: one style for the whole window, no span bookkeeping.
func marqueeText(sum protocol.SessionSummary, lead string, meta []bannerItem) string {
	var parts []string
	if lead != "" {
		parts = append(parts, lead)
		if sum.Description != "" {
			if t := strings.TrimSpace(protocol.FlattenDisplayText(sum.Title)); t != "" {
				parts = append(parts, "task: "+t)
			}
		}
	}
	for _, it := range meta {
		parts = append(parts, it.text)
	}
	return strings.Join(parts, bannerSep)
}

// bannerOverflows reports whether the attached session's banner is too long
// for its row, so it scrolls.
func bannerOverflows(m Model) bool {
	a := m.Attach
	sum, _ := findSession(m.Sessions, a.SessionID)
	lead, meta := bannerParts(m, sum, a.Ended)
	return ansi.StringWidth(marqueeText(sum, lead, meta)) > a.Cols-ansi.StringWidth(bannerLead)
}

// marqueeWindow returns the width-cell slice of text visible elapsed after
// the loop (re)started: still for bannerPause, then one cell per bannerStep
// over text + gap + text, then the cycle repeats, resting at the start again.
// text must be plain (sanitized). The result is always exactly width cells;
// a wide rune cut at either edge becomes a space.
func marqueeWindow(text string, width int, elapsed time.Duration) string {
	if width <= 0 {
		return ""
	}
	period := ansi.StringWidth(text) + ansi.StringWidth(bannerGap)
	offset := 0
	if e := elapsed % (bannerPause + time.Duration(period)*bannerStep); e >= bannerPause {
		offset = int((e - bannerPause) / bannerStep)
	}
	var b strings.Builder
	col, end := 0, offset+width
	for _, r := range text + bannerGap + text {
		if col >= end {
			break
		}
		w := ansi.StringWidth(string(r))
		switch {
		case col+w <= offset: // before the window
		case col < offset || col+w > end: // cut by an edge
			b.WriteString(strings.Repeat(" ", min(col+w, end)-max(col, offset)))
		default:
			b.WriteRune(r)
		}
		col += w
	}
	if w := ansi.StringWidth(b.String()); w < width {
		b.WriteString(strings.Repeat(" ", width-w))
	}
	return b.String()
}

// renderAttachBanner renders the banner row, exactly cols cells wide. A
// banner that fits, or that may not move, is laid out statically.
func renderAttachBanner(m Model, a *AttachState, cols int) string {
	sum, _ := findSession(m.Sessions, a.SessionID)
	lead, meta := bannerParts(m, sum, a.Ended)
	if bannerMotion(m) {
		text := marqueeText(sum, lead, meta)
		if avail := cols - ansi.StringWidth(bannerLead); ansi.StringWidth(text) > avail {
			var elapsed time.Duration
			if !a.BannerSince.IsZero() {
				elapsed = max(m.now().Sub(a.BannerSince), 0)
			}
			arrow := styleArrow.Render(bannerLead)
			return padLine(arrow+styleDim.Render(marqueeWindow(text, avail, elapsed)), cols)
		}
	}
	return padLine(renderBannerStatic(m, a, lead, meta, cols), cols)
}

// bannerTickMsg drives the marquee. Gen tags the chain like SpinnerTickMsg.
type bannerTickMsg struct{ Gen int }

func tickBanner(gen int) tea.Cmd {
	return tea.Tick(bannerStep, func(time.Time) tea.Msg { return bannerTickMsg{Gen: gen} })
}

// wantsBannerTick reports whether a scrolling banner is on screen: the popup
// is visible with a banner row, in marquee mode without reduce_motion, the
// agent is alive and the text overflows. Nothing else needs the chain, so a
// banner that fits costs no CPU.
func (m Model) wantsBannerTick() bool {
	a := m.Attach
	if a == nil || m.saverOn || a.Ended || a.BannerRows != 1 {
		return false
	}
	if m.Focus != FocusAttach && m.Focus != FocusCommand {
		return false
	}
	return bannerMotion(m) && bannerOverflows(m)
}

// armBanner starts the banner's tick chain when one is wanted and none runs;
// Update calls it next to armSpinner. The chain ends itself on the first tick
// that finds nothing scrolling.
func armBanner(m Model) (Model, tea.Cmd) {
	if m.bannerRunning || !m.wantsBannerTick() {
		return m, nil
	}
	m.bannerGen++
	m.bannerRunning = true
	slog.Debug("tui: banner armed", "gen", m.bannerGen)
	return m, tickBanner(m.bannerGen)
}

// handleBannerTick re-arms the current chain while the banner still scrolls.
func handleBannerTick(m Model, msg bannerTickMsg) (Model, tea.Cmd) {
	if msg.Gen != m.bannerGen {
		return m, nil // stale chain
	}
	if m.wantsBannerTick() {
		return m, tickBanner(m.bannerGen)
	}
	m.bannerRunning = false
	slog.Debug("tui: banner idle", "gen", m.bannerGen)
	return m, nil
}

// renderBannerStatic lays the banner out without motion: the lead on the
// left, cut at a word boundary, and as much meta as fits in half the row on
// the right. While the session's description animates, the lead cell shows
// the animation frame.
func renderBannerStatic(m Model, a *AttachState, lead string, meta []bannerItem, cols int) string {
	n := 0
	if cols >= bannerMetaMinCols {
		for i := len(meta); i > 0; i-- {
			if bannerMetaWidth(meta[:i]) <= cols/2 {
				n = i
				break
			}
		}
	}
	right := bannerMetaStyled(meta[:n])
	rightW := ansi.StringWidth(ansi.Strip(right))

	var left string
	if lead != "" {
		leadW := cols - ansi.StringWidth(bannerLead) - rightW
		if rightW > 0 {
			leadW -= 2 // gap
		}
		cell := truncateWords(lead, leadW)
		if anim, ok := m.DescAnim[a.SessionID]; ok && anim.Active(m.now()) && leadW > 0 {
			cell = strings.TrimRight(renderAnimFrame(anim, leadW, m.now()), " ")
		}
		left = styleArrow.Render(bannerLead) + stylePrimary.Render(cell)
	}
	pad := cols - ansi.StringWidth(ansi.Strip(left)) - rightW
	if pad < 0 {
		pad = 0
	}
	return left + strings.Repeat(" ", pad) + right
}

func bannerMetaWidth(items []bannerItem) int {
	w := 0
	for i, it := range items {
		if i > 0 {
			w += ansi.StringWidth(" · ")
		}
		w += ansi.StringWidth(it.text)
	}
	return w
}

func bannerMetaStyled(items []bannerItem) string {
	parts := make([]string, len(items))
	for i, it := range items {
		if it.muted {
			parts[i] = styleMuted.Render(it.text)
		} else {
			parts[i] = styleDim.Render(it.text)
		}
	}
	return strings.Join(parts, styleMuted.Render(" · "))
}

// toggleBanner is ::banner: hide or show the banner for this run only (the
// attach_banner setting is the persistent switch) and resize the agent to the
// rows that remain. A setting of off, or a terminal too short, leaves nothing
// to show; it says so.
func toggleBanner(m Model) Model {
	if bannerModeOf(m) == bannerOff {
		m.Notice = "banner is off — set attach_banner in Settings"
		return m
	}
	m.bannerHidden = !m.bannerHidden
	was := m.Attach.BannerRows
	resizeAttach(m)
	switch {
	case m.bannerHidden:
		m.Notice = "banner hidden — ::banner shows it"
	case m.Attach.BannerRows == 0:
		m.Notice = "terminal too short for the banner (needs 25 rows)"
	default:
		m.Notice = "banner shown — ::banner hides it"
	}
	if was == 0 && m.Attach.BannerRows == 1 {
		m.Attach.BannerSince = m.now()
	}
	return m
}
