package adapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

func TestFor_EchoHeuristic(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, ok := reg.Find("echo")
	require.True(t, ok)

	ad, err := For(tool)
	require.NoError(t, err)
	require.NotNil(t, ad)
}

func TestFor_ClaudeStructured(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, ok := reg.Find("claude")
	require.True(t, ok)

	ad, err := For(tool)
	require.NoError(t, err)
	require.IsType(t, &ClaudeStructured{}, ad)
}

func TestFor_UnknownDetectKind(t *testing.T) {
	_, err := For(registry.Tool{
		ID:     "x",
		Detect: registry.Detect{Kind: "magic"},
		Models: []registry.Model{{ID: "m"}},
	})
	require.ErrorIs(t, err, ErrUnknownDetect)
}

func TestFor_UnsupportedStructuredFormat(t *testing.T) {
	_, err := For(registry.Tool{
		ID:     "x",
		Detect: registry.Detect{Kind: "structured", Format: "unknown"},
		Models: []registry.Model{{ID: "m"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported structured format")
}

func TestFor_HeuristicCompileError(t *testing.T) {
	_, err := For(registry.Tool{
		ID: "x",
		Detect: registry.Detect{
			Kind:        "heuristic",
			PromptRegex: "[",
			IdleMs:      100,
		},
		Models: []registry.Model{{ID: "m"}},
	})
	require.Error(t, err)
}

func TestFor_HeuristicIdleDuration(t *testing.T) {
	h, err := NewHeuristic("> ", "", 500*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, 500*time.Millisecond, h.idle)
}
