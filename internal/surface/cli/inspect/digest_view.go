package inspect

import (
	"fmt"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// digestData is the `-o json` payload of rex digest. Durations are integer
// milliseconds; sessions use the wire tags.
type digestData struct {
	Date     string                    `json:"date"`
	Totals   digestTotalsData          `json:"totals"`
	ByTool   []digestGroupData         `json:"by_tool"`
	ByModel  []digestGroupData         `json:"by_model"`
	Sessions []protocol.SessionSummary `json:"sessions"`
}

type digestTotalsData struct {
	Count      int   `json:"count"`
	ActiveMs   int64 `json:"active_ms"`
	Done       int   `json:"done"`
	NeedsInput int   `json:"needs_input"`
	Failed     int   `json:"failed"`
	Working    int   `json:"working"`
	Queued     int   `json:"queued"`
}

type digestGroupData struct {
	Key      string `json:"key"`
	Count    int    `json:"count"`
	ActiveMs int64  `json:"active_ms"`
}

func groupData(gs []digestGroupJSON, raw map[string]*digestGroup) []digestGroupData {
	out := make([]digestGroupData, len(gs))
	for i, g := range gs {
		out[i] = digestGroupData{Key: g.Key, Count: g.Count, ActiveMs: raw[g.Key].Active.Milliseconds()}
	}
	return out
}

func digestResultData(out digestOutput) digestData {
	t := out.Totals
	return digestData{
		Date: out.Date,
		Totals: digestTotalsData{Count: t.Count, ActiveMs: t.Active.Milliseconds(), Done: t.Done,
			NeedsInput: t.NeedsInput, Failed: t.Failed, Working: t.Working, Queued: t.Queued},
		ByTool:   groupData(out.ByToolJSON, out.ByTool),
		ByModel:  groupData(out.ByModelJSON, out.ByModel),
		Sessions: out.Sessions,
	}
}

// digestDateLabel is the day, or "first → last" for a multi-day range.
func digestDateLabel(dr digestRange) string {
	label := dr.start.Format("2006-01-02")
	if !dr.end.Equal(dr.start.AddDate(0, 0, 1)) {
		label = fmt.Sprintf("%s → %s", label, dr.end.AddDate(0, 0, -1).Format("2006-01-02"))
	}
	return label
}

func seconds(d time.Duration) int { return int(d.Seconds()) }

// digestView builds the summary. The on-disk note qualifies the data, so it
// is part of the result (a Note here, meta.source in json and agent).
func digestView(out digestOutput, daemonDown bool, dr digestRange, g core.Glyphs) core.View {
	label := digestDateLabel(dr)
	v := core.View{core.Header{Title: "Digest", Meta: label}}
	if daemonDown {
		v = append(v, core.Note{Text: "daemon not running, showing on-disk snapshot only"})
	}
	if out.Totals.Count == 0 {
		return append(v, core.Note{Text: "no sessions on " + label},
			core.KV{Pairs: []core.Pair{{Key: "date", Val: core.Cell{Text: out.Date}, Formats: core.SetOf(core.FormatAgent)},
				{Key: "sessions", Val: core.Cell{Text: "0", Raw: 0}, Formats: core.SetOf(core.FormatAgent)}}})
	}

	t := out.Totals
	pair := func(key, label string, n int, always bool) core.Pair {
		p := core.Pair{Key: key, Label: label, Val: core.Cell{Text: fmt.Sprint(n), Raw: n}}
		if !always && n == 0 {
			p.Formats = core.SetOf(core.FormatAgent)
		}
		return p
	}
	v = append(v, core.KV{Pairs: []core.Pair{
		{Key: "date", Label: "date", Val: core.Cell{Text: out.Date}, Formats: core.SetOf(core.FormatTXT, core.FormatAgent)},
		pair("sessions", "sessions", t.Count, true),
		{Key: "active_s", Label: "active", Val: core.Cell{Text: t.ActiveStr, Raw: seconds(t.Active)}},
		pair("done", "done", t.Done, false),
		pair("needs_input", "needs input", t.NeedsInput, false),
		pair("failed", "failed", t.Failed, false),
		pair("working", "working", t.Working, false),
		pair("queued", "queued", t.Queued, false),
	}})

	breakdown := func(title, kind string, gs []digestGroupJSON, raw map[string]*digestGroup) {
		if len(gs) == 0 {
			return
		}
		rows := make([][]core.Cell, len(gs))
		for i, gr := range gs {
			noun := "sessions"
			if gr.Count == 1 {
				noun = "session"
			}
			rows[i] = []core.Cell{{Text: gr.Key}, {Text: fmt.Sprintf("%d %s", gr.Count, noun), Raw: gr.Count},
				{Text: gr.ActiveDuration, Raw: seconds(raw[gr.Key].Active)}}
		}
		v = append(v, core.Section{Title: title}, core.Table{Kind: kind, NoCount: true, Rows: rows, Cols: []core.Col{
			{Key: "key", Title: strings.TrimPrefix(title, "By ")},
			{Key: "sessions", Title: "Sessions", Align: core.AlignRight},
			{Key: "active_s", Title: "Active", Align: core.AlignRight},
		}})
	}
	breakdown("By tool", "tool", out.ByToolJSON, out.ByTool)
	breakdown("By model", "model", out.ByModelJSON, out.ByModel)

	text, txt, md, agent := core.SetOf(core.FormatText), core.SetOf(core.FormatTXT), core.SetOf(core.FormatMD), core.SetOf(core.FormatAgent)
	rows := make([][]core.Cell, len(out.Sessions))
	for i, s := range out.Sessions {
		id := s.ShortID
		if id == "" {
			id = s.ID
			if len(id) > 6 {
				id = id[:6]
			}
		}
		slug := s.Slug
		if slug == "" {
			slug = "(no slug)"
		}
		toolModel := s.ToolID
		if s.ModelID != "" {
			toolModel += "·" + s.ModelID
		}
		active := sessionActiveDuration(s)
		tone := core.StateTone(s.State)
		rows[i] = []core.Cell{
			{Text: g.State[s.State], Tone: tone}, {Text: id}, {Text: slug}, {Text: toolModel},
			{Text: s.ToolID}, {Text: s.ModelID}, {Text: formatDuration(active), Raw: seconds(active), Tone: core.ToneDim},
			{Text: core.StateLabel(s.State), Txt: string(s.State), Raw: string(s.State), Tone: tone},
		}
	}
	return append(v, core.Section{Title: "Sessions"}, core.Table{Kind: "session", Rows: rows, Cols: []core.Col{
		{Key: "mark", Title: "", Formats: text},
		{Key: "id", Title: "ID"},
		{Key: "slug", Title: "Slug", Max: 20, Flex: true, Min: 8},
		{Key: "tool_model", Title: "Tool·Model", Max: 18, Flex: true, Min: 8, Formats: text | txt | md},
		{Key: "tool", Title: "Tool", Formats: agent},
		{Key: "model", Title: "Model", Formats: agent},
		{Key: "active_s", Title: "Active", Align: core.AlignRight},
		{Key: "state", Title: "State"},
	}})
}
