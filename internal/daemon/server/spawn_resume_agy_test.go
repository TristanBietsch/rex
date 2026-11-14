package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

// Resuming an agy session adds --conversation=<id> as one argv element; a
// fresh agy session adds nothing (agy cannot preassign an id).
func TestBuildArgvResumeAntigravity(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, ok := reg.Find("antigravity")
	require.True(t, ok)
	model := tool.Models[0]

	fresh := buildArgv(spawnSpec{Tool: tool, Model: model})
	resumed := buildArgv(spawnSpec{Tool: tool, Model: model, Resume: &resumeArgs{ID: argvTestID}})
	require.Equal(t, append(append([]string{}, fresh...), "--conversation="+argvTestID), resumed)
	require.NotContains(t, buildArgv(spawnSpec{Tool: tool, Model: model, Resume: &resumeArgs{ID: argvTestID, New: true}}),
		"--conversation="+argvTestID, "no new_args for agy")
}
