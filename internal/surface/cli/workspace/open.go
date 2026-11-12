package workspace

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/cli/meta"
	"github.com/tristanbietsch/rex/internal/surface/tui"
	wsp "github.com/tristanbietsch/rex/internal/workspace"
	"github.com/tristanbietsch/rex/internal/workspace/wsconfig"
)

// runBoard opens the board on a workspace. Tests replace it.
var runBoard = func(scope *tui.WorkspaceScope) error { return meta.RunTUIScoped(false, scope) }

// RunHere runs `rex here`: the board scoped to the workspace around the
// current folder.
func RunHere(args []string) error {
	fs := flag.NewFlagSet("here", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	pos, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return core.Errorf(core.ExitInvalidArgs, "rex ws open <path>", "here: takes no arguments (use `rex ws open <path>` for another folder)")
	}
	return openBoard("here", "")
}

// openWs is `rex ws open [path]`.
func openWs(args []string) error {
	fs := flag.NewFlagSet("ws open", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	pos, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return core.Errorf(core.ExitInvalidArgs, "rex ws -h", "ws open: wrong number of arguments: rex ws open [path]")
	}
	arg := ""
	if len(pos) == 1 {
		arg = pos[0]
	}
	return openBoard("ws open", arg)
}

// openBoard finds the workspace around path ("" is the current folder) and
// opens the board fixed to it. Exit 5 while the workspaces setting is off, in
// agent mode, and when no .rex/ is above path; nothing is read or written in
// the first two cases.
func openBoard(cmd, path string) error {
	if err := core.RequireFeature("workspaces"); err != nil {
		return err
	}
	if err := core.Out().RequireText(cmd, "rex ws id"); err != nil {
		return err
	}
	e, err := newEnv()
	if err != nil {
		return err
	}
	dir, err := e.absPath(path)
	if err != nil {
		return core.Errorf(core.ExitInvalidArgs, "", "%s: %w", cmd, err)
	}
	w, err := wsp.Find(dir, e.home)
	if err != nil {
		return wsError(cmd, err)
	}
	if w == nil {
		return core.Errorf(core.ExitInvalidArgs, "rex ws init", "%s: no .rex/ here — run `rex ws init`", cmd)
	}
	// Best effort: a workspace seen at a new folder moves its registry root.
	_ = e.reg.Seen(w)
	// The board applies this workspace's .rex/config.lua (when trusted),
	// whatever folder rex was started in.
	wsconfig.SetDir(w.Root)
	return runBoard(&tui.WorkspaceScope{ID: w.ID, Name: w.Name, Color: w.Color, Root: w.Root})
}
