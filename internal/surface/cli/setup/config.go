package setup

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunConfig dispatches: rex config {list|get|set|reset|edit|lua-export}
func RunConfig(args []string) error {
	if len(args) > 0 && core.IsHelpArg(args[0]) {
		return core.RunGroupHelp("config", args[1:])
	}
	if len(args) == 0 {
		return core.NewExitError(core.ExitInvalidArgs, "config: subcommand required (list|get|set|reset|edit|lua-export)")
	}
	switch args[0] {
	case "list":
		return configList(args[1:])
	case "get":
		return configGet(args[1:])
	case "set":
		return configSet(args[1:])
	case "reset":
		return configReset(args[1:])
	case "edit":
		return configEdit(args[1:])
	case "lua-export":
		return configLuaExport(args[1:])
	default:
		return core.NewExitError(core.ExitInvalidArgs, fmt.Sprintf("config: unknown subcommand %q", args[0]))
	}
}

func loadStore() (*settings.Store, string, error) {
	s, path, err := settings.LoadDefault()
	if err != nil {
		return nil, path, err
	}
	return s, path, nil
}

// loadStoreReporting is loadStore for list and get, which report config.lua
// problems: one warning line on stderr, never on stdout.
func loadStoreReporting() (*settings.Store, error) {
	s, rep, _, err := settings.LoadDefaultReport()
	if err != nil {
		return nil, err
	}
	reportLuaProblems(rep)
	return s, nil
}

// reportLuaProblems warns on stderr about a config.lua that failed or
// refused something. The text is flattened by the evaluator and again by
// Warn.
func reportLuaProblems(rep settings.LayerReport) {
	p := core.Out()
	switch {
	case rep.Err != nil:
		p.Warn(rep.Err.Error() + luaKeptSuffix(rep))
	case len(rep.Warnings) > 0:
		p.Warn(fmt.Sprintf("config.lua: %d warning(s), first: %s", len(rep.Warnings), rep.Warnings[0]))
	}
}

func luaKeptSuffix(rep settings.LayerReport) string {
	if rep.LastGood {
		return " (kept its last good values)"
	}
	return " (config.yaml and defaults apply)"
}

// configRow is one setting in rex config list.
type configRow struct {
	ID      string `json:"id"`
	Section string `json:"section"`
	Value   any    `json:"value"`
	Default any    `json:"default"`
	// Source is "default", "file" (config.yaml) or "lua" (config.lua).
	Source string `json:"source"`
	// SetAt is where config.lua sets it ("config.lua:12", or the
	// workspace's "/path/.rex/config.lua:3"), for source lua.
	SetAt string `json:"set_at,omitempty"`
	// Display is Value as the human formats show it ("max" for
	// master_volume at 50); "" means fmt.Sprint(Value).
	Display string `json:"-"`
}

// display is the human text of r's value.
func (r configRow) display() string {
	if r.Display != "" {
		return r.Display
	}
	return fmt.Sprint(r.Value)
}

// legacyConfigRow is the shape -json has always printed.
type legacyConfigRow struct {
	ID      string `json:"id"`
	Section string `json:"section"`
	Value   any    `json:"value"`
	Default any    `json:"default"`
}

// sourceText is a row's source in the human formats: default, config.yaml,
// or "lua" and where.
func sourceText(src, at string) string {
	switch settings.Source(src) {
	case settings.SourceFile:
		return "config.yaml"
	case settings.SourceLua:
		if settings.WorkspaceSetAt(at) {
			return "workspace " + at
		}
		return strings.TrimSpace("lua " + at)
	case settings.SourceDefault:
	}
	return src
}

