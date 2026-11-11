package setup

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// RunSetup implements `rex setup`.
func RunSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	preview := fs.Bool("preview", false, "print plan only, no writes")
	var doYes bool
	fs.BoolVar(&doYes, "yes", false, "apply defaults non-interactively")
	fs.BoolVar(&doYes, "y", false, "alias for -yes")
	tool := fs.String("tool", "", "override default tool id")
	model := fs.String("model", "", "override default model id")
	effort := fs.String("effort", "", "override default effort")
	noShell := fs.Bool("no-shell", false, "skip shell integration")
	autoTrust := fs.String("auto-trust", "",
		"folders where agents skip the folder trust prompt (only folders whose code you trust)")
	configPath := fs.String("config", "", "path to config.yaml (default ~/.config/rex/config.yaml)")
	socketFlag := core.AddSocketFlag(fs)
	force := fs.Bool("force", false, "overwrite existing config without prompting")
	out := core.AddOutputFlags(fs, core.AllFormats)

	_, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	// Refuse what the daemon would ignore, before anything runs or is written.
	if *autoTrust != "" {
		v, err := settings.CheckAutoTrustDirs(*autoTrust)
		if err != nil {
			return core.NewExitError(core.ExitInvalidArgs, "-auto-trust: "+err.Error())
		}
		*autoTrust = v
	}

	if *configPath == "" {
		path, err := paths.ConfigFile()
		if err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error()+"; pass -config")
		}
		*configPath = path
	}
	socket, err := socketFlag.Path()
	if err != nil {
		return err
	}
	// Same for a tool, model or effort the registry does not have.
	if _, _, err := tui.ResolveSpawnDefaults(*configPath, *tool, *model, *effort); err != nil {
		return core.Errorf(core.ExitInvalidArgs, "", "-tool/-model/-effort: %s", err)
	}

	rexlog.Init("setup")
	defer rexlog.Close()
	slog.Info("setup: started",
		"preview", *preview, "yes", doYes,
		"tool", *tool, "model", *model, "effort", *effort,
		"no_shell", *noShell, "auto_trust", *autoTrust, "config", *configPath, "socket", socket)

	p := core.Out()
	if *preview {
		return emitSetupPreview(p, *configPath, socket, *tool, *model, *effort, *autoTrust, *noShell)
	}

	if doYes {
		// guard: non-empty existing config requires --force
		if !*force {
			if b, err := os.ReadFile(*configPath); err == nil && len(strings.TrimSpace(string(b))) > 0 {
				return core.NewExitError(core.ExitInvalidArgs,
					fmt.Sprintf("config already exists at %s — rerun with -force to overwrite", *configPath))
			}
		}
		rep, err := tui.ApplyDefaults(*configPath, socket, *tool, *model, *effort, *autoTrust, *noShell)
		if err != nil {
			return err
		}
		return emitSetupApplied(p, rep)
	}

	// The wizard is interactive: it needs the text format and a terminal.
	if p.Format != core.FormatText {
		return core.Errorf(core.ExitInvalidArgs, "rex setup -yes",
			"setup: the wizard is interactive and cannot run in %s mode; use -yes (or -preview)", p.Format)
	}
	return tui.RunSetupWizard(*configPath, socket, *autoTrust)
}

// setupData is the `-o json` payload of rex setup -preview and -yes.
type setupData struct {
	// Mode is "preview" or "applied".
	Mode       string `json:"mode"`
	ConfigPath string `json:"config_path"`
	AutoTrust  string `json:"auto_trust,omitempty"`
	ShellRC    string `json:"shell_rc,omitempty"`
	// ShellState: preview says "append", "rewrite", "none" or "skipped";
	// applied says "installed", "present", "failed" or "skipped".
	ShellState      string `json:"shell_state"`
	ShellBlock      string `json:"shell_block,omitempty"`
	Socket          string `json:"socket,omitempty"`
	DaemonReachable *bool  `json:"daemon_reachable,omitempty"`
	NextCommand     string `json:"next_command"`
}

func agentPairs(fields ...core.Field) core.KV {
	pairs := make([]core.Pair, len(fields))
	for i, f := range fields {
		pairs[i] = core.Pair{Key: f.Key, Val: core.Cell{Text: fmt.Sprint(f.Val), Raw: f.Val}, Formats: core.SetOf(core.FormatAgent)}
	}
	return core.KV{Pairs: pairs}
}

