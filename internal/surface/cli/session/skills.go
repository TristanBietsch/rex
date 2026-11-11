package session

import (
	"flag"
	"fmt"

	"github.com/tristanbietsch/rex/internal/features/skills"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunSkills lists the skill library (~/.config/rex/skills): the names
// `rex new -skill` accepts.
func RunSkills(args []string) error {
	fs := flag.NewFlagSet("skills", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	pos, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return core.Errorf(core.ExitInvalidArgs, "rex skills", "skills: unexpected argument %q", pos[0])
	}
	if err := core.RequireFeature("spawn_skills"); err != nil {
		return err
	}
	dir, err := paths.SkillsDir()
	if err != nil {
		return core.Errorf(core.ExitGeneric, "", "skills: %w", err)
	}
	list, problems := skills.List(dir)
	if list == nil {
		list = []skills.Skill{}
	}
	msgs := make([]string, len(problems))
	for i, p := range problems {
		msgs[i] = p.Error()
	}
	return core.Out().Emit(core.Result{
		Cmd:  "skills",
		Data: list,
		Meta: map[string]any{"dir": dir},
		View: skillsView(list, msgs, dir),
	})
}

// skillsView lays out the library: one row per skill, a warning per entry
// that could not be used, and a pointer to where skills go.
func skillsView(list []skills.Skill, problems []string, dir string) core.View {
	cols := []core.Col{
		{Key: "name", Title: "Name", Max: 24},
		{Key: "description", Title: "Description", Flex: true, Min: 12},
	}
	rows := make([][]core.Cell, 0, len(list))
	for _, s := range list {
		rows = append(rows, []core.Cell{{Text: s.Name}, {Text: s.Description}})
	}
	meta := ""
	if len(list) > 0 {
		meta = fmt.Sprintf("%d in %s", len(list), dir)
	}
	v := core.View{
		core.Header{Title: "Skills", Meta: meta},
		core.Table{Kind: "skill", Cols: cols, Rows: rows, Empty: "(no skills)"},
	}
	for _, p := range problems {
		v = append(v, core.Note{Tone: core.ToneWarn, Text: p})
	}
	if len(list) > 0 {
		return append(v, core.Hint{Cmd: "rex new -skill <name>", Text: "load a skill into a new session"})
	}
	return append(v, core.Note{Text: "add one as " + dir + "/<name>/SKILL.md"})
}
