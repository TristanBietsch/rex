package inspect

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestANSIStripper(t *testing.T) {
	in := "a\x1b[31mred\x1b[0m b\x1b[2;5Hc \x1b]0;t\x07 \x1b"
	want := "ared bc \x1b]0;t\x07 "

	var whole bytes.Buffer
	_, err := (&ansiStripper{w: &whole}).Write([]byte(in))
	require.NoError(t, err)
	require.Equal(t, want, whole.String())

	// Split at every byte boundary: an escape cut across reads must still go.
	for i := 1; i < len(in); i++ {
		var got bytes.Buffer
		s := &ansiStripper{w: &got}
		_, _ = s.Write([]byte(in[:i]))
		_, _ = s.Write([]byte(in[i:]))
		require.Equal(t, want, got.String(), "split at %d", i)
	}
}

// syncBuffer is a bytes.Buffer safe to read while streamFile writes it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// -f used to copy appended bytes raw, so escapes leaked after the first dump.
func TestStreamFile_FollowStrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.log")
	require.NoError(t, os.WriteFile(path, []byte("one \x1b[1mbold\x1b[0m\n"), 0o644))

	var out syncBuffer
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- streamFile(path, &out, true, 0, false, stop) }()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString("two \x1b[3")
	require.NoError(t, err)
	time.Sleep(3 * logPollInterval) // let the follow loop read the split escape
	_, err = f.WriteString("2mgreen\x1b[0m\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.Eventually(t, func() bool { return strings.Contains(out.String(), "green") }, 2*time.Second, 10*time.Millisecond)
	close(stop)
	require.NoError(t, <-done)
	require.Equal(t, "one bold\ntwo green\n", out.String())
}

func TestStreamFile_TailAndRaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.log")
	require.NoError(t, os.WriteFile(path, []byte("a\n\x1b[1mb\x1b[0m\nc\n"), 0o644))

	var out bytes.Buffer
	require.NoError(t, streamFile(path, &out, false, 2, false, nil))
	require.Equal(t, "b\nc\n", out.String())

	out.Reset()
	require.NoError(t, streamFile(path, &out, false, 0, true, nil))
	require.Equal(t, "a\n\x1b[1mb\x1b[0m\nc\n", out.String())
}
