# audio

**Path:** `internal/features/audio`
**Depends on:** (none) | external: `github.com/ebitengine/oto/v3`, standard library
**Depended on by:** `internal/surface/tui`
**Entry points:** `New`, `(*Player).Play`, event/soundset constants

## Purpose

Synthesizes UI event sounds (PCM via oto) from named catalogs (Factorio, Evangelion, bell, chiptune).

## Public surface

### Constants

Events: `EventStartup`, `EventCreate`, `EventDone`, `EventDelete`, `EventNav`, `EventOpen`, `EventClose`, `EventCommand`, `EventFilter`, `EventBootOK`, `EventBootWarn`, `EventBootFail`.

Soundsets: `SoundsetFactorio`, `SoundsetEvangelion`, `SoundsetBell`, `SoundsetChiptune`, `SoundsetOff`.

### Types

- `Config` — `Enabled`, `Volume` (0–1), `Soundset`.
- `Player` — `New(cfg Config) *Player`, `Play(event string)`, `SetEnabled`, `SetVolume`, `SetSoundset`.

## Internal structure

- `audio.go` — `Player`, playback goroutine.
- `synth.go` — tone generation.
- `factorio.go`, `evangelion.go`, `bell.go`, `chiptune.go` — event→burst catalogs.
- `audio_test.go`

## Invariants

`New` always returns non-nil `Player`; init failure → no-op `Play`. `SetSoundset` unknown name leaves prior catalog (silence via `Enabled`).

## Side effects

Audio device open (oto). Background goroutine per `Play` until sample completes.

## Error handling

Init failure degrades to silent no-op; no error returned from `New`.

## Tests

`audio_test.go` — catalog presence, degraded player.

## Gotchas

TUI maps settings `sound_enabled`, `soundset`, `master_volume` into `Config`. Headless CI typically gets no-op player.

## See also

- `modules/settings.md` — audio settings IDs.
- `modules/tui.md` — calls `Play` on UX events.
