package cli

import (
	"flag"
	"fmt"
	"strconv"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/tui"
)

// RunRender connects to the daemon, fetches a snapshot, and prints the
// rendered TUI board to stdout. Useful for screenshots and sanity-checking
// the visual layout without launching the interactive TUI.
func RunRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	widthFlag := fs.String("w", "120", "render width")
	heightFlag := fs.String("h", "30", "render height")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, _ := strconv.Atoi(*widthFlag)
	h, _ := strconv.Atoi(*heightFlag)

	c, err := client.Dial(*socket)
	if err != nil {
		return fmt.Errorf("dial daemon: %w", err)
	}
	defer c.Close()
	snap, err := c.Hello("rex-render")
	if err != nil {
		return fmt.Errorf("hello: %w", err)
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
	fmt.Println(m.View())
	return nil
}
