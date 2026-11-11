package inspect

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/features/usage"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// usageTimeout bounds the UsageQuery round trip.
const usageTimeout = 10 * time.Second

// RunUsage prints what the agents used over a range: per tool, per model and
// in total, plus the current plan quotas. It asks the daemon (UsageQuery), so
// it never reads the ledger itself. Exit 5 when the usage setting is off.
func RunUsage(args []string) error {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	rng := fs.String("range", protocol.UsageRangeToday, "today, 7d or 30d")
	tool := fs.String("tool", "", "only this tool id")
	by := fs.String("by", "", "rows: tool (one per tool) or model (one per model); default: tools with their models")
	pos, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return core.Errorf(core.ExitInvalidArgs, "rex usage -h", "usage: unexpected argument %q", pos[0])
	}
	if !protocol.ValidUsageRange(*rng) {
		return core.Errorf(core.ExitInvalidArgs, "rex usage -range 7d", "usage: -range must be today, 7d or 30d, not %q", *rng)
	}
	if *by != "" && *by != "tool" && *by != "model" {
		return core.Errorf(core.ExitInvalidArgs, "rex usage -by model", "usage: -by must be tool or model, not %q", *by)
	}
	if err := core.RequireFeature("usage"); err != nil {
		return err
	}

	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := c.Hello("rex-cli"); err != nil {
		return core.HelloFailed(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
	defer cancel()
	rep, err := c.UsageQuery(ctx, protocol.UsageQuery{Range: *rng, Tool: *tool})
	if err != nil {
		return usageError(err)
	}
	p := core.Out()
	return p.Emit(core.Result{Cmd: "usage", Data: rep, View: usageView(rep, *by, p.Now())})
}

// usageError turns a daemon failure into the CLI's error: an older daemon
// is told to restart, a daemon with usage off gets the Features message.
func usageError(err error) error {
	var de *client.Error
	switch {
	case errors.Is(err, client.ErrNoAcks),
		errors.As(err, &de) && de.Code == protocol.ErrCodeBadIntent && de.Message == "intent not implemented":
		return core.Errorf(core.ExitOperationRefused, "rex daemon restart",
			"rex-daemon is older than rex and does not report usage; run `rex daemon restart`")
	case errors.As(err, &de) && de.Code == protocol.ErrCodeBadIntent && de.Message == usage.ErrOff.Error():
		cmd := "rex config set usage true"
		return core.Errorf(core.ExitInvalidArgs, cmd, "off: enable in Settings → Features (%s)", cmd)
	}
	return err
}

// rangeLabel is the human name of a range.
func rangeLabel(r string) string {
	switch r {
	case protocol.UsageRange7d:
		return "last 7 days"
	case protocol.UsageRange30d:
		return "last 30 days"
	}
	return "today"
}

// usageView lays out a report: the usage table, a total line and the quotas.
func usageView(rep protocol.UsageReport, by string, now time.Time) core.View {
	meta := rangeLabel(rep.Range)
	if rep.Tool != "" {
		meta += " · " + flatText(rep.Tool)
	}
	v := core.View{core.Header{Title: "Usage", Meta: meta}}

	text, txtF, md := core.SetOf(core.FormatText), core.SetOf(core.FormatTXT), core.SetOf(core.FormatMD)
	agent := core.SetOf(core.FormatAgent)
	human := text | txtF | md
	cols := []core.Col{
		{Key: "agent", Title: "Agent / model", Flex: true, Min: 10, Max: 32, Formats: human},
		{Key: "level", Title: "Level", Formats: agent},
		{Key: "tool", Title: "Tool", Formats: agent},
		{Key: "model", Title: "Model", Formats: agent},
		{Key: "sessions", Title: "Sessions", Align: core.AlignRight},
		{Key: "tokens", Title: "Tokens", Align: core.AlignRight},
		{Key: "estimated", Title: "Estimated", Formats: agent},
		{Key: "cost_usd", Title: "Cost", Align: core.AlignRight},
	}
	var rows [][]core.Cell
	estimated := rep.Total.TokensEstimated
	row := func(label, level, tool, model string, r protocol.UsageRow) []core.Cell {
		return []core.Cell{
			{Text: label},
			{Text: level},
			{Text: flatText(tool)},
			{Text: flatText(model)},
			{Text: fmt.Sprint(r.Sessions), Raw: r.Sessions},
			{Text: tokensText(r), Raw: r.Tokens},
			{Text: fmt.Sprint(r.TokensEstimated), Raw: r.TokensEstimated},
			costCell(r.CostUSD),
		}
	}
	for _, t := range rep.Tools {
		switch by {
		case "model":
			for _, m := range t.Models {
				rows = append(rows, row(flatText(t.Tool)+" / "+modelLabel(m.Model), "model", t.Tool, m.Model, m.UsageRow))
			}
		default:
			rows = append(rows, row(flatText(t.Tool), "tool", t.Tool, "", t.UsageRow))
			if by == "" {
				for _, m := range t.Models {
					rows = append(rows, row("  "+modelLabel(m.Model), "model", t.Tool, m.Model, m.UsageRow))
				}
			}
		}
	}
	v = append(v, core.Table{Kind: "usage", Cols: cols, Rows: rows, Empty: "no usage recorded in this range"})

	totalTxt := fmt.Sprintf("total: %d sessions · %s tokens · %s", rep.Total.Sessions, strings.TrimSpace(tokensText(rep.Total)), costText(rep.Total.CostUSD))
	v = append(v, core.Section{Title: "Total"}, core.KV{Inline: true, Txt: totalTxt, Pairs: []core.Pair{
		{Key: "sessions", Label: "sessions", Val: core.Cell{Text: fmt.Sprint(rep.Total.Sessions), Raw: rep.Total.Sessions}},
		{Key: "tokens", Label: "tokens", Val: core.Cell{Text: tokensText(rep.Total), Raw: rep.Total.Tokens}},
		{Key: "cost_usd", Label: "cost", Val: costCell(rep.Total.CostUSD)},
	}})

	if len(rep.Quotas) > 0 {
		qcols := []core.Col{
			{Key: "tool", Title: "Tool"},
			{Key: "window", Title: "Window"},
			{Key: "used", Title: "Used", Align: core.AlignRight},
			{Key: "resets", Title: "Resets in", Align: core.AlignRight, Formats: human},
			{Key: "resets_at", Title: "Resets at", Formats: agent},
			{Key: "stale", Title: "Stale", Formats: agent},
		}
		var qrows [][]core.Cell
		for _, q := range rep.Quotas {
			qrows = append(qrows, quotaRow(q, now))
		}
		v = append(v, core.Section{Title: "Quota"}, core.Table{Kind: "quota", Cols: qcols, Rows: qrows})
	}

	if estimated {
		v = append(v, core.Note{Tone: core.ToneDim, Text: "~ = estimate from rex's own token count; cost is only what the tool reports"})
	}
	return v
}

// dash is the plain-text spelling of a missing value.
func dash(s string) string {
	if s == "—" {
		return "-"
	}
	return s
}

func modelLabel(m string) string {
	if m == "" {
		return "(unknown)"
	}
	return flatText(m)
}

// flatText strips control and escape characters from daemon-supplied text.
func flatText(s string) string { return protocol.FlattenDisplayText(s) }

// tokensText is a row's token count: 1.2M, 410k, 88, with "~" in front when
// any of it is an estimate.
func tokensText(r protocol.UsageRow) string {
	s := compactCount(r.Tokens)
	if r.TokensEstimated {
		return "~" + s
	}
	return s
}

func compactCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e6)) + "M"
	case n >= 1_000:
		if n >= 100_000 {
			return fmt.Sprintf("%dk", n/1000)
		}
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e3)) + "k"
	}
	return fmt.Sprint(n)
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

