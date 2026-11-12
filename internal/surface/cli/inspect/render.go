package inspect

import (
	"flag"
	"fmt"
	"strconv"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// RunRender connects to the daemon, fetches a snapshot, and prints the
// rendered TUI board to stdout. Useful for screenshots and sanity-checking
// the visual layout without launching the interactive TUI.
func RunRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.ContentFormats)
	widthFlag := fs.String("w", "120", "render width")
	heightFlag := fs.String("h", "30", "render height")
	_, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	w, _ := strconv.Atoi(*widthFlag)
	h, _ := strconv.Atoi(*heightFlag)

	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	snap, err := c.Hello("rex-render")
	if err != nil {
		return core.HelloFailed(err)
	}

	m := tui.Model{
		Sessions: snap.Sessions,
		Filter:   "all",
		Width:    w,
		Height:   h,
	}
	if len(snap.Sessions) > 0 {
		m.SelectedID = snap.Sessions[0].ID
	}
	_, err = fmt.Fprintln(core.Out().Out, m.View())
	return err
}
