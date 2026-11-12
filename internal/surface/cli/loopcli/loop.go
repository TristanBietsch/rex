// Package loopcli is `rex loop`: list, start, pause, resume and stop repeat
// loops, read a loop's log, and write a starter loop file.
package loopcli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/features/loop"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// loopTimeout bounds one loop intent's round trip.
const loopTimeout = 30 * time.Second

// defaultLogLines is how many log lines `rex loop log` prints.
const defaultLogLines = 200

// runEditor opens the starter file in $EDITOR. Tests replace it.
var runEditor = func(argv []string, stdout, stderr io.Writer) error {
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec,noctx // G702: $EDITOR is the user's own program; noctx: the editor runs until the user quits it.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	return cmd.Run()
}

// stdinIsTerminal says whether to open the editor. Tests replace it.
var stdinIsTerminal = core.StdinIsTerminal

var shortIDRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// RunLoop dispatches rex loop subcommands:
//
//	ls                          loop files and loops
//	start <name> [-cwd DIR]     start a loop
//	pause|resume|stop <loop-id>
//	log <loop-id> [-n N]        the loop's log
//	new <name>                  write a starter loop file
func RunLoop(args []string) error {
	fs := flag.NewFlagSet("loop", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	cwd := fs.String("cwd", "", "folder the loop runs in (start); default: the file's cwd, else the current folder")
	lines := fs.Int("n", defaultLogLines, "log: how many lines from the end")
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return core.Errorf(core.ExitInvalidArgs, "rex loop -h", "loop: subcommand required (ls|start|pause|resume|stop|log|new)")
	}
	sub, rest := rest[0], rest[1:]
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["cwd"] && sub != "start" {
		return core.Errorf(core.ExitInvalidArgs, "rex loop -h", "loop %s: -cwd only applies to start", sub)
	}
	if set["n"] && sub != "log" {
		return core.Errorf(core.ExitInvalidArgs, "rex loop -h", "loop %s: -n only applies to log", sub)
	}
	switch sub {
	case "ls", "start", "pause", "resume", "stop", "log", "new":
	default:
		return core.Errorf(core.ExitInvalidArgs, "rex loop -h", "loop: unknown subcommand %q (ls|start|pause|resume|stop|log|new)", sub)
	}
	wantArgs := map[string]int{"ls": 0, "start": 1, "pause": 1, "resume": 1, "stop": 1, "log": 1, "new": 1}[sub]
	if len(rest) != wantArgs {
		return core.Errorf(core.ExitInvalidArgs, "rex loop -h", "loop %s: expected %d argument(s), got %d", sub, wantArgs, len(rest))
	}
	if err := core.RequireFeature("loops"); err != nil {
		return err
	}
	switch sub {
	case "ls":
		return runLS(socket)
	case "start":
		return runStart(socket, rest[0], *cwd)
	case "pause", "resume", "stop":
		return runControl(socket, sub, rest[0])
	case "log":
		return runLog(rest[0], *lines)
	default:
		return runNew(rest[0])
	}
}

// loopID accepts "loop-1a2b3c4d" or just the 8 hex digits.
func loopID(arg string) (string, error) {
	if shortIDRE.MatchString(arg) {
		arg = "loop-" + arg
	}
	if !protocol.ValidLoopID(arg) {
		return "", core.Errorf(core.ExitInvalidArgs, "rex loop ls", "loop: %q is not a loop id (like loop-1a2b3c4d; see `rex loop ls`)", arg)
	}
	return arg, nil
}

// --- ls ---

// fileRow is one loop file in the `-o json` payload of rex loop ls.
type fileRow struct {
	Name  string `json:"name"`
	Kind  string `json:"kind,omitempty"`
	Tool  string `json:"tool,omitempty"`
	Model string `json:"model,omitempty"`
	// Check is the check command, joined for display.
	Check         string `json:"check,omitempty"`
	Marker        string `json:"marker,omitempty"`
	MaxIterations int    `json:"max_iterations,omitempty"`
	Error         string `json:"error,omitempty"`
}

