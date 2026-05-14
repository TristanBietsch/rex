package cli

import (
	"flag"
	"os"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// RunLs prints session table or JSONL.
func RunLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	stateFilter := fs.String("state", "", "filter by state")
	toolFilter := fs.String("tool", "", "filter by tool id")
	modelFilter := fs.String("model", "", "filter by model id")
	showArchived := fs.Bool("show-archived", false, "include archived sessions")
	short := fs.Bool("short", false, "compact mode (no-op in Plan B; reserved)")
	asJSON := fs.Bool("json", false, "output JSONL")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	_ = showArchived
	_ = short

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}

	filtered := make([]protocol.SessionSummary, 0, len(snap.Sessions))
	for _, s := range snap.Sessions {
		if *stateFilter != "" && string(s.State) != *stateFilter {
			continue
		}
		if *toolFilter != "" && s.ToolID != *toolFilter {
			continue
		}
		if *modelFilter != "" && s.ModelID != *modelFilter {
			continue
		}
		filtered = append(filtered, s)
	}

	if *asJSON {
		return WriteSessionsJSONL(os.Stdout, filtered)
	}
	return WriteSessionsTable(os.Stdout, filtered)
}
