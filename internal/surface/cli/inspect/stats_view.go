package inspect

import (
	"fmt"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// statsView builds the lifetime summary and the two breakdowns.
func statsView(agg statsAggregate, showAll bool) core.View {
	t := agg.totals
	pair := func(key, label string, n int) core.Pair {
		return core.Pair{Key: key, Label: label, Val: core.Cell{Text: fmt.Sprint(n), Raw: n}}
	}
	v := core.View{
		core.Header{Title: "Stats", Meta: "lifetime"},
		core.KV{Pairs: []core.Pair{
			pair("sessions", "sessions", t.Sessions),
			pair("done", "done", t.Done),
			pair("failed", "failed", t.Failed),
			pair("other", "active/other", t.Other),
		}},
	}

	models := make([][]core.Cell, 0, len(agg.byModel))
	for _, r := range agg.byModel {
		if r.Sessions == 0 && !showAll {
			continue
		}
		models = append(models, []core.Cell{
			{Text: r.Model},
			{Text: fmt.Sprint(r.Sessions), Raw: r.Sessions},
			{Text: formatDuration(r.duration), Raw: seconds(r.duration)},
		})
	}
	v = append(v, core.Section{Title: "By model"}, core.Table{Kind: "model", NoCount: true, Empty: "(no sessions)", Rows: models,
		Cols: []core.Col{
			{Key: "model", Title: "Model"},
			{Key: "sessions", Title: "Sessions", Align: core.AlignRight},
			{Key: "duration_s", Title: "Duration", Align: core.AlignRight},
		}})

	tools := make([][]core.Cell, 0, len(agg.byTool))
	for _, r := range agg.byTool {
		if r.Sessions == 0 && !showAll {
			continue
		}
		tools = append(tools, []core.Cell{{Text: r.Tool}, {Text: fmt.Sprint(r.Sessions), Raw: r.Sessions}})
	}
	return append(v, core.Section{Title: "By tool"}, core.Table{Kind: "tool", NoCount: true, Empty: "(no sessions)", Rows: tools,
		Cols: []core.Col{
			{Key: "tool", Title: "Tool"},
			{Key: "sessions", Title: "Sessions", Align: core.AlignRight},
		}})
}

// statsResultData is the `-o json` payload of rex stats: the legacy -json
// shape, versioned.
func statsResultData(agg statsAggregate, showAll bool) statsJSONOut {
	models := make([]statsModelJSONRow, 0, len(agg.byModel))
	for _, r := range agg.byModel {
		if r.Sessions == 0 && !showAll {
			continue
		}
		models = append(models, statsModelJSONRow{Model: r.Model, Sessions: r.Sessions, DurationMs: r.DurationMs, Tokens: r.Tokens})
	}
	tools := make([]statsToolRow, 0, len(agg.byTool))
	for _, r := range agg.byTool {
		if r.Sessions == 0 && !showAll {
			continue
		}
		tools = append(tools, r)
	}
	return statsJSONOut{ByModel: models, ByTool: tools, Totals: agg.totals}
}
