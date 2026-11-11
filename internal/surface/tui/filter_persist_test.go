package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

func TestHiddenSectionsSettingDefault(t *testing.T) {
	st := settings.NewStore()
	require.Nil(t, hiddenSectionsFrom(st))

	require.NoError(t, st.Set("board_hidden_sections", "done, bogus"))
	require.Equal(t, map[string]bool{"done": true}, hiddenSectionsFrom(st))

	require.NoError(t, st.Set("board_hidden_sections", "Needs,working"))
	require.Equal(t, map[string]bool{"needs": true, "working": true}, hiddenSectionsFrom(st))

	require.NoError(t, st.Set("board_hidden_sections", "waiting"))
	require.Equal(t, map[string]bool{"waiting": true}, hiddenSectionsFrom(st), "waiting is hideable")

	require.NoError(t, st.Set("board_hidden_sections", "needs,waiting,working,done"))
	require.Nil(t, hiddenSectionsFrom(st), "hiding every section is ignored")
	require.Nil(t, hiddenSectionsFrom(nil))
}

func TestSettingsLoadSeedsHiddenSections(t *testing.T) {
	st := settings.NewStore()
	require.NoError(t, st.Set("board_hidden_sections", "done"))
	next, _ := filterModel().dispatch(settingsResultMsg{Store: st, Found: true})
	require.Equal(t, map[string]bool{"done": true}, next.(Model).Hidden)
}

func TestBGSavesHiddenSections(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	m := setSectionHidden(filterModel(), "done", true)
	m, _ = executeCommand(m, "bg")
	require.True(t, m.Quitting)

	st, ok := LoadTUIState()
	require.True(t, ok)
	require.Equal(t, "w1", st.Selection)
	require.NotNil(t, st.HiddenSections)
	require.Equal(t, []string{"done"}, *st.HiddenSections)

	// A restore overrides the settings default once, even to "none hidden".
	m = filterModel()
	m.Hidden = map[string]bool{"needs": true}
	empty := []string{}
	next, _ := m.dispatch(stateRestoreResultMsg{State: persistedState{Selection: "w1", HiddenSections: &empty}, OK: true})
	require.Empty(t, next.(Model).Hidden)

	// A state file from before this field leaves the default alone.
	next, _ = m.dispatch(stateRestoreResultMsg{State: persistedState{Selection: "w1"}, OK: true})
	require.Equal(t, map[string]bool{"needs": true}, next.(Model).Hidden)
}