func emitSetupPreview(p *core.Printer, configPath, socket, toolID, modelID, effortVal, autoTrust string, noShell bool) error {
	slog.Info("setup: preview mode", "config_path", configPath)
	data := setupData{Mode: "preview", ConfigPath: configPath, AutoTrust: autoTrust, Socket: socket, ShellState: "skipped"}

	files := []core.Def{{Name: configPath, Key: configPath, Desc: "config.yaml — written via settings.Store.Save() (atomic temp+rename)"}}
	if autoTrust != "" {
		files = append(files, core.Def{Name: "auto_trust_dirs", Desc: autoTrust})
	}
	var rcNote string
	if !noShell {
		data.ShellState = "none"
		if rc := tui.DetectShellRC(); rc != "" {
			data.ShellRC = rc
			if tui.HasShellBlock(rc) {
				data.ShellState = "rewrite"
				files = append(files, core.Def{Name: rc, Desc: "block already present — rewrite in place"})
			} else {
				data.ShellState = "append"
				files = append(files, core.Def{Name: rc, Desc: "append shell block"})
			}
		} else {
			rcNote = "no shell profile detected — skip shell integration"
		}
	} else {
		rcNote = "-no-shell: shell integration skipped"
	}
	data.ShellBlock = tui.ShellProfileBlock(filepath.Base(os.Getenv("SHELL")))

	reachable := daemonctl.Reachable(socket)
	data.DaemonReachable = &reachable
	daemonLine := "not running — interactive mode offers to start it"
	if reachable {
		daemonLine = "reachable at " + socket
	}
	toolDisplay, modelDisplay := toolID, modelID
	if toolDisplay == "" {
		toolDisplay = "claude"
	}
	if modelDisplay == "" {
		modelDisplay = "opus"
	}
	data.NextCommand = tui.NextCommand(toolDisplay, modelDisplay, effortVal)

	v := core.View{
		core.Header{Title: "Setup", Meta: "preview"},
		core.Section{Title: "Files that would be written"},
		core.Defs{Items: files},
	}
	if rcNote != "" {
		v = append(v, core.Note{Text: rcNote})
	}
	v = append(v,
		core.Section{Title: "Shell block"}, core.Pre{Text: data.ShellBlock},
		core.Section{Title: "Daemon"}, core.Para{Text: daemonLine},
		core.Section{Title: "Next command"}, core.Defs{Items: []core.Def{{Name: data.NextCommand}}},
		core.Note{Text: "Docs: https://github.com/tristanbietsch/rex"},
		agentPairs(core.Field{Key: "mode", Val: "preview"}, core.Field{Key: "config", Val: configPath},
			core.Field{Key: "shell_rc", Val: data.ShellRC}, core.Field{Key: "shell", Val: data.ShellState},
			core.Field{Key: "daemon", Val: map[bool]string{true: "reachable", false: "down"}[reachable]},
			core.Field{Key: "next", Val: data.NextCommand}),
	)
	return p.Emit(core.Result{Cmd: "setup", Data: data, View: v})
}

func emitSetupApplied(p *core.Printer, rep tui.DefaultsReport) error {
	data := setupData{Mode: "applied", ConfigPath: rep.ConfigPath, AutoTrust: rep.AutoTrust, ShellRC: rep.RCPath,
		ShellState: rep.RCState, NextCommand: rep.NextCommand}
	if rep.RCState == "failed" {
		p.Warn(fmt.Sprintf("could not install shell block into %s: %s", rep.RCPath, rep.RCError))
	}
	v := core.View{core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: "wrote " + rep.ConfigPath, Silent: true}}
	if rep.AutoTrust != "" {
		msg := "auto-trust: agents started under " + rep.AutoTrust + " skip the folder trust prompt"
		v = append(v, core.Line{Text: msg, Txt: msg})
	}
	v = append(v,
		core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: "setup complete", Txt: "setup complete. Next: " + rep.NextCommand},
		core.Hint{Cmd: rep.NextCommand},
		core.Note{Text: "Docs: https://github.com/tristanbietsch/rex"},
		agentPairs(core.Field{Key: "mode", Val: "applied"}, core.Field{Key: "config", Val: rep.ConfigPath},
			core.Field{Key: "shell_rc", Val: rep.RCPath}, core.Field{Key: "shell", Val: rep.RCState},
			core.Field{Key: "next", Val: rep.NextCommand}),
	)
	return p.Emit(core.Result{Cmd: "setup", Data: data, View: v})
}
