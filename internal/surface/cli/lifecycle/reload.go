package lifecycle

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunReload asks the rex-daemon on the default socket to reload every layer
// (tools.yaml, config.yaml, config.lua, init.lua, the model cache) with a
// Reload intent and prints its report. It never signals a process; nothing
// answering on the socket exits ExitDaemonUnreachable, and a layer that
// failed exits ExitGeneric after the report.
func RunReload(args []string) error {
	fs := flag.NewFlagSet("reload", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	socket, err := paths.Socket()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return reloader{socket: socket, out: core.Out().Out, errOut: core.Out().Err, reload: daemonctl.ReloadDaemonReport}.run()
}

// reloader is rex reload; reload is daemonctl.ReloadDaemonReport outside
// tests.
type reloader struct {
	socket      string
	out, errOut io.Writer
	reload      func(ctx context.Context, socket string) (protocol.ReloadReport, error)
}

func (r reloader) run() error {
	rep, err := r.reload(context.Background(), r.socket)
	if err != nil {
		if errors.Is(err, daemonctl.ErrNotRunning) {
			return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
		}
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	p := printerFor(r.out, r.errOut)
	data := reloadData{Reloaded: len(rep.Errors) == 0, ReloadReport: rep}
	if rep.Legacy {
		// txt stays silent, as it always was.
		line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: "reloaded (this daemon predates reload reports)", Silent: true}
		return p.Emit(core.Result{Cmd: "reload", Data: data, View: core.ActionView(line, core.Field{Key: "reloaded", Val: true})})
	}
	view := reloadView(p, rep)
	if err := p.Emit(core.Result{Cmd: "reload", Data: data, View: view}); err != nil {
		return err
	}
	if len(rep.Errors) > 0 {
		// The failed layers go to stderr as the error (rex: ...), one per
		// line; the rest of the report is on stdout.
		return core.NewExitError(core.ExitGeneric, "rex-daemon could not reload: "+strings.Join(rep.Errors, "\n"))
	}
	return nil
}

// reloadView is the text of a reload report: what was read, what changed,
// what needs a daemon restart, then each error and warning.
func reloadView(p *core.Printer, rep protocol.ReloadReport) core.View {
	var view core.View
	head := "reloaded " + strings.Join(rep.Reloaded, ", ")
	if len(rep.Reloaded) == 0 {
		head = "reload read no layer"
	}
	tone, glyph := core.ToneOK, p.Glyphs.OK
	if len(rep.Errors) > 0 {
		tone, glyph = core.ToneWarn, p.Glyphs.Warn
	}
	view = append(view, core.Line{Tone: tone, Glyph: glyph, Text: head, Silent: true})
	if len(rep.Changed) == 0 {
		view = append(view, core.Line{Text: "no setting changed", Silent: true})
	} else {
		view = append(view, core.Line{Text: "changed: " + strings.Join(rep.Changed, ", ")})
	}
	if len(rep.Restart) > 0 {
		view = append(view, core.Line{Tone: core.ToneWarn, Glyph: p.Glyphs.Warn, Text: "needs a daemon restart: " + strings.Join(rep.Restart, ", ") + " (rex daemon restart keeps sessions)"})
	}
	for _, w := range rep.Warnings {
		view = append(view, core.Line{Tone: core.ToneWarn, Glyph: p.Glyphs.Warn, Text: w})
	}
	return view
}

// reloadData is the `-o json` payload of rex reload.
type reloadData struct {
	Reloaded bool `json:"reloaded"`
	protocol.ReloadReport
}
