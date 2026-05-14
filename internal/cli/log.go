package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunLog prints (or tails) the transcript for a session.
//
// Plan B reads the transcript file directly from disk. Plan C will optionally
// route through the daemon's SessionOutput stream for live in-memory tailing.
func RunLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	stateDir := fs.String("state-dir", defaultStateDir(), "state dir (rarely needed)")
	follow := fs.Bool("f", false, "follow the transcript (like tail -f)")
	tailN := fs.Int("n", 0, "show only the last N lines (0 = all)")
	asBytes := fs.Bool("bytes", false, "print raw bytes (including ANSI)")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "log: exactly one selector required")
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

	path := filepath.Join(*stateDir, "sessions", sess.ID, "transcript.log")
	if err := streamFile(path, os.Stdout, *follow, *tailN, *asBytes); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}

// defaultStateDir mirrors rex-daemon's default state dir choice.
func defaultStateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "rex")
}

func streamFile(path string, out io.Writer, follow bool, tailN int, raw bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if tailN > 0 {
		if err := emitTail(f, out, tailN, raw); err != nil {
			return err
		}
	} else {
		if err := emitAll(f, out, raw); err != nil {
			return err
		}
	}
	if !follow {
		return nil
	}
	// Follow mode: poll for new bytes every 100ms.
	for {
		_, err := io.Copy(out, f)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func emitAll(r io.Reader, w io.Writer, raw bool) error {
	if raw {
		_, err := io.Copy(w, r)
		return err
	}
	return copyStripped(r, w)
}

func emitTail(f *os.File, w io.Writer, n int, raw bool) error {
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	lines := splitLastN(data, n)
	if raw {
		_, err := w.Write(lines)
		return err
	}
	cleaned := stripANSI(lines)
	_, err = w.Write(cleaned)
	return err
}

func copyStripped(r io.Reader, w io.Writer) error {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			cleaned := stripANSI(buf[:n])
			if _, werr := w.Write(cleaned); werr != nil {
				return werr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
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

// stripANSI removes CSI escape sequences (color and cursor codes).
func stripANSI(b []byte) []byte {
	out := make([]byte, 0, len(b))
	i := 0
	for i < len(b) {
		if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '[' {
			j := i + 2
			for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
				j++
			}
			if j < len(b) {
				j++
			}
			i = j
			continue
		}
		out = append(out, b[i])
		i++
	}
	return out
}
