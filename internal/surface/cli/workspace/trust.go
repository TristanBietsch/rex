package workspace

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	wsp "github.com/tristanbietsch/rex/internal/workspace"
)

// trustInfo is the json payload of rex ws trust.
type trustInfo struct {
	Action  string `json:"action"` // "trust" or "revoke"
	Changed bool   `json:"changed"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Root    string `json:"root,omitempty"`
	// Config is true when the workspace has a .rex/config.lua now.
	Config bool `json:"config"`
	// Denied lists the settings a workspace config.lua can never set.
	Denied []string `json:"denied"`
}

func openTrustStore() (*wsp.TrustStore, error) {
	p, err := paths.WorkspaceTrustFile()
	if err != nil {
		return nil, core.Errorf(core.ExitGeneric, "", "ws trust: %w", err)
	}
	s, err := wsp.OpenTrust(p)
	if err != nil {
		return nil, core.Errorf(core.ExitGeneric, "", "ws trust: %w", err)
	}
	return s, nil
}

// trust is `rex ws trust [path]` (let the workspace's .rex/config.lua
// apply) and `rex ws trust -rm [id|path]`.
func (e env) trust(pos []string, rm bool) error {
	ref := ""
	if len(pos) > 0 {
		ref = pos[0]
	}
	// Trust takes a folder, never an id: the registry maps an id to the
	// folder it was last seen at, and a copy of a committed workspace.json
	// (a cloned repo) carries the same id, so an id could name the copy.
	if !rm && wsp.ValidID(ref) {
		return core.Errorf(core.ExitInvalidArgs, "rex ws trust <folder>",
			"ws trust: name the folder, not the id (an id can also be a copy of this workspace elsewhere)")
	}
	ts, err := openTrustStore()
	if err != nil {
		return err
	}
	w, rerr := wsp.Resolve(ref, e.cwd, e.home, e.reg)
	if rm {
		return e.revoke(ts, ref, w, rerr)
	}
	if rerr != nil {
		return wsError("ws trust", rerr)
	}
	was := ts.Trusted(w)
	if err := ts.Trust(w); err != nil {
		return wsError("ws trust", err)
	}
	_ = e.reg.Seen(w) // best effort, as rex ws id
	data := trustInfo{Action: "trust", Changed: !was, ID: w.ID, Name: w.Name, Root: w.Root,
		Config: wsp.HasConfig(w.Root), Denied: settings.WorkspaceDenyIDs}
	text := "trusted"
	tone := core.ToneOK
	if was {
		text, tone = "already trusted", core.ToneDim
	}
	v := core.View{
		core.Line{Tone: tone, Glyph: "✓", Text: fmt.Sprintf("%s %s %s", text, w.ID, flat(w.Name)), Txt: w.ID},
		infoKV(info{ID: w.ID, Name: w.Name, Color: w.Color, Root: w.Root}),
	}
	if data.Config {
		v = append(v, core.Note{Tone: core.ToneDim, Text: "its .rex/config.lua now applies when rex runs in this folder: " + flat(wsp.ConfigPath(w.Root))})
	} else {
		v = append(v, core.Note{Tone: core.ToneDim, Text: "no .rex/config.lua yet; one added later applies without asking again"})
	}
	v = append(v,
		core.Note{Tone: core.ToneDim, Text: "a workspace config.lua can never set: " + strings.Join(settings.WorkspaceDenyIDs, ", ")},
		core.Hint{Cmd: "rex ws trust -rm " + w.ID, Text: "undo"})
	return core.Out().Emit(core.Result{Cmd: "ws trust", Data: data, View: v})
}

// revoke removes trust for the workspace ref names. A workspace that moved
// or is gone is still revoked by its id, or by the folder named.
func (e env) revoke(ts *wsp.TrustStore, ref string, w *wsp.Workspace, rerr error) error {
	id, root := "", ""
	switch {
	case rerr == nil:
		id, root = w.ID, w.Root
	case wsp.ValidID(ref):
		id = ref
	case errors.Is(rerr, wsp.ErrNotFound):
		dir, err := e.absPath(ref)
		if err != nil {
			return core.Errorf(core.ExitInvalidArgs, "", "ws trust -rm: %w", err)
		}
		root = dir
	default:
		return wsError("ws trust -rm", rerr)
	}
	changed, err := ts.Revoke(id, root)
	if err != nil {
		return wsError("ws trust -rm", err)
	}
	if !changed && rerr != nil {
		return wsError("ws trust -rm", rerr)
	}
	data := trustInfo{Action: "revoke", Changed: changed, ID: id, Root: root, Denied: settings.WorkspaceDenyIDs}
	if w != nil {
		data.Name, data.Config = w.Name, wsp.HasConfig(w.Root)
	}
	shown := id
	if shown == "" {
		shown = flat(root)
	}
	text, tone := "no longer trusted "+shown, core.ToneOK
	if !changed {
		text, tone = "was not trusted "+shown, core.ToneDim
	}
	return core.Out().Emit(core.Result{Cmd: "ws trust", Data: data, View: core.View{
		core.Line{Tone: tone, Glyph: "✓", Text: text, Txt: shown},
	}})
}
