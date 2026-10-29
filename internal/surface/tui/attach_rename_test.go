package tui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// "::rename" used to set the display name (like "::name") while the board's
// ":rename" changes the slug. It now renames the attached session's slug.
func TestAttachRename_ChangesSlugOfAttachedSession(t *testing.T) {
	d := startFakeBoardDaemon(t)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.Subscribe(""))
	conn := d.nextConn(t)

	fn := &fakeNamer{}
	m := attachedModel()
	m.Client, m.namer = c, fn
	m, cmd := executeAttachCommand(m, "rename new-slug")
	require.Equal(t, FocusAttach, m.Focus, "stays attached")
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Empty(t, fn.calls, "display name untouched")

	env, err := protocol.NewReader(conn).Read()
	require.NoError(t, err)
	require.Equal(t, protocol.IntentRename, env.Type)
	var req protocol.Rename
	require.NoError(t, json.Unmarshal(env.Data, &req))
	require.Equal(t, protocol.Rename{SessionID: "a", Slug: "new-slug"}, req)
}

func TestAttachRename_RejectsBadSlug(t *testing.T) {
	m := attachedModel()
	m, cmd := executeAttachCommand(m, "rename bad\x1bslug")
	require.Nil(t, cmd)
	require.Contains(t, m.Err, "rename: ")
	require.Equal(t, FocusAttach, m.Focus)
}

func TestAttachName_SetsDisplayName(t *testing.T) {
	fn := &fakeNamer{}
	m := attachedModel()
	m.namer = fn
	_, cmd := executeAttachCommand(m, "name Fix the auth bug")
	runCmd(cmd)
	require.Equal(t, []string{"a=Fix the auth bug"}, fn.calls)
}
