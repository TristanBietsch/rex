package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// skillTools: an echo tool that can load skills, one that cannot, and a
// hook-detected stand-in to see where skill args sit relative to hook args.
const skillTools = `tools:
  - id: echo
    skill_args: ["--plugin-dir", "{plugin_dir}"]
    models:
      - id: argv
        name: "Prints its argv"
        args: ['echo "ARGV: $0 $*"; sleep 0.3']
  - id: plain
    category: self_hosted
    command: ["sh", "-c"]
    detect: { kind: heuristic, prompt_regex: "\\$ $", idle_ms: 200 }
    models:
      - id: argv
        name: "No skill support"
        args: ['echo "ARGV: $0 $*"; sleep 0.3']
`

func writeLibSkill(t *testing.T, lib, name string) {
	t.Helper()
	dir := filepath.Join(lib, name)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: "+name+"\n---\nbody of "+name+"\n"), 0o600))
}

// skillEnv is a resume server with a skills library under a temp dir.
func skillEnv(t *testing.T, tools string, on func() bool, seed ...*state.Session) (*retireEnv, string) {
	t.Helper()
	e := startResumeServer(t, tools, 0, seed...)
	lib := t.TempDir()
	e.srv.cfg.SkillsDir = lib
	e.srv.cfg.SkillsEnabled = on
	writeLibSkill(t, lib, "review")
	writeLibSkill(t, lib, "docs")
	return e, lib
}

func (e *retireEnv) newSession(t *testing.T, p protocol.NewSession) (code, msg string) {
	t.Helper()
	conn, w, r := e.dial(t)
	if p.CWD == "" {
		p.CWD = e.dir
	}
	return call(t, conn, w, r, protocol.IntentNewSession, p)
}

func (e *retireEnv) onlySession(t *testing.T) protocol.SessionSummary {
	t.Helper()
	require.Eventually(t, func() bool { return len(e.store.Snapshot()) == 1 }, 5*time.Second, 20*time.Millisecond)
	return e.store.Snapshot()[0]
}

func TestNewSession_SkillsAddPluginDir(t *testing.T) {
	e, _ := skillEnv(t, skillTools, nil)
	code, msg := e.newSession(t, protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "sk", Skills: []string{"review", "docs"}, Name: "  my   agent "})
	require.Empty(t, code, msg)
	sum := e.onlySession(t)
	require.Equal(t, []string{"review", "docs"}, sum.Skills)
	require.Equal(t, "my agent", sum.Name)

	plugin := filepath.Join(e.dir, "sessions", sum.ID, "plugin")
	require.Eventually(t, func() bool { return strings.Contains(e.transcript(sum.ID), "ARGV:") }, 10*time.Second, 20*time.Millisecond)
	require.Contains(t, e.transcript(sum.ID), "--plugin-dir "+plugin)
	for _, p := range []string{".claude-plugin/plugin.json", "skills/review/SKILL.md", "skills/docs/SKILL.md"} {
		_, err := os.Stat(filepath.Join(plugin, p))
		require.NoError(t, err, p)
	}

	// meta.json keeps the skills for Resume.
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(filepath.Join(e.dir, "sessions", sum.ID, "meta.json"))
		if err != nil {
			return false
		}
		var m protocol.SessionSummary
		return json.Unmarshal(raw, &m) == nil && len(m.Skills) == 2
	}, 5*time.Second, 20*time.Millisecond)
	loaded, err := state.LoadMeta(e.dir, sum.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"review", "docs"}, loaded.Skills)
}

func TestNewSession_NoSkillsNoPluginDir(t *testing.T) {
	e, _ := skillEnv(t, skillTools, nil)
	code, msg := e.newSession(t, protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "nosk"})
	require.Empty(t, code, msg)
	sum := e.onlySession(t)
	require.Eventually(t, func() bool { return strings.Contains(e.transcript(sum.ID), "ARGV:") }, 10*time.Second, 20*time.Millisecond)
	require.NotContains(t, e.transcript(sum.ID), "--plugin-dir")
	_, err := os.Stat(filepath.Join(e.dir, "sessions", sum.ID, "plugin"))
	require.True(t, os.IsNotExist(err))
}

func TestNewSession_SkillsRejected(t *testing.T) {
	tooMany := make([]string, 17)
	for i := range tooMany {
		tooMany[i] = "s" + string(rune('a'+i))
	}
	for _, tc := range []struct {
		name string
		p    protocol.NewSession
		want string
	}{
		{"unsupported tool", protocol.NewSession{ToolID: "plain", ModelID: "argv", Slug: "a", Skills: []string{"review"}}, "cannot load skills"},
		{"unknown skill", protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "a", Skills: []string{"nope"}}, "not found"},
		{"traversal", protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "a", Skills: []string{"../review"}}, "invalid skill name"},
		{"duplicate", protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "a", Skills: []string{"review", "review"}}, "duplicate"},
		{"too many", protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "a", Skills: tooMany}, "too many"},
		{"name too long", protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "a", Name: strings.Repeat("x", 500)}, "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := skillEnv(t, skillTools, nil)
			code, msg := e.newSession(t, tc.p)
			require.Equal(t, protocol.ErrCodeBadIntent, code)
			require.Contains(t, msg, tc.want)
			require.Empty(t, e.store.Snapshot(), "nothing is created")
			entries, _ := os.ReadDir(filepath.Join(e.dir, "sessions"))
			require.Empty(t, entries, "no session directory is left behind")
		})
	}
}

