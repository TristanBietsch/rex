package session

import "github.com/tristanbietsch/rex/internal/catalog/settings"

// featureOn reads a Features switch from the user's config. A config that
// will not load reads the registry defaults, so a broken file never breaks a
// command.
func featureOn(id string) bool {
	store, _, err := settings.LoadDefault()
	if err != nil {
		store = nil
	}
	return settings.FeatureOn(store, id)
}
