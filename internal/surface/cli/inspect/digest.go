package inspect

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// digestRange describes a time window (one or more days).
type digestRange struct {
	start time.Time
	end   time.Time
}

// contains returns true if the session belongs to this range.
// A session counts if StartedAt OR LastEventAt falls within [start, end).
func (r digestRange) contains(s protocol.SessionSummary) bool {
	ref := s.LastEventAt
	if ref.IsZero() {
		ref = s.StartedAt
	}
	inRange := func(t time.Time) bool {
		return !t.Before(r.start) && t.Before(r.end)
	}
	return inRange(s.StartedAt) || inRange(ref)
}

// sessionActiveDuration computes the active time for a session.
func sessionActiveDuration(s protocol.SessionSummary) time.Duration {
	end := s.LastEventAt
	if end.IsZero() {
		end = s.StartedAt
	}
	switch s.State {
	case protocol.StateDone, protocol.StateFailed, protocol.StateCrashed:
		// use recorded end
	default:
		// live — use now
		end = time.Now()
	}
	d := end.Sub(s.StartedAt)
	if d < 0 {
		return 0
	}
	return d
}

// formatDuration formats a duration as "1h 24m" or "58m" or "< 1m".
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "< 1m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// digestTotals holds aggregate counts.
type digestTotals struct {
	Count      int           `json:"count"`
	Active     time.Duration `json:"-"`
	ActiveStr  string        `json:"active_duration"`
	Done       int           `json:"done"`
	NeedsInput int           `json:"needs_input"`
	Failed     int           `json:"failed"`
	Working    int           `json:"working"`
	Queued     int           `json:"queued"`
}

// digestGroup is one row in a by-tool or by-model breakdown.
type digestGroup struct {
	Key    string
	Count  int
	Active time.Duration
}

// digestOutput is the structured result for both display and JSON.
type digestOutput struct {
	Date     string                    `json:"date"`
	Totals   digestTotals              `json:"totals"`
	ByTool   map[string]*digestGroup   `json:"-"`
	ByModel  map[string]*digestGroup   `json:"-"`
	Sessions []protocol.SessionSummary `json:"sessions"`

	// JSON-friendly sorted slices
	ByToolJSON  []digestGroupJSON `json:"by_tool"`
	ByModelJSON []digestGroupJSON `json:"by_model"`
}

type digestGroupJSON struct {
	Key            string `json:"key"`
	Count          int    `json:"count"`
	ActiveDuration string `json:"active_duration"`
}

// buildDigestOutput aggregates sessions into a digestOutput.
func buildDigestOutput(sessions []protocol.SessionSummary, date time.Time) digestOutput {
	out := digestOutput{
		Date:    date.Format("2006-01-02"),
		ByTool:  make(map[string]*digestGroup),
		ByModel: make(map[string]*digestGroup),
	}

	for _, s := range sessions {
		d := sessionActiveDuration(s)

		out.Totals.Count++
		out.Totals.Active += d

		switch s.State {
		case protocol.StateDone:
			out.Totals.Done++
		case protocol.StateNeedsInput:
			out.Totals.NeedsInput++
		case protocol.StateFailed, protocol.StateCrashed:
			out.Totals.Failed++
		case protocol.StateWorking:
			out.Totals.Working++
		case protocol.StateQueued:
			out.Totals.Queued++
		}

		if s.ToolID != "" {
			g := out.ByTool[s.ToolID]
			if g == nil {
				g = &digestGroup{Key: s.ToolID}
				out.ByTool[s.ToolID] = g
			}
			g.Count++
			g.Active += d
		}

		modelKey := s.ModelID
		if modelKey == "" {
			modelKey = "unknown"
		}
		gm := out.ByModel[modelKey]
		if gm == nil {
			gm = &digestGroup{Key: modelKey}
			out.ByModel[modelKey] = gm
		}
		gm.Count++
		gm.Active += d
	}

	out.Totals.ActiveStr = formatDuration(out.Totals.Active)
	out.Sessions = sessions

	// Build sorted JSON-friendly slices (descending by count, then name).
	sortedGroups := func(m map[string]*digestGroup) []digestGroupJSON {
		gs := make([]digestGroup, 0, len(m))
		for _, g := range m {
			gs = append(gs, *g)
		}
		sort.Slice(gs, func(i, j int) bool {
			if gs[i].Count != gs[j].Count {
				return gs[i].Count > gs[j].Count
			}
			return gs[i].Key < gs[j].Key
		})
		out := make([]digestGroupJSON, len(gs))
		for i, g := range gs {
			out[i] = digestGroupJSON{
				Key:            g.Key,
				Count:          g.Count,
				ActiveDuration: formatDuration(g.Active),
			}
		}
		return out
	}

	out.ByToolJSON = sortedGroups(out.ByTool)
	out.ByModelJSON = sortedGroups(out.ByModel)

	return out
}

// mergeSessionSummaries merges disk sessions into live sessions by ID.
// The live (daemon) view wins on conflict.
func mergeSessionSummaries(live []protocol.SessionSummary, disk []*state.Session) []protocol.SessionSummary {
	seen := make(map[string]struct{}, len(live))
	for _, s := range live {
		seen[s.ID] = struct{}{}
	}
	merged := make([]protocol.SessionSummary, len(live))
	copy(merged, live)
	for _, s := range disk {
		if _, ok := seen[s.ID]; ok {
			continue
		}
		merged = append(merged, s.Summary())
	}
	return merged
}

