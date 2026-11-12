// Package spawnctx turns the new-agent page's task and context items (pastes,
// file and folder references, URLs) into the one InitialPrompt string a new
// session starts with, and holds the sanitizer every pasted or typed text
// goes through first. Assemble and Sanitize are pure. The workspace side
// (paths.go, walk.go) only reads: it checks a path against the path rules,
// lists the folder for the import picker and reads a bounded preview; it
// never writes a file and never touches the network. The daemon delivers
// the prompt exactly like any other first prompt (one argv element, or a
// PTY paste for prompt_paste tools).
package spawnctx

import (
	"errors"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Kind is what a context item carries.
type Kind int

// The context item kinds. A paste carries its text; the others are
// references the agent reads itself.
const (
	Paste Kind = iota
	File
	Dir
	URL
)

// String is the kind's word in the prompt and in ContextRef.Kind.
func (k Kind) String() string {
	switch k {
	case Paste:
		return "paste"
	case File:
		return "file"
	case Dir:
		return "dir"
	case URL:
		return "url"
	}
	return "unknown"
}

// Item is one context item.
type Item struct {
	Kind Kind
	// Name is "paste N", a workspace-relative path with "/" separators, or
	// the URL.
	Name string
	// Text is a paste's body; empty for references.
	Text string
	// Bytes is a paste's length, a file's size on disk, or the total of a
	// dir's listed files. Informational: caps measure Text itself.
	Bytes int
	// Files is how many files a dir lists.
	Files int
}

// Caps bounds Assemble, in UTF-8 bytes after sanitizing. A zero field means
// no limit.
type Caps struct {
	Paste      int // one paste body
	PasteTotal int // all paste bodies together
	Prompt     int // the whole assembled prompt
	Items      int // number of context items
}

// DefaultCaps are the new-agent page's limits. Prompt equals the daemon's
// own check, protocol.MaxInitialPromptBytes.
var DefaultCaps = Caps{Paste: 32 << 10, PasteTotal: 64 << 10, Prompt: protocol.MaxInitialPromptBytes, Items: 32}

// PTYPasteCap is the prompt limit for prompt_paste tools, whose REPLs choke
// on huge pastes.
const PTYPasteCap = 16 << 10

// ErrTooBig is wrapped, with the item name and sizes, when a cap is passed.
var ErrTooBig = errors.New("too big")

// ErrBadItem is wrapped when an item cannot go into the prompt: an unknown
// kind, an empty name, or a name that would break the prompt's structure
// (control or bidi characters, a quote in a paste name).
var ErrBadItem = errors.New("bad context item")

// Summary describes items for NewSession.ContextSummary: kind, name and
// size, never content. Nil for no items.
func Summary(items []Item) []protocol.ContextRef {
	if len(items) == 0 {
		return nil
	}
	out := make([]protocol.ContextRef, len(items))
	for i, it := range items {
		out[i] = protocol.ContextRef{Kind: it.Kind.String(), Name: protocol.FlattenDisplayText(it.Name), Bytes: it.Bytes}
	}
	return out
}
