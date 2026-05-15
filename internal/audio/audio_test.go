package audio

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBakeNonEmpty verifies that every catalog event renders to non-zero PCM.
// Doesn't open an audio device (CI-safe).
func TestBakeNonEmpty(t *testing.T) {
	p := &Player{pcm: make(map[string][]byte)}
	p.bakeAll()
	for _, ev := range []string{EventStartup, EventCreate, EventDone, EventDelete, EventNav, EventOpen, EventClose} {
		b, ok := p.pcm[ev]
		require.True(t, ok, "no pcm for %s", ev)
		require.Greater(t, len(b), 0, "pcm empty for %s", ev)
	}
}

// TestPlayDegradedNoop ensures Play does nothing when the player isn't enabled.
func TestPlayDegradedNoop(t *testing.T) {
	p := &Player{pcm: make(map[string][]byte)}
	p.bakeAll()
	// Player not enabled (no oto context). Should not panic.
	p.Play(EventStartup)
}

// TestNewDisabledIsNoop covers the explicit-disabled path.
func TestNewDisabledIsNoop(t *testing.T) {
	p := New(Config{Enabled: false})
	require.NotNil(t, p)
	require.False(t, p.enabled)
	p.Play(EventStartup) // should not panic, not block
}
