package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

func builtinTool(t *testing.T, id string) registry.Tool {
	t.Helper()
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, ok := reg.Find(id)
	require.True(t, ok, id)
	return tool
}

func TestInitialPromptDelivery_ClaudeArgvWithSeparator(t *testing.T) {
	base := []string{"claude", "--model", "opus"}
	d := initialPromptDelivery(builtinTool(t, "claude"), base, "  this is a test  ")
	require.Equal(t, []string{"claude", "--model", "opus", "--", "this is a test"}, d.argv)
	require.Equal(t, []string{"claude", "--model", "opus"}, base, "base argv must not be mutated")
}

func TestInitialPromptDelivery_CodexDashPromptStaysPositional(t *testing.T) {
	d := initialPromptDelivery(builtinTool(t, "codex"), []string{"codex"}, "-v explain")
	require.Equal(t, []string{"codex", "--", "-v explain"}, d.argv)
}

func TestInitialPromptDelivery_GeminiInteractiveFlag(t *testing.T) {
	d := initialPromptDelivery(builtinTool(t, "gemini"), []string{"gemini", "--model", "gemini-2.5-pro"}, "hi")
	require.Equal(t, []string{"gemini", "--model", "gemini-2.5-pro", "--prompt-interactive=hi"}, d.argv)

	// agy: -p is one-shot; --prompt-interactive keeps the session going.
	d = initialPromptDelivery(builtinTool(t, "antigravity"), []string{"agy", "--model=gemini-3.8-flash-high"}, "-starts with a dash")
	require.Equal(t, []string{"agy", "--model=gemini-3.8-flash-high", "--prompt-interactive=-starts with a dash"}, d.argv)
}

// `ollama run MODEL PROMPT` answers once and exits, so the prompt must be
// pasted into the REPL instead.
func TestInitialPromptDelivery_OllamaPastesPlain(t *testing.T) {
	d := initialPromptDelivery(builtinTool(t, "ollama"), []string{"ollama", "run", "llama3.1"}, "ping")
	require.Nil(t, d.argv)
	require.True(t, d.plainPaste)
	require.Equal(t, 800*time.Millisecond, d.readySettle)
}

// The new agents take the prompt through prompt_args, one argv element each,
// whatever the text holds.
func TestInitialPromptDelivery_NewAgents(t *testing.T) {
	d := initialPromptDelivery(builtinTool(t, "qwen"), []string{"qwen", "-m", "qfmodel"}, "-fix  the bug")
	require.Equal(t, []string{"qwen", "-m", "qfmodel", "-i", "-fix  the bug"}, d.argv)

	d = initialPromptDelivery(builtinTool(t, "grok"), []string{"grok", "-m", "grok-4.7"}, "do it")
	require.Equal(t, []string{"grok", "-m", "grok-4.7", "do it"}, d.argv)

	d = initialPromptDelivery(builtinTool(t, "cursor"), []string{"cursor-agent", "--model", "auto"}, "a b; $(c)")
	require.Equal(t, []string{"cursor-agent", "--model", "auto", "a b; $(c)"}, d.argv)
}

func TestInitialPromptDelivery_NoPromptMethodPastes(t *testing.T) {
	d := initialPromptDelivery(builtinTool(t, "echo"), []string{"bash", "-c", "read"}, "ping")
	require.Nil(t, d.argv)
	require.False(t, d.plainPaste)
}

func TestInitialPromptDelivery_BlankPrompt(t *testing.T) {
	d := initialPromptDelivery(builtinTool(t, "claude"), []string{"claude"}, "   ")
	require.Nil(t, d.argv)
}
