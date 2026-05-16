// Package audio synthesizes event sounds and plays them via oto.
//
// Catalogs (soundsets) live in sibling files (factorio.go, evangelion.go) and
// expose a map[event][]burst. The Player bakes the active catalog to PCM at
// startup and re-bakes on SetSoundset. Playback is fire-and-forget via a
// background goroutine that keeps the oto.Player alive until the sample
// completes.
package audio

import (
	"bytes"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// Event names.
const (
	EventStartup  = "startup"
	EventCreate   = "create"
	EventDone     = "done"
	EventDelete   = "delete"
	EventNav      = "nav"
	EventOpen     = "open"
	EventClose    = "close"
	EventCommand  = "command"
	EventFilter   = "filter"
	EventBootOK   = "boot_ok"
	EventBootWarn = "boot_warn"
	EventBootFail = "boot_fail"
)

// Soundset names.
const (
	SoundsetFactorio   = "factorio"
	SoundsetEvangelion = "evangelion"
	SoundsetOff        = "off"
)

// catalogs registers every named soundset. Adding a new set is one entry here
// plus a sibling file returning map[event][]burst.
var catalogs = map[string]func() map[string][]burst{
	SoundsetFactorio:   factorioCatalog,
	SoundsetEvangelion: evangelionCatalog,
}

// Config tunes playback.
type Config struct {
	Enabled  bool
	Volume   float64 // 0.0 – 1.0
	Soundset string  // catalog name; empty defaults to factorio
}

// Player synthesizes and plays event sounds. Safe for concurrent calls.
//
// If audio device init fails (no device, headless CI), the Player becomes a
// no-op; Play() silently returns.
type Player struct {
	cfg      Config
	ctx      *oto.Context
	enabled  bool
	soundset string
	pcm      map[string][]byte
	mu       sync.Mutex
}

// New initializes a Player. Returns a non-nil *Player even on init failure; it
// just no-ops in degraded mode.
func New(cfg Config) *Player {
	if cfg.Soundset == "" {
		cfg.Soundset = SoundsetFactorio
	}
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
	p.bake(cfg.Soundset)
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

// SetSoundset swaps the active catalog and re-bakes PCM. Unknown or "off"
// names leave the existing catalog in place (the enabled flag governs silence).
func (p *Player) SetSoundset(name string) {
	if name == "" || name == SoundsetOff {
		return
	}
	if _, ok := catalogs[name]; !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg.Soundset = name
	if p.enabled {
		p.bake(name)
	}
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

// bake renders every event in the named catalog into the pcm map. Caller holds
// p.mu (except during New where the player isn't yet shared).
func (p *Player) bake(name string) {
	build, ok := catalogs[name]
	if !ok {
		build = catalogs[SoundsetFactorio]
	}
	p.soundset = name
	p.pcm = make(map[string][]byte)
	for ev, bursts := range build() {
		p.pcm[ev] = bakeSequence(bursts...)
	}
}
