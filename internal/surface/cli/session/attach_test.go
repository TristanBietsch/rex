package session

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndexByte(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		c    byte
		want int
	}{
		{"empty", nil, 0x1d, -1},
		{"absent", []byte("hello"), 0x1d, -1},
		{"first", []byte{0x1d, 'a'}, 0x1d, 0},
		{"middle", []byte("ab\x1dcd"), 0x1d, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := indexByte(c.in, c.c)
			if got != c.want {
				t.Fatalf("want %d got %d", c.want, got)
			}
		})
	}
}

// chunkReader returns one chunk per Read, like a terminal delivering
// keystrokes, then err (io.EOF when nil).
type chunkReader struct {
	chunks []string
	err    error
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if r.chunks[0] == "" {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

// runPump runs pumpInput over chunks and returns what it sent and whether
// it reported a detach.
func runPump(t *testing.T, r io.Reader, readOnly bool) (sent []string, detached bool) {
	t.Helper()
	detached = pumpInput(r, readOnly, func(b []byte) { sent = append(sent, string(b)) })
	return sent, detached
}

func TestPumpInput_forwardsUntilDetach(t *testing.T) {
	sent, detached := runPump(t, &chunkReader{chunks: []string{"ab", "c\x1dd"}}, false)
	require.True(t, detached)
	require.Equal(t, []string{"ab", "c"}, sent, "bytes after the detach key must not reach the session")
}

func TestPumpInput_detachKeyFirst(t *testing.T) {
	sent, detached := runPump(t, &chunkReader{chunks: []string{"\x1dxyz"}}, false)
	require.True(t, detached)
	require.Empty(t, sent)
}

func TestPumpInput_EOFEndsWithoutDetach(t *testing.T) {
	sent, detached := runPump(t, &chunkReader{chunks: []string{"ab"}}, false)
	require.False(t, detached)
	require.Equal(t, []string{"ab"}, sent)
}

func TestPumpInput_readErrorEndsWithoutDetach(t *testing.T) {
	_, detached := runPump(t, &chunkReader{err: errors.New("read /dev/stdin: input/output error")}, false)
	require.False(t, detached)
}

// P0-10: read-only attach put the terminal in raw mode (no SIGINT) but never
// read stdin, so neither Ctrl+] nor Ctrl+C could get the user out.
func TestPumpInput_readOnlyDetaches(t *testing.T) {
	sent, detached := runPump(t, &chunkReader{chunks: []string{"hello\r", "\x03", "x\x1dy"}}, true)
	require.True(t, detached)
	require.Empty(t, sent, "read-only attach forwarded input")
}

func TestPumpInput_readOnlyEOFDiscards(t *testing.T) {
	sent, detached := runPump(t, &chunkReader{chunks: []string{"typed"}}, true)
	require.False(t, detached)
	require.Empty(t, sent)
}

func TestKeepReadingStdin(t *testing.T) {
	require.True(t, keepReadingStdin(false, false), "interactive attach always forwards stdin")
	require.True(t, keepReadingStdin(false, true))
	require.True(t, keepReadingStdin(true, true), "read-only attach in raw mode needs the detach reader")
	require.False(t, keepReadingStdin(true, false), "read-only attach without a tty keeps normal signals")
}

func TestCloseAfterInput(t *testing.T) {
	require.True(t, closeAfterInput(false, true))
	require.True(t, closeAfterInput(false, false), "interactive attach ends when stdin closes")
	require.True(t, closeAfterInput(true, true))
	require.False(t, closeAfterInput(true, false), "an observer keeps watching after stdin closes")
}
