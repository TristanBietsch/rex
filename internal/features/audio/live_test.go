package audio

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeDevice records every sample the player hands it. It never touches a
// real audio device.
type fakeDevice struct {
	mu    sync.Mutex
	plays []fakePlay
}

type fakePlay struct {
	event string
	pcm   []byte
	gain  float64
}

func (d *fakeDevice) play(event string, pcm []byte, gain float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.plays = append(d.plays, fakePlay{event: event, pcm: pcm, gain: gain})
}

func (d *fakeDevice) all() []fakePlay {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]fakePlay(nil), d.plays...)
}

func (d *fakeDevice) last(t *testing.T) fakePlay {
	t.Helper()
	all := d.all()
	require.NotEmpty(t, all, "nothing played")
	return all[len(all)-1]
}

// fakePlayer is a Player whose device is dev; opens counts device opens.
func fakePlayer(cfg Config) (*Player, *fakeDevice, *int) {
	dev := &fakeDevice{}
	opens := 0
	p := newPlayer(cfg, func() (device, error) {
		opens++
		return dev, nil
	})
	return p, dev, &opens
}

func bakedPCM(t *testing.T, soundset, event string) []byte {
	t.Helper()
	q := &Player{pcm: make(map[string][]byte)}
	q.bake(soundset)
	require.NotEmpty(t, q.pcm[event])
	return q.pcm[event]
}

// A player started muted (sound_enabled false) must play once sound is
// turned on, without a restart.
func TestSetEnabledAfterMutedStartPlays(t *testing.T) {
	p, dev, opens := fakePlayer(Config{Enabled: false, Volume: 0.8, Soundset: SoundsetFactorio})
	p.Play(EventNav)
	require.Empty(t, dev.all(), "muted: silent")
	require.Zero(t, *opens, "muted start opens no device")

	p.SetEnabled(true)
	p.Play(EventNav)
	got := dev.last(t)
	require.Equal(t, EventNav, got.event)
	require.Equal(t, bakedPCM(t, SoundsetFactorio, EventNav), got.pcm)
	require.InDelta(t, 0.8, got.gain, 1e-9)

	p.SetEnabled(false)
	p.SetEnabled(true)
	require.Equal(t, 1, *opens, "the device opens once")
}

func TestSetEnabledFalseSilences(t *testing.T) {
	p, dev, _ := fakePlayer(Config{Enabled: true, Volume: 1, Soundset: SoundsetBell})
	p.SetEnabled(false)
	p.Play(EventDone)
	require.Empty(t, dev.all())
}

// A soundset picked while muted must be the one heard once sound is on.
func TestSetSoundsetWhileMutedIsUsedOnEnable(t *testing.T) {
	p, dev, _ := fakePlayer(Config{Enabled: false, Volume: 1, Soundset: SoundsetFactorio})
	p.SetSoundset(SoundsetChiptune)
	p.SetEnabled(true)
	p.Play(EventDelete)
	require.Equal(t, bakedPCM(t, SoundsetChiptune, EventDelete), dev.last(t).pcm)
}

func TestSetSoundsetChangesSamplesLive(t *testing.T) {
	p, dev, _ := fakePlayer(Config{Enabled: true, Volume: 1, Soundset: SoundsetFactorio})
	p.Play(EventDelete)
	require.Equal(t, bakedPCM(t, SoundsetFactorio, EventDelete), dev.last(t).pcm)
	p.SetSoundset(SoundsetEvangelion)
	p.Play(EventDelete)
	require.Equal(t, bakedPCM(t, SoundsetEvangelion, EventDelete), dev.last(t).pcm)
}

// A muted start with soundset "off" bakes the default catalog, so turning
// sound on later still has samples.
func TestMutedOffStartHasSamplesOnEnable(t *testing.T) {
	p, dev, _ := fakePlayer(Config{Enabled: false, Volume: 1, Soundset: SoundsetOff})
	p.SetEnabled(true)
	p.Play(EventOpen)
	require.Equal(t, bakedPCM(t, SoundsetFactorio, EventOpen), dev.last(t).pcm)
}

func TestSetVolumeAppliesToNextPlay(t *testing.T) {
	p, dev, _ := fakePlayer(Config{Enabled: false, Volume: 0.8, Soundset: SoundsetFactorio})
	p.SetVolume(0.3) // set while muted: kept
	p.SetEnabled(true)
	p.Play(EventNav)
	require.InDelta(t, 0.3, dev.last(t).gain, 1e-9)
	p.SetVolume(0)
	p.Play(EventNav)
	require.InDelta(t, 0, dev.last(t).gain, 1e-9)
	p.SetVolume(2)
	p.Play(EventNav)
	require.InDelta(t, 1, dev.last(t).gain, 1e-9)
}

func TestBootVoiceAfterMutedStart(t *testing.T) {
	p, dev, _ := fakePlayer(Config{Enabled: false, Volume: 1, Soundset: SoundsetBell})
	p.SetEnabled(true)
	p.SetBootVoice(true)
	p.mu.Lock()
	p.pcm[EventStartup] = nil // no delay before the voice
	p.mu.Unlock()
	p.Play(EventBootVoice)
	require.Eventually(t, func() bool {
		for _, pl := range dev.all() {
			if pl.event == EventBootVoice {
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond)
}

// A device that fails to open leaves the player muted and is not retried on
// every toggle.
func TestDeviceOpenFailureStaysMuted(t *testing.T) {
	opens := 0
	p := newPlayer(Config{Enabled: false}, func() (device, error) {
		opens++
		return nil, errors.New("no device")
	})
	p.SetEnabled(true)
	p.Play(EventNav)
	p.SetEnabled(false)
	p.SetEnabled(true)
	require.Equal(t, 1, opens)
}
