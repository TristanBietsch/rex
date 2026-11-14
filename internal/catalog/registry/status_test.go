package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateDetectStatus(t *testing.T) {
	tool := Tool{ID: "x", Models: []Model{{ID: "m"}},
		Detect: Detect{Kind: "heuristic", PromptRegex: "p", IdleMs: 100, Status: StatusOSC7501}}
	require.NoError(t, validate([]Tool{tool}))

	tool.Detect.Status = "osc9999"
	require.ErrorContains(t, validate([]Tool{tool}), `unknown detect.status "osc9999"`)

	tool.Detect = Detect{Kind: "hooks", Format: "claude", Status: StatusOSC7501}
	require.ErrorContains(t, validate([]Tool{tool}), "needs detect.kind heuristic")
}

func TestBuiltinAntigravityStatus(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	agy, ok := reg.Find("antigravity")
	require.True(t, ok)
	require.Equal(t, StatusOSC7501, agy.Detect.Status)
	claude, _ := reg.Find("claude")
	require.Empty(t, claude.Detect.Status)
}
