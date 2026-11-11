package session

import (
	"flag"
	"io"
	"os"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunSend forwards raw stdin bytes to a session's PTY.
func RunSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	rest, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "send: exactly one selector required")
	}
	sel := rest[0]

	c, err := socket.Dial()
	if err != nil {
		return err
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
