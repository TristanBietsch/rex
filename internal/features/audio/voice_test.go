package audio

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBootVoiceAsset checks the embedded VOX asset is whole s16le frames and a
// sane length.
func TestBootVoiceAsset(t *testing.T) {
	require.NotEmpty(t, bootVoicePCM)
	require.Zero(t, len(bootVoicePCM)%bytesPerFrame, "not frame aligned")
	require.Less(t, len(bootVoicePCM), 150*1024)
	d := pcmDuration(bootVoicePCM)
	require.Greater(t, d.Milliseconds(), int64(300))
	require.Less(t, d.Seconds(), 3.0)
}

// TestBootVoiceGating confirms the voice no-ops unless enabled + boot_voice,
// and never panics without a device (ctx nil).
func TestBootVoiceGating(t *testing.T) {
	p := &Player{pcm: make(map[string][]byte)}
	p.bake(SoundsetFactorio)
	p.Play(EventBootVoice) // degraded: not enabled

	p = &Player{pcm: make(map[string][]byte), enabled: true, cfg: Config{Enabled: true}}
	p.bake(SoundsetFactorio)
	p.Play(EventBootVoice) // boot voice off: skipped, no timer
	p.SetBootVoice(true)
	require.True(t, p.cfg.BootVoice)
	p.SetBootVoice(false)
	require.False(t, p.cfg.BootVoice)
}
