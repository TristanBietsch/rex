// Package settings defines the canonical Rex settings registry plus a YAML-backed store.
//
// One Setting struct per spec entry. The TUI page (internal/tui/settings.go), the
// `rex config` CLI (internal/cli/config.go), and any future Lua bridge all read
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
	SectionOnboarding Section = "Onboarding"
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
}
