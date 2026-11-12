package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusEnvValidation(t *testing.T) {
	tool := Tool{ID: "x", Models: []Model{{ID: "m"}},
		Detect: Detect{Kind: "heuristic", PromptRegex: "p", IdleMs: 100, Status: StatusOSC7501}}
	tool.StatusEnv = []string{"AGY_CLI_OSC7501=on", "A_B=", "_x1=y=z"}
	require.NoError(t, validate([]Tool{tool}))
	for _, bad := range []string{"NOEQUALS", "=v", "1A=v", "A B=v", "A-B=v"} {
		tool.StatusEnv = []string{bad}
		require.ErrorContains(t, validate([]Tool{tool}), "NAME=value", bad)
	}
	tool.StatusEnv = []string{"A=b"}
	tool.Detect.Status = ""
	require.ErrorContains(t, validate([]Tool{tool}), "needs detect.status")
}

func TestBuiltinStatusEnv(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	agy, _ := reg.Find("antigravity")
	require.Equal(t, []string{"AGY_CLI_OSC7501=on"}, agy.StatusEnv)
}
