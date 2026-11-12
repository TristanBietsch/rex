package settings

import (
	"path/filepath"
	"slices"
	"strings"
)

// TrustSettingIDs are the settings that grant trust: they decide whether new
// sessions run without the agent's permission and folder-trust prompts. The
// daemon fails them closed when config.yaml does not parse, and rex tells the
// daemon at once when it changes one (so revoking takes effect immediately
// instead of at the next poll).
var TrustSettingIDs = []string{"skip_permissions", "auto_trust_dirs"}

// IsTrustSetting reports whether id is one of TrustSettingIDs.
func IsTrustSetting(id string) bool { return slices.Contains(TrustSettingIDs, id) }

// WorkspaceDenyIDs are the settings a workspace's .rex/config.lua can never
// set, even in a workspace the user trusted (rex ws trust): the trust
// settings, and the ones that would let a cloned repo pick a more
// permissive agent, quiet or widen the audit trail, or switch the features
// that gate .rex/ and config.lua themselves. A denied rex.set is a warning
// and leaves the value alone (it does not fail closed).
var WorkspaceDenyIDs = append(slices.Clone(TrustSettingIDs),
	"default_spawn_tool",
	"extra_agents",
	"audit_trail",
	"audit_full_prompt",
	"workspaces",
	"lua_config",
)

// WorkspaceDenied reports whether id is one of WorkspaceDenyIDs.
func WorkspaceDenied(id string) bool { return slices.Contains(WorkspaceDenyIDs, id) }

// WorkspaceSetAt reports whether at, a config.lua position from
// Store.SetAt, is in a workspace's .rex/config.lua rather than the user's
// own config.lua or its modules.
func WorkspaceSetAt(at string) bool {
	sep := string(filepath.Separator)
	return strings.Contains(at, sep+".rex"+sep+"config.lua:")
}
