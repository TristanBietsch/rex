package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func countEvent(events []string, ev string) int {
	n := 0
	for _, e := range events {
		if e == ev {
			n++
		}
	}
	return n
}

// The attention sound plays once per user prompt: a second READY entry in
// the same turn (same prompts count) is silent.
func TestAttentionOncePerTurn(t *testing.T) {
	m, rec, now := notifyModel(t, protocol.StateWorking)
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"prompts": 1}))
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Equal(t, 1, countEvent(rec.Events, audio.EventAttention))

	*now = now.Add(time.Minute) // well past the debounce
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	*now = now.Add(time.Minute)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Equal(t, 1, countEvent(rec.Events, audio.EventAttention), "same turn: silent")

	// The user sends a new prompt: the next READY plays.
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"prompts": 2}))
	*now = now.Add(time.Minute)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	*now = now.Add(time.Minute)
	_ = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Equal(t, 2, countEvent(rec.Events, audio.EventAttention))
}

// A new question in the same turn plays, even though the READY entry was
// silent: the wait_reason input patch follows the state patch.
func TestAttentionNewQuestionSameTurn(t *testing.T) {
	m, rec, now := notifyModel(t, protocol.StateWorking)
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"prompts": 3}))
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"wait_reason": protocol.WaitInput}))
	require.Equal(t, 1, countEvent(rec.Events, audio.EventAttention), "state + reason of one question: one sound")

	// Answered; the turn then ends (Stop, same prompts): that READY chimes.
	*now = now.Add(time.Minute)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"wait_reason": ""}))
	*now = now.Add(time.Minute)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"wait_reason": protocol.WaitReview}))
	require.Equal(t, 2, countEvent(rec.Events, audio.EventAttention), "the turn end after a question chimes")

	// A later READY in the same turn is silent; a new question chimes.
	*now = now.Add(time.Minute)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"wait_reason": ""}))
	*now = now.Add(time.Minute)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Equal(t, 2, countEvent(rec.Events, audio.EventAttention), "same turn: silent")
	_ = m.applyEvent(retireUpdate(t, "s1", map[string]any{"wait_reason": protocol.WaitInput}))
	require.Equal(t, 3, countEvent(rec.Events, audio.EventAttention), "a new question plays")
}

// A review reason never plays on its own.
func TestAttentionReviewReasonSilent(t *testing.T) {
	m, rec, _ := notifyModel(t, protocol.StateNeedsInput)
	_ = m.applyEvent(retireUpdate(t, "s1", map[string]any{"wait_reason": protocol.WaitReview}))
	require.Empty(t, rec.Events)
}

// Without a prompt count (other agents, sessions started before it
// existed) every READY entry plays, subject to the debounce.
func TestAttentionWithoutPromptsUnchanged(t *testing.T) {
	m, rec, now := notifyModel(t, protocol.StateWorking)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	*now = now.Add(time.Minute)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	*now = now.Add(time.Minute)
	_ = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Equal(t, 2, countEvent(rec.Events, audio.EventAttention))
}

// A count that goes down (the agent restarted) re-arms the sound.
func TestAttentionPromptsResetRearms(t *testing.T) {
	m, rec, now := notifyModel(t, protocol.StateWorking)
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"prompts": 2}))
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"prompts": 0}))
	m = m.applyEvent(retireUpdate(t, "s1", map[string]any{"prompts": 2}))
	*now = now.Add(time.Minute)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	*now = now.Add(time.Minute)
	_ = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Equal(t, 2, countEvent(rec.Events, audio.EventAttention))
}
