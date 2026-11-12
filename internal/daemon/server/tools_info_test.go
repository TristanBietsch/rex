package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func stubOnPath(t *testing.T, dir, name string) {
	t.Helper()
	file, content := name, "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		file, content = name+".bat", "@echo off\r\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o755)) //nolint:gosec // a test stub must be executable
}

func toolByID(t *testing.T, tools []protocol.ToolInfo, id string) protocol.ToolInfo {
	t.Helper()
	for _, ti := range tools {
		if ti.ID == id {
			return ti
		}
	}
	require.FailNow(t, "tool missing from snapshot", id)
	return protocol.ToolInfo{}
}

// The Hello snapshot says which tool binaries are on the daemon's PATH and
// lists each tool's models (detected ones flagged).
func TestHello_SnapshotCarriesTools(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	stubOnPath(t, bin, "qwen")
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)

	_, _, snap := helloAs(t, dialTest(t, sock), "rex-tui")
	require.NotEmpty(t, snap.Tools)
	require.True(t, toolByID(t, snap.Tools, "qwen").Installed)
	require.False(t, toolByID(t, snap.Tools, "grok").Installed)
	require.False(t, toolByID(t, snap.Tools, "cursor").Installed)
	require.NotEmpty(t, toolByID(t, snap.Tools, "qwen").Models)

	// A reload that finds more binaries, and detected models, is seen by the
	// next Hello.
	stubOnPath(t, bin, "cursor-agent")
	reg, err := registry.Load("")
	require.NoError(t, err)
	cursor, _ := reg.Find("cursor")
	reg = reg.WithModels(map[string][]registry.Model{
		"cursor": {{ID: "auto", Name: "auto", Args: cursor.ModelArgsFor("auto"), Detected: true}},
	})
	conn := dialTest(t, sock)
	_, r, _ := helloAs(t, conn, "rex-tui")
	srv.SetRegistry(reg)
	env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventToolsChanged })
	var changed protocol.ToolsChanged
	require.NoError(t, json.Unmarshal(env.Data, &changed))
	require.True(t, toolByID(t, changed.Tools, "cursor").Installed, "a running client is told about the new list")
	_, _, snap = helloAs(t, dialTest(t, sock), "rex-tui")
	got := toolByID(t, snap.Tools, "cursor")
	require.True(t, got.Installed)
	require.Equal(t, []protocol.ModelInfo{{ID: "auto", Name: "Auto", Detected: true}}, got.Models)
}
