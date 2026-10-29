package inspect

import (
	"flag"
	"fmt"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// RunRender connects to the daemon, fetches a snapshot, and prints the
// rendered TUI board to stdout. Useful for screenshots and sanity-checking
// the visual layout without launching the interactive TUI.
func RunRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	w := fs.Int("w", 120, "render width")
	h := fs.Int("h", 30, "render height")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 0, 0, "rex render [-w cols] [-h rows] [-socket path]"); err != nil {
		return err
	}
	if *w <= 0 || *h <= 0 {
		return core.NewExitError(core.ExitInvalidArgs, fmt.Sprintf("render: -w and -h must be positive (got %dx%d)", *w, *h))
	}

	c, snap, err := core.Dial(*socket)
	if err != nil {
		return err
	}
	defer c.Close()

	m := tui.Model{
		Sessions: snap.Sessions,
		Filter:   "all",
		Width:    *w,
		Height:   *h,
	}
	if len(snap.Sessions) > 0 {
		m.SelectedID = snap.Sessions[0].ID
	}
	fmt.Println(m.View())
	return nil
}
