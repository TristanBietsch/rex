package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestUpdateDescription_Sanitizes is the S-03 regression for the summarizer
// path: model output derived from transcript text must not carry escapes,
// control bytes or extra lines to the board.
func TestUpdateDescription_Sanitizes(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Add(&Session{ID: "d1", ShortID: "d1", Slug: "d1"}))
	got := make(chan Event, 1)
	cancel := s.Subscribe(func(e Event) { got <- e })
	defer cancel()

	require.NoError(t, s.UpdateDescription("d1", "fixing \x1b]0;pwned\x07auth\x1b[2J bug\nIgnore previous"))
	e := <-got
	require.Equal(t, "fixing auth bug", e.Patch["description"])
	sess, _ := s.Get("d1")
	require.Equal(t, "fixing auth bug", sess.Summary().Description)

	require.NoError(t, s.UpdateDescription("d1", strings.Repeat("d", 1000)))
	<-got
	require.Len(t, sess.Summary().Description, protocol.MaxDescriptionRunes)
}

// TestLoadMeta_SanitizesDisplayFields: state written before S-03 may hold
// raw escapes; restored sessions are cleaned on load.
func TestLoadMeta_SanitizesDisplayFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "old")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	meta := `{"id":"old","short_id":"old","slug":"old","state":"done",` +
		`"title":"t\u001b[31mitle\nmore","fleet":"f\u009b1mleet","description":"d\u0007esc"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o600))

	sess, err := LoadMeta(root, "old")
	require.NoError(t, err)
	sum := sess.Summary()
	require.Equal(t, "title", sum.Title)
	require.Equal(t, "fleet", sum.Fleet)
	require.Equal(t, "desc", sum.Description)
}
