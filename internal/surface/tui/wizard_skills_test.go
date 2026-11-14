package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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

func TestWizard_SkillsStepHiddenForAgy(t *testing.T) {
	w := skillWizard(3)
	w.ToolIdx = 1                     // antigravity: no skill_args
	m := wizKey(wizardOn(w), "enter") // provider -> describe
	require.Equal(t, wizDescribe, m.Wizard.Step, "no skill_args, no skills step")
	m = wizKey(m, "b")
	require.Equal(t, wizProvider, m.Wizard.Step)
}

func TestWizard_SkillsStepHiddenWhenOffOrEmpty(t *testing.T) {
	off := skillWizard(3)
	off.SkillsOn = false
	require.False(t, off.stepApplies(wizSkills))
	require.Nil(t, off.chosenSkills())

	empty := skillWizard(0)
	require.False(t, empty.stepApplies(wizSkills))

	m := wizKey(wizardOn(off), "enter")
	require.Equal(t, wizDescribe, m.Wizard.Step)
}

func TestWizard_SkillToggleSendsSkills(t *testing.T) {
	m := wizardOn(skillWizard(3))
	m = wizKey(m, "enter") // provider
	require.Equal(t, wizSkills, m.Wizard.Step)

	m = wizKey(m, " ") // toggle skill-00
	m = wizKey(m, "j", "j", " ")
	require.Equal(t, 2, m.Wizard.SkillCursor)
	m = wizKey(m, "j") // clamps
	require.Equal(t, 2, m.Wizard.SkillCursor)
	m = wizKey(m, "k", " ", " ") // skill-01 on then off again
	require.Equal(t, []string{"skill-00", "skill-02"}, m.Wizard.chosenSkills())

	m = wizKey(m, "enter")
	require.Equal(t, wizDescribe, m.Wizard.Step)
	req := m.Wizard.request("/w", nil)
	require.Equal(t, []string{"skill-00", "skill-02"}, req.Skills)
	require.Equal(t, "claude", req.ToolID)

	// Back returns to the skills step with the choice kept.
	m = wizKey(m, "b")
	require.Equal(t, wizSkills, m.Wizard.Step)
	require.Equal(t, []string{"skill-00", "skill-02"}, m.Wizard.chosenSkills())
}

func TestWizard_NoSkillsChosenSendsNone(t *testing.T) {
	m := wizKey(wizardOn(skillWizard(2)), "enter", "enter")
	require.Equal(t, wizDescribe, m.Wizard.Step)
	require.Empty(t, m.Wizard.request("/w", nil).Skills)
}

// A choice made while a skill-capable tool was selected is not sent once
// another tool is.
func TestWizard_SkillsDroppedWhenToolChanges(t *testing.T) {
	m := wizardOn(skillWizard(2))
	m = wizKey(m, "enter", " ")
	require.Equal(t, []string{"skill-00"}, m.Wizard.chosenSkills())
	m.Wizard.ToolIdx = 1
	require.Nil(t, m.Wizard.chosenSkills())
	require.Empty(t, m.Wizard.request("/w", nil).Skills)
}

func TestWizard_SkillsStepEsc(t *testing.T) {
	m := wizKey(wizardOn(skillWizard(2)), "enter", "esc")
	require.Nil(t, m.Wizard)
	require.Equal(t, FocusBoard, m.Focus)
}

func TestWizard_SkillsStepRenders(t *testing.T) {
	w := skillWizard(3)
	w.Step = wizSkills
	w.SkillSel = map[string]bool{"skill-01": true}
	out := ansi.Strip(renderWizard(wizardOn(w)))
	require.Contains(t, out, "load skills? (optional)")
	require.Contains(t, out, "[ ] skill-00")
	require.Contains(t, out, "[x] skill-01")
	require.Contains(t, out, "does thing 2")
	require.Contains(t, wizardFooter(wizSkills), "space toggle")
}

// Many skills scroll inside the fixed popup height, keeping the cursor in view.
func TestWizard_SkillsStepScrolls(t *testing.T) {
	w := skillWizard(40)
	w.Step = wizSkills
	base := strings.Count(renderWizard(wizardOn(w)), "\n")

	w.SkillCursor = 30
	out := ansi.Strip(renderWizard(wizardOn(w)))
	require.Equal(t, base, strings.Count(out, "\n"), "height does not change")
	require.Contains(t, out, "skill-30")
	require.NotContains(t, out, "skill-00")
	require.Contains(t, out, "↑ more")
	require.Contains(t, out, "↓ more")
	for _, l := range strings.Split(out, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(l), wizardWidth, l)
	}
}

func TestWizard_SkillsStepTruncatesLongText(t *testing.T) {
	w := skillWizard(1)
	w.Step = wizSkills
	w.Skills[0] = skills.Skill{Name: strings.Repeat("n", 40), Description: strings.Repeat("d", 300)}
	for _, l := range strings.Split(ansi.Strip(renderWizard(wizardOn(w))), "\n") {
		require.LessOrEqual(t, ansi.StringWidth(l), wizardWidth)
	}
}

func writeHomeSkill(t *testing.T, home, name, desc string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "rex", "skills", name)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: "+desc+"\n---\n"), 0o600))
}

func TestWizard_OpenLoadsLibraryWhenFeatureOn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeHomeSkill(t, home, "review", "Review a diff")

	m := commandModel(t)
	m, _ = openWizard(m)
	require.NotNil(t, m.Wizard)
	require.True(t, m.Wizard.SkillsOn)
	require.Len(t, m.Wizard.Skills, 1)
	require.Equal(t, "review", m.Wizard.Skills[0].Name)

	// No store yet: the feature reads its default (on).
	n, _ := openWizard(Model{Focus: FocusBoard})
	require.True(t, n.Wizard.SkillsOn)
}

// spawn_skills off is inert: no library read, no step, no skills sent.
func TestWizard_SkillsOffIsInert(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeHomeSkill(t, home, "review", "Review a diff")

	m := commandModel(t)
	require.NoError(t, m.Store.Set("spawn_skills", false))
	m, _ = openWizard(m)
	require.NotNil(t, m.Wizard)
	require.False(t, m.Wizard.SkillsOn)
	require.Empty(t, m.Wizard.Skills, "the library is not even read")
	for i := range m.Wizard.Tools {
		m.Wizard.ToolIdx = i
		require.False(t, m.Wizard.stepApplies(wizSkills), m.Wizard.Tools[i].ID)
		require.Empty(t, m.Wizard.request("/w", nil).Skills)
	}
}

func TestWizard_OldDaemonIgnoringSkillsIsReported(t *testing.T) {
	w := skillWizard(2)
	w.SkillSel = map[string]bool{"skill-00": true}
	req := w.request("/w", nil)
	require.Equal(t, pendingSpawn{Skills: []string{"skill-00"}}, pendingSpawn{Skills: req.Skills})
}
