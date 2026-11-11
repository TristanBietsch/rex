package server

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

// Synthetic screens worded like agy's trust dialog (no real capture).
const (
	agyDialogYes = "Do you trust the contents of this project?\n\n> Yes, I trust this folder\n  No, exit\n\n↑/↓ Navigate · enter Confirm\n"
	agyDialogNo  = "Do you trust the contents of this project?\n\n  Yes, I trust this folder\n> No, exit\n\n↑/↓ Navigate · enter Confirm\n"
)

// The builtin agy trust_prompt answers the dialog only in a trusted folder,
// confirms when "Yes" is selected and moves down when "No" is.
func TestAntigravityTrustPromptFallback(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	agy, ok := reg.Find("antigravity")
	require.True(t, ok)
	require.NotNil(t, agy.TrustPrompt)

	dir := t.TempDir()
	srv := &Server{cfg: Config{AutoTrustDirs: func() []string { return []string{dir} }}}
	a := srv.trustDecisionFor(agy, filepath.Join(dir, "p"), false).Answer
	require.NotNil(t, a, "trusted folder")
	require.Nil(t, srv.trustDecisionFor(agy, t.TempDir(), false).Answer, "untrusted folder: the user answers")
	require.Nil(t, (&Server{}).trustDecisionFor(agy, dir, false).Answer, "no auto_trust_dirs")

	require.True(t, a.Shown.MatchString(agyDialogYes))
	require.True(t, a.Selected.MatchString(agyDialogYes))
	require.True(t, a.Shown.MatchString(agyDialogNo))
	require.False(t, a.Selected.MatchString(agyDialogNo), "No selected: move down first")
	require.Equal(t, "\x1b[B", string(a.Move))
	require.Equal(t, "\r", string(a.Confirm))

	// Other screens never look like the dialog.
	require.False(t, a.Shown.MatchString("> Yes, I trust this folder"), "needs the header")
	require.False(t, a.Shown.MatchString("Do you trust the contents of this project?"), "needs the option")
	require.False(t, a.Selected.MatchString("The model said: > Yes, I trust this folder"), "marker must start the line")
}
