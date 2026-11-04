package inspect

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var stripCases = []struct {
	name, in, want string
}{
	{"plain", "plain text\n", "plain text\n"},
	{"SGR", "\x1b[31mred\x1b[0m\n", "red\n"},
	{"private CSI", "\x1b[?25l\x1b[?2026hx\x1b[?25h", "x"},
	// F-02: the lone ESC used to survive and fuse with the trailing '['.
	{"F-02 crasher", "\x1b\x1b[A[", "["},
	{"OSC BEL", "\x1b]0;title\x07after", "after"},
	{"OSC ST", "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\", "link"},
	{"DCS", "a\x1bP1$r0m\x1b\\b", "ab"},
	{"APC", "a\x1b_Gf=100;AAAA\x1b\\b", "ab"},
	{"two-byte escape", "\x1b7saved\x1b8", "saved"},
	{"charset designation", "\x1b(Bascii", "ascii"},
	{"trailing ESC", "trailing\x1b", "trailing"},
	{"unterminated CSI", "a\x1b[31", "a"},
	{"CAN cancels CSI", "a\x1b[31\x18b", "ab"},
	{"ESC restarts CSI", "a\x1b[31\x1b[0mb", "ab"},
	{"C0 inside CSI runs", "a\x1b[3\n1mb", "a\nb"},
	{"newline ends unterminated OSC", "\x1b]0;broken\nnext line\n", "\nnext line\n"},
	{"ESC in OSC starts new sequence", "\x1b]0;t\x1b[31mred", "red"},
	{"UTF-8 kept", "\x1b[1mcafé ✓\x1b[0m", "café ✓"},
	{"non-ASCII ends malformed CSI", "\x1b[3é", "é"},
	{"broken UTF-8 kept", "\xff\xfe x", "\xff\xfe x"},
}

// stripANSI strips b in one call.
func stripANSI(b []byte) []byte {
	var s ansiStripper
	return s.strip(make([]byte, 0, len(b)), b)
}

func TestStripANSI(t *testing.T) {
	for _, c := range stripCases {
		t.Run(c.name, func(t *testing.T) {
			got := stripANSI([]byte(c.in))
			require.Equal(t, c.want, string(got))
			require.NotContains(t, string(got), "\x1b")
		})
	}
}

// The stripper keeps state between writes, so a sequence split across two
// reads of the transcript is still removed.
func TestStripWriter_everySplitPoint(t *testing.T) {
	for _, c := range stripCases {
		for cut := 0; cut <= len(c.in); cut++ {
			var out bytes.Buffer
			w := newStripWriter(&out)
			n, err := w.Write([]byte(c.in[:cut]))
			require.NoError(t, err)
			require.Equal(t, cut, n)
			_, err = w.Write([]byte(c.in[cut:]))
			require.NoError(t, err)
			require.Equal(t, c.want, out.String(), "%s split at %d", c.name, cut)
		}
	}
}

func writeTranscript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.log")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestStreamFile(t *testing.T) {
	path := writeTranscript(t, "\x1b[1mone\x1b[0m\ntwo\n\x1b[31mthree\x1b[0m\n")
	cases := []struct {
		name  string
		tailN int
		raw   bool
		want  string
	}{
		{"all stripped", 0, false, "one\ntwo\nthree\n"},
		{"all raw", 0, true, "\x1b[1mone\x1b[0m\ntwo\n\x1b[31mthree\x1b[0m\n"},
		{"tail stripped", 2, false, "two\nthree\n"},
		{"tail raw", 1, true, "\x1b[31mthree\x1b[0m\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, streamFile(context.Background(), path, &out, false, c.tailN, c.raw))
			require.Equal(t, c.want, out.String())
		})
	}
}

func TestStreamFile_missing(t *testing.T) {
	var out bytes.Buffer
	require.Error(t, streamFile(context.Background(), filepath.Join(t.TempDir(), "nope"), &out, false, 0, false))
}

// syncBuffer is a bytes.Buffer safe for the follow goroutine and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// `rex log -f` copied appended bytes raw, so escapes reached the terminal.
// Appends that split a sequence must still be stripped.
func TestStreamFile_followStripsAcrossAppends(t *testing.T) {
	path := writeTranscript(t, "start\x1b[3")
	ctx, cancel := context.WithCancel(context.Background())
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- streamFile(ctx, path, &out, true, 0, false) }()

	require.Eventually(t, func() bool { return out.String() == "start" }, 5*time.Second, time.Millisecond)
	for _, chunk := range []string{"1mred\x1b", "]0;ti", "tle\x07", " done\n"} {
		appendFile(t, path, chunk)
		time.Sleep(3 * followInterval)
	}
	require.Eventually(t, func() bool { return out.String() == "startred done\n" }, 5*time.Second, time.Millisecond,
		"got %q", out.String())

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("follow did not stop on cancel")
	}
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(s)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestSplitLastN(t *testing.T) {
	require.Equal(t, "c\n", string(splitLastN([]byte("a\nb\nc\n"), 1)))
	require.Equal(t, "b\nc", string(splitLastN([]byte("a\nb\nc"), 1)))
	require.Equal(t, "a\nb\n", string(splitLastN([]byte("a\nb\n"), 5)))
	require.Equal(t, "x", string(splitLastN([]byte("x"), 0)))
}
