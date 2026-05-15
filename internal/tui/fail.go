package tui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// FailState lives on Model when Focus == FocusFail. It's a snapshot —
// captured at open so the popup is stable while the user scrolls.
type FailState struct {
	Sessions []protocol.SessionSummary
	LogLines []string
	Scroll   int
}

const (
	failPopupWidth = 110
	failLogTail    = 60
)

func openFail(m Model) (Model, tea.Cmd) {
	failed := make([]protocol.SessionSummary, 0)
	for _, s := range m.Sessions {
		if s.State == protocol.StateFailed || s.State == protocol.StateCrashed {
			failed = append(failed, s)
		}
	}
	m.Fail = &FailState{
		Sessions: failed,
		LogLines: tailDaemonLog(failLogTail),
	}
	m.Focus = FocusFail
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	return m, nil
}

func closeFail(m Model) Model {
	m.Fail = nil
	m.Focus = FocusBoard
	if m.Audio != nil {
		m.Audio.Play(audio.EventClose)
	}
	return m
}

func updateFailKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Fail == nil {
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		return closeFail(m), nil
	case "j", "down":
		m.Fail.Scroll++
		return m, nil
	case "k", "up":
		if m.Fail.Scroll > 0 {
			m.Fail.Scroll--
		}
		return m, nil
	case "g":
		m.Fail.Scroll = 0
		return m, nil
	case "G":
		m.Fail.Scroll = 1 << 30 // clamped on render
		return m, nil
	}
	return m, nil
}

func tailDaemonLog(n int) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{"(could not resolve $HOME)"}
	}
	path := filepath.Join(home, ".local", "state", "rex", "daemon.log")
	f, err := os.Open(path)
	if err != nil {
		return []string{fmt.Sprintf("(no log at %s)", path)}
	}
	defer f.Close()
	// Ring buffer of last n lines.
	ring := make([]string, 0, n+1)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		ring = append(ring, sc.Text())
		if len(ring) > n {
			ring = ring[1:]
		}
	}
	return ring
}

func renderFail(m Model) string {
	if m.Fail == nil {
		return ""
	}
	var b strings.Builder

	header := styleSlug.Render("failed sessions")
	b.WriteString(header)
	b.WriteString("\n\n")

	if len(m.Fail.Sessions) == 0 {
		b.WriteString(styleDim.Render("  (none)"))
	} else {
		for _, s := range m.Fail.Sessions {
			b.WriteString(renderFailEntry(s))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(styleSlug.Render("daemon log — last " + fmt.Sprintf("%d", failLogTail) + " lines"))
	b.WriteString("\n\n")

	lines := m.Fail.LogLines
	// Show a windowed view of the log. Keep the popup body bounded so the
	// surrounding frame stays the same size; user scrolls via j/k.
	const logBodyHeight = 22
	if m.Fail.Scroll > len(lines)-logBodyHeight {
		m.Fail.Scroll = max(0, len(lines)-logBodyHeight)
	}
	end := m.Fail.Scroll + logBodyHeight
	if end > len(lines) {
		end = len(lines)
	}
	for _, ln := range lines[m.Fail.Scroll:end] {
		b.WriteString(styleDim.Render(truncate(ln, failPopupWidth-2)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styleDim.Render("  j/k scroll · g/G top/bottom · esc close"))

	// Pad/truncate every line to the popup's inner width so the surrounding
	// lipgloss border doesn't shrink-wrap to the longest content.
	lines2 := strings.Split(b.String(), "\n")
	for i, l := range lines2 {
		lines2[i] = padLine(l, failPopupWidth)
	}
	return strings.Join(lines2, "\n")
}

func renderFailEntry(s protocol.SessionSummary) string {
	when := durationAgoOrDash(s.LastEventAt)
	exit := "?"
	if s.ExitCode != nil {
		exit = fmt.Sprintf("%d", *s.ExitCode)
	}
	id := lipgloss.NewStyle().Foreground(colorFgDim).Render(s.ShortID)
	slug := styleSlug.Render(s.Slug)
	meta := styleDim.Render(fmt.Sprintf("%s · %s · exit=%s · %s ago", s.ToolID, s.ModelID, exit, when))
	line1 := fmt.Sprintf("  %s  %s    %s", id, slug, meta)
	if s.LastLine == "" {
		return line1
	}
	return line1 + "\n    " + styleDim.Render(truncate(s.LastLine, failPopupWidth-6))
}

func durationAgoOrDash(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return durationAgo(t)
}