// parseDigestRange parses --date and --since flags into a digestRange.
func parseDigestRange(dateFlag, sinceFlag string) (digestRange, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	if sinceFlag != "" && dateFlag != "" {
		return digestRange{}, fmt.Errorf("digest: -date and -since are mutually exclusive")
	}

	if sinceFlag != "" {
		sinceFlag = strings.TrimSpace(sinceFlag)
		var n int
		var unit string
		if _, err := fmt.Sscanf(sinceFlag, "%d%s", &n, &unit); err != nil || n <= 0 {
			return digestRange{}, fmt.Errorf("digest: -since must be like 7d or 24h, got %q", sinceFlag)
		}
		switch unit {
		case "d":
			start := today.AddDate(0, 0, -n+1)
			return digestRange{start: start, end: today.AddDate(0, 0, 1)}, nil
		case "h":
			start := now.Add(-time.Duration(n) * time.Hour)
			return digestRange{start: start, end: now.Add(time.Minute)}, nil
		default:
			return digestRange{}, fmt.Errorf("digest: -since unit must be d or h, got %q", unit)
		}
	}

	if dateFlag != "" {
		var d time.Time
		var err error
		switch strings.ToLower(dateFlag) {
		case "yesterday":
			d = today.AddDate(0, 0, -1)
		default:
			d, err = time.ParseInLocation("2006-01-02", dateFlag, now.Location())
			if err != nil {
				return digestRange{}, fmt.Errorf("digest: invalid date %q, use YYYY-MM-DD or 'yesterday'", dateFlag)
			}
		}
		return digestRange{
			start: d,
			end:   d.AddDate(0, 0, 1),
		}, nil
	}

	// default: today
	return digestRange{start: today, end: today.AddDate(0, 0, 1)}, nil
}

// RunDigest prints a daily summary of sessions.
func RunDigest(args []string) error {
	fs := flag.NewFlagSet("digest", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	dateFlag := fs.String("date", "", "specific day: YYYY-MM-DD or 'yesterday'")
	sinceFlag := fs.String("since", "", "relative window: 7d or 24h")
	out := core.AddOutputFlags(fs, core.AllFormats, core.WithLegacyJSON())
	_, err := out.Parse(fs, args)
	if err != nil {
		return err
	}

	slog.Debug("digest: starting", "date", *dateFlag, "since", *sinceFlag)

	dr, err := parseDigestRange(*dateFlag, *sinceFlag)
	if err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}

	// --- connect to daemon (best-effort) ---
	var liveSessions []protocol.SessionSummary
	daemonDown := false

	var c *client.Client
	sock, dialErr := socket.Path()
	if dialErr == nil {
		c, dialErr = client.Dial(sock)
	}
	if dialErr != nil {
		slog.Info("digest: daemon unreachable, falling back to disk", "err", dialErr)
		daemonDown = true
	} else {
		defer c.Close()
		snap, helloErr := c.Hello("rex-cli")
		if helloErr != nil {
			slog.Info("digest: daemon hello failed, falling back to disk", "err", helloErr)
			daemonDown = true
		} else {
			liveSessions = snap.Sessions
			slog.Debug("digest: loaded live sessions from daemon", "count", len(liveSessions))
		}
	}

	// --- load from disk ---
	var diskSessions []*state.Session
	root, diskErr := paths.DataDir()
	if diskErr == nil {
		diskSessions, diskErr = state.LoadAll(root)
	}
	if diskErr != nil {
		slog.Warn("digest: failed to load disk sessions", "err", diskErr)
		// non-fatal — continue with whatever we have
		diskSessions = nil
	} else {
		slog.Debug("digest: loaded disk sessions", "count", len(diskSessions))
	}

	// --- merge ---
	all := mergeSessionSummaries(liveSessions, diskSessions)
	slog.Debug("digest: merged sessions", "total", len(all))

	// --- filter to range ---
	filtered := make([]protocol.SessionSummary, 0, len(all))
	for _, s := range all {
		if dr.contains(s) {
			filtered = append(filtered, s)
		}
	}

	// Sort sessions: done/failed first (by LastEventAt desc), then working/live
	sort.Slice(filtered, func(i, j int) bool {
		ti := filtered[i].LastEventAt
		if ti.IsZero() {
			ti = filtered[i].StartedAt
		}
		tj := filtered[j].LastEventAt
		if tj.IsZero() {
			tj = filtered[j].StartedAt
		}
		return ti.After(tj)
	})

	slog.Debug("digest: sessions in range", "count", len(filtered), "range_start", dr.start, "range_end", dr.end)

	// Label date: for --since multi-day we still label the earliest day.
	labelDate := dr.start

	result := buildDigestOutput(filtered, labelDate)

	p := core.Out()
	r := core.Result{
		Cmd:    "digest",
		Data:   digestResultData(result),
		View:   digestView(result, daemonDown, dr, p.Glyphs),
		Legacy: func(w io.Writer) error { return writeDigestJSON(w, result) },
	}
	if daemonDown {
		r.Meta = map[string]any{"source": "disk"}
	}
	return p.Emit(r)
}

func writeDigestJSON(w io.Writer, out digestOutput) error {
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("digest: marshal json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
