package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Full-screen agents repaint from the daemon's screen mirror on attach.
func TestRedrawKickToolsCoverFullScreenAgents(t *testing.T) {
	for _, id := range []string{"claude", "codex", "antigravity", "qwen", "cursor", "grok"} {
		require.True(t, redrawKickTools[id], id)
	}
	require.False(t, redrawKickTools["ollama"], "readline treats Ctrl+L as clear")
}
