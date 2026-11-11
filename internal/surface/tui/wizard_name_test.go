package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func nameStep(text string) Model {
	return wizardOn(&WizardState{Step: wizName, Tools: wizTools(), ToolIdx: 1, NameText: text})
}

func TestWizard_NameStepEnterSkips(t *testing.T) {
	m := wizKey(nameStep(""), "enter")
	require.Equal(t, wizDescribe, m.Wizard.Step, "an empty name skips the step")
	require.Empty(t, m.Wizard.NameText)
	req := m.Wizard.request("/w", nil)
	require.Empty(t, req.Name)
}

func TestWizard_NameStepTypesAndKeeps(t *testing.T) {
	m := typeText(nameStep(""), "Auth  fix")
	require.Equal(t, "Auth  fix", m.Wizard.NameText)
	m = wizKey(m, "backspace")
	require.Equal(t, "Auth  fi", m.Wizard.NameText)
	m = wizKey(m, "x", "enter")
	require.Equal(t, wizDescribe, m.Wizard.Step)
	req := m.Wizard.request("/w", nil)
	require.Equal(t, "Auth fix", req.Name, "the daemon-side normalization is applied up front")

	// Back from describe returns to the name step with the text kept.
	m = wizKey(m, "b")
	require.Equal(t, wizName, m.Wizard.Step)
	require.Equal(t, "Auth  fix", m.Wizard.NameText)
}

func TestWizard_NameStepBIsBackOnlyWhenEmpty(t *testing.T) {
	m := wizKey(nameStep(""), "b")
	require.Equal(t, wizProvider, m.Wizard.Step)
	require.Empty(t, m.Wizard.NameText)

	m = typeText(nameStep(""), "ab")
	require.Equal(t, wizName, m.Wizard.Step)
	require.Equal(t, "ab", m.Wizard.NameText, "b is a letter once the field has text")
}

func TestWizard_NameStepEscCancels(t *testing.T) {
	m := wizKey(nameStep("x"), "esc")
	require.Nil(t, m.Wizard)
	require.Equal(t, FocusBoard, m.Focus)
}

func TestWizard_InvalidNameShowsError(t *testing.T) {
	long := strings.Repeat("x", protocol.MaxSessionNameRunes+1)
	m := nameStep(long)
	require.Error(t, m.Wizard.nameError())
	out := renderWizard(m)
	require.Contains(t, out, protocol.ErrSessionNameTooLong.Error())

	m = wizKey(m, "enter")
	require.Equal(t, wizName, m.Wizard.Step, "enter does not leave an invalid name")

	m = wizKey(m, "backspace", "enter")
	require.Equal(t, wizDescribe, m.Wizard.Step, "fixed, it moves on")
	require.NotContains(t, renderWizard(nameStep("ok")), protocol.ErrSessionNameTooLong.Error())
}

func TestWizard_NameStepBoundsInput(t *testing.T) {
	m := nameStep(strings.Repeat("x", wizardInputMaxRunes))
	m = wizKey(m, "y", " ")
	require.Len(t, []rune(m.Wizard.NameText), wizardInputMaxRunes)
}

func TestWizard_NameProvidesSlugWhenNoTask(t *testing.T) {
	w := &WizardState{Tools: wizTools(), NameText: "Fix the Auth"}
	require.Equal(t, "cc.opus.fix-the-auth", w.request("/w", nil).Slug)
	w.TaskText = "refactor db"
	req := w.request("/w", nil)
	require.Equal(t, "cc.opus.refactor-db", req.Slug, "a task wins over the name")
	require.Equal(t, "Fix the Auth", req.Name)
}

// An older daemon drops the name silently; the SessionAdded summary shows it.
func TestWizard_OldDaemonIgnoringNameIsReported(t *testing.T) {
	added := func(sum protocol.SessionSummary) protocol.Envelope {
		data, err := json.Marshal(sum)
		require.NoError(t, err)
		return protocol.Envelope{Kind: protocol.KindEvent, Type: protocol.EventSessionAdded, Data: data}
	}
	m := boardModel()
	m.PendingSelectSlug = "cc.opus.x"
	m.PendingSpawn = pendingSpawn{Name: "Auth"}
	m = m.applyEvent(added(protocol.SessionSummary{ID: "1", Slug: "cc.opus.x"}))
	require.Equal(t, "this rex-daemon ignored the name; run `rex daemon restart`", m.Err)
	require.Equal(t, pendingSpawn{}, m.PendingSpawn)

	m = boardModel()
	m.PendingSelectSlug = "cc.opus.x"
	m.PendingSpawn = pendingSpawn{Name: "Auth"}
	m = m.applyEvent(added(protocol.SessionSummary{ID: "1", Slug: "cc.opus.x", Name: "Auth"}))
	require.Empty(t, m.Err, "a daemon that kept the name is fine")

	// A rejected spawn drops the expectation with the pending selection.
	m = boardModel()
	m.PendingSelectSlug = "cc.opus.x"
	m.PendingSpawn = pendingSpawn{Name: "Auth"}
	data, err := json.Marshal(protocol.ErrorEvent{Code: protocol.ErrCodeBadIntent, Message: "nope"})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Kind: protocol.KindEvent, Type: protocol.EventError, Data: data})
	require.Equal(t, pendingSpawn{}, m.PendingSpawn)

	require.Equal(t, "this rex-daemon ignored the name and skills; run `rex daemon restart`",
		pendingSpawn{Name: "n", Skills: []string{"a"}}.ignoredBy(protocol.SessionSummary{}))
	require.Equal(t, "this rex-daemon ignored skills; run `rex daemon restart`",
		pendingSpawn{Skills: []string{"a"}}.ignoredBy(protocol.SessionSummary{}))
}

func TestWizard_NameStepRenders(t *testing.T) {
	out := renderWizard(nameStep("hello"))
	require.Contains(t, out, "name it? (optional)")
	require.Contains(t, out, "hello")
	require.Contains(t, wizardFooter(wizName), "empty skips")
}
