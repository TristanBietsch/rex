// Package main is the rex CLI entry point.
package main

import (
	"fmt"
	"os"

	"github.com/tristanbietsch/rex/internal/cli"
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
		return cli.RunTUI()
	}
	switch args[0] {
	case "--help", "-h", "help":
		return cli.RunHelp()
	case "--version", "-v", "version":
		return cli.RunVersion()
	case "status":
		return cli.RunStatus(args[1:])
	case "ls":
		return cli.RunLs(args[1:])
	case "new":
		return cli.RunNew(args[1:])
	case "attach":
		return cli.RunAttach(args[1:])
	case "reply":
		return cli.RunReply(args[1:])
	case "send":
		return cli.RunSend(args[1:])
	case "log":
		return cli.RunLog(args[1:])
	case "wait":
		return cli.RunWait(args[1:])
	case "rm":
		return cli.RunRm(args[1:])
	case "rename":
		return cli.RunRename(args[1:])
	case "archive":
		return cli.RunArchive(args[1:])
	case "reload":
		return cli.RunReload(args[1:])
	case "daemon":
		return cli.RunDaemon(args[1:])
	case "completion":
		return cli.RunCompletion(args[1:])
	case "render":
		return cli.RunRender(args[1:])
	case "config":
		return cli.RunConfig(args[1:])
	default:
		return fmt.Errorf("unknown command %q (try `rex --help`)", args[0])
	}
}

func exitCodeFor(err error) int {
	if e, ok := err.(cli.ExitCoder); ok {
		return e.ExitCode()
	}
	return 1
}
