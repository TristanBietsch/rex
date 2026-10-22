package tui

import (
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// sessionRetirer is the narrow client surface the mark-done toggle needs.
// *client.Client satisfies it; tests inject a recorder.
type sessionRetirer interface {
	Retire(sessionID string, ttl time.Duration, reqID string) error
	Unretire(sessionID, reqID string) error
}

// retireTTL is the configured linger time for a marked-done session. Sent
// with the intent so a settings change applies without a daemon restart.
func retireTTL(store *settings.Store) time.Duration {
	if store != nil {
		if h, ok := store.Get("retire_after_hours").(int); ok && h > 0 {
			return time.Duration(h) * time.Hour
		}
	}
	return 7 * 24 * time.Hour
}

// toggleRetire marks the selected session done, or undoes an existing mark.
func toggleRetire(m Model, id string) (Model, tea.Cmd) {
	sum, ok := findSession(m.Sessions, id)
	if !ok {
		slog.Debug("tui: retire toggle on unknown session", "session", id)
		return m, nil
	}
	r := retirerOf(m)
	if r == nil {
		return m, nil
	}
	retiring := !sum.RetireAt.IsZero()
	ttl := retireTTL(m.Store)
	label := displayName(sum)
	if retiring {
		m.Notice = "unmarked " + label
	} else {
		m.Notice = "marked done: " + label + " clears in " + shortDuration(ttl) + " (c to undo)"
	}
	if m.Audio != nil {
		m.Audio.Play(audio.EventClose)
	}
	slog.Info("tui: retire toggle", "session", id, "action", map[bool]string{true: "unmark", false: "mark"}[retiring], "ttl_s", int64(ttl.Seconds()))
	return m, retireToggleCmd(r, id, retiring, ttl)
}

// retirerOf mirrors namerOf for the retire intents.
func retirerOf(m Model) sessionRetirer {
	if m.retirer != nil {
		return m.retirer
	}
	if m.Client == nil {
		return nil
	}
	return m.Client
}

func retireToggleCmd(r sessionRetirer, id string, retiring bool, ttl time.Duration) tea.Cmd {
	return func() tea.Msg {
		var err error
		if retiring {
			err = r.Unretire(id, "unretire-"+id)
		} else {
			err = r.Retire(id, ttl, "retire-"+id)
		}
		if err != nil {
			slog.Error("tui: retire intent failed", "session", id, "err", err)
			return CmdErrMsg{Op: "mark done", Err: err}
		}
		return nil
	}
}

// retireLabel is the clear countdown shown in the attach footer (the board
// row shows the done badge instead).
func retireLabel(now time.Time, s protocol.SessionSummary) string {
	if s.RetireAt.IsZero() {
		return ""
	}
	left := s.RetireAt.Sub(now)
	if left <= 0 {
		if s.State == protocol.StateWorking || s.State == protocol.StateQueued {
			return "marked done · clears when idle"
		}
		return "marked done · clearing…"
	}
	return "marked done · clears in " + shortDuration(left)
}

// doneBadgeText is the unpadded badge label for a marked-done session: the
// clock time when marked today, else a short date. Marks that predate the
// RetiredAt field have no time and read just "done".
func doneBadgeText(now time.Time, s protocol.SessionSummary) string {
	if s.RetiredAt.IsZero() {
		return "done"
	}
	t := s.RetiredAt.In(now.Location())
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return "done " + t.Format("15:04")
	}
	return "done " + t.Format("Jan 2")
}

// doneBadge renders the marked-done badge: padded text on an elevated
// background, in the done color. Empty for unmarked sessions.
func doneBadge(now time.Time, s protocol.SessionSummary) string {
	if !isMarkedDone(s) {
		return ""
	}
	return lipgloss.NewStyle().Foreground(colorDone).Background(colorBgElev).
		Render(" " + doneBadgeText(now, s) + " ")
}

// shortDuration renders d with one unit: 45s, 12m, 5h, 6d.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

// Long-running tiers: how long a session has been working without a break.
// Below the first tier the badge is blank.
var longRunTiers = []struct {
	min   time.Duration
	style func() lipgloss.Style
}{
	{6 * time.Hour, func() lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(colorFailed)
	}},
	{2 * time.Hour, func() lipgloss.Style { return lipgloss.NewStyle().Bold(true).Foreground(colorFailed) }},
	{30 * time.Minute, func() lipgloss.Style { return lipgloss.NewStyle().Foreground(colorNeeds) }},
	{10 * time.Minute, func() lipgloss.Style { return lipgloss.NewStyle().Foreground(colorFgDim) }},
}

// longRunTier returns the tier index (0 = most severe) or -1 when the
// session isn't working or hasn't been working long enough to flag.
func longRunTier(now time.Time, s protocol.SessionSummary) int {
	if s.State != protocol.StateWorking || s.WorkingSince.IsZero() {
		return -1
	}
	d := now.Sub(s.WorkingSince)
	for i, t := range longRunTiers {
		if d >= t.min {
			return i
		}
	}
	return -1
}

// longRunBadge renders the small long-running column: "▴42m", colored by tier.
func longRunBadge(now time.Time, s protocol.SessionSummary) string {
	tier := longRunTier(now, s)
	if tier < 0 {
		return ""
	}
	return longRunTiers[tier].style().Render("▴" + shortDuration(now.Sub(s.WorkingSince)))
}

// displayName is the name column text: the user's label, else the slug.
func displayName(s protocol.SessionSummary) string {
	if s.Name != "" {
		return s.Name
	}
	return s.Slug
}
