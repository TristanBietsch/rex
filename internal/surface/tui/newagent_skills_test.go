package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/features/skills"
)

func libSkills(n int) []skills.Skill {
	out := make([]skills.Skill, n)
	for i := range out {
		out[i] = skills.Skill{Name: fmt.Sprintf("skill-%02d", i), Description: fmt.Sprintf("does thing %d", i)}
	}
	return out
}

// skillTools: a tool that loads skills per session and one that cannot
// (like Antigravity).
func skillTools() []registry.Tool {
	return []registry.Tool{
		{ID: "claude", Name: "Claude Code", SkillArgs: []string{"--plugin-dir", "{plugin_dir}"},
			Models: []registry.Model{{ID: "opus", Name: "Opus"}}},
		{ID: "antigravity", Name: "Antigravity", Models: []registry.Model{{ID: "flash", Name: "Flash"}}},
	}
}

func skillWizard(n int) *WizardState {
	return &WizardState{Tools: skillTools(), Skills: libSkills(n), SkillsOn: true}
}

func writeHomeSkill(t *testing.T, home, name, desc string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "rex", "skills", name)
	require.NoError(t, os.MkdirAll(dir, 0o700))                                                                          //nolint:gosec // G703: a test writes under its own temp HOME.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: "+desc+"\n---\n"), 0o600)) //nolint:gosec // G703: a test writes under its own temp HOME.
}

// Skills apply only for a tool with skill_args, with the feature on and a
// library that has some.
func TestSkills_ApplyRules(t *testing.T) {
	w := skillWizard(3)
	require.True(t, w.skillsApply())
	w.ToolIdx = 1 // antigravity: no skill_args
	require.False(t, w.skillsApply())

	off := skillWizard(3)
	off.SkillsOn = false
	require.False(t, off.skillsApply())
	require.Nil(t, off.chosenSkills())

	require.False(t, skillWizard(0).skillsApply())
}

// A choice made while a skill-capable tool was selected is not sent once
// another tool is.
func TestSkills_DroppedWhenToolChanges(t *testing.T) {
	w := skillWizard(2)
	w.SkillSel = map[string]bool{"skill-00": true}
	require.Equal(t, []string{"skill-00"}, w.chosenSkills())
	w.ToolIdx = 1
	require.Nil(t, w.chosenSkills())
}

func TestSkills_ToggleKeepsLibraryOrder(t *testing.T) {
	w := skillWizard(3)
	for _, i := range []int{2, 0, 1, 1} {
		w.SkillCursor = i
		w.toggleSkill()
	}
	require.Equal(t, []string{"skill-00", "skill-02"}, w.chosenSkills())
}

// The page reads the library when it opens with spawn_skills on.
func TestNewAgent_OpenLoadsSkillLibrary(t *testing.T) {
	m := naModel(t)
	home := os.Getenv("HOME")
	writeHomeSkill(t, home, "review", "Review a diff")
	m = naOpen(t, m)
	require.True(t, m.NewAgent.SkillsOn)
	require.Len(t, m.NewAgent.Skills, 1)
	require.Equal(t, "review", m.NewAgent.Skills[0].Name)
}

// spawn_skills off is inert: no library read, no skills sent.
func TestNewAgent_SkillsOffIsInert(t *testing.T) {
	m := naModel(t)
	writeHomeSkill(t, os.Getenv("HOME"), "review", "Review a diff")
	require.NoError(t, m.Store.Set("spawn_skills", false))
	m = naOpen(t, m)
	require.False(t, m.NewAgent.SkillsOn)
	require.Empty(t, m.NewAgent.Skills, "the library is not even read")
	req, err := m.NewAgent.request("/w", nil)
	require.NoError(t, err)
	require.Empty(t, req.Skills)
}
