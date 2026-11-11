package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/skills"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func sampleSkills() []skills.Skill {
	return []skills.Skill{
		{Name: "review", Description: "Review a diff for bugs"},
		{Name: "docs", Description: "Write docs"},
		{Name: "日本語", Description: "a|b `c` <i>"},
	}
}

func emitSkills(t *testing.T, format string, width int, list []skills.Skill, problems []string) string {
	t.Helper()
	p, out := lsPrinter(t, format, width, false)
	require.NoError(t, p.Emit(core.Result{Cmd: "skills", Data: list, Meta: map[string]any{"dir": "/h/.config/rex/skills"},
		View: skillsView(list, problems, "/h/.config/rex/skills")}))
	return out.String()
}

func TestSkillsGolden(t *testing.T) {
	problems := []string{`skill "Bad_Name": invalid name (use lowercase letters, digits and hyphens)`}
	golden.Check(t, "testdata/golden/skills.txtar",
		golden.Section{Name: "text.w40", Body: emitSkills(t, "text", 40, sampleSkills(), nil)},
		golden.Section{Name: "text.w80", Body: emitSkills(t, "text", 80, sampleSkills(), problems)},
		golden.Section{Name: "txt", Body: emitSkills(t, "txt", 80, sampleSkills(), nil)},
		golden.Section{Name: "md", Body: emitSkills(t, "md", 80, sampleSkills(), nil)},
		golden.Section{Name: "json", Body: emitSkills(t, "json", 80, sampleSkills(), nil)},
		golden.Section{Name: "agent", Body: emitSkills(t, "agent", 80, sampleSkills(), nil)},
		golden.Section{Name: "empty.text", Body: emitSkills(t, "text", 80, []skills.Skill{}, nil)},
		golden.Section{Name: "empty.json", Body: emitSkills(t, "json", 80, []skills.Skill{}, nil)},
		golden.Section{Name: "empty.agent", Body: emitSkills(t, "agent", 80, []skills.Skill{}, nil)},
	)
}

func TestSkillsFlattensUntrustedCells(t *testing.T) {
	evil := []skills.Skill{{Name: "x", Description: "d\x1b]0;pwn\a\nline"}}
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		out := emitSkills(t, f, 80, evil, nil)
		require.NotContains(t, out, "\x1b", f)
		require.NotContains(t, out, "\a", f)
	}
}

func useHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte(body), 0o600))
}

func captureOut(t *testing.T, format string) *bytes.Buffer {
	t.Helper()
	p, out := lsPrinter(t, format, 80, false)
	prev := core.Out()
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(prev) })
	return out
}

func TestRunSkills_ListsLibrary(t *testing.T) {
	home := useHome(t)
	lib := filepath.Join(home, ".config", "rex", "skills")
	for _, n := range []string{"review", "docs"} {
		require.NoError(t, os.MkdirAll(filepath.Join(lib, n), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(lib, n, "SKILL.md"), []byte("---\ndescription: about "+n+"\n---\n"), 0o600))
	}
	out := captureOut(t, "agent")
	require.NoError(t, RunSkills(nil))
	require.Contains(t, out.String(), "skill name=docs")
	require.Contains(t, out.String(), "skill name=review")
	require.Contains(t, out.String(), "ok")
}

func TestRunSkills_EmptyLibrary(t *testing.T) {
	useHome(t)
	out := captureOut(t, "json")
	require.NoError(t, RunSkills(nil))
	require.Contains(t, out.String(), `"data":[]`)
}

func TestRunSkills_OffExitsFive(t *testing.T) {
	home := useHome(t)
	writeConfig(t, home, "spawn_skills: false\n")
	captureOut(t, "txt")
	err := RunSkills(nil)
	require.Error(t, err)
	info := core.Describe(err)
	require.Equal(t, core.ExitInvalidArgs, info.Code)
	require.Equal(t, "off: enable in Settings → Features (rex config set spawn_skills true)", info.Msg)
}

func TestRunSkills_RejectsArgs(t *testing.T) {
	useHome(t)
	captureOut(t, "txt")
	err := RunSkills([]string{"extra"})
	require.Equal(t, core.ExitInvalidArgs, core.Describe(err).Code)
}

func TestCheckNewExtras(t *testing.T) {
	home := useHome(t)

	name, err := checkNewExtras("  my   agent ", nil)
	require.NoError(t, err)
	require.Equal(t, "my agent", name)

	_, err = checkNewExtras(strings.Repeat("x", 500), nil)
	require.Equal(t, core.ExitInvalidArgs, core.Describe(err).Code)
	_, err = checkNewExtras("\x1b\x07", nil)
	require.Equal(t, core.ExitInvalidArgs, core.Describe(err).Code, "nothing displayable")

	name, err = checkNewExtras("", []string{"review", "docs"})
	require.NoError(t, err)
	require.Empty(t, name)

	for _, bad := range [][]string{{"../x"}, {"a", "a"}, {"A"}} {
		_, err = checkNewExtras("", bad)
		require.Equal(t, core.ExitInvalidArgs, core.Describe(err).Code, bad)
		require.Contains(t, core.Describe(err).Msg, "new: -skill")
	}

	// Switched off: -skill exits 5 with the Features message; -name is not gated.
	writeConfig(t, home, "spawn_skills: false\n")
	_, err = checkNewExtras("", []string{"review"})
	require.Equal(t, "off: enable in Settings → Features (rex config set spawn_skills true)", core.Describe(err).Msg)
	_, err = checkNewExtras("still fine", nil)
	require.NoError(t, err)
}

func TestIgnoredByDaemon(t *testing.T) {
	req := protocol.NewSession{Name: "n", Skills: []string{"a"}}
	require.Empty(t, ignoredByDaemon(req, protocol.SessionSummary{Name: "n", Skills: []string{"a"}}))
	require.Empty(t, ignoredByDaemon(protocol.NewSession{}, protocol.SessionSummary{}))
	require.Equal(t, "this rex-daemon ignored the name and skills; run `rex daemon restart`",
		ignoredByDaemon(req, protocol.SessionSummary{}))
	require.Equal(t, "this rex-daemon ignored skills; run `rex daemon restart`",
		ignoredByDaemon(req, protocol.SessionSummary{Name: "n"}))
}
