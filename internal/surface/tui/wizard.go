package tui

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/features/skills"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// WizardState is the agent choice (tool, model, effort, skills) the
// new-agent page builds with its selection helpers. The popup wizard is
// gone (R21); the name stays until a follow-up rename.
type WizardState struct {
	Tools   []registry.Tool
	ToolIdx int
	// ModelIdx indexes the selected tool's Models.
	ModelIdx  int
	EffortIdx int
	// Skills is the skill library (~/.config/rex/skills) read when the
	// page opened; SkillsOn is the spawn_skills feature at that moment.
	// Skills apply only when both allow it and the selected tool declares
	// skill_args.
	Skills   []skills.Skill
	SkillsOn bool
	// SkillCursor indexes Skills; SkillSel holds the toggled names.
	SkillCursor int
	SkillSel    map[string]bool
}

// loadWizardSkills lists the skill library. A library that cannot be read
// shows no skills step; entries that cannot be used are skipped (rex skills
// names them).
func loadWizardSkills() []skills.Skill {
	dir, err := paths.SkillsDir()
	if err != nil {
		slog.Warn("tui: skills library path", "err", err)
		return nil
	}
	list, problems := skills.List(dir)
	for _, p := range problems {
		slog.Warn("tui: skill skipped", "err", p)
	}
	return list
}

// loadToolsStrict loads the built-in registry merged with the user's
// tools.yaml (paths.ToolsFile). Unlike loadUserTools it fails when the user
// file cannot be used, or its path resolved, instead of falling back.
func loadToolsStrict() (*registry.Registry, error) {
	path, err := paths.ToolsFile()
	if err != nil {
		return nil, err
	}
	return registry.Load(path)
}

// loadUserTools is loadToolsOrBuiltin on the user's tools.yaml
// (paths.ToolsFile), also returning that path. Without a home directory
// there is no user file: the built-ins load, path is "" and userErr says
// why.
func loadUserTools() (reg *registry.Registry, path string, userErr, err error) {
	path, userErr = paths.ToolsFile()
	if userErr != nil {
		reg, err = registry.Load("")
		return reg, "", userErr, err
	}
	reg, userErr, err = loadToolsOrBuiltin(path)
	return reg, path, userErr, err
}

// loadToolsOrBuiltin loads the built-in registry merged with the user's
// tools.yaml at path. registry.Load returns a nil registry on any error, so
// callers that ignored the error dereferenced nil (P0-08). Here a user file
// that can't be read, parsed or validated degrades to the built-in registry,
// and userErr says why so the caller can show it. err is set only when the
// built-in registry itself fails to load (a build defect); reg is then nil.
func loadToolsOrBuiltin(path string) (reg *registry.Registry, userErr, err error) {
	reg, userErr = registry.Load(path)
	if userErr == nil {
		return reg, nil, nil
	}
	reg, err = registry.Load("")
	return reg, userErr, err
}

var hiddenInWizard = map[string]bool{
	"echo": true, // internal test adapter, never user-visible
}

func visibleTools(tools []registry.Tool) []registry.Tool {
	out := make([]registry.Tool, 0, len(tools))
	for _, t := range tools {
		if hiddenInWizard[t.ID] {
			continue
		}
		if t.EnabledByDefault != nil && !*t.EnabledByDefault {
			continue
		}
		out = append(out, t)
	}
	return out
}

// selectedTool returns the tool under the provider cursor.
func (w *WizardState) selectedTool() registry.Tool {
	if w.ToolIdx < 0 || w.ToolIdx >= len(w.Tools) {
		return registry.Tool{}
	}
	return w.Tools[w.ToolIdx]
}

// selectedModel returns the chosen model of the selected tool. The index is
// clamped, so a stale one (the tool changed) still gives a model.
func (w *WizardState) selectedModel() registry.Model {
	models := w.selectedTool().Models
	if len(models) == 0 {
		return registry.Model{}
	}
	return models[min(max(w.ModelIdx, 0), len(models)-1)]
}

// skillsApply reports whether the skills step is shown: the feature is on,
// the selected tool can load skills per session, and the library has some.
func (w *WizardState) skillsApply() bool {
	return w.SkillsOn && len(w.Skills) > 0 && len(w.selectedTool().SkillArgs) > 0
}

// chosenSkills returns the toggled skill names in library order, or nil when
// the step does not apply (a choice made for one tool is not sent to another).
func (w *WizardState) chosenSkills() []string {
	if !w.skillsApply() {
		return nil
	}
	var out []string
	for _, s := range w.Skills {
		if w.SkillSel[s.Name] {
			out = append(out, s.Name)
		}
	}
	return out
}

func (w *WizardState) toggleSkill() {
	if w.SkillCursor < 0 || w.SkillCursor >= len(w.Skills) {
		return
	}
	if w.SkillSel == nil {
		w.SkillSel = map[string]bool{}
	}
	name := w.Skills[w.SkillCursor].Name
	if w.SkillSel[name] {
		delete(w.SkillSel, name)
	} else {
		w.SkillSel[name] = true
	}
}