// lsData is the `-o json` payload of rex loop ls.
type lsData struct {
	Dir   string                 `json:"dir"`
	Files []fileRow              `json:"files"`
	Loops []protocol.LoopSummary `json:"loops"`
	// Daemon is "ok", "unreachable" or "old" (it does not know loops).
	Daemon string `json:"daemon"`
}

func runLS(socket *core.SocketFlag) error {
	dir, err := paths.LoopsDir()
	if err != nil {
		return core.Errorf(core.ExitGeneric, "", "loop: %w", err)
	}
	entries, err := loop.List(dir)
	if err != nil {
		return core.Errorf(core.ExitGeneric, "", "loop: %w", err)
	}
	data := lsData{Dir: dir, Files: []fileRow{}, Loops: []protocol.LoopSummary{}, Daemon: "ok"}
	for _, e := range entries {
		row := fileRow{Name: e.Name}
		if e.Err != nil {
			row.Error = e.Err.Error()
		} else {
			row.Kind, row.Tool, row.Model = e.Def.Kind, e.Def.Tool, e.Def.Model
			row.Check, row.Marker = strings.Join(e.Def.Until.Check, " "), e.Def.Until.Marker
			row.MaxIterations = e.Def.Until.MaxIterations
		}
		data.Files = append(data.Files, row)
	}
	var note string
	if c, derr := socket.Dial(); derr != nil {
		data.Daemon, note = "unreachable", "rex-daemon is not running: no loops shown (rex daemon start)"
	} else {
		defer c.Close()
		snap, herr := c.Hello("rex-cli")
		if herr != nil {
			return core.HelloFailed(herr)
		}
		data.Loops = append(data.Loops, snap.Loops...)
		if snap.DaemonVersion == "" {
			data.Daemon, note = "old", "rex-daemon predates loops; run `rex daemon restart`"
		}
	}
	return core.Out().Emit(core.Result{Cmd: "loop", Data: data, View: lsView(data, note, core.Out().Now())})
}

func lsView(d lsData, note string, now time.Time) core.View {
	fileCols := []core.Col{
		{Key: "name", Title: "File", Max: 24},
		{Key: "tool", Title: "Tool", Max: 12},
		{Key: "model", Title: "Model", Max: 16},
		{Key: "stop", Title: "Stops when", Flex: true, Min: 12},
		{Key: "max", Title: "Max", Align: core.AlignRight},
	}
	var fileRows [][]core.Cell
	for _, f := range d.Files {
		if f.Error != "" {
			fileRows = append(fileRows, []core.Cell{{Text: f.Name}, {Text: ""}, {Text: ""}, {Text: "invalid: " + f.Error, Tone: core.ToneErr}, {Text: ""}})
			continue
		}
		stop := []string{}
		if f.Check != "" {
			stop = append(stop, "`"+f.Check+"` exits 0")
		}
		if f.Marker != "" {
			stop = append(stop, "line "+f.Marker)
		}
		fileRows = append(fileRows, []core.Cell{{Text: f.Name}, {Text: f.Tool}, {Text: f.Model},
			{Text: strings.Join(stop, " or ")}, {Text: fmt.Sprint(f.MaxIterations)}})
	}
	loopCols := []core.Col{
		{Key: "id", Title: "Loop", Max: 13},
		{Key: "name", Title: "File", Max: 24},
		{Key: "state", Title: "State"},
		{Key: "iter", Title: "Turn", Align: core.AlignRight},
		{Key: "why", Title: "Why", Flex: true, Min: 10},
		{Key: "age", Title: "Updated", Align: core.AlignRight},
	}
	var loopRows [][]core.Cell
	for _, l := range d.Loops {
		why := l.PauseReason
		if why == "" {
			why = l.StopReason
		}
		loopRows = append(loopRows, []core.Cell{{Text: l.ID}, {Text: l.Name}, {Text: string(l.State), Tone: loopTone(l.State)},
			{Text: fmt.Sprintf("%d/%d", l.Iteration, l.MaxIterations)}, {Text: why}, {Text: age(now, l.UpdatedAt)}})
	}
	v := core.View{
		core.Header{Title: "Loops", Meta: d.Dir},
		core.Table{Kind: "loop_file", Cols: fileCols, Rows: fileRows, Empty: "(no loop files; rex loop new <name>)"},
		core.Table{Kind: "loop", Cols: loopCols, Rows: loopRows, Empty: "(no loops)"},
	}
	if note != "" {
		v = append(v, core.Note{Tone: core.ToneWarn, Text: note})
	}
	for _, l := range d.Loops {
		if l.Notice != "" {
			v = append(v, core.Note{Text: l.ID + ": " + l.Notice})
		}
	}
	return v
}

