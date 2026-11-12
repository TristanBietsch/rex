// Package wsconfig applies a workspace's own .rex/config.lua (R06 W3): the
// settings layer above the user's config.lua, for the rex processes that
// run in the workspace (the CLI started inside it, rex here, rex ws open).
// The system-wide board and the daemon never apply one.
//
// A .rex/config.lua is untrusted input: a cloned repo can ship one. It is
// evaluated only when all of these hold, and is otherwise never read:
//   - the workspaces feature is on (config.yaml or the user's config.lua),
//     and lua_config is on (settings.LoadLayered checks that one);
//   - the user trusted that workspace, its real folder and id together,
//     with rex ws trust (workspace.TrustStore);
//   - the file passes workspace.ReadConfig's checks (no symlink, yours,
//     not group- or world-writable, 256 KiB).
//
// Even then it runs with Deny = settings.WorkspaceDenyIDs, so it can never
// grant or widen trust, and require is refused (no module directory). A
// file that fails (refused, a syntax error, a timeout) fails the trust
// settings closed for the process, as a broken user config.lua does.
package wsconfig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/settings/luacfg"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
	"github.com/tristanbietsch/rex/internal/workspace"
)

// State is what happened to the workspace config of the current folder.
type State string

const (
	// StateNone: no folder (the system board), no workspace around it, or
	// a workspace without .rex/config.lua.
	StateNone State = "none"
	// StateOff: the workspaces feature is off; nothing under .rex/ is read.
	StateOff State = "off"
	// StateUnsafe: the .rex/ around the folder is refused (workspace.Find).
	StateUnsafe State = "unsafe"
	// StateUntrusted: the workspace has a config.lua the user has not
	// trusted (rex ws trust); it is not read.
	StateUntrusted State = "untrusted"
	// StateApplied: the file ran and its values are on top.
	StateApplied State = "applied"
	// StateFailed: the trusted file was refused or did not run; the trust
	// settings read their defaults.
	StateFailed State = "failed"
)

// Info describes the workspace config of the current folder. All text is
// safe to print except Root (a path from disk; flatten it).
type Info struct {
	State  State
	ID     string
	Name   string
	Root   string
	File   string // Root/.rex/config.lua
	Detail string // why, for StateUnsafe; flattened
	Report settings.LayerReport
}

// scope is the folder this process applies the workspace config of.
var (
	mu  sync.Mutex
	dir string
)

// Counters for tests: looks counts walks for a .rex/ (any filesystem touch
// of a workspace), reads counts reads of a .rex/config.lua.
var looks, reads atomic.Int64

// Install makes this process's settings layer the user's config.lua with,
// on top, the trusted workspace config around folder (see the package
// doc). folder "" applies none: the system-wide board. Call it once at
// process start, instead of luacfg.Install.
func Install(folder string) {
	SetDir(folder)
	user := luacfg.Layer()
	settings.SetLayerStamped(Layer(user), Stamp(luacfg.HomeStamp))
}

// SetDir changes the folder whose workspace applies (rex ws open <path>).
// The next settings load sees it.
func SetDir(folder string) {
	mu.Lock()
	defer mu.Unlock()
	dir = folder
}

func currentDir() string {
	mu.Lock()
	defer mu.Unlock()
	return dir
}

// Layer stacks the current folder's workspace config on user (the user's
// config.lua layer). Without one it is user unchanged.
func Layer(user settings.Layer) settings.Layer {
	return func() settings.LayerReport {
		u := user()
		info := evaluate(u)
		if info.State == StateNone || info.State == StateOff {
			return u
		}
		return settings.StackLayers(
			func() settings.LayerReport { return u },
			func() settings.LayerReport { return info.Report },
		)()
	}
}

// Stamp is the layer stamp: the user's, plus the workspace part while one
// applies (trusted or not), so a poll sees an edit, a new file or a trust
// change.
func Stamp(user func() string) func() string {
	return func() string {
		u := user()
		ws := stampFor(luacfg.Layer())
		if ws == "" {
			return u
		}
		return u + "|ws:" + ws
	}
}

