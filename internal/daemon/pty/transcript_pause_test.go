package pty

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// toggleTranscript fails while fail is set and records what it wrote.
type toggleTranscript struct {
	fail  bool
	got   []byte
	tries int
}

func (w *toggleTranscript) Write(b []byte) error {
	w.tries++
	if w.fail {
		return errors.New("no space left on device")
	}
	w.got = append(w.got, b...)
	return nil
}

func (w *toggleTranscript) Close() error { return nil }

func TestPausingTranscript(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w := &toggleTranscript{}
	p := newPausingTranscript(w, "s1", func() time.Time { return now })

	p.Write([]byte("a"))
	require.Equal(t, "a", string(w.got))

	w.fail = true
	p.Write([]byte("bb")) // fails: pauses
	require.True(t, p.paused())
	tries := w.tries
	now = now.Add(transcriptRetryEvery - time.Second)
	p.Write([]byte("ccc")) // paused: not even tried
	require.Equal(t, tries, w.tries)
	require.EqualValues(t, 5, p.dropped)

	now = now.Add(time.Second)
	p.Write([]byte("dddd")) // retry fails: paused again
	require.Equal(t, tries+1, w.tries)
	require.True(t, p.paused())

	w.fail = false
	now = now.Add(transcriptRetryEvery)
	p.Write([]byte("e")) // retry succeeds: resumed
	require.False(t, p.paused())
	require.Zero(t, p.dropped)
	require.Equal(t, "ae", string(w.got))
}
