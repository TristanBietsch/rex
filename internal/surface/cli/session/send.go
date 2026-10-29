package session

import (
	"flag"
	"io"
	"os"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// RunSend forwards raw stdin bytes to a session's PTY.
func RunSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 1, 1, "rex send <sel> [-socket path] < input"); err != nil {
		return err
	}
	sel := pos[0]

	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, sel)
	if err != nil {
		return err
	}

	buf := make([]byte, 4096)
	for {
		n, rerr := os.Stdin.Read(buf)
		if n > 0 {
			if err := c.SendInput(sess.ID, buf[:n]); err != nil {
				return core.NewExitError(core.ExitGeneric, err.Error())
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return core.NewExitError(core.ExitGeneric, rerr.Error())
		}
	}
}
