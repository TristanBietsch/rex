package server

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Titles and fleets from one client are drawn on every client's board: they
// must arrive single-line, escape-free and bounded.
func TestIntake_TitleAndFleetAreSanitized(t *testing.T) {
	sock, dir, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	prompt := "fix the bug\n\x1b[2J\x1b]0;pwned\x07 now" + strings.Repeat(" more", 300)
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "intake", CWD: dir,
		Title: prompt, Fleet: "team\x1b[31m\nred",
	}))
	added := readUntil(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionAdded })
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(added.Data, &sum))
	require.True(t, strings.HasPrefix(sum.Title, "fix the bug [2J]0;pwned now more"), sum.Title)
	require.NotContains(t, sum.Title, "\x1b")
	require.Equal(t, maxTitleRunes, utf8.RuneCountInString(sum.Title))
	require.Equal(t, "team[31m red", sum.Fleet)

	// Rename's title goes through the same path.
	require.NoError(t, w.WriteIntent(protocol.IntentRename, "rn", protocol.Rename{SessionID: sum.ID, Title: "a\r\nb\x1b"}))
	upd := readUntil(t, conn, r, func(env protocol.Envelope) bool {
		var u protocol.SessionUpdated
		return env.Type == protocol.EventSessionUpdated && json.Unmarshal(env.Data, &u) == nil && u.Patch["title"] != nil
	})
	var u protocol.SessionUpdated
	require.NoError(t, json.Unmarshal(upd.Data, &u))
	require.Equal(t, "a b", u.Patch["title"])

	// An over-long fleet is rejected, not truncated (it's a label).
	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionFleet, "f", protocol.SetSessionFleet{
		SessionID: sum.ID, Fleet: strings.Repeat("f", maxFleetRunes+1),
	}))
	ee := decodeError(t, readUntil(t, conn, r, errorFor("f")))
	require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
}

// A missing or non-directory cwd fails the NewSession itself instead of
// showing up later as a failed session.
func TestIntake_RejectsBadCWD(t *testing.T) {
	sock, dir, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	for i, cwd := range []string{filepath.Join(dir, "missing"), filepath.Join(dir, "rex.sock")} {
		id := string(rune('a' + i))
		require.NoError(t, w.WriteIntent(protocol.IntentNewSession, id, protocol.NewSession{
			ToolID: "echo", ModelID: "short", Slug: "cwd-" + id, CWD: cwd,
		}))
		ee := decodeError(t, readUntil(t, conn, r, func(env protocol.Envelope) bool {
			return env.Type == protocol.EventError || env.Type == protocol.EventSessionAdded
		}))
		require.Equal(t, id, ee.ID, "cwd %s: expected an Error, not a spawn", cwd)
		require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
		require.Contains(t, ee.Message, "cwd")
	}
}

func TestTruncateRunes(t *testing.T) {
	require.Equal(t, "héé", truncateRunes("héééé", 3))
	require.Equal(t, "hé", truncateRunes("hé", 3))
	require.Equal(t, "", truncateRunes("abc", 0))
}