func loopTone(s protocol.LoopState) core.Tone {
	switch s {
	case protocol.LoopDone:
		return core.ToneOK
	case protocol.LoopFailed:
		return core.ToneErr
	case protocol.LoopPaused, protocol.LoopCapped:
		return core.ToneWarn
	case protocol.LoopRunning, protocol.LoopStopped:
		return core.ToneNone
	}
	return core.ToneNone
}

func age(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// --- start, pause, resume, stop ---

func runStart(socket *core.SocketFlag, name, cwdFlag string) error {
	if !loop.ValidName(name) {
		return core.Errorf(core.ExitInvalidArgs, "rex loop ls", "loop start: %q is not a loop name (lowercase letters, digits, '.', '_', '-')", name)
	}
	dir, err := paths.LoopsDir()
	if err != nil {
		return core.Errorf(core.ExitGeneric, "", "loop: %w", err)
	}
	// Read the file here too, so a typo or an unsafe file fails before the
	// daemon is asked, and to default the folder.
	def, err := loop.LoadFile(dir, name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return core.Errorf(core.ExitSelectorNotFound, "rex loop ls", "loop start: no loop file %s", loop.FilePath(dir, name))
		}
		return core.Errorf(core.ExitInvalidArgs, "rex loop ls", "loop start: %w", err)
	}
	cwd := cwdFlag
	if cwd == "" {
		cwd = def.CWD
	}
	if cwd == "" {
		if cwd, err = os.Getwd(); err != nil {
			return core.Errorf(core.ExitGeneric, "rex loop start "+name+" -cwd DIR", "loop start: cannot read the current folder: %w", err)
		}
	}
	if !filepath.IsAbs(cwd) || slices.Contains(strings.FieldsFunc(cwd, func(r rune) bool { return r == '/' || r == '\\' }), "..") {
		return core.Errorf(core.ExitInvalidArgs, "rex loop start "+name+" -cwd /abs/path", "loop start: -cwd must be an absolute path without \"..\"")
	}
	return callLoop(socket, "start", func(ctx context.Context, c *client.Client) (protocol.LoopSummary, error) {
		return c.StartLoop(ctx, protocol.StartLoop{Name: name, CWD: cwd})
	})
}

func runControl(socket *core.SocketFlag, verb, arg string) error {
	id, err := loopID(arg)
	if err != nil {
		return err
	}
	return callLoop(socket, verb, func(ctx context.Context, c *client.Client) (protocol.LoopSummary, error) {
		switch verb {
		case "pause":
			return c.PauseLoop(ctx, id)
		case "resume":
			return c.ResumeLoop(ctx, id)
		}
		return c.StopLoop(ctx, id)
	})
}

func callLoop(socket *core.SocketFlag, verb string, do func(context.Context, *client.Client) (protocol.LoopSummary, error)) error {
	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := c.Hello("rex-cli"); err != nil {
		return core.HelloFailed(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), loopTimeout)
	defer cancel()
	l, err := do(ctx, c)
	if err != nil {
		return loopError(verb, err)
	}
	past := map[string]string{"start": "started", "pause": "paused", "resume": "resumed", "stop": "stopped"}[verb]
	p := core.Out()
	text := fmt.Sprintf("%s %s %s", past, l.ID, l.Name)
	if l.State == protocol.LoopPaused && l.PauseReason != "" {
		text += " (paused: " + l.PauseReason + ")"
	}
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: text, Txt: l.ID}
	v := core.ActionView(line,
		core.Field{Key: "loop", Val: l.ID}, core.Field{Key: "state", Val: string(l.State)},
		core.Field{Key: "session", Val: l.SessionID}, core.Field{Key: "iteration", Val: l.Iteration})
	if l.Notice != "" {
		v = append(v, core.Note{Tone: core.ToneWarn, Text: l.Notice})
	}
	return p.Emit(core.Result{Cmd: "loop", Data: l, View: v})
}

