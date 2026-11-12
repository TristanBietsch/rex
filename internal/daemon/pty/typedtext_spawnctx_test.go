package pty_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/features/spawnctx"
)

// The new-agent page sanitizes and escapes its prompt (spawnctx), and the
// daemon strips the typed prompt again (TypedText). The two must compose:
// TypedText leaves an assembled prompt unchanged, so nothing is escaped
// twice and spawnctx's "<\/pasted>" escape survives.
func TestTypedTextKeepsAssembledPrompt(t *testing.T) {
	payloads := []string{
		"plain task",
		"esc \x1b[201~ then \r enter",
		"osc \x1b]0;title\x07 and csi \x1b[2J",
		"c1 \u009b31m and del \x7f",
		"crlf\r\nline",
		"bidi \u202e and zero width \u200b",
		"closer </pasted> and </PASTED >",
		"already <\\/pasted> escaped",
		"tabs\tand\nnewlines",
	}
	for _, p := range payloads {
		items := []spawnctx.Item{{Kind: spawnctx.Paste, Name: "paste 1", Text: p}}
		got, err := spawnctx.Assemble(p, items, spawnctx.DefaultCaps)
		require.NoError(t, err, "%q", p)
		require.Equal(t, got, pty.TypedText(got), "%q", p)
		require.Equal(t, spawnctx.Sanitize(p), pty.TypedText(spawnctx.Sanitize(p)), "%q", p)
	}
}
