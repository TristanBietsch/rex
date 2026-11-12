package inspect

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunStats prints lifetime usage statistics across all known sessions.
func RunStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats, core.WithLegacyJSON())
	showAll := fs.Bool("all", false, "include zero-usage models")
	_, err := out.Parse(fs, args)
	if err != nil {
		return err
	}

	sessions, fromDaemon := loadSessionsForStats(socket)
	slog.Debug("stats: loaded sessions", "count", len(sessions), "from_daemon", fromDaemon)

	agg := aggregateSessions(sessions)

	p := core.Out()
	return p.Emit(core.Result{
		Cmd:    "stats",
		Data:   statsResultData(agg, *showAll),
		View:   statsView(agg, *showAll),
		Legacy: func(w io.Writer) error { return writeStatsJSON(w, agg, *showAll) },
	})
}

// loadSessionsForStats tries the daemon first, then falls back to on-disk state.
// Returns the sessions and a bool indicating whether the daemon was used.
func loadSessionsForStats(socket *core.SocketFlag) ([]protocol.SessionSummary, bool) {
	c, err := socket.Dial()
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

	root, err := paths.DataDir()
	if err != nil {
		slog.Warn("stats: no session data dir", "err", err)
		return nil, false
	}
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
	byModel   []statsModelRow
	byTool    []statsToolRow
	totals    statsTotals
	hasTokens bool
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

func writeStatsJSON(w io.Writer, agg statsAggregate, showAll bool) error {
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
