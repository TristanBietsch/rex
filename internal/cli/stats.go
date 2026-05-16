package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

// RunStats prints lifetime usage statistics across all known sessions.
func RunStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	asJSON := fs.Bool("json", false, "output JSON")
	showAll := fs.Bool("all", false, "include zero-usage models")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}

	sessions, fromDaemon := loadSessionsForStats(*socket)
	slog.Debug("stats: loaded sessions", "count", len(sessions), "from_daemon", fromDaemon)

	agg := aggregateSessions(sessions)

	if *asJSON {
		return writeStatsJSON(os.Stdout, agg, *showAll)
	}
	return writeStatsTable(os.Stdout, agg, *showAll)
}

// loadSessionsForStats tries the daemon first, then falls back to on-disk state.
// Returns the sessions and a bool indicating whether the daemon was used.
func loadSessionsForStats(socket string) ([]protocol.SessionSummary, bool) {
	c, err := client.Dial(socket)
	if err == nil {
		defer c.Close()
		snap, err := c.Hello("rex-cli")
		if err == nil {
			slog.Debug("stats: using daemon snapshot")
			return snap.Sessions, true
		}
		slog.Warn("stats: daemon hello failed, falling back to disk", "err", err)
	} else {
		slog.Debug("stats: daemon unreachable, falling back to disk", "err", err)
	}

	root := defaultShareDir()
	sessions, err := state.LoadAll(root)
	if err != nil {
		slog.Warn("stats: LoadAll failed", "root", root, "err", err)
		return nil, false
	}
	summaries := make([]protocol.SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		summaries = append(summaries, s.Summary())
	}
	return summaries, false
}

func defaultShareDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "rex")
}

// statsModelRow holds per-model aggregated data.
type statsModelRow struct {
	Model      string        `json:"model"`
	Sessions   int           `json:"sessions"`
	DurationMs int64         `json:"duration_ms"`
	Tokens     int64         `json:"tokens"`
	duration   time.Duration // computed, not serialized separately
}

// statsToolRow holds per-tool aggregated data.
type statsToolRow struct {
	Tool     string `json:"tool"`
	Sessions int    `json:"sessions"`
}

// statsTotals holds lifetime totals.
type statsTotals struct {
	Sessions int   `json:"sessions"`
	Done     int   `json:"done"`
	Failed   int   `json:"failed"`
	Other    int   `json:"other"`
	Tokens   int64 `json:"tokens,omitempty"`
}

// statsAggregate is the full computed stats.
type statsAggregate struct {
	byModel     []statsModelRow
	byTool      []statsToolRow
	totals      statsTotals
	hasTokens   bool
}

// aggregateSessions computes stats from a slice of SessionSummary.
func aggregateSessions(sessions []protocol.SessionSummary) statsAggregate {
	modelMap := map[string]*statsModelRow{}
	toolMap := map[string]*statsToolRow{}
	var totals statsTotals
	var totalTokens int64

	for _, s := range sessions {
		totals.Sessions++
		switch s.State {
		case protocol.StateDone:
			totals.Done++
		case protocol.StateFailed, protocol.StateCrashed:
			totals.Failed++
		default:
			totals.Other++
		}

		dur := sessionDuration(s)

		model := s.ModelID
		if model == "" {
			model = "(unknown)"
		}
		mr, ok := modelMap[model]
		if !ok {
			mr = &statsModelRow{Model: model}
			modelMap[model] = mr
		}
		mr.Sessions++
		mr.duration += dur
		mr.DurationMs = int64(mr.duration / time.Millisecond)

		tool := s.ToolID
		if tool == "" {
			tool = "(unknown)"
		}
		tr, ok := toolMap[tool]
		if !ok {
			tr = &statsToolRow{Tool: tool}
			toolMap[tool] = tr
		}
		tr.Sessions++
	}

	// Build sorted slices — descending by session count.
	byModel := make([]statsModelRow, 0, len(modelMap))
	for _, r := range modelMap {
		byModel = append(byModel, *r)
	}
	sort.Slice(byModel, func(i, j int) bool {
		if byModel[i].Sessions != byModel[j].Sessions {
			return byModel[i].Sessions > byModel[j].Sessions
		}
		return byModel[i].Model < byModel[j].Model
	})

	byTool := make([]statsToolRow, 0, len(toolMap))
	for _, r := range toolMap {
		byTool = append(byTool, *r)
	}
	sort.Slice(byTool, func(i, j int) bool {
		if byTool[i].Sessions != byTool[j].Sessions {
			return byTool[i].Sessions > byTool[j].Sessions
		}
		return byTool[i].Tool < byTool[j].Tool
	})

	hasTokens := totalTokens > 0
	if hasTokens {
		totals.Tokens = totalTokens
	}

	return statsAggregate{
		byModel:   byModel,
		byTool:    byTool,
		totals:    totals,
		hasTokens: hasTokens,
	}
}

// sessionDuration computes the elapsed time for a session.
// Matches the convention: LastEventAt - StartedAt (or now - StartedAt for live).
func sessionDuration(s protocol.SessionSummary) time.Duration {
	if s.StartedAt.IsZero() {
		return 0
	}
	end := s.LastEventAt
	if end.IsZero() || s.State == protocol.StateWorking || s.State == protocol.StateNeedsInput {
		end = time.Now()
	}
	d := end.Sub(s.StartedAt)
	if d < 0 {
		return 0
	}
	return d
}

// formatTokens formats a token count as "214K tk", "1.2M tk", etc.
func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM tk", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK tk", n/1_000)
	default:
		return fmt.Sprintf("%d tk", n)
	}
}

