package server

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/features/loop"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Small pure branches that kept internal/daemon/server on its coverage floor.

// replyErr keeps the wrapped error's message and cause, and tags its code.
func TestIntentError_KeepsMessageCodeAndCause(t *testing.T) {
	cause := errors.New("disk full")
	err := replyErr(protocol.ErrCodeSpawn, cause)
	require.Equal(t, "disk full", err.Error())
	require.ErrorIs(t, err, cause)
	require.Equal(t, protocol.ErrCodeSpawn, errorCode(err))
	require.Equal(t, protocol.ErrCodeSpawn, errorCode(fmt.Errorf("handler: %w", err)), "a wrapped code still reports")
}

// An error with no code is a handler bug, reported as internal_error.
func TestErrorCode_UntaggedErrorIsInternal(t *testing.T) {
	require.Equal(t, protocol.ErrCodeInternal, errorCode(errors.New("no code")))
}

// badIntent keeps the cause reachable and is still recognized when wrapped.
func TestBadIntent_UnwrapsAndSurvivesWrapping(t *testing.T) {
	cause := errors.New("bad field")
	err := badIntent(cause)
	require.ErrorIs(t, err, cause)
	require.Equal(t, "bad field", err.Error())
	require.True(t, isBadIntent(fmt.Errorf("outer: %w", err)))
	require.False(t, isBadIntent(cause))
}

// A NewSession needs a tool and a model; blank counts as missing.
func TestValidateNewSession_RequiresToolAndModel(t *testing.T) {
	require.ErrorContains(t, validateNewSession(protocol.NewSession{ModelID: "m"}), "tool_id required")
	require.ErrorContains(t, validateNewSession(protocol.NewSession{ToolID: "  ", ModelID: "m"}), "tool_id required")
	require.ErrorContains(t, validateNewSession(protocol.NewSession{ToolID: "claude"}), "model_id required")
	require.ErrorContains(t, validateNewSession(protocol.NewSession{ToolID: "claude", ModelID: "\t"}), "model_id required")
}

// loopReplyErr gives every loop error kind its wire code and keeps the message.
func TestLoopReplyErr_Codes(t *testing.T) {
	plain := errors.New("boom")
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"not a loop error", plain, protocol.ErrCodeBadIntent},
		{"bad state", &loop.Error{Kind: loop.KindBadState, Msg: "not running"}, protocol.ErrCodeBadState},
		{"spawn, client's fault", &loop.Error{Kind: loop.KindSpawn, Msg: "spawn failed", Err: badIntent(plain)}, protocol.ErrCodeBadIntent},
		{"spawn, session cap", &loop.Error{Kind: loop.KindSpawn, Msg: "too many concurrent sessions (cap=8)", Err: plain}, protocol.ErrCodeTooManySessions},
		{"spawn, anything else", &loop.Error{Kind: loop.KindSpawn, Msg: "exec failed", Err: plain}, protocol.ErrCodeSpawn},
		{"bad input", &loop.Error{Kind: loop.KindBadInput, Msg: "loop file is wrong"}, protocol.ErrCodeBadIntent},
		{"not found", &loop.Error{Kind: loop.KindNotFound, Msg: "no such loop"}, protocol.ErrCodeBadIntent},
		{"unknown kind", &loop.Error{Kind: loop.Kind(99), Msg: "?"}, protocol.ErrCodeBadIntent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := loopReplyErr(tc.err)
			require.Equal(t, tc.want, errorCode(got))
			require.Equal(t, tc.err.Error(), got.Error(), "the message is unchanged")
		})
	}
}

// A trust prompt whose regex does not compile, or an untrusted folder, gets
// no auto answer: the user answers the dialog.
func TestTrustAnswerFor_DeclinesWithoutAUsablePrompt(t *testing.T) {
	s := &Server{}
	tool := func(shown, selected string) registry.Tool {
		return registry.Tool{ID: "t", TrustPrompt: &registry.TrustPrompt{Shown: shown, Selected: selected, Move: "j", Confirm: "\r"}}
	}
	require.Nil(t, s.trustAnswerFor(tool("(", "ok"), "/work", "auto_trust_dirs"), "shown does not compile")
	require.Nil(t, s.trustAnswerFor(tool("ok", "("), "/work", "auto_trust_dirs"), "selected does not compile")
	require.Nil(t, s.trustAnswerFor(tool("ok", "ok"), "/work", ""), "the folder is not trusted")
	ans := s.trustAnswerFor(tool("ok", "ok"), "/work", "auto_trust_dirs")
	require.NotNil(t, ans)
	require.Equal(t, []byte("j"), ans.Move)
	require.Equal(t, []byte("\r"), ans.Confirm)
}

// sameInstalled compares tool ids and installed bits, in order.
func TestSameInstalled(t *testing.T) {
	a := []protocol.ToolInfo{{ID: "x", Installed: true}, {ID: "y"}}
	require.True(t, sameInstalled(a, slices.Clone(a)))
	require.False(t, sameInstalled(a, a[:1]), "different length")
	renamed := slices.Clone(a)
	renamed[1].ID = "z"
	require.False(t, sameInstalled(a, renamed), "different tool")
	flipped := slices.Clone(a)
	flipped[0].Installed = false
	require.False(t, sameInstalled(a, flipped), "different installed bit")
}

// With no registry there is no tool list and nothing to rescan.
func TestToolInfosAndRescanInstalled_NoRegistry(t *testing.T) {
	s := &Server{}
	require.Nil(t, s.ToolInfos(), "nothing before a registry is set")
	require.False(t, s.RescanInstalled(), "no registry, nothing to rescan")
}

// A rescan sends nothing while the installed set is unchanged, and the new
// list to subscribers once it changes. PATH is empty so no host binary counts.
func TestRescanInstalled_UnchangedThenChanged(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	reg, err := registry.Load("")
	require.NoError(t, err)
	s := &Server{}
	s.SetRegistry(reg)
	var told []protocol.ToolInfo
	cancel := s.SubscribeTools(func(m protocol.ToolsChanged) { told = m.Tools })
	defer cancel()

	require.False(t, s.RescanInstalled(), "same PATH, same installed set: nothing to send")
	require.Nil(t, told)

	// A binary appears or goes away: the stored list is out of date.
	stale := slices.Clone(s.ToolInfos())
	require.NotEmpty(t, stale)
	stale[0].Installed = !stale[0].Installed
	s.tools.Store(&stale)

	require.True(t, s.RescanInstalled())
	require.Equal(t, s.ToolInfos(), told, "subscribers get the new list")
	require.False(t, s.RescanInstalled(), "and a second rescan finds nothing new")
}
