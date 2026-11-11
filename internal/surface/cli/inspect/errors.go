package inspect

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunErrors shows the CLI error log, newest first (see core.RecordError).
//
//	rex errors [-n 20] [-since 24h] [-clear] [-o FORMAT]
func RunErrors(args []string) error {
	fs := flag.NewFlagSet("errors", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	n := fs.Int("n", 20, "show at most this many records (0 = all)")
	since := fs.String("since", "", "only records newer than this, like 90m, 24h or 7d")
	clearLog := fs.Bool("clear", false, "delete the log and its backup")
	var force bool
	fs.BoolVar(&force, "force", false, "do not ask before -clear (required without a terminal)")
	fs.BoolVar(&force, "yes", false, "alias for -force")
	fs.BoolVar(&force, "y", false, "alias for -force")
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return core.NewExitError(core.ExitInvalidArgs, "errors: takes no arguments")
	}
	if *n < 0 {
		return core.NewExitError(core.ExitInvalidArgs, fmt.Sprintf("errors: -n %d: must not be negative", *n))
	}
	cutoff, err := parseSinceCutoff(*since, time.Now())
	if err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	path, err := core.ErrLogPath()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	p := core.Out()
	if *clearLog {
		return clearErrors(p, path, force)
	}

	recs, err := core.ReadErrLog(path)
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	recs = filterErrors(recs, cutoff, *n)
	return p.Emit(core.Result{Cmd: "errors", Data: nonNilRecords(recs), View: errorsView(recs)})
}

func nonNilRecords(r []core.ErrLogRecord) []core.ErrLogRecord {
	if r == nil {
		return []core.ErrLogRecord{}
	}
	return r
}

// parseSinceCutoff turns "-since 24h" (or 90m, 7d) into the oldest time to
// show; empty means no limit.
func parseSinceCutoff(since string, now time.Time) (time.Time, error) {
	if since == "" {
		return time.Time{}, nil
	}
	d, err := parseSpan(since)
	if err != nil || d <= 0 {
		return time.Time{}, fmt.Errorf("errors: -since must be like 90m, 24h or 7d, got %q", since)
	}
	return now.Add(-d), nil
}

// parseSpan reads a Go duration, or Nd for N days.
func parseSpan(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// filterErrors keeps the records at or after cutoff (zero keeps all) and at
// most n of them (0 keeps all). recs is newest first.
func filterErrors(recs []core.ErrLogRecord, cutoff time.Time, n int) []core.ErrLogRecord {
	var out []core.ErrLogRecord
	for _, r := range recs {
		if !cutoff.IsZero() {
			if t, err := time.Parse(time.RFC3339, r.Time); err == nil && t.Before(cutoff) {
				continue
			}
		}
		out = append(out, r)
		if n > 0 && len(out) == n {
			break
		}
	}
	return out
}

func clearErrors(p *core.Printer, path string, force bool) error {
	if !force {
		ok, err := p.Confirm("delete the CLI error log?", core.Refusal{Cmd: "errors", Flag: "-force", What: "clear",
			Hint: "rex errors -clear -force"})
		if err != nil {
			return err
		}
		if !ok {
			return core.Errorf(core.ExitOperationRefused, "", "errors: not confirmed, nothing deleted")
		}
	}
	if err := core.ClearErrLog(path); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: "cleared the CLI error log", Silent: true}
	return p.Emit(core.Result{Cmd: "errors", Data: struct {
		Cleared bool `json:"cleared"`
	}{true}, View: core.ActionView(line, core.Field{Key: "cleared", Val: true})})
}

// errorsView lists the records: TIME EXIT CMD ERROR in text, txt and md, and
// one `error_entry` line per record in agent mode.
func errorsView(recs []core.ErrLogRecord) core.View {
	human := core.SetOf(core.FormatText, core.FormatTXT, core.FormatMD)
	agent := core.SetOf(core.FormatAgent)
	rows := make([][]core.Cell, len(recs))
	for i, r := range recs {
		shown, utc := r.Time, r.Time
		if t, err := time.Parse(time.RFC3339, r.Time); err == nil {
			shown, utc = t.Local().Format("2006-01-02 15:04:05"), t.UTC().Format(time.RFC3339)
		}
		rows[i] = []core.Cell{
			{Text: shown}, {Text: utc}, {Text: strconv.Itoa(r.Exit), Raw: r.Exit}, {Text: r.Kind}, {Text: r.Cmd},
			{Text: strings.Join(r.Args, " ")}, {Text: r.Error}, {Text: r.Error}, {Text: r.Hint},
		}
	}
	v := core.View{core.Header{Title: "Errors", Meta: fmt.Sprintf("%d logged", len(recs))},
		core.Table{Kind: "error_entry", Empty: "(no errors logged)", Rows: rows, Cols: []core.Col{
			{Key: "time", Title: "Time", Formats: human},
			{Key: "time", Title: "Time", Formats: agent},
			{Key: "exit", Title: "Exit", Align: core.AlignRight},
			{Key: "kind", Title: "Kind", Formats: agent},
			{Key: "cmd", Title: "Cmd"},
			{Key: "args", Title: "Args", Formats: agent},
			{Key: "msg", Title: "Error", Flex: true, Min: 12, Formats: human},
			{Key: "msg", Title: "Error", Formats: agent},
			{Key: "hint", Title: "Hint", Formats: agent},
		}}}
	if len(recs) > 0 {
		v = append(v, core.Hint{Cmd: "rex errors -clear", Text: "delete the log"})
	}
	return v
}
