package luacfg

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// readerOpts evaluates script through Opts.Read (no file on disk) under the
// chunk name chunk.
func readerOpts(script, chunk string) Opts {
	return Opts{
		File:  filepath.Join("/nowhere", ".rex", "config.lua"),
		Chunk: chunk,
		Read:  func() ([]byte, error) { return []byte(script), nil },
	}
}

func TestEval_ReadReplacesTheFileRead(t *testing.T) {
	ResetMemo()
	r := Eval(context.Background(), readerOpts("\nrex.set('theme', 'noir')\n", "/w/.rex/config.lua"))
	require.NoError(t, r.Err)
	require.True(t, r.Exists)
	require.Equal(t, "noir", r.Values["theme"])
	require.Equal(t, "/w/.rex/config.lua:2", r.SetAt["theme"], "positions carry the chunk name")
}

func TestEval_ReadMissingIsEmpty_ErrorFails(t *testing.T) {
	o := readerOpts("", "x")
	o.Read = func() ([]byte, error) { return nil, fs.ErrNotExist }
	r := Eval(context.Background(), o)
	require.False(t, r.Exists)
	require.NoError(t, r.Err)

	o.Read = func() ([]byte, error) { return nil, errors.New("unsafe .rex\x1b[2J") }
	r = Eval(context.Background(), o)
	require.True(t, r.Exists)
	require.ErrorContains(t, r.Err, "x: unsafe .rex")
	require.NotContains(t, r.Err.Error(), "\x1b", "the reason is flattened")
}

func TestEval_ChunkNamesErrors(t *testing.T) {
	r := Eval(context.Background(), readerOpts("error('boom')", "/w/.rex/config.lua"))
	require.ErrorContains(t, r.Err, "/w/.rex/config.lua:1")
	r = Eval(context.Background(), readerOpts("while true do end", "/w/.rex/config.lua"))
	require.ErrorContains(t, r.Err, "/w/.rex/config.lua: timed out")
}

func TestEval_ReadSourceIsCapped(t *testing.T) {
	big := make([]byte, MaxScript+1)
	for i := range big {
		big[i] = ' '
	}
	o := readerOpts(string(big), "w")
	r := Eval(context.Background(), o)
	require.ErrorContains(t, r.Err, "larger than")
}

// One test per security-critical denied key: a workspace file (Deny =
// WorkspaceDenyIDs) cannot set any of them, even to a valid value, and the refusal neither stores
// nor fails anything closed.
func TestEval_WorkspaceDenyRefusesEveryKey(t *testing.T) {
	valid := map[string]string{
		"skip_permissions":   `true`,
		"auto_trust_dirs":    `{ "~/dev" }`,
		"default_spawn_tool": `"codex"`,
		"extra_agents":       `true`,
		"audit_trail":        `false`,
		"audit_full_prompt":  `true`,
		"workspaces":         `false`,
		"lua_config":         `false`,
	}
	for id := range valid {
		require.True(t, settings.WorkspaceDenied(id), id)
	}
	for id := range valid {
		t.Run(id, func(t *testing.T) {
			lit, ok := valid[id]
			require.True(t, ok, "add a valid value for %s", id)
			script := "rex.set('" + id + "', " + lit + ")"
			// Without the deny the value is accepted (lua_config never is),
			// so the deny is what refuses it.
			if id != "lua_config" {
				open := Eval(context.Background(), readerOpts(script, "u"))
				require.NoError(t, open.Err)
				require.Contains(t, open.Values, id, "valid without the deny")
			}
			o := readerOpts(script, "/w/.rex/config.lua")
			o.Deny = settings.WorkspaceDenyIDs
			r := Eval(context.Background(), o)
			require.NoError(t, r.Err)
			require.NotContains(t, r.Values, id)
			require.NotContains(t, r.SetAt, id)
			require.Empty(t, r.FailClosed)
			require.Len(t, r.Warnings, 1)
			require.Contains(t, r.Warnings[0], "cannot be set from Lua")
		})
	}
}

func TestEval_ChunkWithEscapeIsFlattenedInPositions(t *testing.T) {
	r := Eval(context.Background(), readerOpts("rex.set('theme', 'noir')", "/w\x1b[2J/.rex/config.lua"))
	require.NoError(t, r.Err)
	require.NotContains(t, r.SetAt["theme"], "\x1b")
	require.Contains(t, r.SetAt["theme"], "/.rex/config.lua:1")
}