func stampFor(user settings.Layer) string {
	w, _, state := locate(user)
	if w == nil || state != "" {
		return ""
	}
	if !workspace.HasConfig(w.Root) {
		return "none"
	}
	if !trusted(w) {
		return "untrusted"
	}
	return "trusted:" + luacfg.Stamp(opts(w))
}

// Current reports the workspace config of the current folder, evaluating
// it (memoised) when it applies. rex doctor shows it.
func Current() Info { return evaluate(luacfg.Layer()()) }

// locate finds the workspace around the current folder. state is
// StateNone, StateOff or StateUnsafe when there is nothing to apply, else
// "".
func locate(user settings.Layer) (*workspace.Workspace, string, State) {
	folder := currentDir()
	if folder == "" {
		return nil, "", StateNone
	}
	if !workspacesOn(user()) {
		return nil, "", StateOff
	}
	home, err := paths.Home()
	if err != nil {
		return nil, "", StateNone
	}
	looks.Add(1)
	w, err := workspace.Find(folder, home)
	switch {
	case err != nil:
		return nil, protocol.FlattenDisplayText(err.Error()), StateUnsafe
	case w == nil:
		return nil, "", StateNone
	}
	return w, "", ""
}

// evaluate works out the workspace config given the user's layer report u.
func evaluate(u settings.LayerReport) Info {
	w, detail, state := locate(func() settings.LayerReport { return u })
	switch state {
	case StateNone, StateOff:
		return Info{State: state}
	case StateUnsafe:
		return Info{State: state, Detail: detail, Report: settings.LayerReport{
			Warnings: []string{"workspace: " + detail + "; its .rex/config.lua is not applied"},
		}}
	}
	info := Info{ID: w.ID, Name: w.Name, Root: w.Root, File: workspace.ConfigPath(w.Root)}
	if !workspace.HasConfig(w.Root) {
		info.State = StateNone
		return info
	}
	shown := protocol.FlattenDisplayText(info.File)
	if !trusted(w) {
		info.State = StateUntrusted
		info.Report = settings.LayerReport{Exists: true, Warnings: []string{
			shown + ": not applied: workspace not trusted (rex ws trust, after reading it)",
		}}
		return info
	}
	r := luacfg.Load(context.Background(), opts(w))
	info.Report = settings.LayerReport{
		Exists: true, Values: r.Values, SetAt: r.SetAt,
		Themes: r.Themes, ThemeIssues: r.ThemeIssues, Warnings: r.Warnings,
		Err: r.Err, LastGood: r.LastGood, FailClosed: r.FailClosed,
	}
	info.State = StateApplied
	if r.Err != nil {
		info.State = StateFailed
	}
	return info
}

// opts evaluates w's config.lua with the workspace rules: the workspace
// package's read, the deny list, no modules, positions naming the path.
func opts(w *workspace.Workspace) luacfg.Opts {
	root := w.Root
	file := workspace.ConfigPath(root)
	return luacfg.Opts{
		File:  file,
		Chunk: file,
		Deny:  settings.WorkspaceDenyIDs,
		Read: func() ([]byte, error) {
			reads.Add(1)
			return workspace.ReadConfig(root)
		},
	}
}

// workspacesOn is the effective workspaces switch: the user's config.lua
// when it sets it, else config.yaml.
func workspacesOn(u settings.LayerReport) bool {
	if v, ok := u.Values["workspaces"].(bool); ok {
		return v
	}
	path, err := paths.ConfigFile()
	if err != nil {
		return false
	}
	s := settings.NewStore()
	if err := s.Load(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false
	}
	return settings.FeatureOn(s, "workspaces")
}

// openTrust opens the user's workspace trust store.
func openTrust() (*workspace.TrustStore, error) {
	p, err := paths.WorkspaceTrustFile()
	if err != nil {
		return nil, fmt.Errorf("workspace trust: %w", err)
	}
	return workspace.OpenTrust(p)
}

func trusted(w *workspace.Workspace) bool {
	s, err := openTrust()
	return err == nil && s.Trusted(w)
}
