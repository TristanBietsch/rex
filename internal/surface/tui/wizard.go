package tui

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/features/skills"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// wizardStep names the phases of the new-agent flow, in order. A step that
// does not apply (one model, no effort levels) is skipped in both directions
// (see stepApplies). Slug edit and confirm steps are gone; the slug is derived
// from the task description and pressing enter on the describe step launches
// the session directly.
type wizardStep int

const (
	wizProvider wizardStep = iota
	wizModel
	wizEffort
	wizName
	wizSkills
	wizDescribe
)

// WizardState lives on Model when Focus == FocusWizard.
type WizardState struct {
	Step    wizardStep
	Tools   []registry.Tool
	ToolIdx int
	// ModelIdx indexes the selected tool's Models.
	ModelIdx  int
	EffortIdx int
	// NameText is the optional display name being typed; it is validated
	// with protocol.NormalizeSessionName as it changes.
	NameText string
	// Skills is the skill library (~/.config/rex/skills) read when the
	// wizard opened; SkillsOn is the spawn_skills feature at that moment.
	// The skills step shows only when both allow it and the selected tool
	// declares skill_args.
	Skills   []skills.Skill
	SkillsOn bool
	// SkillCursor indexes Skills; SkillSel holds the toggled names.
	SkillCursor int
	SkillSel    map[string]bool
	TaskText    string
	// SpawnTool and SpawnModel are the quick-spawn defaults
	// (default_spawn_tool / default_spawn_model): the model step starts on
	// SpawnModel when its tool is the selected one.
	SpawnTool, SpawnModel string
}