func TestNewSession_SkillsOffIsInert(t *testing.T) {
	e, _ := skillEnv(t, skillTools, func() bool { return false })
	code, msg := e.newSession(t, protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "off", Skills: []string{"review"}})
	require.Equal(t, protocol.ErrCodeBadIntent, code)
	require.Equal(t, "skills are off", msg)
	require.Empty(t, e.store.Snapshot())

	// A session without skills is unaffected, and gets no plugin dir.
	code, msg = e.newSession(t, protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "off2"})
	require.Empty(t, code, msg)
	sum := e.onlySession(t)
	require.Eventually(t, func() bool { return strings.Contains(e.transcript(sum.ID), "ARGV:") }, 10*time.Second, 20*time.Millisecond)
	require.NotContains(t, e.transcript(sum.ID), "--plugin-dir")
}

func TestNewSession_SkillsNeedLibrary(t *testing.T) {
	e := startResumeServer(t, skillTools, 0)
	code, msg := e.newSession(t, protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "nolib", Skills: []string{"review"}})
	require.Equal(t, protocol.ErrCodeBadIntent, code)
	require.Contains(t, msg, "library")
}

func TestNewSession_SlugTakenCleansPlugin(t *testing.T) {
	e, _ := skillEnv(t, skillTools, nil, &state.Session{ID: "taken", ShortID: "tk", Slug: "dup", ToolID: "echo", ModelID: "argv", CWD: "/", State: protocol.StateDone})
	code, _ := e.newSession(t, protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "dup", Skills: []string{"review"}})
	require.Equal(t, protocol.ErrCodeBadIntent, code)
	entries, _ := os.ReadDir(filepath.Join(e.dir, "sessions"))
	for _, en := range entries {
		require.Equal(t, "taken", en.Name(), "no staged plugin left for the rejected session")
	}
}

// Skill args sit after resume args and before hook args, all before the prompt.
func TestBuildArgv_SkillArgsOrder(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, model, ok := reg.FindModel("claude", "opus")
	if !ok {
		tool, _ = reg.Find("claude")
		model = tool.Models[0]
	}
	argv := buildArgv(spawnSpec{
		Tool: tool, Model: model, CWD: "/w", SessionID: "sid",
		Resume:    &resumeArgs{ID: testResumeID, New: true},
		PluginDir: "/s/sessions/x/plugin dir",
	})
	pi := indexOf(argv, "--plugin-dir")
	require.GreaterOrEqual(t, pi, 0, argv)
	require.Equal(t, "/s/sessions/x/plugin dir", argv[pi+1], "one argument, spaces kept")
	require.Greater(t, pi, indexOf(argv, "--session-id"), "after resume args")
	require.Less(t, pi, indexOf(argv, "--settings"), "before hook args")

	argv = buildArgv(spawnSpec{Tool: tool, Model: model, CWD: "/w", SessionID: "sid"})
	require.Equal(t, -1, indexOf(argv, "--plugin-dir"), "no PluginDir, no args")

	// A tool without skill_args ignores PluginDir.
	plain := registry.Tool{ID: "p", Command: []string{"p"}}
	require.Equal(t, []string{"p"}, buildArgv(spawnSpec{Tool: plain, PluginDir: "/x"}))
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func TestResume_ReaddsPluginDir(t *testing.T) {
	const tools = `tools:
  - id: echo
    skill_args: ["--plugin-dir", "{plugin_dir}"]
    resume:
      args: ["--resume", "{id}"]
    models:
      - id: argv
        name: "Prints its argv"
        args: ['echo "ARGV: $0 $*"; sleep 0.3']
`
	for _, tc := range []struct {
		name      string
		stagedDir bool // plugin dir survives from the first run
		on        bool
		want      bool
	}{
		{"restaged from library", false, true, true},
		{"existing snapshot reused", true, true, true},
		{"flag off is inert", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := exited(os.TempDir, "a", "done", "argv", testResumeID)
			s.Skills = []string{"review"}
			e, _ := skillEnv(t, tools, func() bool { return tc.on }, s)
			plugin := filepath.Join(e.dir, "sessions", "a", "plugin")
			if tc.stagedDir {
				require.NoError(t, os.MkdirAll(filepath.Join(plugin, "skills", "review"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(plugin, "skills", "review", "SKILL.md"), []byte("snapshot"), 0o600))
			}
			conn, w, r := e.dial(t)
			code, msg := call(t, conn, w, r, protocol.IntentResume, map[string]string{"session_id": "a"})
			require.Empty(t, code, msg)
			require.Eventually(t, func() bool { return strings.Contains(e.transcript("a"), "ARGV:") }, 10*time.Second, 20*time.Millisecond)
			tr := e.transcript("a")
			require.Contains(t, tr, "--resume "+testResumeID)
			require.Equal(t, tc.want, strings.Contains(tr, "--plugin-dir "+plugin), tr)
			if tc.want {
				body, err := os.ReadFile(filepath.Join(plugin, "skills", "review", "SKILL.md"))
				require.NoError(t, err)
				if tc.stagedDir {
					require.Equal(t, "snapshot", string(body))
				} else {
					require.Contains(t, string(body), "body of review")
				}
			}
			e.waitIdle(t, "a")
		})
	}
}
