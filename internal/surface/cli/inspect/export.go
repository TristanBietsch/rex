package inspect

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/features/export"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// exportArgs is a parsed `rex export` command line.
type exportArgs struct {
	socket    *core.SocketFlag
	stateDir  string
	sel       string
	format    export.Format
	outPath   string // "" = stdout
	force     bool
	maxFrames int
	fromAgent bool
}

// parseExportArgs parses the arguments after `rex export`. The format is -o,
// else the --out extension (json when it has none we know), else json.
func parseExportArgs(args []string) (exportArgs, error) {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	a := exportArgs{socket: core.AddSocketFlag(fs)}
	fs.StringVar(&a.stateDir, "state-dir", "", "state dir (default ~/.local/share/rex; rarely needed)")
	formatName := fs.String("o", "", "format: json (default), yaml, md or txt; else from the --out extension")
	outSet := false
	fs.Func("out", "write the export to `file` (mode 0600) instead of stdout", func(v string) error {
		if v == "" {
			return errors.New("file name must not be empty")
		}
		outSet = true
		a.outPath = v
		return nil
	})
	fs.BoolVar(&a.force, "force", false, "replace the --out file if it exists")
	fs.IntVar(&a.maxFrames, "max-frames", 0, "keep only the last `N` frames (0 = all)")
	fs.BoolVar(&a.fromAgent, "from-agent-log", false, "read Claude Code's own conversation log (roles and text) instead of the transcript")
	rest, err := core.ParseArgs(fs, args)
	if err != nil {
		return a, err
	}
	if len(rest) != 1 {
		return a, core.Errorf(core.ExitInvalidArgs, "rex export <session> [-o json|yaml|md|txt] [--out FILE]",
			"export: exactly one session selector required")
	}
	a.sel = rest[0]
	switch {
	case a.maxFrames < 0:
		return a, core.Errorf(core.ExitInvalidArgs, "", "export: --max-frames %d: must not be negative", a.maxFrames)
	case a.force && !outSet:
		return a, core.Errorf(core.ExitInvalidArgs, "", "export: --force needs --out")
	}
	a.format = export.FormatJSON
	switch {
	case *formatName != "":
		if a.format, err = export.ParseFormat(*formatName); err != nil {
			return a, core.Errorf(core.ExitInvalidArgs, "", "export: %w", err)
		}
	case outSet:
		if f, ok := export.InferFormat(a.outPath); ok {
			a.format = f
		}
	}
	return a, nil
}

// RunExport writes one session as a readable document (json by default):
// its metadata and the transcript as clean text, to stdout or --out. Nothing
// leaves the machine.
func RunExport(args []string) error {
	a, err := parseExportArgs(args)
	if err != nil {
		return err
	}
	if err := core.RequireFeature("session_export"); err != nil {
		return err
	}
	if f := core.Out().Flags.Output; f != "" && f != "agent" {
		return core.Errorf(core.ExitInvalidArgs, "rex export <session> -o "+f,
			"export: -o %s goes after the command: it picks the export format", f)
	}
	c, err := a.socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, a.sel)
	if err != nil {
		return err
	}
	return doExport(a, sess)
}

// exportResult is what a --out export reports (json envelope, agent line).
type exportResult struct {
	Path   string `json:"path"`
	Format string `json:"format"`
	Source string `json:"source"`
	Frames int    `json:"frames"`
	Turns  int    `json:"turns"`
	Bytes  int    `json:"bytes"`
	Notice string `json:"notice,omitempty"`
}

// doExport builds, renders and writes the export of sess.
func doExport(a exportArgs, sess protocol.SessionSummary) error {
	root := a.stateDir
	if root == "" {
		var err error
		if root, err = paths.DataDir(); err != nil {
			return core.Errorf(core.ExitGeneric, "", "export: %w; pass -state-dir", err)
		}
	}
	doc, err := export.Build(export.Options{
		Session:        sess,
		TranscriptPath: filepath.Join(root, "sessions", sess.ID, "transcript.log"),
		MaxFrames:      a.maxFrames,
		FromAgentLog:   a.fromAgent,
	})
	if err != nil {
		return exportFailure(err)
	}
	body, err := export.Render(doc, a.format)
	if err != nil {
		return exportFailure(err)
	}
	p := core.Out()
	if doc.Notice != "" {
		_, _ = fmt.Fprintln(p.Err, "rex: export: "+doc.Notice)
	}
	if a.outPath == "" {
		if _, err := p.Out.Write(body); err != nil {
			return core.Errorf(core.ExitGeneric, "", "export: %w", err)
		}
		return nil
	}
	if err := export.WriteFile(a.outPath, body, a.force); err != nil {
		return exportFailure(err)
	}
	return reportExport(p, exportResult{
		Path: a.outPath, Format: string(a.format), Source: doc.Source,
		Frames: len(doc.Frames), Turns: len(doc.Turns), Bytes: len(body), Notice: doc.Notice,
	})
}

// exportFailure maps an export error to its exit code and hint.
func exportFailure(err error) error {
	switch {
	case errors.Is(err, export.ErrNoTranscript):
		return core.Errorf(core.ExitGeneric, "", "export: %w (a pruned session leaves nothing to export)", err)
	case errors.Is(err, export.ErrOutputTooLarge):
		return core.Errorf(core.ExitOperationRefused, "rex export <session> --max-frames 50",
			"export: %w; keep only the last frames with --max-frames N", err)
	case errors.Is(err, export.ErrTooLarge):
		return core.Errorf(core.ExitOperationRefused, "rex log <session>",
			"export: %w (over 512 MiB of transcript or 200000 agent-log messages); read it with rex log instead", err)
	case errors.Is(err, export.ErrSymlink), errors.Is(err, export.ErrExists):
		return core.Errorf(core.ExitOperationRefused, "", "export: %w", err)
	}
	return core.Errorf(core.ExitGeneric, "", "%w", err)
}

// reportExport prints what a --out export wrote, in the printer's format.
func reportExport(p *core.Printer, r exportResult) error {
	pairs := []core.Pair{
		{Key: "path", Label: "Wrote", Val: core.Cell{Text: r.Path}},
		{Key: "format", Label: "Format", Val: core.Cell{Text: r.Format}},
		{Key: "source", Label: "Source", Val: core.Cell{Text: r.Source}},
		{Key: "frames", Label: "Frames", Val: core.Cell{Text: fmt.Sprint(r.Frames), Raw: r.Frames}},
		{Key: "turns", Label: "Turns", Val: core.Cell{Text: fmt.Sprint(r.Turns), Raw: r.Turns}},
		{Key: "bytes", Label: "Bytes", Val: core.Cell{Text: fmt.Sprint(r.Bytes), Raw: r.Bytes}},
	}
	v := core.View{core.KV{Kind: "export", Pairs: pairs}}
	if r.Notice != "" {
		v = append(v, core.Note{Tone: core.ToneWarn, Text: r.Notice})
	}
	v = append(v, core.Note{Tone: core.ToneDim, Text: "the file holds whatever the agent printed, secrets included; keep it private"})
	return p.Emit(core.Result{Cmd: "export", Data: r, View: v})
}
