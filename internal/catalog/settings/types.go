// Package settings defines the canonical Rex settings registry plus a YAML-backed store.
//
// One Setting struct per spec entry. The TUI page (internal/tui/settings.go), the
// `rex config` CLI (internal/surface/cli/config.go), and any future Lua bridge all read
// the same Registry and Store.
package settings

// Type discriminates the value shape of a Setting.
type Type string

const (
	TypeEnum   Type = "enum"
	TypeBool   Type = "bool"
	TypeInt    Type = "int"
	TypeFloat  Type = "float"
	TypeString Type = "string"
)

// Section groups settings on the page.
type Section string

const (
	SectionAppearance Section = "Appearance"
	SectionAudio      Section = "Audio"
	SectionBehavior   Section = "Behavior"
	SectionFeatures   Section = "Features"
	SectionOnboarding Section = "Onboarding"
	SectionSummary    Section = "AI summary"
	SectionSpawn      Section = "Spawn"
	SectionAdvanced   Section = "Advanced"
)

// Setting is one row in the registry.
type Setting struct {
	ID       string
	Label    string
	Section  Section
	Type     Type
	Default  any
	Options  []string // for TypeEnum
	Min      float64  // for TypeInt / TypeFloat
	Max      float64
	Help     string
	ReadOnly bool
	// Apply says when a change takes effect.
	Apply ApplyMode
	// Hidden keeps the row off the TUI settings page. The setting stays in
	// the registry, the store and `rex config`.
	Hidden bool
	// Free says any value that passes Normalize is accepted: the TUI opens a
	// text editor on enter (Options, if any, are only suggestions for the
	// arrow keys). It is opt-in and needs a Normalize.
	Free bool
	// Normalize, when set, validates and tidies a string value in Store.Set
	// (after type coercion). Load does not call it: a bad stored value stays
	// visible so doctor can report it.
	Normalize func(string) (string, error)
}

// ApplyMode says when a changed setting takes effect.
type ApplyMode string

const (
	// ApplyLive: at once, in the TUI or through the daemon's settings
	// watcher (within seconds).
	ApplyLive ApplyMode = "live"
	// ApplyNextAttach: read when an attach popup opens or resizes.
	ApplyNextAttach ApplyMode = "next_attach"
	// ApplyNextSpawn: read when a session is spawned; running ones keep
	// their value.
	ApplyNextSpawn ApplyMode = "next_spawn"
	// ApplyDaemonRestart: read once when the daemon starts.
	ApplyDaemonRestart ApplyMode = "daemon_restart"
)

// ApplyModes lists every mode, in order of how soon they take effect.
var ApplyModes = []ApplyMode{ApplyLive, ApplyNextAttach, ApplyNextSpawn, ApplyDaemonRestart}

// Label is the short text the settings page shows for the mode ("" for an
// unknown mode).
func (m ApplyMode) Label() string {
	switch m {
	case ApplyLive:
		return "applies now"
	case ApplyNextAttach:
		return "next attach"
	case ApplyNextSpawn:
		return "next new session"
	case ApplyDaemonRestart:
		return "after a daemon restart"
	default:
		return ""
	}
}
