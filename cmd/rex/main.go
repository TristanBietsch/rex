// Package main is the rex CLI entry point.
package main

import (
	"errors"
	"os"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/cli/inspect"
	"github.com/tristanbietsch/rex/internal/surface/cli/lifecycle"
	"github.com/tristanbietsch/rex/internal/surface/cli/meta"
	"github.com/tristanbietsch/rex/internal/surface/cli/session"
	"github.com/tristanbietsch/rex/internal/surface/cli/setup"
)

func main() {
	restoreVT := core.EnableVT(os.Stdout, os.Stderr)
	args, err := core.InitOutput(os.Args[1:], os.Environ())
	if err == nil {
		err = run(args)
	}
	restoreVT()
	if err != nil {
		core.Out().RenderError(err)
		core.RecordError(os.Args[1:], err, core.ErrLogOptions{})
		os.Exit(exitCodeFor(err))
	}
}

// interactiveVerbs are the commands that need a terminal: the board (bare
// `rex`) and attach. They cannot render as data, so an output flag before
// one of them is refused, with the query that answers the same question.
var interactiveVerbs = map[string]string{
	"rex":    "rex ls",
	"attach": "rex log <sel> -n 50",
}

func run(args []string) error {
	verb := "rex"
	if len(args) > 0 {
		verb = args[0]
	}
	if hint, ok := interactiveVerbs[verb]; ok && core.Out().Flags.Explicit() {
		return core.Errorf(core.ExitInvalidArgs, hint,
			"%s: interactive; -o, --agent and -json do not apply (try `%s`)", verb, hint)
	}
	if len(args) == 0 {
		// Includes agent mode switched on by CLAUDECODE: a board has no
		// agent form.
		if err := core.Out().RequireText("rex", interactiveVerbs["rex"]); err != nil {
			return err
		}
		return meta.RunTUI()
	}
	switch args[0] {
	case "--help", "-h":
		return core.RunHelp(args[1:])
	case "help":
		// `rex help <command>` is `rex <command> -h`.
		if len(args) >= 2 {
			if _, ok := core.FindCommand(args[1]); ok {
				return run(append([]string{args[1], "-h"}, args[2:]...))
			}
		}
		return core.RunHelp(args[1:])
	case "agent-help":
		return core.RunAgentHelp(args[1:])
	case "errors":
		return inspect.RunErrors(args[1:])
	case "--version", "-v", "version":
		return core.RunVersion(args[1:])
	case "status":
		return inspect.RunStatus(args[1:])
	case "ls":
		return session.RunLs(args[1:])
	case "new":
		return session.RunNew(args[1:])
	case "attach":
		return session.RunAttach(args[1:])
	case "reply":
		return session.RunReply(args[1:])
	case "send":
		return session.RunSend(args[1:])
	case "log":
		return inspect.RunLog(args[1:])
	case "wait":
		return session.RunWait(args[1:])
	case "rm":
		return session.RunRm(args[1:])
	case "prune":
		return session.RunPrune(args[1:])
	case "rename":
		return session.RunRename(args[1:])
	case "name":
		return session.RunName(args[1:])
	case "done", "retire":
		return session.RunDone(args[1:])
	case "undone", "unretire":
		return session.RunUndone(args[1:])
	case "archive":
		return session.RunArchive(args[1:])
	case "resume":
		return session.RunResume(args[1:])
	case "complete":
		return meta.RunComplete(args[1:])
	case "reload":
		return lifecycle.RunReload(args[1:])
	case "daemon":
		return lifecycle.RunDaemon(args[1:])
	case "completion":
		return meta.RunCompletion(args[1:])
	case "render":
		return inspect.RunRender(args[1:])
	case "config":
		return setup.RunConfig(args[1:])
	case "setup":
		return setup.RunSetup(args[1:])
	case "doctor":
		return setup.RunDoctor(args[1:])
	case "update":
		return setup.RunUpdate(args[1:])
	case "uninstall":
		return setup.RunUninstall(args[1:])
	case "digest":
		return inspect.RunDigest(args[1:])
	case "stats":
		return inspect.RunStats(args[1:])
	case "fleet":
		return inspect.RunFleet(args[1:])
	default:
		return unknownCommand(args[0])
	}
}

// commands is every name the switch in run accepts, for "did you mean". It
// comes from the command table (TestRun_EveryCommandIsKnown checks the two
// agree).
var commands = core.CommandNames()

// unknownCommand is the exit-5 error for a command rex does not have, with
// the closest real one when it is within two edits.
func unknownCommand(name string) error {
	if s := core.Suggest(name, commands); s != "" {
		return core.Errorf(core.ExitInvalidArgs, "rex "+s,
			"unknown command %q; did you mean %q? (try `rex --help`)", name, s)
	}
	return core.Errorf(core.ExitInvalidArgs, "rex --help", "unknown command %q (try `rex --help`)", name)
}

func exitCodeFor(err error) int {
	var e core.ExitCoder
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return 1
}
