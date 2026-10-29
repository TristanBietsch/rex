package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The default-model field used to edit at a separate cursor that started at
// 0 (so typing landed in front of the prefilled model while the rendered
// cursor sat at the end) and mixed byte offsets with rune counts.
func TestSetupWizardModelField_EditsAtEnd(t *testing.T) {
	m := SetupWizardModel{defaultModel: "opus", defaultsField: defaultsModel}
	for _, k := range []string{"-", "é", " ", "x"} {
		out, _ := m.keyDefaults(key(k))
		m = out.(SetupWizardModel)
	}
	require.Equal(t, "opus-é x", m.defaultModel)

	for range 3 {
		out, _ := m.keyDefaults(key("backspace"))
		m = out.(SetupWizardModel)
	}
	require.Equal(t, "opus-", m.defaultModel, "backspace removes whole runes")
}
