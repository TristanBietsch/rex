// Package workspace is the rex ws command: create and inspect workspaces
// (project folders marked with a .rex/ identity). The domain logic lives in
// internal/workspace; this package parses arguments and renders results.
package workspace

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
	wsp "github.com/tristanbietsch/rex/internal/workspace"
)

const usageLine = "rex ws init [path] [--name N] | id [path] | ls | rename <id|path> <name> | color <id|path> <#RRGGBB|swatch-number> | open [path]"

// info is the json payload of a workspace.
type info struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Root        string `json:"root"`
	Created     bool   `json:"created,omitempty"`
	GitExcluded bool   `json:"git_excluded,omitempty"`
}

// lsRow is the json payload of one registry row.
type lsRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Root     string `json:"root"`
	LastSeen string `json:"last_seen"`
	Present  bool   `json:"present"`
}

// env is what the subcommands read from the machine.
type env struct {
	home string
	cwd  string
	reg  *wsp.Registry
}

func newEnv() (env, error) {
	home, err := paths.Home()
	if err != nil {
		return env{}, core.Errorf(core.ExitGeneric, "", "ws: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return env{}, core.Errorf(core.ExitGeneric, "", "ws: %w", err)
	}
	regPath, err := paths.WorkspacesFile()
	if err != nil {
		return env{}, core.Errorf(core.ExitGeneric, "", "ws: %w", err)
	}
	reg, err := wsp.OpenRegistry(regPath)
	if err != nil {
		return env{}, core.Errorf(core.ExitGeneric, "", "ws: %w", err)
	}
	return env{home: home, cwd: cwd, reg: reg}, nil
}

// RunWs runs `rex ws <init|id|ls|rename|color|open>`. Exit 5 while the workspaces
// setting is off; nothing is read or written then.
func RunWs(args []string) error {
	if len(args) == 0 {
		return core.Errorf(core.ExitInvalidArgs, "rex ws -h", "ws: subcommand required: %s", usageLine)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "-h", "--help", "help":
		fmt.Fprintln(core.Out().Err, "usage: "+usageLine)
		return core.ErrHelpShown
	case "open":
		return openWs(rest)
	case "init", "id", "ls", "rename", "color":
	default:
		return core.Errorf(core.ExitInvalidArgs, "rex ws -h", "ws: unknown subcommand %q: %s", sub, usageLine)
	}

	fs := flag.NewFlagSet("ws "+sub, flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	var name string
	if sub == "init" {
		fs.StringVar(&name, "name", "", "workspace name (default: the folder's name)")
	}
	pos, err := out.Parse(fs, rest)
	if err != nil {
		return err
	}
	if err := checkArity(sub, pos); err != nil {
		return err
	}
	if err := core.RequireFeature("workspaces"); err != nil {
		return err
	}
	e, err := newEnv()
	if err != nil {
		return err
	}
	switch sub {
	case "init":
		return e.init(pos, name)
	case "id":
		return e.id(pos)
	case "ls":
		return e.ls()
	case "rename":
		return e.rename(pos[0], pos[1])
	default:
		return e.color(pos[0], pos[1])
	}
}

func checkArity(sub string, pos []string) error {
	want := map[string][2]int{"init": {0, 1}, "id": {0, 1}, "ls": {0, 0}, "rename": {2, 2}, "color": {2, 2}}[sub]
	if len(pos) < want[0] || len(pos) > want[1] {
		return core.Errorf(core.ExitInvalidArgs, "rex ws -h", "ws %s: wrong number of arguments: %s", sub, usageLine)
	}
	return nil
}

// absPath makes p absolute against cwd ("" is cwd) without resolving links.
func (e env) absPath(p string) (string, error) { return wsp.AbsPath(p, e.cwd) }

func (e env) init(pos []string, name string) error {
	arg := ""
	if len(pos) > 0 {
		arg = pos[0]
	}
	root, err := e.absPath(arg)
	if err != nil {
		return core.Errorf(core.ExitInvalidArgs, "", "ws init: %w", err)
	}
	before, err := wsp.Load(root)
	if err != nil {
		return wsError("ws init", err)
	}
	w, err := wsp.Init(root, name, e.reg)
	if err != nil {
		return wsError("ws init", err)
	}
	excluded, err := wsp.ExcludeInGit(w.Root)
	if err != nil {
		return wsError("ws init", err)
	}
	data := info{ID: w.ID, Name: w.Name, Color: w.Color, Root: w.Root, Created: before == nil, GitExcluded: excluded}
	verb := "created"
	if before != nil {
		verb = "already a workspace:"
	}
	v := core.View{
		core.Line{Tone: core.ToneOK, Glyph: "✓", Text: fmt.Sprintf("%s %s %s", verb, w.ID, flat(w.Name)), Txt: w.ID},
		infoKV(data),
	}
	if excluded {
		v = append(v, core.Note{Tone: core.ToneDim, Text: "added /.rex/ to .git/info/exclude"})
	}
	return core.Out().Emit(core.Result{Cmd: "ws init", Data: data, View: v})
}

func (e env) id(pos []string) error {
	arg := ""
	if len(pos) > 0 {
		arg = pos[0]
	}
	dir, err := e.absPath(arg)
	if err != nil {
		return core.Errorf(core.ExitInvalidArgs, "", "ws id: %w", err)
	}
	w, err := wsp.Find(dir, e.home)
	if err != nil {
		return wsError("ws id", err)
	}
	if w == nil {
		return core.Errorf(core.ExitSelectorNotFound, "rex ws init", "ws id: no .rex/ here — run `rex ws init`")
	}
	// Best effort: a workspace seen at a new folder moves its registry root.
	_ = e.reg.Seen(w)
	data := info{ID: w.ID, Name: w.Name, Color: w.Color, Root: w.Root}
	return core.Out().Emit(core.Result{Cmd: "ws id", Data: data, View: core.View{infoKV(data)}})
}

func (e env) ls() error {
	es, err := e.reg.List()
	if err != nil {
		return wsError("ws ls", err)
	}
	rows := make([]lsRow, 0, len(es))
	cells := make([][]core.Cell, 0, len(es))
	for _, en := range es {
		_, serr := os.Lstat(filepath.Join(en.Root, wsp.DirName, wsp.FileName))
		r := lsRow{ID: en.ID, Name: en.Name, Color: en.Color, Root: en.Root,
			LastSeen: en.LastSeen.UTC().Format("2006-01-02T15:04:05Z"), Present: serr == nil}
		rows = append(rows, r)
		root := flat(r.Root)
		if !r.Present {
			root += " (missing)"
		}
		cells = append(cells, []core.Cell{
			{Text: r.ID}, {Text: flat(r.Name)}, {Text: r.Color},
			{Text: root, Raw: r.Root}, {Text: r.LastSeen[:10], Raw: r.LastSeen}, {Text: strconv.FormatBool(r.Present), Raw: r.Present},
		})
	}
	v := core.View{core.Table{Kind: "workspace", Empty: "no workspaces yet: run `rex ws init` in a project folder",
		Cols: []core.Col{
			{Key: "id", Title: "ID"},
			{Key: "name", Title: "Name", Flex: true, Min: 8, Max: 24},
			{Key: "color", Title: "Color"},
			{Key: "root", Title: "Folder", Flex: true, Min: 12},
			{Key: "last_seen", Title: "Last seen"},
			{Key: "present", Title: "Present", Formats: core.SetOf(core.FormatAgent)},
		},
		Rows: cells}}
	return core.Out().Emit(core.Result{Cmd: "ws ls", Data: rows, View: v})
}

// resolve finds the workspace named by ref: an 8-hex id (looked up in the
// registry) or a folder (the workspace around it).
func (e env) resolve(cmd, ref string) (*wsp.Workspace, error) {
	w, err := wsp.Resolve(ref, e.cwd, e.home, e.reg)
	if err != nil {
		return nil, wsError(cmd, err)
	}
	return w, nil
}

func (e env) rename(ref, newName string) error {
	w, err := e.resolve("ws rename", ref)
	if err != nil {
		return err
	}
	clean := wsp.CleanName(newName)
	if clean == "" {
		return core.Errorf(core.ExitInvalidArgs, "", "ws rename: name must not be empty")
	}
	w.Name = clean
	return e.save("ws rename", w)
}

func (e env) color(ref, c string) error {
	w, err := e.resolve("ws color", ref)
	if err != nil {
		return err
	}
	sw := wsp.Swatches()
	switch {
	case wsp.ValidColor(c):
		w.Color = strings.ToUpper(c)
	default:
		n, nerr := strconv.Atoi(c)
		if nerr != nil || n < 1 || n > len(sw) {
			return core.Errorf(core.ExitInvalidArgs, "", "ws color: %q is not #RRGGBB or a swatch number 1-%d (%s)",
				flat(c), len(sw), strings.Join(sw, " "))
		}
		w.Color = sw[n-1]
	}
	return e.save("ws color", w)
}

func (e env) save(cmd string, w *wsp.Workspace) error {
	if err := wsp.Save(w); err != nil {
		return wsError(cmd, err)
	}
	if err := e.reg.Seen(w); err != nil {
		return wsError(cmd, err)
	}
	data := info{ID: w.ID, Name: w.Name, Color: w.Color, Root: w.Root}
	return core.Out().Emit(core.Result{Cmd: cmd, Data: data, View: core.View{
		core.Line{Tone: core.ToneOK, Glyph: "✓", Text: fmt.Sprintf("%s %s", w.ID, flat(w.Name)), Txt: w.ID},
		infoKV(data),
	}})
}

func infoKV(d info) core.KV {
	return core.KV{Kind: "workspace", Pairs: []core.Pair{
		{Key: "id", Label: "id", Val: core.Cell{Text: d.ID}},
		{Key: "name", Label: "name", Val: core.Cell{Text: flat(d.Name)}},
		{Key: "color", Label: "color", Val: core.Cell{Text: d.Color}},
		{Key: "root", Label: "folder", Val: core.Cell{Text: flat(d.Root), Raw: d.Root}},
	}}
}

// wsError maps a workspace package error to the CLI's exit codes.
func wsError(cmd string, err error) error {
	switch {
	case errors.Is(err, wsp.ErrNotFound):
		return core.Errorf(core.ExitSelectorNotFound, "rex ws ls", "%s: %w", cmd, err)
	case errors.Is(err, wsp.ErrMoved):
		return core.Errorf(core.ExitSelectorNotFound, "rex ws ls", "%s: %w; run `rex ws id` inside its folder", cmd, err)
	case errors.Is(err, wsp.ErrRefusedRoot):
		return core.Errorf(core.ExitInvalidArgs, "", "%s: refusing to use your home folder or / as a workspace; pick a project folder", cmd)
	case errors.Is(err, wsp.ErrUnsafe):
		return core.Errorf(core.ExitOperationRefused, "", "%s: %w (a .rex/ must be a real folder you own that others cannot write to)", cmd, err)
	case errors.Is(err, wsp.ErrInvalid):
		return core.Errorf(core.ExitOperationRefused, "", "%s: %w", cmd, err)
	}
	return core.Errorf(core.ExitGeneric, "", "%s: %w", cmd, err)
}

func flat(s string) string { return protocol.FlattenDisplayText(s) }
