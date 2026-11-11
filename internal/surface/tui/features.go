package tui

import "github.com/tristanbietsch/rex/internal/catalog/settings"

// featureOn reports whether the Features switch id is on. A model with no
// store yet reads the registry default.
func (m Model) featureOn(id string) bool { return settings.FeatureOn(m.Store, id) }

// offMessage is the refusal a command or key gives while its feature is off.
func offMessage(id string) string {
	return "off: enable in Settings → Features (rex config set " + id + " true)"
}

// refuseOff sets the refusal on m.Err and reports true while feature id is
// off.
func refuseOff(m *Model, id string) bool {
	if m.featureOn(id) {
		return false
	}
	m.Err = offMessage(id)
	return true
}
