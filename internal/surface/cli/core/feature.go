package core

import "github.com/tristanbietsch/rex/internal/catalog/settings"

// RequireFeature returns nil when the Features setting id is on, else the
// exit-5 refusal every switched-off feature gives. An unreadable config uses
// the defaults, as the daemon does.
func RequireFeature(id string) error {
	store, _, _ := settings.LoadDefault()
	if settings.FeatureOn(store, id) {
		return nil
	}
	cmd := "rex config set " + id + " true"
	return Errorf(ExitInvalidArgs, cmd, "off: enable in Settings → Features (%s)", cmd)
}
