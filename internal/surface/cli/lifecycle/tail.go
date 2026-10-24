package lifecycle

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"
)

// logTailLines is how many lines `rex daemon logs` prints before following.
const logTailLines = 200

// tailPollInterval is how often `rex daemon logs -f` checks for new output.
const tailPollInterval = 250 * time.Millisecond

// tailFile writes the last n lines of path to w, then (follow) keeps writing
// what is appended until the process is interrupted. Pure Go so it works
// where there is no `tail` (Windows).
func tailFile(w io.Writer, path string, n int, follow bool) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("read log: %w", err)
	}
	if _, err := w.Write(lastLines(b, n)); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	buf := make([]byte, 32*1024)
	for {
		m, rerr := f.Read(buf)
		if m > 0 {
			if _, err := w.Write(buf[:m]); err != nil {
				return err
			}
		}
		if rerr == io.EOF {
			time.Sleep(tailPollInterval)
			continue
		}
		if rerr != nil {
			return fmt.Errorf("read log: %w", rerr)
		}
	}
}

// lastLines returns the tail of b holding its last n lines.
func lastLines(b []byte, n int) []byte {
	end := len(b)
	if end > 0 && b[end-1] == '\n' {
		end-- // a trailing newline ends the last line; it does not start a new one
	}
	for i := 0; i < n; i++ {
		j := bytes.LastIndexByte(b[:end], '\n')
		if j < 0 {
			return b
		}
		end = j
	}
	return b[end+1:]
}