// loopError turns a daemon failure into the CLI's error: an older daemon is
// told to restart, the feature-off refusal is the standard message, and a
// refusal of the request is exit 6.
func loopError(verb string, err error) error {
	var de *client.Error
	switch {
	case errors.Is(err, client.ErrNoAcks),
		errors.As(err, &de) && de.Code == protocol.ErrCodeBadIntent && de.Message == "intent not implemented":
		return core.Errorf(core.ExitOperationRefused, "rex daemon restart",
			"rex-daemon is older than rex and does not run loops; run `rex daemon restart`")
	case errors.As(err, &de) && de.Message == loop.MsgOff:
		return core.OffError("loops")
	case errors.As(err, &de) && (de.Code == protocol.ErrCodeBadState || de.Code == protocol.ErrCodeBadIntent ||
		de.Code == protocol.ErrCodeTooManySessions || de.Code == protocol.ErrCodeSpawn):
		return core.Errorf(core.ExitOperationRefused, "rex loop ls", "loop %s: %s", verb, de.Message)
	}
	return err
}

// --- log ---

type logData struct {
	LoopID string `json:"loop_id"`
	Lines  int    `json:"lines"`
	Log    string `json:"log"`
}

func runLog(arg string, n int) error {
	id, err := loopID(arg)
	if err != nil {
		return err
	}
	if n < 1 {
		return core.Errorf(core.ExitInvalidArgs, "rex loop log "+id+" -n 50", "loop log: -n must be at least 1")
	}
	dir, err := paths.LoopStateDir()
	if err != nil {
		return core.Errorf(core.ExitGeneric, "", "loop: %w", err)
	}
	b, err := os.ReadFile(loop.LogPath(dir, id))
	if errors.Is(err, os.ErrNotExist) {
		return core.Errorf(core.ExitSelectorNotFound, "rex loop ls", "loop log: no log for %s", id)
	}
	if err != nil {
		return core.Errorf(core.ExitGeneric, "", "loop log: %w", err)
	}
	all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	tail := all[max(len(all)-n, 0):]
	text := strings.Join(tail, "\n")
	return core.Out().Emit(core.Result{
		Cmd: "loop", Data: logData{LoopID: id, Lines: len(tail), Log: text},
		View: core.View{core.Header{Title: "Loop log", Meta: id}, core.Pre{Text: text}},
	})
}

// --- new ---

type newData struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func runNew(name string) error {
	if !loop.ValidName(name) {
		return core.Errorf(core.ExitInvalidArgs, "rex loop new my-loop", "loop new: %q is not a loop name (lowercase letters, digits, '.', '_', '-')", name)
	}
	dir, err := paths.LoopsDir()
	if err != nil {
		return core.Errorf(core.ExitGeneric, "", "loop: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return core.Errorf(core.ExitGeneric, "", "loop new: %w", err)
	}
	path := loop.FilePath(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return core.Errorf(core.ExitOperationRefused, "rex loop start "+name, "loop new: %s already exists", path)
	}
	if err != nil {
		return core.Errorf(core.ExitGeneric, "", "loop new: %w", err)
	}
	_, werr := f.WriteString(loop.Starter(name))
	if err := errors.Join(werr, f.Close()); err != nil {
		_ = os.Remove(path)
		return core.Errorf(core.ExitGeneric, "", "loop new: %w", err)
	}
	p := core.Out()
	if editor := strings.Fields(os.Getenv("EDITOR")); len(editor) > 0 && stdinIsTerminal() {
		if err := runEditor(append(editor, path), p.Out, p.Err); err != nil {
			return core.Errorf(core.ExitGeneric, "", "loop new: wrote %s but the editor failed: %w", path, err)
		}
	}
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: "wrote " + path, Txt: path}
	return p.Emit(core.Result{Cmd: "loop", Data: newData{Name: name, Path: path},
		View: append(core.ActionView(line, core.Field{Key: "path", Val: path}),
			core.Hint{Cmd: "rex loop start " + name, Text: "start it"})})
}
