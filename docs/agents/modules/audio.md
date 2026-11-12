# audio

**Path:** `internal/features/audio`
**Depends on:** (none) | external: `github.com/ebitengine/oto/v3`, standard library
**Depended on by:** `internal/surface/tui`
**Entry points:** `New`, `(*Player).Play`, event/soundset constants

## Purpose

Synthesizes UI event sounds (PCM via oto) from named catalogs (Factorio, Evangelion, bell, chiptune).

## Public surface

### Constants

Events: `EventStartup`, `EventCreate`, `EventDone`, `EventDelete`, `EventNav`, `EventOpen`, `EventClose`, `EventCommand`, `EventFilter`, `EventBootOK`, `EventBootWarn`, `EventBootFail`, `EventAttention` (working → needs_input), `EventResume` (needs_input → working), `EventBootVoice` (embedded spoken "rex", lifeOS boot-voice recipe; scheduled after the startup tone, gated by `boot_voice`, not part of any catalog — see `voice.go`, regenerate asset with `voice/gen.sh`).

Soundsets: `SoundsetFactorio`, `SoundsetEvangelion`, `SoundsetBell`, `SoundsetChiptune`, `SoundsetOff`.

### Types

- `Config` — `Enabled`, `Volume` (0–1), `Soundset`, `BootVoice`.
- `Player` — `New(cfg Config) *Player`, `Play(event string)`, `SetEnabled`, `SetVolume`, `SetSoundset`, `SetBootVoice`. Every setter applies live.

## Internal structure

- `audio.go` — `Player`; `device` seam (`otoDevice` in production, a fake in tests via `newPlayer`); playback goroutine.
- `synth.go` — tone generation.
- `factorio.go`, `evangelion.go`, `bell.go`, `chiptune.go` — event→burst catalogs.
- `audio_test.go`

## Invariants

`New` always returns non-nil `Player`; init failure → no-op `Play`. The catalog is baked even while muted, and `SetSoundset` re-bakes muted or not. The device opens the first time sound is on (`New` with `Enabled`, or a later `SetEnabled(true)`), at most once; a failed open is not retried. `SetSoundset` unknown name leaves prior catalog (silence via `Enabled`).

## Side effects

Audio device open (oto), lazily on first enable (one oto context per process). Background goroutine per `Play` until sample completes.

## Error handling

Init failure degrades to silent no-op; no error returned from `New`.

## Tests

`audio_test.go` — catalog presence, degraded player. `live_test.go` — fake device records event/sample/gain: enable after a muted start, soundset and volume changes while muted and live, boot voice, open failure.

## Gotchas

TUI maps settings `sound_enabled`, `soundset`, `master_volume` into `Config` (`master_volume` is 0–50; `settings.VolumeGain` turns it into the 0–1 `Volume`). `notify_on_state_change` is read by the TUI (`internal/surface/tui/notify.go`), not the Player: it gates `EventAttention`/`EventResume` and debounces them per session (1.5 s). Every catalog must define every event; `TestBakeNonEmpty` enforces it. Headless CI typically gets no-op player.

## See also

- `modules/settings.md` — audio settings IDs.
- `modules/tui.md` — calls `Play` on UX events.
