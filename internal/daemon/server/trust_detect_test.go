package server

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

// An untrusted folder keeps the tool's dialog text so the supervisor can log
// that it went unanswered; a trusted one is answered and needs no detector.
func TestTrustDecision_DetectOnlyWhenUntrusted(t *testing.T) {
	dir := t.TempDir()
	tool := registry.Tool{ID: "claude", TrustPrompt: &registry.TrustPrompt{Shown: `Yes, I trust`, Selected: "t", Move: "\x1b[B", Confirm: "\r"}}
	srv := &Server{cfg: Config{AutoTrustDirs: func() []string { return []string{dir} }}}

	d := srv.trustDecisionFor(tool, t.TempDir(), false)
	require.NotNil(t, d.Detect)
	require.True(t, d.Detect.MatchString("Yes, I trust this folder"))
	require.Nil(t, d.Answer)
	require.False(t, d.Trusted)

	d = srv.trustDecisionFor(tool, filepath.Join(dir, "x"), false)
	require.Nil(t, d.Detect, "an armed answerer needs no detector")
	require.NotNil(t, d.Answer)

	require.Nil(t, srv.trustDecisionFor(registry.Tool{ID: "codex"}, dir, false).Detect, "no trust_prompt")

	bad := registry.Tool{ID: "x", TrustPrompt: &registry.TrustPrompt{Shown: "(", Selected: "t", Move: "m", Confirm: "c"}}
	require.Nil(t, srv.trustDecisionFor(bad, t.TempDir(), false).Detect, "a pattern that does not compile is skipped")
}