func openWizard(m Model) (Model, tea.Cmd) {
	reg, err := loadToolsStrict()
	if err != nil {
		m.Err = "wizard: " + err.Error()
		return m, nil
	}
	visible := visibleTools(reg.Tools)
	if len(visible) == 0 {
		m.Err = "wizard: no tools enabled"
		return m, nil
	}
	w := &WizardState{Step: wizProvider, Tools: visible}
	if m.Store != nil {
		w.SpawnTool, _ = m.Store.Get("default_spawn_tool").(string)
		w.SpawnModel, _ = m.Store.Get("default_spawn_model").(string)
	}
	if m.featureOn("spawn_skills") {
		w.SkillsOn = true
		w.Skills = loadWizardSkills()
	}
	presetModelDefault(w)
	presetEffortDefault(w)
	m.Wizard = w
	m.Focus = FocusWizard
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	return m, nil
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

// presetModelDefault points ModelIdx at the quick-spawn model when the
// selected tool is the quick-spawn tool, else at the first model (tools.yaml
// lists the strongest model first).
func presetModelDefault(w *WizardState) {
	w.ModelIdx = 0
	t := w.selectedTool()
	if t.ID != w.SpawnTool {
		return
	}
	for i, md := range t.Models {
		if md.ID == w.SpawnModel {
			w.ModelIdx = i
			return
		}
	}
}

// stepApplies reports whether the wizard shows step for the current tool and
// model.
// nameError is why the typed name cannot be used, or nil (an empty name is
// fine: the slug is shown).
func (w *WizardState) nameError() error {
	_, err := protocol.NormalizeSessionName(w.NameText)
	return err
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

func (w *WizardState) stepApplies(step wizardStep) bool {
	switch step {
	case wizSkills:
		return w.skillsApply()
	case wizModel:
		return len(w.selectedTool().Models) > 1
	case wizEffort:
		return w.effortApplies()
	case wizProvider, wizName, wizDescribe:
		return true
	}
	return false
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

// nextWizardStep moves to the next step that applies. Leaving the provider
// or model step re-derives what depends on it (the effort default).
func nextWizardStep(w *WizardState) {
	if w.Step == wizProvider || w.Step == wizModel {
		presetEffortDefault(w)
	}
	for s := w.Step + 1; s <= wizDescribe; s++ {
		if w.stepApplies(s) {
			w.Step = s
			return
		}
	}
	// Last step: updateWizardDescribeStep owns enter there.
}

// prevWizardStep moves to the previous step that applies.
func prevWizardStep(w *WizardState) {
	for s := w.Step - 1; s >= wizProvider; s-- {
		if w.stepApplies(s) {
			w.Step = s
			return
		}
	}
	// First step: nothing to go back to.
}

func updateWizardKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Wizard == nil {
		return m, nil
	}
	switch m.Wizard.Step {
	case wizName:
		return updateWizardNameStep(m, k)
	case wizDescribe:
		return updateWizardDescribeStep(m, k)
	case wizProvider, wizModel, wizEffort, wizSkills:
	}
	switch k.String() {
	case "esc":
		return closeWizard(m), nil
	case "b":
		prevWizardStep(m.Wizard)
		playNav(m)
		return m, nil
	case "j", "down":
		return moveWizard(m, +1), nil
	case "k", "up":
		return moveWizard(m, -1), nil
	case "enter":
		nextWizardStep(m.Wizard)
		playNav(m)
		return m, nil
	case " ":
		if m.Wizard.Step == wizSkills {
			m.Wizard.toggleSkill()
			playNav(m)
		}
		return m, nil
	}
	return m, nil
}

func moveWizard(m Model, delta int) Model {
	if m.Wizard == nil {
		return m
	}
	moved := false
	switch m.Wizard.Step {
	case wizProvider:
		next := m.Wizard.ToolIdx + delta
		if next >= 0 && next < len(m.Wizard.Tools) {
			m.Wizard.ToolIdx = next
			presetModelDefault(m.Wizard)
			presetEffortDefault(m.Wizard)
			moved = true
		}
	case wizModel:
		next := m.Wizard.ModelIdx + delta
		if next >= 0 && next < len(m.Wizard.selectedTool().Models) {
			m.Wizard.ModelIdx = next
			presetEffortDefault(m.Wizard)
			moved = true
		}
	case wizEffort:
		opts := m.Wizard.selectedModel().Effort.Options
		next := m.Wizard.EffortIdx + delta
		if next >= 0 && next < len(opts) {
			m.Wizard.EffortIdx = next
			moved = true
		}
	case wizSkills:
		next := m.Wizard.SkillCursor + delta
		if next >= 0 && next < len(m.Wizard.Skills) {
			m.Wizard.SkillCursor = next
			moved = true
		}
	case wizName, wizDescribe:
		// Free-text steps: updateWizardNameStep / updateWizardDescribeStep own the keys.
	}
	if moved {
		playNav(m)
	}
	return m
}

// wizardInputMaxRunes bounds what a free-text step keeps, so a held key
// cannot grow the text without limit. The name's own limit (64) is lower and
// reported as an error.
const wizardInputMaxRunes = 4096

// updateWizardNameStep handles the optional name step: enter moves on (an
// empty field skips it, an invalid name stays put with the error shown), b
// goes back while the field is empty.
func updateWizardNameStep(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	w := m.Wizard
	switch k.Type {
	case tea.KeyEsc:
		return closeWizard(m), nil
	case tea.KeyEnter:
		if w.nameError() != nil {
			return m, nil
		}
		nextWizardStep(w)
		playNav(m)
	case tea.KeyBackspace:
		w.NameText = dropLastRune(w.NameText)
	case tea.KeyRunes:
		if k.String() == "b" && w.NameText == "" {
			prevWizardStep(w)
			playNav(m)
			return m, nil
		}
		if len([]rune(w.NameText)) < wizardInputMaxRunes {
			w.NameText += string(k.Runes)
		}
	case tea.KeySpace:
		if len([]rune(w.NameText)) < wizardInputMaxRunes {
			w.NameText += " "
		}
	}
	return m, nil
}

func updateWizardDescribeStep(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if k.Type == tea.KeyEsc || k.String() == "esc" {
		return closeWizard(m), nil
	}
	switch k.Type {
	case tea.KeyEnter:
		cwd, _ := os.Getwd()
		req := m.Wizard.request(cwd, existingSlugs(m))
		m.PendingSelectSlug = req.Slug
		m.PendingSpawn = pendingSpawn{Name: req.Name, Skills: req.Skills}
		return closeWizard(m), wizardLaunchCmd(m.Client, req)
	case tea.KeyBackspace:
		if len(m.Wizard.TaskText) > 0 {
			m.Wizard.TaskText = dropLastRune(m.Wizard.TaskText)
		}
		return m, nil
	case tea.KeyRunes:
		if k.String() == "b" && m.Wizard.TaskText == "" {
			// Only treat 'b' as "back" when the input is empty — otherwise it's a character.
			prevWizardStep(m.Wizard)
			playNav(m)
			return m, nil
		}
		m.Wizard.TaskText += string(k.Runes)
		return m, nil
	case tea.KeySpace:
		m.Wizard.TaskText += " "
		return m, nil
	}
	return m, nil
}

func closeWizard(m Model) Model {
	m.Wizard = nil
	m.Focus = FocusBoard
	if m.Audio != nil {
		m.Audio.Play(audio.EventClose)
	}
	return m
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

// pendingSpawn is the part of a wizard spawn an older daemon would drop.
type pendingSpawn struct {
	Name   string
	Skills []string
}

// ignoredBy says what the daemon that created sum left out of it, or "".
func (p pendingSpawn) ignoredBy(sum protocol.SessionSummary) string {
	var what []string
	if p.Name != "" && sum.Name == "" {
		what = append(what, "the name")
	}
	if len(p.Skills) > 0 && len(sum.Skills) == 0 {
		what = append(what, "skills")
	}
	if len(what) == 0 {
		return ""
	}
	return "this rex-daemon ignored " + strings.Join(what, " and ") + "; run `rex daemon restart`"
}

// request builds the NewSession the wizard's choices stand for: the slug is
// derived from the task and made unique against existing.
func (w *WizardState) request(cwd string, existing []string) protocol.NewSession {
	task := strings.TrimSpace(w.TaskText)
	name, _ := protocol.NormalizeSessionName(w.NameText)
	tool, model := w.selectedTool(), w.selectedModel()
	// With no task, the name stands in for it in the slug.
	slugTask := task
	if slugTask == "" {
		slugTask = name
	}
	return protocol.NewSession{
		ToolID:        tool.ID,
		ModelID:       model.ID,
		Effort:        w.currentEffort(),
		Slug:          deriveAgentSlug(tool.ID, model.ID, slugTask, existing),
		Title:         task,
		CWD:           cwd,
		InitialPrompt: task,
		Name:          name,
		Skills:        w.chosenSkills(),
	}
}

func wizardLaunchCmd(c *client.Client, req protocol.NewSession) tea.Cmd {
	return func() tea.Msg {
		if req.Slug == "" {
			req.Slug = "session"
		}
		err := c.NewSession(req)
		if err != nil {
			return CmdErrMsg{Op: "spawn", Err: err}
		}
		return nil
	}
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

// ── Rendering ──────────────────────────────────────────────────────────────

// Wizard inner content dimensions: fixed so every step renders at the same size.
const (
	wizardWidth     = 78
	wizardBodyLines = 12
	wizardInputCols = 60
)

func renderWizard(m Model) string {
	if m.Wizard == nil {
		return ""
	}

	var (
		title string
		body  []string
	)

	switch m.Wizard.Step {
	case wizProvider:
		title = styleSlug.Render("which provider?")
		body = append(body, "")
		for i, t := range m.Wizard.Tools {
			body = append(body, wizOption(i == m.Wizard.ToolIdx, t.Icon, t.Name, t.Color))
		}
	case wizModel:
		title = styleSlug.Render("which model?")
		body = append(body, "")
		for i, md := range m.Wizard.selectedTool().Models {
			body = append(body, wizOption(i == m.Wizard.ModelIdx, "", md.Name, ""))
		}
	case wizEffort:
		title = styleSlug.Render("reasoning effort?")
		body = append(body, "")
		opts := m.Wizard.selectedModel().Effort.Options
		for i, opt := range opts {
			body = append(body, wizOption(i == m.Wizard.EffortIdx, "", opt, ""))
		}
	case wizName:
		title = styleSlug.Render("name it? (optional)")
		body = append(body, "")
		body = append(body, wizInputBoxLines(m.Wizard.NameText, m)...)
		if err := m.Wizard.nameError(); err != nil {
			body = append(body, "", "  "+lipgloss.NewStyle().Foreground(colorFailed).Render(err.Error()))
		} else {
			body = append(body, "", styleDim.Render("  shown on the board instead of the slug"))
		}
	case wizSkills:
		title = styleSlug.Render("load skills? (optional)")
		body = append(body, "")
		body = append(body, wizSkillLines(m.Wizard)...)
	case wizDescribe:
		title = styleSlug.Render("describe the task")
		body = append(body, "")
		body = append(body, wizInputBoxLines(m.Wizard.TaskText, m)...)
	}

	// Pad / truncate to a fixed line budget so the popup stays the same size.
	body = padOrTrim(body, wizardBodyLines)

	footer := wizardFooter(m.Wizard.Step)

	lines := []string{title}
	lines = append(lines, body...)
	lines = append(lines, "", footer)

	for i, line := range lines {
		lines[i] = padLine(line, wizardWidth)
	}
	return strings.Join(lines, "\n")
}

func wizardFooter(step wizardStep) string {
	switch step {
	case wizProvider:
		return styleDim.Render("  j/k select   enter next   esc cancel")
	case wizModel, wizEffort:
		return styleDim.Render("  j/k select   enter next   b back   esc cancel")
	case wizName:
		return styleDim.Render("  enter next (empty skips)   b back (when empty)   esc cancel")
	case wizSkills:
		return styleDim.Render("  j/k move   space toggle   enter next   b back   esc cancel")
	case wizDescribe:
		return styleDim.Render("  enter launch   b back (when empty)   esc cancel")
	}
	return ""
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

// wizOption renders a single selectable row: cursor + optional brand glyph + name.
func wizOption(selected bool, glyph, name, glyphColor string) string {
	var cursor string
	if selected {
		cursor = styleArrow.Render("▸") + " "
	} else {
		cursor = "  "
	}
	var g string
	if glyph != "" {
		if glyphColor != "" {
			g = lipgloss.NewStyle().Foreground(lipgloss.Color(glyphColor)).Render(glyph) + "  "
		} else {
			g = glyph + "  "
		}
	}
	return "  " + cursor + g + styleSlug.Render(name)
}

// wizSkillLines renders the skills checklist: cursor, [x] box, name and the
// description cut to the popup width, scrolled to keep the cursor visible
// inside the fixed body height (one row is the blank line above).
func wizSkillLines(w *WizardState) []string {
	nameW := 0
	for _, s := range w.Skills {
		nameW = max(nameW, min(ansi.StringWidth(s.Name), 24))
	}
	lines := make([]string, len(w.Skills))
	for i, s := range w.Skills {
		cursor, box := "  ", "[ ]"
		if i == w.SkillCursor {
			cursor = styleArrow.Render("▸") + " "
		}
		if w.SkillSel[s.Name] {
			box = "[x]"
		}
		name := truncate(s.Name, 24)
		desc := ""
		if s.Description != "" {
			desc = styleDim.Render(truncate(s.Description, wizardWidth-nameW-12))
		}
		lines[i] = "  " + cursor + box + " " + styleSlug.Render(name) +
			strings.Repeat(" ", nameW-ansi.StringWidth(name)) + "  " + desc
	}
	return windowLines(lines, w.SkillCursor, wizardBodyLines-1)
}

// wizInputBoxLines renders the task input box as a slice of lines (so it
// composes cleanly into the fixed-line body).
func wizInputBoxLines(value string, m Model) []string {
	content := styleSlug.Render(value) + cursorBlock(m)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWorking).
		Padding(0, 2).
		Width(wizardInputCols).
		Render(content)
	out := strings.Split(box, "\n")
	for i := range out {
		out[i] = "  " + out[i]
	}
	return out
}
