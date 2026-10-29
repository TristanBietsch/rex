package session

import (
	"flag"
	"os"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunLs prints session table or JSONL.
func RunLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	stateFilter := fs.String("state", "", "filter by state")
	toolFilter := fs.String("tool", "", "filter by tool id")
	modelFilter := fs.String("model", "", "filter by model id")
	showArchived := fs.Bool("show-archived", false, "include archived sessions (rex archive)")
	asJSON := fs.Bool("json", false, "output JSONL")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 0, 0, "rex ls [-state s] [-tool id] [-model id] [-show-archived] [-json] [-socket path]"); err != nil {
		return err
	}

	c, snap, err := core.Dial(*socket)
	if err != nil {
		return err
	}
	defer c.Close()

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
		if !*showArchived && isArchived(s) {
			continue
		}
		filtered = append(filtered, s)
	}

	if *asJSON {
		return core.WriteSessionsJSONL(os.Stdout, filtered)
	}
	return core.WriteSessionsTable(os.Stdout, filtered)
}
