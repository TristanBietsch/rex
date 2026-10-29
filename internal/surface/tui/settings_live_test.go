package tui

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Changing max_concurrent_sessions used to write SetMaxConcurrent to the
// daemon socket inside Update. It is now a Cmd; nothing is sent until the
// Cmd runs.
func TestSettingsMaxConcurrent_SentFromCmd(t *testing.T) {
	d := startFakeBoardDaemon(t)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.Subscribe(""))
	conn := d.nextConn(t)

	m := Model{
		Client:    c,
		Focus:     FocusSettings,
		Store:     settings.NewStore(),
		StorePath: filepath.Join(t.TempDir(), "config.yaml"),
		Settings:  &SettingsState{CursorID: "max_concurrent_sessions"},
	}
	m, cmd := settingsKey(m, key("+"))
	require.Equal(t, 17, m.Store.Get("max_concurrent_sessions"))
	require.NotNil(t, cmd)
	require.Nil(t, cmd())

	env, err := protocol.NewReader(conn).Read()
	require.NoError(t, err)
	require.Equal(t, protocol.IntentSetMaxConcurrent, env.Type)
	var req protocol.SetMaxConcurrent
	require.NoError(t, json.Unmarshal(env.Data, &req))
	require.Equal(t, 17, req.N)

	// Settings the daemon doesn't enforce need no round trip.
	m.Settings.CursorID = "reduce_motion"
	_, cmd = settingsKey(m, key("enter"))
	require.Nil(t, cmd)
}
