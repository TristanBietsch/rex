package setup

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
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
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 0, 0, "rex config list [-json]"); err != nil {
		return err
	}
	store, _, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
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
		return core.NewExitError(core.ExitInvalidArgs, "config get: <id> required")
	}
	store, _, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if _, ok := settings.Find(args[0]); !ok {
		return core.NewExitError(core.ExitInvalidArgs, fmt.Sprintf("unknown setting %q", args[0]))
	}
	fmt.Println(store.String(args[0]))
	return nil
}

func configSet(args []string) error {
	if len(args) != 2 {
		return core.NewExitError(core.ExitInvalidArgs, "config set: <id> <value>")
	}
	store, path, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := store.Set(args[0], args[1]); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if err := store.Save(path); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}

func configReset(args []string) error {
	if len(args) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "config reset: <id> required")
	}
	store, path, err := loadStore()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := store.Reset(args[0]); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if err := store.Save(path); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}

// configEdit opens the Lua config (lua_config_path, the file the daemon
// loads) in $EDITOR.
func configEdit(args []string) error {
	if err := core.CheckArgs(args, 0, 0, "rex config edit"); err != nil {
		return err
	}
	store, _, err := loadStore()
	if err != nil {
		// Defaults still name a usable path; say which one is opened.
		fmt.Fprintf(os.Stderr, "rex: %v (using the default lua_config_path)\n", err)
		store = settings.NewStore()
	}
	path := luaConfigPath(store)
	if path == "" {
		return core.NewExitError(core.ExitInvalidArgs, "config edit: lua_config_path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	cmd := editorCommand(os.Getenv("EDITOR"), path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// luaConfigPath resolves lua_config_path the way the daemon does: a leading
// "~/" is the home directory.
func luaConfigPath(store *settings.Store) string {
	p := store.String("lua_config_path")
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[2:])
	}
	return p
}

// editorCommand runs editor on path. editor may carry arguments
// ("code -w"); empty means vi (notepad on Windows).
func editorCommand(editor, path string) *exec.Cmd {
	argv := strings.Fields(editor)
	if len(argv) == 0 {
		argv = []string{"vi"}
		if runtime.GOOS == "windows" {
			argv = []string{"notepad"}
		}
	}
	return exec.Command(argv[0], append(argv[1:], path)...)
}
