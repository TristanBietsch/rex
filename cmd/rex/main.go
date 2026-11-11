// Package main is the rex CLI entry point.
package main

import (
	"errors"
	"fmt"
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
		os.Exit(exitCodeFor(err))
	}
}

// kitVerbs are the commands rendered through the output kit. They accept
// -o/--output, --agent and -json; the rest still print their own format, so
// an explicit output flag before one of them is refused rather than ignored.
var kitVerbs = map[string]bool{"ls": true, "status": true}

func run(args []string) error {
	if core.Out().Flags.Explicit() {
		verb := "rex"
		if len(args) > 0 {
			verb = args[0]
		}
		if !kitVerbs[verb] {
			return core.NewExitError(core.ExitInvalidArgs,
				verb+": -o, --agent and -json are not supported yet")
		}
	}
	if len(args) == 0 {
		return meta.RunTUI()
	}
	switch args[0] {
	case "--help", "-h", "help":
		return core.RunHelp()
	case "--version", "-v", "version":
		return core.RunVersion()
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
		return fmt.Errorf("unknown command %q (try `rex --help`)", args[0])
	}
}

func exitCodeFor(err error) int {
	var e core.ExitCoder
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return 1
}
