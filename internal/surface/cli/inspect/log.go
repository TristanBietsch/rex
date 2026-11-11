package inspect

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunLog prints (or tails) the transcript for a session by reading the
// transcript file directly from disk.
func RunLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	stateDir := fs.String("state-dir", "", "state dir (default ~/.local/share/rex; rarely needed)")
	follow := fs.Bool("f", false, "follow the transcript (like tail -f)")
	tailN := fs.Int("n", 0, "show only the last N lines (0 = all)")
	asBytes := fs.Bool("bytes", false, "print raw bytes (including ANSI)")
	rest, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "log: exactly one selector required")
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

	root := *stateDir
	if root == "" {
		if root, err = paths.DataDir(); err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error()+"; pass -state-dir")
		}
	}
	path := filepath.Join(root, "sessions", sess.ID, "transcript.log")
	// -f runs until the process is interrupted; the default SIGINT handler
	// ends it, so no cancellation is wired here.
	if err := streamFile(context.Background(), path, os.Stdout, *follow, *tailN, *asBytes); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}

// followInterval is how often `rex log -f` polls the transcript for growth.
const followInterval = 100 * time.Millisecond

// streamFile prints the transcript at path to out: the last tailN lines
// (all when tailN <= 0), then, with follow, everything appended until ctx
// is done. Unless raw, escape sequences are stripped by one stateful
// stripper for the whole stream, so a sequence split between the initial
// read and a later append (or between two appends) is still removed.
func streamFile(ctx context.Context, path string, out io.Writer, follow bool, tailN int, raw bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dst := out
	if !raw {
		dst = newStripWriter(out)
	}
	if tailN > 0 {
		err = emitTail(f, dst, tailN)
	} else {
		_, err = io.Copy(dst, f)
	}
	if err != nil || !follow {
		return err
	}
	return followFile(ctx, f, dst, followInterval)
}

// emitTail writes the last n lines of r to w.
func emitTail(r io.Reader, w io.Writer, n int) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	_, err = w.Write(splitLastN(data, n))
	return err
}

// followFile copies whatever is appended to f into w, polling every
// interval, until ctx is done.
func followFile(ctx context.Context, f io.Reader, w io.Writer, interval time.Duration) error {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if _, err := io.Copy(w, f); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func splitLastN(b []byte, n int) []byte {
	if n <= 0 || len(b) == 0 {
		return b
	}
	count := 0
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == '\n' {
			count++
			if count == n+1 {
				return b[i+1:]
			}
		}
	}
	return b
}
