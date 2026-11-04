package server

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestNewSession_SanitizesTitleAndFleet is the S-03 regression for spawn:
// a client-supplied title or fleet reached every TUI verbatim, escapes and
// newlines included.
func TestNewSession_SanitizesTitleAndFleet(t *testing.T) {
	srv, sock, dir := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "clean", CWD: dir,
		Title: "fix \x1b]0;pwned\x07the\x1b[31m bug\nsecond line of the prompt",
		Fleet: "back\u009b31mend",
	}))
	added := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventSessionAdded })
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(added.Data, &sum))
	require.Equal(t, "fix the bug", sum.Title)
	require.Equal(t, "backend", sum.Fleet)
}

func TestNewSession_CapsTitle(t *testing.T) {
	srv, sock, dir := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "long-title", CWD: dir,
		Title: strings.Repeat("界", 2*protocol.MaxTitleRunes),
	}))
	added := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventSessionAdded })
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(added.Data, &sum))
	require.Equal(t, protocol.MaxTitleRunes, utf8.RuneCountInString(sum.Title))
}

func TestRenameAndFleet_Sanitize(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(doneSession("s1")))
	srv, sock, _ := newTestServer(t, store)
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)
	updated := func() map[string]any {
		env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventSessionUpdated })
		var upd protocol.SessionUpdated
		require.NoError(t, json.Unmarshal(env.Data, &upd))
		return upd.Patch
	}

	require.NoError(t, w.WriteIntent(protocol.IntentRename, "r", protocol.Rename{SessionID: "s1", Title: "new\x1b[1m title\r\nx"}))
	require.Equal(t, "new title", updated()["title"])

	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionFleet, "f", protocol.SetSessionFleet{SessionID: "s1", Fleet: "\tops\x1b]8;;http://x\x1b\\"}))
	require.Equal(t, "ops", updated()["fleet"])
	sess, _ := store.Get("s1")
	require.Equal(t, "ops", sess.Summary().Fleet)
}

// TestRenameAndFleet_RejectTextThatSanitizesAway: a non-empty value with no
// displayable text is rejected with a clear message instead of silently
// clearing the field.
func TestRenameAndFleet_RejectTextThatSanitizesAway(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(doneSession("s1")))
	srv, sock, _ := newTestServer(t, store)
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)
	isErr := func(e protocol.Envelope) bool { return e.Type == protocol.EventError }

	require.NoError(t, w.WriteIntent(protocol.IntentRename, "r", protocol.Rename{SessionID: "s1", Title: "\x1b[2J\x07"}))
	ev := readError(t, waitEvent(t, conn, r, isErr))
	require.Equal(t, protocol.ErrCodeBadIntent, ev.Code)
	require.Contains(t, ev.Message, "title")

	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionFleet, "f", protocol.SetSessionFleet{SessionID: "s1", Fleet: "\x1b]0;x\x07"}))
	ev = readError(t, waitEvent(t, conn, r, isErr))
	require.Equal(t, protocol.ErrCodeBadIntent, ev.Code)
	require.Contains(t, ev.Message, "fleet")

	sess, _ := store.Get("s1")
	require.Equal(t, "s1", sess.Summary().Slug)
	require.Empty(t, sess.Summary().Fleet)
}

func TestNormalizeNewSession(t *testing.T) {
	p, err := normalizeNewSession(protocol.NewSession{Title: " a\x1b[0m ", Fleet: ""})
	require.NoError(t, err)
	require.Equal(t, "a", p.Title)
	require.Empty(t, p.Fleet, "an empty fleet stays empty")
	_, err = normalizeNewSession(protocol.NewSession{Fleet: "\x1b[0m"})
	require.ErrorContains(t, err, "fleet")
}