func configRows(store *settings.Store) []configRow {
	rows := make([]configRow, 0, len(settings.Registry))
	for _, s := range settings.Registry {
		rows = append(rows, configRow{ID: s.ID, Section: string(s.Section), Value: store.Get(s.ID), Default: s.Default,
			Source: string(store.Source(s.ID)), SetAt: store.SetAt(s.ID), Display: store.Display(s.ID)})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Section != rows[j].Section {
			return rows[i].Section < rows[j].Section
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

// configListView groups the rows by section. txt keeps the layout
// `rex config list` always printed (`# Section`, then `  id value` with the
// id padded to 26); text and md show an ID, VALUE, SOURCE table per section;
// agent mode gets one `setting` line per row.
func configListView(rows []configRow) core.View {
	var v core.View
	agent := core.SetOf(core.FormatAgent)
	human := core.SetOf(core.FormatText, core.FormatMD)
	for i := 0; i < len(rows); {
		sec := rows[i].Section
		var txt []string
		var cells [][]core.Cell
		for ; i < len(rows) && rows[i].Section == sec; i++ {
			r := rows[i]
			txt = append(txt, fmt.Sprintf("  %-26s %v", r.ID, r.Value))
			src := core.Cell{Text: sourceText(r.Source, r.SetAt), Tone: core.ToneDim}
			cells = append(cells, []core.Cell{{Text: r.ID}, {Text: r.display()}, src})
		}
		v = append(v, core.Section{Title: sec},
			core.KV{Txt: strings.Join(txt, "\n")},
			core.Table{Rows: cells, Cols: []core.Col{
				{Key: "id", Title: "ID", Formats: human},
				{Key: "value", Title: "Value", Flex: true, Formats: human},
				{Key: "source", Title: "Source", Formats: human},
			}})
	}
	// One agent line per setting, in the same order.
	cells := make([][]core.Cell, len(rows))
	for i, r := range rows {
		cells[i] = []core.Cell{{Text: r.ID}, {Text: r.Section}, {Text: fmt.Sprint(r.Value), Raw: r.Value}, {Text: fmt.Sprint(r.Default), Raw: r.Default},
			{Text: r.Source}, {Text: r.SetAt}}
	}
	return append(v, core.Table{Kind: "setting", Rows: cells, Cols: []core.Col{
		{Key: "id", Title: "ID", Formats: agent}, {Key: "section", Title: "Section", Formats: agent},
		{Key: "value", Title: "Value", Formats: agent}, {Key: "default", Title: "Default", Formats: agent},
		{Key: "source", Title: "Source", Formats: agent}, {Key: "set_at", Title: "Set at", Formats: agent},
	}})
}

func configList(args []string) error {
	fs := flag.NewFlagSet("config list", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats, core.WithLegacyJSON())
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	store, err := loadStoreReporting()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	rows := configRows(store)
	return core.Out().Emit(core.Result{
		Cmd:  "config",
		Data: rows,
		View: configListView(rows),
		Legacy: func(w io.Writer) error {
			for _, r := range rows {
				b, _ := json.Marshal(legacyConfigRow{ID: r.ID, Section: r.Section, Value: r.Value, Default: r.Default})
				if _, err := w.Write(append(b, '\n')); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// configValueData is the `-o json` payload of config get, set and reset.
// get adds where the value comes from.
type configValueData struct {
	ID     string `json:"id"`
	Value  string `json:"value"`
	Source string `json:"source,omitempty"`
	SetAt  string `json:"set_at,omitempty"`
}

func configGet(args []string) error {
	fs := flag.NewFlagSet("config get", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "config get: <id> required")
	}
	if _, ok := settings.Find(rest[0]); !ok {
		return core.NewExitError(core.ExitInvalidArgs, fmt.Sprintf("unknown setting %q", rest[0]))
	}
	store, err := loadStoreReporting()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	id := rest[0]
	val, src, at := store.String(id), string(store.Source(id)), store.SetAt(id)
	text := store.Display(id)
	if at != "" {
		text += "  (" + sourceText(src, at) + ")"
	}
	fields := []core.Field{{Key: "id", Val: id}, {Key: "value", Val: val}, {Key: "source", Val: src}}
	if at != "" {
		fields = append(fields, core.Field{Key: "set_at", Val: at})
	}
	return core.Out().Emit(core.Result{
		Cmd:  "config",
		Data: configValueData{ID: id, Value: val, Source: src, SetAt: at},
		View: core.ActionView(core.Line{Text: text, Txt: val}, fields...),
	})
}

func configSet(args []string) error {
	fs := flag.NewFlagSet("config set", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return core.NewExitError(core.ExitInvalidArgs, "config set: <id> <value>")
	}
	store, path, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := store.Set(rest[0], rest[1]); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	// After Set, so an unknown id or a refused value exits 5 first.
	if err := requireTerminalForSetting("config set", rest[0], "rex config set "+rest[0]+" "+rest[1]); err != nil {
		return err
	}
	if err := store.Save(path); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	reloadDaemonForTrust(rest[0])
	return emitConfigChange("set", rest[0], store.String(rest[0]), store.Display(rest[0]))
}

func configReset(args []string) error {
	fs := flag.NewFlagSet("config reset", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "config reset: <id> required")
	}
	store, path, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := store.Reset(rest[0]); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if err := requireTerminalForSetting("config reset", rest[0], "rex config reset "+rest[0]); err != nil {
		return err
	}
	if err := store.Save(path); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	reloadDaemonForTrust(rest[0])
	return emitConfigChange("reset", rest[0], store.String(rest[0]), store.Display(rest[0]))
}

// reloadDaemonForTrust tells a running daemon to re-read config.yaml right
// after rex changed id, when id is one of the trust settings, so revoking
// skip_permissions or auto_trust_dirs does not wait for the daemon's poll.
// It is best effort: no daemon running means nothing to tell (it reads the
// file at start), and any failure leaves the command's outcome alone.
func reloadDaemonForTrust(id string) {
	if !settings.IsTrustSetting(id) {
		return
	}
	if err := daemonctl.Reload(); err != nil && !errors.Is(err, daemonctl.ErrNotRunning) {
		slog.Warn("config: could not tell the daemon to reload (it applies the change within 5 s)", "id", id, "err", err)
	}
}

// emitConfigChange reports a set or reset. txt stays silent, as it always
// was. display is value as the human line shows it ("max"); the data and
// agent fields keep value.
func emitConfigChange(verb, id, value, display string) error {
	p := core.Out()
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: fmt.Sprintf("%s %s = %s", verb, id, display), Silent: true}
	return p.Emit(core.Result{
		Cmd:  "config",
		Data: configValueData{ID: id, Value: value},
		View: core.ActionView(line, core.Field{Key: "action", Val: verb}, core.Field{Key: "id", Val: id}, core.Field{Key: "value", Val: value}),
	})
}

func configEdit(args []string) error {
	fs := flag.NewFlagSet("config edit", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.TextFormats)
	luaCfg := fs.Bool("lua", false, "open ~/.config/rex/config.lua (settings in Lua), created from a commented template the first time")
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	var path string
	var err error
	if *luaCfg {
		if err := requireLuaConfig(); err != nil {
			return err
		}
		path, err = ensureLuaConfig()
	} else {
		path, err = paths.LuaInitFile()
	}
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad"
		}
	}
	p := core.Out()
	cmd := exec.Command(editor, path) //nolint:gosec,noctx // G702: $EDITOR is the user's own program; noctx: the editor runs until the user quits it.
	cmd.Stdin = os.Stdin
	cmd.Stdout = p.Out
	cmd.Stderr = p.Err
	return cmd.Run()
}
