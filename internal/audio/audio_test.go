package audio

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var allEvents = []string{
	EventStartup, EventCreate, EventDone, EventDelete,
	EventNav, EventOpen, EventClose, EventCommand, EventFilter,
	EventBootOK, EventBootWarn, EventBootFail,
}

// TestBakeNonEmpty verifies that every catalog renders non-empty PCM for every
// event. Doesn't open an audio device (CI-safe).
func TestBakeNonEmpty(t *testing.T) {
	for name := range catalogs {
		t.Run(name, func(t *testing.T) {
			p := &Player{pcm: make(map[string][]byte)}
			p.bake(name)
			for _, ev := range allEvents {
				b, ok := p.pcm[ev]
				require.True(t, ok, "no pcm for %s in %s", ev, name)
				require.Greater(t, len(b), 0, "pcm empty for %s in %s", ev, name)
			}
		})
	}
}

// TestPlayDegradedNoop ensures Play does nothing when the player isn't enabled.
func TestPlayDegradedNoop(t *testing.T) {
	p := &Player{pcm: make(map[string][]byte)}
	p.bake(SoundsetFactorio)
	p.Play(EventStartup)
}

// TestNewDisabledIsNoop covers the explicit-disabled path.
func TestNewDisabledIsNoop(t *testing.T) {
	p := New(Config{Enabled: false})
	require.NotNil(t, p)
	require.False(t, p.enabled)
	p.Play(EventStartup)
}

// TestSetSoundsetRebakes verifies that swapping the catalog replaces PCM.
func TestSetSoundsetRebakes(t *testing.T) {
	p := &Player{pcm: make(map[string][]byte), enabled: true}
	p.bake(SoundsetFactorio)
	factorioDelete := p.pcm[EventDelete]
	p.SetSoundset(SoundsetEvangelion)
	require.Equal(t, SoundsetEvangelion, p.soundset)
	require.NotEqual(t, factorioDelete, p.pcm[EventDelete])
}

// TestSetSoundsetIgnoresOff confirms "off" / unknown names don't wipe state.
func TestSetSoundsetIgnoresOff(t *testing.T) {
	p := &Player{pcm: make(map[string][]byte), enabled: true}
	p.bake(SoundsetFactorio)
	p.SetSoundset(SoundsetOff)
	require.Equal(t, SoundsetFactorio, p.soundset)
	p.SetSoundset("does-not-exist")
	require.Equal(t, SoundsetFactorio, p.soundset)
}
