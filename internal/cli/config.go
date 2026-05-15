package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/tristanbietsch/rex/internal/settings"
)

// RunConfig dispatches: rex config {list|get|set|reset|edit}
func RunConfig(args []string) error {
	if len(args) == 0 {
		return NewExitError(ExitInvalidArgs, "config: subcommand required (list|get|set|reset|edit)")
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
		return NewExitError(ExitInvalidArgs, fmt.Sprintf("config: unknown subcommand %q", args[0]))
	}
}

func loadStore() (*settings.Store, string, error) {
	path := settings.DefaultPath()
	s := settings.NewStore()
	if err := s.Load(path); err != nil {
		return nil, path, err
	}
	return s, path, nil
}

func configList(args []string) error {
	fs := flag.NewFlagSet("config list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	store, _, err := loadStore()
	if err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}

	type row struct {
		ID      string `json:"id"`
		Section string `json:"section"`
		Value   any    `json:"value"`
		Default any    `json:"default"`
	}
	rows := make([]row, 0, len(settings.Registry))
	for _, s := range settings.Registry {
		rows = append(rows, row{ID: s.ID, Section: string(s.Section), Value: store.Get(s.ID), Default: s.Default})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Section != rows[j].Section {
			return rows[i].Section < rows[j].Section
		}
		return rows[i].ID < rows[j].ID
	})

	if *asJSON {
		for _, r := range rows {
			b, _ := json.Marshal(r)
			os.Stdout.Write(append(b, '\n'))
		}
		return nil
	}
	curSection := ""
	for _, r := range rows {
		if r.Section != curSection {
			if curSection != "" {
				fmt.Println()
			}
			fmt.Printf("# %s\n", r.Section)
			curSection = r.Section
		}
		fmt.Printf("  %-26s %v\n", r.ID, r.Value)
	}
	return nil
}

func configGet(args []string) error {
	if len(args) != 1 {
		return NewExitError(ExitInvalidArgs, "config get: <id> required")
	}
	store, _, err := loadStore()
	if err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	if _, ok := settings.Find(args[0]); !ok {
		return NewExitError(ExitInvalidArgs, fmt.Sprintf("unknown setting %q", args[0]))
	}
	fmt.Println(store.String(args[0]))
	return nil
}

func configSet(args []string) error {
	if len(args) != 2 {
		return NewExitError(ExitInvalidArgs, "config set: <id> <value>")
	}
	store, path, err := loadStore()
	if err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	if err := store.Set(args[0], args[1]); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if err := store.Save(path); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}

func configReset(args []string) error {
	if len(args) != 1 {
		return NewExitError(ExitInvalidArgs, "config reset: <id> required")
	}
	store, path, err := loadStore()
	if err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	if err := store.Reset(args[0]); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if err := store.Save(path); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}

func configEdit(args []string) error {
	_ = args
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".config", "rex", "init.lua")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
