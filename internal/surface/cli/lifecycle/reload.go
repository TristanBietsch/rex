package lifecycle

import (
	"errors"
	"flag"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunReload asks the rex-daemon on the default socket to re-read tools.yaml
// with a Reload intent. It never signals a process; nothing answering on
// the socket exits ExitDaemonUnreachable.
func RunReload(args []string) error {
	fs := flag.NewFlagSet("reload", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	_, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if err := daemonctl.Reload(); err != nil {
		if errors.Is(err, daemonctl.ErrNotRunning) {
			return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
		}
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	p := core.Out()
	// txt stays silent, as it always was.
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: "reloaded tools.yaml", Silent: true}
	return p.Emit(core.Result{Cmd: "reload", Data: reloadData{Reloaded: true}, View: core.ActionView(line, core.Field{Key: "reloaded", Val: true})})
}

// reloadData is the `-o json` payload of rex reload.
type reloadData struct {
	Reloaded bool `json:"reloaded"`
}
