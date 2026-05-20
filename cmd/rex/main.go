// Package main is the rex CLI entry point.
package main

import (
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
	if err := run(os.Args[1:]); err != nil {
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, "rex:", err)
		}
		os.Exit(exitCodeFor(err))
	}
}

func run(args []string) error {
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
	case "rename":
		return session.RunRename(args[1:])
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
	if e, ok := err.(core.ExitCoder); ok {
		return e.ExitCode()
	}
	return 1
}
