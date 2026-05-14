package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// WriteSessionsTable prints sessions as a fixed-width table to w.
func WriteSessionsTable(w io.Writer, sessions []protocol.SessionSummary) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(w, "(no sessions)")
		return err
	}
	hdr := fmt.Sprintf("%-5s  %-12s  %-9s  %-20s  %-24s  %s",
		"ID", "STATE", "TOOL", "MODEL", "SLUG", "LAST EVENT")
	if _, err := fmt.Fprintln(w, hdr); err != nil {
		return err
	}
	for _, s := range sessions {
		ago := durationAgo(s.LastEventAt)
		model := s.ModelID
		if s.Effort != "" {
			model = model + " · " + s.Effort
		}
		row := fmt.Sprintf("%-5s  %-12s  %-9s  %-20s  %-24s  %s",
			s.ShortID, s.State, s.ToolID, truncate(model, 20), truncate(s.Slug, 24), ago)
		if _, err := fmt.Fprintln(w, row); err != nil {
			return err
		}
	}
	return nil
}

// WriteSessionsJSONL prints one JSON object per session per line.
func WriteSessionsJSONL(w io.Writer, sessions []protocol.SessionSummary) error {
	for _, s := range sessions {
		b, err := json.Marshal(s)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// WriteAggregateLine prints "N awaiting input · N working · N completed".
func WriteAggregateLine(w io.Writer, sessions []protocol.SessionSummary) error {
	var working, needsInput, done, failed, crashed int
	for _, s := range sessions {
		switch s.State {
		case protocol.StateWorking:
			working++
		case protocol.StateNeedsInput:
			needsInput++
		case protocol.StateDone:
			done++
		case protocol.StateFailed:
			failed++
		case protocol.StateCrashed:
			crashed++
		}
	}
	parts := []string{
		fmt.Sprintf("%d awaiting input", needsInput),
		fmt.Sprintf("%d working", working),
		fmt.Sprintf("%d completed", done),
	}
	if failed+crashed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed+crashed))
	}
	_, err := fmt.Fprintln(w, strings.Join(parts, " · "))
	return err
}

// WriteAggregateJSON prints counts as one JSON object.
func WriteAggregateJSON(w io.Writer, sessions []protocol.SessionSummary) error {
	counts := map[string]int{
		"awaiting_input": 0, "working": 0, "completed": 0, "failed": 0, "crashed": 0,
	}
	for _, s := range sessions {
		switch s.State {
		case protocol.StateWorking:
			counts["working"]++
		case protocol.StateNeedsInput:
			counts["awaiting_input"]++
		case protocol.StateDone:
			counts["completed"]++
		case protocol.StateFailed:
			counts["failed"]++
		case protocol.StateCrashed:
			counts["crashed"]++
		}
	}
	b, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func durationAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
}
