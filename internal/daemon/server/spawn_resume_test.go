package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/adapter"
)

const argvTestID = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"

func TestBuildArgvResume(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	claude, model, ok := reg.FindModel("claude", "opus")
	require.True(t, ok)
	trust := trustDecision{Args: []string{"--t"}}

	base := append([]string{}, claude.Command...)
	base = append(base, "--model", "opus", "--effort=max", "--t", "--dangerously-skip-permissions")

	spec := spawnSpec{Tool: claude, Model: model, Effort: "max", Trust: trust, SkipPermissions: true}

	// New session: --session-id <id> after trust and permission args, before
	// the hook args (the prompt's `--` comes after all of it).
	spec.Resume = &resumeArgs{ID: argvTestID, New: true}
	got := buildArgv(spec)
	want := append(append([]string{}, base...), "--session-id", argvTestID)
	want = append(want, adapter.ClaudeHookArgs()...)
	require.Equal(t, want, got)

	// Resume: --resume <id>, never --session-id.
	spec.Resume = &resumeArgs{ID: argvTestID}
	got = buildArgv(spec)
	want = append(append([]string{}, base...), "--resume", argvTestID)
	want = append(want, adapter.ClaudeHookArgs()...)
	require.Equal(t, want, got)
	require.NotContains(t, got, "--session-id")
}

// A placeholder inside one element stays one element (agy-style), and the id
// never adds or splits arguments.
func TestBuildArgvResumeEmbeddedPlaceholder(t *testing.T) {
	tool := registry.Tool{
		ID: "x", Command: []string{"x"}, Detect: registry.Detect{Kind: "heuristic"},
		Resume: &registry.ResumeSpec{Args: []string{"--conversation={id}"}, NewArgs: []string{"--id={id}", "--fresh"}},
	}
	require.Equal(t, []string{"x", "--conversation=" + argvTestID},
		buildArgv(spawnSpec{Tool: tool, Resume: &resumeArgs{ID: argvTestID}}))
	require.Equal(t, []string{"x", "--id=" + argvTestID, "--fresh"},
		buildArgv(spawnSpec{Tool: tool, Resume: &resumeArgs{ID: argvTestID, New: true}}))
}

// Tools with no resume block, and sessions with no resume request, build the
// argv they always did, for every builtin.
func TestBuildArgvResumeNoOps(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	for _, tool := range reg.Tools {
		model := tool.Models[0]
		plain := buildArgv(spawnSpec{Tool: tool, Model: model})
		require.Equal(t, plain, buildArgv(spawnSpec{Tool: tool, Model: model, Resume: nil}), tool.ID)
		if tool.Resume == nil {
			for _, newSession := range []bool{true, false} {
				require.Equal(t, plain,
					buildArgv(spawnSpec{Tool: tool, Model: model, Resume: &resumeArgs{ID: argvTestID, New: newSession}}),
					"%s has no resume block", tool.ID)
			}
		}
	}

	// A resume block without new_args adds nothing for a new session.
	tool := registry.Tool{ID: "x", Command: []string{"x"}, Resume: &registry.ResumeSpec{Args: []string{"--r", "{id}"}}}
	require.Equal(t, []string{"x"}, buildArgv(spawnSpec{Tool: tool, Resume: &resumeArgs{ID: argvTestID, New: true}}))
}

// buildArgv is the last line of defense before an id reaches exec: an id that
// is not a canonical UUID adds nothing.
func TestBuildArgvResumeRejectsInvalidID(t *testing.T) {
	tool := registry.Tool{ID: "x", Command: []string{"x"},
		Resume: &registry.ResumeSpec{Args: []string{"--resume", "{id}"}, NewArgs: []string{"--session-id", "{id}"}}}
	for _, bad := range []string{"", "--dangerously-skip-permissions", argvTestID + " --x", "-" + argvTestID[1:]} {
		for _, newSession := range []bool{true, false} {
			require.Equal(t, []string{"x"}, buildArgv(spawnSpec{Tool: tool, Resume: &resumeArgs{ID: bad, New: newSession}}), "%q", bad)
		}
	}
}
