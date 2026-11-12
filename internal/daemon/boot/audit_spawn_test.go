package boot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/audit"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const quickTools = `tools:
  - id: echo
    models:
      - id: quick
        name: "Prints and exits"
        args: ['echo hi; sleep 0.2']
`

// spawnAndAudit starts a daemon whose server uses a real Audit, spawns a
// quick agent with prompt, and returns the one record it writes.
func spawnAndAudit(t *testing.T, kv map[string]any, prompt string) audit.Record {
	t.Helper()
	store := state.NewStore()
	var a *Audit
	d := testutil.StartDaemonWithConfig(t, quickTools, func(cfg *server.Config) {
		cfg.Store = store
		a = NewAudit(AuditConfig{StateDir: cfg.StateDir}, store)
		a.Configure(auditSettings(t, kv))
		cfg.Audit = a
	})
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.NewSession(protocol.NewSession{
		ToolID: "echo", ModelID: "quick", Slug: "audited", CWD: d.StateDir, InitialPrompt: prompt,
	}))

	file := filepath.Join(d.StateDir, paths.AuditFileName)
	var got []audit.Record
	require.Eventually(t, func() bool {
		got, _ = audit.Read([]string{file}, audit.Filter{})
		return len(got) == 1
	}, 15*time.Second, 50*time.Millisecond)
	a.Close()
	return got[0]
}

func TestAudit_SpawnPromptFirstLineAndFullPromptFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and PTY children")
	}
	prompt := "fix the login bug\nsecond line holds a secret"

	r := spawnAndAudit(t, nil, prompt)
	require.Equal(t, "fix the login bug", r.PromptFirstLine, "from the spawn's initial_prompt")
	require.Empty(t, r.Prompt, "no full prompt without audit_full_prompt")
	require.Equal(t, "audited", r.Name)

	r = spawnAndAudit(t, map[string]any{"audit_full_prompt": true}, prompt)
	require.Equal(t, "fix the login bug", r.PromptFirstLine)
	require.Equal(t, prompt, r.Prompt)
}

func TestAudit_UnwritableTrailMakesEndFailAndRecover(t *testing.T) {
	r := newAuditRig(t, nil, "")
	require.NoError(t, os.MkdirAll(r.global(), 0o750)) // a directory where the file should be
	s := auditSession("aaaa1111", "/w")
	require.NoError(t, r.store.Add(s))
	require.Error(t, r.a.End(s))
	require.False(t, s.AuditIsWritten())
	r.a.Close()
}
