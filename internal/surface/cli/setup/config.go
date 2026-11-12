package setup

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunConfig dispatches: rex config {list|get|set|reset|edit}
func RunConfig(args []string) error {
	if len(args) == 0 {
		return core.NewExitError(core.ExitInvalidArgs, "config: subcommand required (list|get|set|reset|edit)")
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

// configRow is one setting in rex config list.
type configRow struct {
	ID      string `json:"id"`
	Section string `json:"section"`
	Value   any    `json:"value"`
	Default any    `json:"default"`
}

func configRows(store *settings.Store) []configRow {
	rows := make([]configRow, 0, len(settings.Registry))
	for _, s := range settings.Registry {
		rows = append(rows, configRow{ID: s.ID, Section: string(s.Section), Value: store.Get(s.ID), Default: s.Default})
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
// id padded to 26); agent mode gets one `setting` line per row.
func configListView(rows []configRow) core.View {
	var v core.View
	agent := core.SetOf(core.FormatAgent)
	human := core.SetOf(core.FormatText, core.FormatMD)
	for i := 0; i < len(rows); {
		sec := rows[i].Section
		var pairs []core.Pair
		var txt []string
		for ; i < len(rows) && rows[i].Section == sec; i++ {
			r := rows[i]
			val := fmt.Sprint(r.Value)
			txt = append(txt, fmt.Sprintf("  %-26s %v", r.ID, r.Value))
			pairs = append(pairs, core.Pair{Key: r.ID, Label: r.ID, Val: core.Cell{Text: val}, Formats: human | core.SetOf(core.FormatTXT)})
		}
		v = append(v, core.Section{Title: sec}, core.KV{Pairs: pairs, Txt: strings.Join(txt, "\n")})
	}
	// One agent line per setting, in the same order.
	cells := make([][]core.Cell, len(rows))
	for i, r := range rows {
		cells[i] = []core.Cell{{Text: r.ID}, {Text: r.Section}, {Text: fmt.Sprint(r.Value), Raw: r.Value}, {Text: fmt.Sprint(r.Default), Raw: r.Default}}
	}
	return append(v, core.Table{Kind: "setting", Rows: cells, Cols: []core.Col{
		{Key: "id", Title: "ID", Formats: agent}, {Key: "section", Title: "Section", Formats: agent},
		{Key: "value", Title: "Value", Formats: agent}, {Key: "default", Title: "Default", Formats: agent},
	}})
}

func configList(args []string) error {
	fs := flag.NewFlagSet("config list", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats, core.WithLegacyJSON())
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	store, _, err := loadStore()
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
				b, _ := json.Marshal(r)
				if _, err := w.Write(append(b, '\n')); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// configValueData is the `-o json` payload of config get, set and reset.
type configValueData struct {
	ID    string `json:"id"`
	Value string `json:"value"`
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
	store, _, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if _, ok := settings.Find(rest[0]); !ok {
		return core.NewExitError(core.ExitInvalidArgs, fmt.Sprintf("unknown setting %q", rest[0]))
	}
	val := store.String(rest[0])
	return core.Out().Emit(core.Result{
		Cmd:  "config",
		Data: configValueData{ID: rest[0], Value: val},
		View: core.ActionView(core.Line{Text: val, Txt: val}, core.Field{Key: "id", Val: rest[0]}, core.Field{Key: "value", Val: val}),
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
	if err := store.Save(path); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return emitConfigChange("set", rest[0], store.String(rest[0]))
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
	if err := store.Save(path); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return emitConfigChange("reset", rest[0], store.String(rest[0]))
}

// emitConfigChange reports a set or reset. txt stays silent, as it always
// was.
func emitConfigChange(verb, id, value string) error {
	p := core.Out()
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: fmt.Sprintf("%s %s = %s", verb, id, value), Silent: true}
	return p.Emit(core.Result{
		Cmd:  "config",
		Data: configValueData{ID: id, Value: value},
		View: core.ActionView(line, core.Field{Key: "action", Val: verb}, core.Field{Key: "id", Val: id}, core.Field{Key: "value", Val: value}),
	})
}

func configEdit(args []string) error {
	fs := flag.NewFlagSet("config edit", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.TextFormats)
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	path, err := paths.LuaInitFile()
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