// effortOptions are the chosen model's effort options, none when it has no
// effort block.
func (w *WizardState) effortOptions() []string {
	if e := w.selectedModel().Effort; e != nil {
		return e.Options
	}
	return nil
}

func (w *WizardState) currentEffort() string {
	m := w.selectedModel()
	if m.Effort == nil || len(m.Effort.Options) == 0 {
		return ""
	}
	if w.EffortIdx < 0 || w.EffortIdx >= len(m.Effort.Options) {
		return ""
	}
	return m.Effort.Options[w.EffortIdx]
}

func (w *WizardState) effortApplies() bool {
	m := w.selectedModel()
	return m.Effort != nil && len(m.Effort.Options) > 0
}

// presetEffortDefault snaps EffortIdx onto the model's declared default.
func presetEffortDefault(w *WizardState) {
	if !w.effortApplies() {
		w.EffortIdx = 0
		return
	}
	m := w.selectedModel()
	for i, opt := range m.Effort.Options {
		if opt == m.Effort.Default {
			w.EffortIdx = i
			return
		}
	}
	w.EffortIdx = 0
}

func playNav(m Model) {
	if m.Audio != nil {
		m.Audio.Play(audio.EventNav)
	}
}

func existingSlugs(m Model) []string {
	out := make([]string, 0, len(m.Sessions))
	for _, s := range m.Sessions {
		if s.Slug != "" {
			out = append(out, s.Slug)
		}
	}
	return out
}

// pendingSpawn is the part of a new-agent spawn an older daemon would drop.
type pendingSpawn struct {
	Skills []string
}

// ignoredBy says what the daemon that created sum left out of it, or "".
func (p pendingSpawn) ignoredBy(sum protocol.SessionSummary) string {
	if len(p.Skills) == 0 || len(sum.Skills) > 0 {
		return ""
	}
	return "this rex-daemon ignored skills; run `rex daemon restart`"
}

// ── Slug schema ────────────────────────────────────────────────────────────
//
// Format: <toolShort>.<modelShort>.<taskKebab>
//
//	cc.opus.fix-auth-bug
//	cx.gpt-5-codex.migrate-billing
//	gm.2-5-pro.refactor-auth
//	ol.llama3-1.write-readme
//
// Empty task → <toolShort>.<modelShort>.<hash4>, e.g. cc.opus.a3f2
// Collision → <slug>-2, <slug>-3, … against the client's known session set.

var toolShortcodes = map[string]string{
	"claude":      "cc",
	"codex":       "cx",
	"antigravity": "ag",
	"gemini":      "gm",
	"ollama":      "ol",
	"grok":        "gk",
	"deepseek":    "ds",
	"kimi":        "km",
	"echo":        "ec",
}

func toolShort(id string) string {
	if s, ok := toolShortcodes[id]; ok {
		return s
	}
	if len(id) >= 2 {
		return id[:2]
	}
	return id
}

// modelShort canonicalizes a model ID into a slug-safe fragment by lowercasing,
// turning dots into dashes, and dropping anything else.
func modelShort(id string) string {
	s := strings.ToLower(id)
	s = strings.ReplaceAll(s, ".", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

// kebabTask normalizes free-form task text into kebab-case, truncated to ~32
// chars on a word boundary.
func kebabTask(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 32 {
		cut := out[:32]
		if i := strings.LastIndexByte(cut, '-'); i > 16 {
			cut = cut[:i]
		}
		out = cut
	}
	return out
}

// hash4 returns 4 hex characters from an FNV-1a hash of s. Used as a fallback
// task fragment when the user gave no description.
func hash4(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%04x", h&0xffff)
}

// deriveAgentSlug builds the namespaced slug and disambiguates against the
// given set of existing slugs by appending -2, -3, … on collision.
func deriveAgentSlug(toolID, modelID, task string, existing []string) string {
	base := toolShort(toolID)
	if ms := modelShort(modelID); ms != "" {
		base += "." + ms
	}
	taskFrag := kebabTask(task)
	if taskFrag == "" {
		taskFrag = hash4(fmt.Sprintf("%s|%d", modelID, os.Getpid()))
	}
	candidate := base + "." + taskFrag
	if !inSlice(existing, candidate) {
		return candidate
	}
	for n := 2; n < 1000; n++ {
		c := fmt.Sprintf("%s-%d", candidate, n)
		if !inSlice(existing, c) {
			return c
		}
	}
	return candidate
}

func inSlice(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func padOrTrim(body []string, n int) []string {
	if len(body) < n {
		for len(body) < n {
			body = append(body, "")
		}
		return body
	}
	if len(body) > n {
		return body[:n]
	}
	return body
}
