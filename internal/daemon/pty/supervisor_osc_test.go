package pty

import (
	"context"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/adapter"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func runOSC(t *testing.T, id string, ad adapter.Adapter, script string, env, drop []string, base ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh and a Unix tty")
	}
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: id, ShortID: id, ToolID: "qwen", Slug: id,
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command: []string{"sh", "-c", script},
		Env:     env, DropEnv: drop, Adapter: ad, BaseEnv: baseEnvArg(base),
		IdleTick: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))
	tail, _ := state.TranscriptTail(stateDir, id, 4096)
	return string(tail)
}

// The child asks, the supervisor's adapter answers on the PTY: the child
// reads the reply from its stdin. The query is split across two writes and
// still gets exactly one answer.
func TestSupervisor_AnswersOSC7501Query(t *testing.T) {
	h, err := adapter.NewHeuristic(`x`, "", 0)
	require.NoError(t, err)
	ad := adapter.NewOSCStatus(h)
	// bash reads the reply with a timeout; a second read must time out (one answer).
	script := `bash -c 'stty raw -echo; printf "\033]7501"; sleep 0.2; printf ";?\007"; ` +
		`read -t 2 -r -d "\\" a; read -t 0.5 -r -d "\\" b; echo "first=[$a]"; echo "second=[$b]"; echo END'`
	out := runOSC(t, "oscq1", ad, script, nil, nil)
	require.Contains(t, out, "END")
	require.Contains(t, out, "7501;?:states=idle,working,done,blocked,error", "reply written to the child's stdin: %q", out)
	require.Contains(t, out, "second=[]", "one answer per query: %q", out)
}

func TestSupervisor_NoReplyWithoutOSCAdapter(t *testing.T) {
	h, err := adapter.NewHeuristic(`x`, "", 0)
	require.NoError(t, err)
	script := `bash -c 'stty raw -echo; printf "\033]7501;?\007"; if read -t 0.5 -n 1 c; then echo GOTREPLY; else echo NOREPLY; fi'`
	out := runOSC(t, "oscq2", h, script, nil, nil)
	require.Contains(t, out, "NOREPLY")
}

func TestSupervisor_DropEnv(t *testing.T) {
	t.Setenv("REX_DROP_ME", "secret")
	t.Setenv("REX_KEEP_ME", "kept")
	out := runOSC(t, "envd1", nil, `printf 'drop=[%s] keep=[%s] add=[%s]\n' "$REX_DROP_ME" "$REX_KEEP_ME" "$REX_ADD"`,
		[]string{"REX_ADD=yes"}, []string{"REX_DROP_ME"})
	require.Contains(t, out, "drop=[] keep=[kept] add=[yes]")
}

// BaseEnv replaces the daemon's own environment; Env and DropEnv still apply
// on top of it.
func TestSupervisor_BaseEnvReplacesInherited(t *testing.T) {
	t.Setenv("REX_INHERITED", "leak")
	out := runOSC(t, "envb1", nil,
		`printf 'inh=[%s] base=[%s] drop=[%s] add=[%s]\n' "$REX_INHERITED" "$REX_BASE" "$REX_BASE_DROP" "$REX_ADD"`,
		[]string{"REX_ADD=yes"}, []string{"REX_BASE_DROP"},
		"PATH="+os.Getenv("PATH"), "REX_BASE=b", "REX_BASE_DROP=d")
	require.Contains(t, out, "inh=[] base=[b] drop=[] add=[yes]")
}

// A non-nil empty BaseEnv is still a base: the child inherits nothing.
func TestSupervisor_EmptyBaseEnvInheritsNothing(t *testing.T) {
	t.Setenv("REX_INHERITED", "leak")
	out := runOSC(t, "envb2", nil, `/bin/sh -c 'echo "inh=[$REX_INHERITED]"'`,
		nil, nil, "PATH=/usr/bin:/bin")
	require.Contains(t, out, "inh=[]")
}

// baseEnvArg maps runOSC's variadic base to SupervisorConfig.BaseEnv: absent
// is nil (inherit), present is a non-nil slice even when empty.
func baseEnvArg(base []string) []string {
	if base == nil {
		return nil
	}
	return slices.Clone(base)
}