func costText(c *float64) string {
	if c == nil {
		return "—"
	}
	return fmt.Sprintf("$%.2f", *c)
}

func costCell(c *float64) core.Cell {
	if c == nil {
		return core.Cell{Text: "—", Txt: "-", Raw: "none", Tone: core.ToneDim}
	}
	return core.Cell{Text: costText(c), Raw: fmt.Sprintf("%.2f", *c)}
}

// quotaRow is one quota window: tool, window, used (a percentage, or the
// money of a spend limit), when it resets and whether the reading is old.
func quotaRow(q protocol.UsageQuota, now time.Time) []core.Cell {
	used, raw, tone := "—", "none", core.ToneDim
	switch {
	case q.UsedPct != nil:
		used, raw, tone = fmt.Sprintf("%.0f%%", *q.UsedPct), fmt.Sprintf("%.0f", *q.UsedPct), quotaTone(*q.UsedPct)
		if q.UsedUSD != nil && q.LimitUSD != nil {
			used += fmt.Sprintf(" ($%.2f of $%.2f)", *q.UsedUSD, *q.LimitUSD)
		}
	case q.UsedUSD != nil:
		used, raw, tone = fmt.Sprintf("$%.2f", *q.UsedUSD), fmt.Sprintf("%.2f", *q.UsedUSD), core.ToneNone
		if q.LimitUSD != nil {
			used += fmt.Sprintf(" of $%.2f", *q.LimitUSD)
		}
	}
	stale := q.Stale || now.Sub(q.At) > 15*time.Minute
	if stale {
		used += " (as of " + q.At.In(now.Location()).Format("15:04") + ")"
		tone = core.ToneDim
	}
	resets, resetsAt := "—", "none"
	if !q.ResetsAt.IsZero() {
		resets, resetsAt = untilShort(q.ResetsAt, now), q.ResetsAt.UTC().Format(time.RFC3339)
	}
	return []core.Cell{
		{Text: flatText(q.Tool)},
		{Text: flatText(q.Window)},
		{Text: used, Txt: dash(used), Raw: raw, Tone: tone},
		{Text: resets, Txt: dash(resets), Tone: core.ToneDim},
		{Text: resetsAt, Raw: resetsAt},
		{Text: fmt.Sprint(stale), Raw: stale},
	}
}

// quotaTone colors a used percentage: green below 60, yellow below 85, red
// from there.
func quotaTone(pct float64) core.Tone {
	switch {
	case pct < 60:
		return core.ToneDone
	case pct < 85:
		return core.ToneWarn
	}
	return core.ToneFailed
}

// untilShort formats the time left until t as 2h10m, 4d or 35m ("now" once
// it has passed).
func untilShort(t, now time.Time) string {
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd", int(d.Hours())/24)
}
