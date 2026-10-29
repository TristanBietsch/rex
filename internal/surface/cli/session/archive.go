package session

import (
	"flag"
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// archivedPrefix marks an archived session's Title; rex ls hides those
// unless -show-archived.
const archivedPrefix = "[archived] "

func isArchived(s protocol.SessionSummary) bool {
	return strings.HasPrefix(s.Title, archivedPrefix)
}

// RunArchive marks a completed session as archived by prefixing its Title.
func RunArchive(args []string) error {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 1, 1, "rex archive <sel> [-socket path]"); err != nil {
		return err
	}
	sel := pos[0]

	c, sess, err := core.Connect(*socket, sel)
	if err != nil {
		return err
	}
	defer c.Close()
	if isArchived(sess) {
		fmt.Printf("%s: already archived\n", sess.Slug)
		return nil
	}
	newTitle := archivedPrefix + sess.Title
	reqID := "cli-archive-" + sess.ShortID
	if err := c.Rename(sess.ID, "", newTitle, reqID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := core.AwaitPatch(c, sess.ID, reqID, "title"); err != nil {
		return err
	}
	fmt.Printf("%s: archived\n", sess.Slug)
	return nil
}