// --- table output ---

var (
	statsAccent  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B8DEF"))
	statsBold    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6E6E6"))
	statsDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	statsHR      = lipgloss.NewStyle().Foreground(lipgloss.Color("#262A36"))
	statsLabel   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6"))
	statsMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("#4A4F5A"))
)

func writeStatsTable(w *os.File, agg statsAggregate, showAll bool) error {
	var b strings.Builder
	hr := statsHR.Render(strings.Repeat("─", 44))

	b.WriteString("\n")
	b.WriteString("  " + statsAccent.Render("∴") + " " + statsBold.Render("rex stats") + "\n")
	b.WriteString("  " + hr + "\n")

	// Lifetime summary line.
	t := agg.totals
	lifeline := fmt.Sprintf("%s sessions · %s done · %s failed · %s active/other",
		statsBold.Render(fmt.Sprintf("%d", t.Sessions)),
		statsDim.Render(fmt.Sprintf("%d", t.Done)),
		statsDim.Render(fmt.Sprintf("%d", t.Failed)),
		statsDim.Render(fmt.Sprintf("%d", t.Other)),
	)
	b.WriteString("\n  " + statsLabel.Render("lifetime") + "   " + lifeline + "\n")

	// By model section.
	b.WriteString("\n  " + statsLabel.Render("by model") + "\n")

	rows := agg.byModel
	if !showAll {
		// filter out zero-session rows (shouldn't happen, but be safe)
		filtered := rows[:0]
		for _, r := range rows {
			if r.Sessions > 0 {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	if len(rows) == 0 {
		b.WriteString("    " + statsDim.Render("(no sessions)") + "\n")
	} else {
		// Compute column widths.
		maxModel := 0
		for _, r := range rows {
			if l := len(r.Model); l > maxModel {
				maxModel = l
			}
		}
		maxModel = max(maxModel, 5) // min "model" header width

		var totalTokens int64
		for _, r := range rows {
			totalTokens += r.Tokens
		}

		for _, r := range rows {
			pad := strings.Repeat(" ", maxModel-len(r.Model)+2)
			line := fmt.Sprintf("    %s%s%s · %s",
				statsLabel.Render(r.Model),
				pad,
				statsDim.Render(fmt.Sprintf("%d sessions", r.Sessions)),
				statsDim.Render(formatDuration(r.duration)),
			)
			if agg.hasTokens && r.Tokens > 0 {
				line += " · " + statsDim.Render(formatTokens(r.Tokens))
			}
			b.WriteString(line + "\n")
		}

		if agg.hasTokens && totalTokens > 0 {
			pad := strings.Repeat(" ", maxModel+2)
			sepLine := fmt.Sprintf("    %s%s",
				pad,
				statsHR.Render(strings.Repeat("─", 12)),
			)
			b.WriteString(sepLine + "\n")
			totalLine := fmt.Sprintf("    %s%s%s",
				statsDim.Render("total"),
				strings.Repeat(" ", maxModel-3),
				statsLabel.Render(formatTokens(totalTokens)),
			)
			b.WriteString(totalLine + "\n")
		}
	}

	// By tool section.
	b.WriteString("\n  " + statsLabel.Render("by tool") + "\n")

	toolRows := agg.byTool
	if !showAll {
		filtered := toolRows[:0]
		for _, r := range toolRows {
			if r.Sessions > 0 {
				filtered = append(filtered, r)
			}
		}
		toolRows = filtered
	}

	if len(toolRows) == 0 {
		b.WriteString("    " + statsDim.Render("(no sessions)") + "\n")
	} else {
		maxTool := 0
		for _, r := range toolRows {
			if l := len(r.Tool); l > maxTool {
				maxTool = l
			}
		}
		maxTool = max(maxTool, 4)
		for _, r := range toolRows {
			pad := strings.Repeat(" ", maxTool-len(r.Tool)+2)
			line := fmt.Sprintf("    %s%s%s",
				statsLabel.Render(r.Tool),
				pad,
				statsDim.Render(fmt.Sprintf("%d sessions", r.Sessions)),
			)
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n")
	_ = statsMuted
	fmt.Fprint(w, b.String())
	return nil
}

// --- JSON output ---

type statsJSONOut struct {
	ByModel []statsModelJSONRow `json:"by_model"`
	ByTool  []statsToolRow      `json:"by_tool"`
	Totals  statsTotals         `json:"totals"`
}

type statsModelJSONRow struct {
	Model      string `json:"model"`
	Sessions   int    `json:"sessions"`
	DurationMs int64  `json:"duration_ms"`
	Tokens     int64  `json:"tokens,omitempty"`
}

func writeStatsJSON(w *os.File, agg statsAggregate, showAll bool) error {
	rows := agg.byModel
	if !showAll {
		filtered := make([]statsModelRow, 0, len(rows))
		for _, r := range rows {
			if r.Sessions > 0 {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	jsonModels := make([]statsModelJSONRow, 0, len(rows))
	for _, r := range rows {
		jsonModels = append(jsonModels, statsModelJSONRow{
			Model:      r.Model,
			Sessions:   r.Sessions,
			DurationMs: r.DurationMs,
			Tokens:     r.Tokens,
		})
	}

	toolRows := agg.byTool
	if !showAll {
		filtered := make([]statsToolRow, 0, len(toolRows))
		for _, r := range toolRows {
			if r.Sessions > 0 {
				filtered = append(filtered, r)
			}
		}
		toolRows = filtered
	}

	out := statsJSONOut{
		ByModel: jsonModels,
		ByTool:  toolRows,
		Totals:  agg.totals,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("stats: marshal JSON: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
