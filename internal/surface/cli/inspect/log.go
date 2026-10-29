package inspect

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunLog prints (or tails) the transcript for a session by reading the
// transcript file directly from disk.
func RunLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	stateDir := fs.String("state-dir", daemonctl.DefaultStateDir(), "state dir (rarely needed)")
	follow := fs.Bool("f", false, "follow the transcript (like tail -f)")
	tailN := fs.Int("n", 0, "show only the last N lines (0 = all)")
	asBytes := fs.Bool("bytes", false, "print raw bytes (including ANSI)")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 1, 1, "rex log <sel> [-f] [-n lines] [-bytes] [-state-dir dir] [-socket path]"); err != nil {
		return err
	}
	sel := pos[0]

	c, sess, err := core.Connect(*socket, sel)
	if err != nil {
		return err
	}
	defer c.Close()

	path := filepath.Join(*stateDir, "sessions", sess.ID, "transcript.log")
	if err := streamFile(path, os.Stdout, *follow, *tailN, *asBytes, nil); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}

// logPollInterval is how often `rex log -f` checks for new transcript bytes.
const logPollInterval = 100 * time.Millisecond

// streamFile writes the transcript at path to w: all of it, or its last
// tailN lines; with follow it then keeps writing what is appended until stop
// closes (nil: never), across the daemon's rotations. Unless raw, CSI
// escapes are stripped from all of it.
func streamFile(path string, w io.Writer, follow bool, tailN int, raw bool, stop <-chan struct{}) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	// f changes when the transcript rotates; close whichever is open last.
	defer func() { _ = f.Close() }()

	out := w
	if !raw {
		out = &ansiStripper{w: w}
	}
	if tailN > 0 {
		data, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		if _, err := out.Write(splitLastN(data, tailN)); err != nil {
			return err
		}
	} else if _, err := io.Copy(out, f); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	for {
		select {
		case <-stop:
			return nil
		case <-time.After(logPollInterval):
		}
		if _, err := io.Copy(out, f); err != nil {
			return err
		}
		if f, err = followRotation(path, f, out); err != nil {
			return err
		}
	}
}

// followRotation returns the file to keep following once f is drained. The
// daemon rotates a transcript by closing it, renaming it to
// transcript.log.1 and starting a new file at path: when path no longer
// names f, what is left in f is copied to out and the new file is opened,
// to be read from its start. A file truncated in place is read again from
// its start.
func followRotation(path string, f *os.File, out io.Writer) (*os.File, error) {
	cur, err := f.Stat()
	if err != nil {
		return f, err
	}
	next, statErr := os.Stat(path)
	if statErr == nil && os.SameFile(cur, next) {
		if off, err := f.Seek(0, io.SeekCurrent); err == nil && next.Size() < off {
			_, err = f.Seek(0, io.SeekStart)
			return f, err
		}
		return f, nil
	}
	if statErr == nil {
		if nf, err := os.Open(path); err == nil {
			// The daemon closed f before the rename: this read gets the
			// last bytes it wrote there.
			if _, err := io.Copy(out, f); err != nil {
				_ = nf.Close()
				return f, err
			}
			_ = f.Close()
			return nf, nil
		}
	}
	// path is missing mid-rotation (or not yet openable): keep f, and the
	// next poll looks again.
	return f, nil
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

// ansiStripper writes to w with CSI escape sequences (color and cursor
// codes) removed. It keeps its parse state between writes, so a sequence
// split across two reads is still removed. An ESC that does not start a CSI
// passes through.
type ansiStripper struct {
	w     io.Writer
	state stripState
}

type stripState int

const (
	stripText stripState = iota
	stripEsc             // saw ESC; held until the next byte shows whether it starts a CSI
	stripCSI             // inside ESC [ … up to the final byte 0x40–0x7e
)

// Write implements io.Writer; it reports len(p) on success.
func (s *ansiStripper) Write(p []byte) (int, error) {
	out := make([]byte, 0, len(p)+1)
	for _, b := range p {
		switch s.state {
		case stripEsc:
			if b == '[' {
				s.state = stripCSI
				continue
			}
			out = append(out, 0x1b)
			s.state = stripText
			fallthrough
		case stripText:
			if b == 0x1b {
				s.state = stripEsc
				continue
			}
			out = append(out, b)
		case stripCSI:
			if b >= 0x40 && b <= 0x7e {
				s.state = stripText
			}
		}
	}
	if _, err := s.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}
