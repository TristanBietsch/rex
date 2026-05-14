package cli

import (
	"flag"
	"io"
	"os"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunSend forwards raw stdin bytes to a session's PTY.
func RunSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "send: exactly one selector required")
	}
	sel := fs.Arg(0)

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := ResolveSelector(c, sel)
	if err != nil {
		return err
	}

	buf := make([]byte, 4096)
	for {
		n, rerr := os.Stdin.Read(buf)
		if n > 0 {
			if err := c.SendInput(sess.ID, buf[:n]); err != nil {
				return NewExitError(ExitGeneric, err.Error())
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return NewExitError(ExitGeneric, rerr.Error())
		}
	}
}
