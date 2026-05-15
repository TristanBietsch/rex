// Package audio synthesizes Factorio-inspired event sounds and plays them via oto.
//
// Each event is rendered to PCM at startup with a sharp-attack/exponential-decay
// envelope so it reads as a mechanical click/chirp/thunk. Playback is fire-and-forget
// via a background goroutine that keeps the oto.Player alive until the sample completes.
package audio

import (
	"bytes"
	"math"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

const (
	sampleRate = 44100
	channels   = 1
)

// Event names.
const (
	EventStartup = "startup"
	EventCreate  = "create"
	EventDone    = "done"
	EventDelete  = "delete"
	EventNav     = "nav"
	EventOpen    = "open"
	EventClose   = "close"
)

// Config tunes playback.
type Config struct {
	Enabled bool
	Volume  float64 // 0.0 – 1.0
}

// tone is one sine burst with an exponential decay envelope.
type tone struct {
	freq    float64 // Hz
	durMs   int
	decayMs float64 // τ
}

// burst groups one or more concurrent tones (overlay).
type burst struct {
	tones []tone
}

// Player synthesizes and plays event sounds. Safe for concurrent calls.
//
// If audio device init fails (no device, headless CI), the Player becomes a no-op;
// Play() silently returns.
type Player struct {
	cfg     Config
	ctx     *oto.Context
	enabled bool
	pcm     map[string][]byte
	mu      sync.Mutex
}

// New initializes a Player. Returns a non-nil *Player even on init failure; it just
// no-ops in degraded mode.
func New(cfg Config) *Player {
	p := &Player{cfg: cfg, pcm: make(map[string][]byte)}
	if !cfg.Enabled {
		return p
	}
	op := &oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	}
	ctx, ready, err := oto.NewContext(op)
	if err != nil {
		return p
	}
	<-ready
	p.ctx = ctx
	p.enabled = true
	p.bakeAll()
	return p
}

// SetVolume adjusts playback volume (0.0–1.0).
func (p *Player) SetVolume(v float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	p.cfg.Volume = v
}

// SetEnabled flips playback on/off. Off plays = no-op.
func (p *Player) SetEnabled(b bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg.Enabled = b
}

// Play kicks off an asynchronous render of the named event.
func (p *Player) Play(event string) {
	p.mu.Lock()
	if !p.enabled || !p.cfg.Enabled {
		p.mu.Unlock()
		return
	}
	pcm := p.pcm[event]
	vol := p.cfg.Volume
	ctx := p.ctx
	p.mu.Unlock()
	if pcm == nil || ctx == nil {
		return
	}
	go func() {
		pl := ctx.NewPlayer(bytes.NewReader(pcm))
		pl.SetVolume(vol)
		pl.Play()
		// Keep the player alive until playback finishes; oto v3.4+ auto-cleans on GC.
		for pl.IsPlaying() {
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

// --- catalog ---

func (p *Player) bakeAll() {
	p.pcm[EventStartup] = bakeSequence(
		burst{tones: []tone{{220, 60, 30}}},
		burst{tones: []tone{{440, 60, 30}}},
		burst{tones: []tone{{880, 100, 60}, {1760, 40, 20}}},
	)
	p.pcm[EventCreate] = bakeSequence(
		burst{tones: []tone{{220, 30, 20}}},
		burst{tones: []tone{{440, 40, 25}}},
		burst{tones: []tone{{660, 50, 30}}},
	)
	p.pcm[EventDone] = bakeSequence(
		burst{tones: []tone{{880, 80, 70}, {1760, 40, 20}}},
	)
	p.pcm[EventDelete] = bakeSequence(
		burst{tones: []tone{{330, 40, 25}}},
		burst{tones: []tone{{165, 40, 30}}},
	)
	p.pcm[EventNav] = bakeSequence(
		burst{tones: []tone{{1200, 10, 6}}},
	)
	p.pcm[EventOpen] = bakeSequence(
		burst{tones: []tone{{330, 30, 18}}},
		burst{tones: []tone{{660, 30, 18}}},
	)
	p.pcm[EventClose] = bakeSequence(
		burst{tones: []tone{{660, 30, 18}}},
		burst{tones: []tone{{330, 30, 18}}},
	)
}

// bakeSequence concatenates burst PCMs in time.
func bakeSequence(bursts ...burst) []byte {
	var out []byte
	for _, b := range bursts {
		out = append(out, renderBurst(b)...)
	}
	return out
}

// renderBurst renders one burst: the longest tone defines the duration; shorter
// overlay tones are mixed in starting at sample 0.
func renderBurst(b burst) []byte {
	if len(b.tones) == 0 {
		return nil
	}
	// Use max duration as the burst length.
	maxDur := 0
	for _, t := range b.tones {
		if t.durMs > maxDur {
			maxDur = t.durMs
		}
	}
	totalSamples := sampleRate * maxDur / 1000
	mixed := make([]float64, totalSamples)
	for _, t := range b.tones {
		nSamples := sampleRate * t.durMs / 1000
		tau := t.decayMs * float64(sampleRate) / 1000
		for i := 0; i < nSamples; i++ {
			env := math.Exp(-float64(i) / tau)
			sine := math.Sin(2 * math.Pi * t.freq * float64(i) / float64(sampleRate))
			mixed[i] += sine * env
		}
	}
	pcm := make([]byte, totalSamples*2)
	for i, v := range mixed {
		// Soft clip via tanh; -0.5 headroom so multi-tone overlays don't saturate.
		s := int16(math.Tanh(v) * 32767 * 0.5)
		pcm[i*2] = byte(s)
		pcm[i*2+1] = byte(s >> 8)
	}
	return pcm
}
