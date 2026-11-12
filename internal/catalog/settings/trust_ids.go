package settings

import "slices"

// TrustSettingIDs are the settings that grant trust: they decide whether new
// sessions run without the agent's permission and folder-trust prompts. The
// daemon fails them closed when config.yaml does not parse, and rex tells the
// daemon at once when it changes one (so revoking takes effect immediately
// instead of at the next poll).
var TrustSettingIDs = []string{"skip_permissions", "auto_trust_dirs"}

// IsTrustSetting reports whether id is one of TrustSettingIDs.
func IsTrustSetting(id string) bool { return slices.Contains(TrustSettingIDs, id) }
