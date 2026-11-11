package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInitialPromptDelivery_ClaudeArgvWithSeparator(t *testing.T) {
	base := []string{"claude", "--model", "opus"}
	d := initialPromptDelivery("claude", base, "  this is a test  ")
	require.Equal(t, []string{"claude", "--model", "opus", "--", "this is a test"}, d.argv)
	require.Equal(t, []string{"claude", "--model", "opus"}, base, "base argv must not be mutated")
}

func TestInitialPromptDelivery_CodexDashPromptStaysPositional(t *testing.T) {
	d := initialPromptDelivery("codex", []string{"codex"}, "-v explain")
	require.Equal(t, []string{"codex", "--", "-v explain"}, d.argv)
}

func TestInitialPromptDelivery_GeminiInteractiveFlag(t *testing.T) {
	d := initialPromptDelivery("gemini", []string{"gemini", "--model", "gemini-2.5-pro"}, "hi")
	require.Equal(t, []string{"gemini", "--model", "gemini-2.5-pro", "--prompt-interactive=hi"}, d.argv)

	// agy: -p is one-shot; --prompt-interactive keeps the session going.
	d = initialPromptDelivery("antigravity", []string{"agy", "--model=gemini-3.8-flash-high"}, "-starts with a dash")
	require.Equal(t, []string{"agy", "--model=gemini-3.8-flash-high", "--prompt-interactive=-starts with a dash"}, d.argv)
}

// `ollama run MODEL PROMPT` answers once and exits, so the prompt must be
// pasted into the REPL instead.
func TestInitialPromptDelivery_OllamaPastesPlain(t *testing.T) {
	d := initialPromptDelivery("ollama", []string{"ollama", "run", "llama3.1"}, "ping")
	require.Nil(t, d.argv)
	require.True(t, d.plainPaste)
	require.Equal(t, 800*time.Millisecond, d.readySettle)
}

func TestInitialPromptDelivery_UnknownToolPastes(t *testing.T) {
	d := initialPromptDelivery("echo", []string{"bash", "-c", "read"}, "ping")
	require.Nil(t, d.argv)
	require.False(t, d.plainPaste)
}

func TestInitialPromptDelivery_BlankPrompt(t *testing.T) {
	d := initialPromptDelivery("claude", []string{"claude"}, "   ")
	require.Nil(t, d.argv)
}
