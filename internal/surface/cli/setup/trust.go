package setup

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

const trustSettingID = "auto_trust_dirs"

// trustData is the `-o json` payload of rex trust (add and remove).
type trustData struct {
	Action  string `json:"action"`
	Dir     string `json:"dir"`
	Changed bool   `json:"changed"`
	Value   string `json:"value"`
}

// trustListData is the `-o json` payload of rex trust -ls.
type trustListData struct {
	Value string         `json:"value"`
	Dirs  []trustDirItem `json:"dirs"`
}

type trustDirItem struct {
	Entry string `json:"entry"`
	// Dir is the folder the entry trusts, resolved through symlinks; empty
	// when the daemon would ignore the entry.
	Dir string `json:"dir"`
	// Ignored says why the daemon skips the entry; empty when it is used.
	Ignored string `json:"ignored,omitempty"`
}

// RunTrust is `rex trust [dir]`, `rex trust -ls` and `rex trust -rm <dir>`:
// it edits auto_trust_dirs through the same checks `rex config set` applies.
func RunTrust(args []string) error {
	fs := flag.NewFlagSet("trust", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	ls := fs.Bool("ls", false, "list the trusted folders")
	rm := fs.String("rm", "", "stop trusting this `folder`")
	pos, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	switch {
	case *ls && *rm != "":
		return trustUsage("trust: -ls and -rm cannot be combined")
	case *ls:
		if len(pos) > 0 {
			return trustUsage("trust -ls: unexpected argument %q", pos[0])
		}
		return trustList()
	case *rm != "":
		if len(pos) > 0 {
			return trustUsage("trust -rm: unexpected argument %q", pos[0])
		}
		return trustRemove(*rm)
	}
	if len(pos) > 1 {
		return trustUsage("trust: at most one folder (got %d)", len(pos))
	}
	dir := ""
	if len(pos) == 1 {
		dir = pos[0]
	}
	return trustAdd(dir)
}

func trustUsage(format string, a ...any) error {
	return core.Errorf(core.ExitInvalidArgs, "rex trust -h", format, a...)
}

func trustRefused(verb string, err error) error {
	return core.Errorf(core.ExitInvalidArgs, "rex trust -ls", "trust%s: %w", verb, err)
}

// trustAdd trusts dir, or the suggested folder for the working directory.
func trustAdd(dir string) error {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
		if dir = settings.SuggestTrustDir(wd); dir == "" {
			return core.Errorf(core.ExitInvalidArgs, "rex trust <folder>",
				"trust: nothing to trust at %s; run it in a project folder inside your home folder, or name the folder", wd)
		}
	}
	resolved, err := settings.TrustDir(dir)
	if err != nil {
		return trustRefused("", err)
	}
	store, path, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	next, changed, err := settings.AppendTrustDir(store.String(trustSettingID), dir)
	if err != nil {
		return trustRefused("", err)
	}
	if changed {
		if err := store.Set(trustSettingID, next); err != nil {
			return core.NewExitError(core.ExitInvalidArgs, err.Error())
		}
		if err := store.Save(path); err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
		reloadDaemonForTrust(trustSettingID)
	}
	return emitTrust("add", resolved, changed, store.String(trustSettingID))
}

func trustRemove(dir string) error {
	resolved, err := settings.TrustDir(dir)
	if err != nil {
		return trustRefused(" -rm", err)
	}
	store, path, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	next, changed, err := settings.RemoveTrustDir(store.String(trustSettingID), dir)
	if err != nil {
		return trustRefused(" -rm", err)
	}
	if changed {
		if err := store.Set(trustSettingID, next); err != nil {
			return core.NewExitError(core.ExitInvalidArgs, err.Error())
		}
		if err := store.Save(path); err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
		reloadDaemonForTrust(trustSettingID)
	}
	return emitTrust("remove", resolved, changed, store.String(trustSettingID))
}

func emitTrust(action, dir string, changed bool, value string) error {
	p := core.Out()
	text, tone := "", core.ToneOK
	switch {
	case action == "add" && changed:
		text = "trusted " + dir
	case action == "add":
		text, tone = "already trusted "+dir, core.ToneDim
	case changed:
		text = "no longer trusted " + dir
	default:
		text, tone = "not in the list "+dir, core.ToneDim
	}
	line := core.Line{Tone: tone, Glyph: p.Glyphs.OK, Text: text, Txt: dir}
	view := core.ActionView(line,
		core.Field{Key: "action", Val: action},
		core.Field{Key: "dir", Val: dir},
		core.Field{Key: "changed", Val: boolWord(changed)},
		core.Field{Key: "value", Val: value})
	if action == "add" && changed {
		view = append(view, core.Hint{Cmd: "rex trust -rm " + dir, Text: "undo"})
	}
	return p.Emit(core.Result{
		Cmd:  "trust",
		Data: trustData{Action: action, Dir: dir, Changed: changed, Value: value},
		View: view,
	})
}

func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func trustList() error {
	store, _, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	value := store.String(trustSettingID)
	items := trustItems(value)
	cells := make([][]core.Cell, len(items))
	for i, it := range items {
		shown := it.Dir
		if it.Ignored != "" {
			shown = "(ignored: " + it.Ignored + ")"
		}
		cells[i] = []core.Cell{{Text: it.Entry}, {Text: shown}}
	}
	view := core.View{
		core.Header{Title: "Trusted folders", Meta: trustSettingID},
		core.Table{Kind: "trust_dir", Cols: []core.Col{{Key: "entry", Title: "Entry"}, {Key: "dir", Title: "Folder"}},
			Rows: cells, Empty: "none: every session asks (rex trust adds the project folder)"},
	}
	return core.Out().Emit(core.Result{Cmd: "trust", Data: trustListData{Value: value, Dirs: items}, View: view})
}

// trustItems lists the entries of an auto_trust_dirs value with the folder
// each trusts. A value the checks refuse as a whole yields one ignored item.
func trustItems(value string) []trustDirItem {
	items := []trustDirItem{}
	if value == "" {
		return items
	}
	entries, err := settings.SplitTrustDirs(value)
	if err != nil {
		return append(items, trustDirItem{Entry: value, Ignored: err.Error()})
	}
	for _, e := range entries {
		if e == "" {
			continue
		}
		d, err := settings.TrustDir(e)
		if err != nil {
			items = append(items, trustDirItem{Entry: e, Ignored: err.Error()})
			continue
		}
		items = append(items, trustDirItem{Entry: e, Dir: filepath.Clean(d)})
	}
	return items
}
