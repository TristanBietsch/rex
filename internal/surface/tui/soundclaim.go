package tui

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// soundClaimer is the subset of *audio.Claims the TUI uses: Claim reports
// whether this board is the one to play the sound named by key.
type soundClaimer interface {
	Claim(key string) bool
}

// audibler is implemented by *audio.Player: whether a Play would make a
// sound. Fakes without it count as audible.
type audibler interface {
	Audible(event string) bool
}

// audioDedupe reads the audio_dedupe setting live. Defaults on.
func audioDedupe(m Model) bool {
	if m.Store == nil {
		return true
	}
	b, ok := m.Store.Get("audio_dedupe").(bool)
	if !ok {
		return true
	}
	return b
}

// playShared plays a session sound that every open board hears about: the
// daemon sends each board the same event, and each would play it, a few
// milliseconds apart (heard as a double or an echo). With audio_dedupe on,
// the board first claims key+ev in the shared claim folder, and only the
// board that wins plays. A board that would play nothing (muted, volume 0,
// no device) does not take part, so it never silences one that would.
//
// key must be the same on every board for one daemon event: the session id
// for an add or remove, patchSoundKey for an update. Only boards that would
// play the sound race for it, so a workspace board never silences a sound
// another board filtered out, and the other way round.
func (m Model) playShared(ev, key string) Model {
	if m.Audio == nil {
		return m
	}
	if a, ok := m.Audio.(audibler); ok && !a.Audible(ev) {
		m.Audio.Play(ev) // a no-op; keeps the player the one place that decides
		return m
	}
	if m.SoundClaims != nil && audioDedupe(m) && !m.SoundClaims.Claim(key+"|"+ev) {
		session, _, _ := strings.Cut(key, "|")
		slog.Debug("tui: sound played by another board", "event", ev, "session", session)
		return m
	}
	m.Audio.Play(ev)
	return m
}

// patchSoundKey names a SessionUpdated event the same way on every board:
// the session id and the patch, re-encoded (encoding/json sorts map keys).
// A state patch carries the daemon's last_event_at, so two transitions never
// share a key; a patch without a timestamp (wait_reason) can repeat, and
// a repeat within the claim TTL plays once.
func patchSoundKey(upd protocol.SessionUpdated) string {
	b, err := json.Marshal(upd.Patch)
	if err != nil {
		return upd.SessionID
	}
	return upd.SessionID + "|" + string(b)
}

// newSoundClaims returns the shared claim folder's Claims, or nil (no
// dedupe) without a home directory.
func newSoundClaims() soundClaimer {
	dir, err := paths.SoundClaimsDir()
	if err != nil {
		return nil
	}
	return audio.NewClaims(dir)
}
