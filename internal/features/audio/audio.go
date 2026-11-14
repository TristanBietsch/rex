// Package audio synthesizes event sounds and plays them via oto.
//
// Catalogs (soundsets) live in sibling files (factorio.go, bell.go, …) and
// expose a map[event][]burst. The Player bakes the active catalog to PCM at
// startup and re-bakes on SetSoundset. The device opens the first time sound
// is on, so a Player started muted plays once it is turned on. Playback is
// fire-and-forget via a background goroutine that keeps the oto.Player alive
// until the sample completes.
package audio

import (
	"bytes"
	"log/slog"
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
	// EventAttention: a session went working → needs_input (agent needs you).
	EventAttention = "attention"
	// EventResume: a session went needs_input → working (agent picked back up).
	EventResume = "resume"
)

// Soundset names.
const (
	SoundsetFactorio   = "factorio"
	SoundsetEvangelion = "evangelion"
	SoundsetBell       = "bell"
	SoundsetChiptune   = "chiptune"
	SoundsetOff        = "off"
)

// catalogs registers every named soundset. Adding a new set is one entry here
// plus a sibling file returning map[event][]burst.
var catalogs = map[string]func() map[string][]burst{
	SoundsetFactorio:   factorioCatalog,
	SoundsetEvangelion: evangelionCatalog,
	SoundsetBell:       bellCatalog,
	SoundsetChiptune:   chiptuneCatalog,
}

// Config tunes playback.
type Config struct {
	Enabled  bool
	Volume   float64 // 0.0 – 1.0
	Soundset string  // catalog name; empty defaults to factorio
	// BootVoice enables the spoken boot announcement (EventBootVoice).
	BootVoice bool
}

// Player synthesizes and plays event sounds. Safe for concurrent calls.
//
// If audio device init fails (no device, headless CI), the Player becomes a
// no-op; Play() silently returns.
type Player struct {
	cfg Config
	// open opens the device; it runs once, when sound is first on.
	open     func() (device, error)
	dev      device // nil while no device is open: muted
	opened   bool   // open was tried
	soundset string
	pcm      map[string][]byte
	mu       sync.Mutex
}

// device plays one baked sample at a gain. The oto context implements it;
// tests use a fake.
type device interface {
	play(event string, pcm []byte, gain float64)
}

// New initializes a Player. Returns a non-nil *Player even on init failure; it
// just no-ops in degraded mode.
func New(cfg Config) *Player { return newPlayer(cfg, openOto) }

func newPlayer(cfg Config, open func() (device, error)) *Player {
	if cfg.Soundset == "" {
		cfg.Soundset = SoundsetFactorio
	}
	p := &Player{cfg: cfg, open: open, pcm: make(map[string][]byte)}
	// Bake even while muted: turning sound on later must have samples.
	p.bake(cfg.Soundset)
	if cfg.Enabled {
		p.openDevice()
	}
	return p
}

// openDevice opens the audio device the first time sound is on. A failure
// (no device, headless CI) leaves the player muted and is not retried.
// Caller holds p.mu, or the player is not yet shared.
func (p *Player) openDevice() {
	if p.dev != nil || p.opened {
		return
	}
	p.opened = true
	if p.open == nil {
		return
	}
	dev, err := p.open()
	if err != nil {
		slog.Warn("audio: device init failed; running muted", "err", err)
		return
	}
	p.dev = dev
	slog.Info("audio: player ready", "soundset", p.cfg.Soundset, "volume", p.cfg.Volume, "boot_voice", p.cfg.BootVoice)
}

// openOto opens the process's one oto context.
func openOto() (device, error) {
	op := &oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	}
	ctx, ready, err := oto.NewContext(op)
	if err != nil {
		return nil, err
	}
	<-ready
	return otoDevice{ctx}, nil
}

// otoDevice plays samples on an oto context.
type otoDevice struct{ ctx *oto.Context }

// play is fire-and-forget: a goroutine keeps the oto player alive until the
// sample ends.
func (d otoDevice) play(event string, pcm []byte, gain float64) {
	go func() {
		pl := d.ctx.NewPlayer(bytes.NewReader(pcm))
		pl.SetVolume(gain)
		pl.Play()
		// Keep the player alive until playback finishes; oto v3.4+ auto-cleans on GC.
		for pl.IsPlaying() {
			time.Sleep(10 * time.Millisecond)
		}
		slog.Debug("audio: playback finished", "event", event)
	}()
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

// SetEnabled flips playback on/off. Off plays = no-op. The first time sound
// turns on, the audio device opens, so a player started muted plays once
// sound_enabled is turned on, without a restart.
func (p *Player) SetEnabled(b bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg.Enabled = b
	if b {
		p.openDevice()
	}
}

// SetSoundset swaps the active catalog and re-bakes PCM, muted or not, so the
// set picked while muted is the one heard when sound turns on. Unknown or
// "off" names leave the existing catalog in place (the enabled flag governs
// silence).
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
	p.bake(name)
}

// Play kicks off an asynchronous render of the named event.
func (p *Player) Play(event string) {
	if event == EventBootVoice {
		p.playBootVoice()
		return
	}
	p.mu.Lock()
	pcm := p.pcm[event]
	p.mu.Unlock()
	p.playPCM(event, pcm)
}

// Audible reports whether Play(event) would make a sound now: a device is
// open, sound is on, the volume is above 0 and the soundset has the event.
// The TUI asks before it claims a shared sound, so a muted board never wins
// a claim and silences a board that would have played.
func (p *Player) Audible(event string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dev != nil && p.cfg.Enabled && p.cfg.Volume > 0 && len(p.pcm[event]) > 0
}

// playPCM plays pcm if the player is enabled; volume is read at call time.
func (p *Player) playPCM(event string, pcm []byte) {
	p.mu.Lock()
	if p.dev == nil || !p.cfg.Enabled {
		p.mu.Unlock()
		return
	}
	vol := p.cfg.Volume
	dev := p.dev
	p.mu.Unlock()
	if pcm == nil || dev == nil {
		return
	}
	dev.play(event, pcm, vol)
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
