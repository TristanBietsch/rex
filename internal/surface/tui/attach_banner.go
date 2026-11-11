package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	if bannerModeOf(m) == bannerOff {
		return 0
	}
	if _, rows := attachPopupInnerSize(m.Width, m.Height, 0); rows < bannerMinAgentRows+1 {
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
		if q := strings.TrimSpace(protocol.FlattenDisplayText(sum.LastLine)); strings.HasSuffix(q, "?") {
			lead = q
		}
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
	return lead, meta
}

// renderAttachBanner renders the banner row, exactly cols cells wide.
func renderAttachBanner(m Model, a *AttachState, cols int) string {
	sum, _ := findSession(m.Sessions, a.SessionID)
	lead, meta := bannerParts(m, sum, a.Ended)
	return padLine(renderBannerStatic(m, a, lead, meta, cols), cols)
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
		left = lipgloss.NewStyle().Foreground(colorWorking).Render(bannerLead) + stylePrimary.Render(cell)
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
