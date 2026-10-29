package core

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var hexShortID = regexp.MustCompile(`^[0-9a-f]{4,}$`)
var fullUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// stateAliases are selectors naming every session in a state. Resolving one
// that matches several sessions is ambiguous, like any other selector.
var stateAliases = map[string]protocol.State{
	"@needs":   protocol.StateNeedsInput,
	"@working": protocol.StateWorking,
	"@done":    protocol.StateDone,
}

// ResolveSelector finds a session by id, slug, short_id, or @state alias
// from the daemon's snapshot.
func ResolveSelector(c *client.Client, sel string) (protocol.SessionSummary, error) {
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return protocol.SessionSummary{}, err
	}
	return ResolveInSnapshot(snap.Sessions, sel)
}

// ResolveInSnapshot is the testable core.
func ResolveInSnapshot(sessions []protocol.SessionSummary, sel string) (protocol.SessionSummary, error) {
	if sel == "" {
		return protocol.SessionSummary{}, NewExitError(ExitInvalidArgs, "empty selector")
	}
	matches := matchAll(sessions, sel)
	switch len(matches) {
	case 0:
		return protocol.SessionSummary{}, NewExitError(ExitSelectorNotFound, fmt.Sprintf("no session matches %q", sel))
	case 1:
		return matches[0], nil
	default:
		shorts := make([]string, len(matches))
		for i, m := range matches {
			shorts[i] = m.ShortID
		}
		slog.Debug("cli: ambiguous selector", "selector", sel, "matches", shorts)
		return protocol.SessionSummary{}, NewExitError(ExitAmbiguousSelector,
			fmt.Sprintf("selector %q matches %d sessions (%s); use a short id", sel, len(matches), strings.Join(shorts, ", ")))
	}
}

func matchAll(sessions []protocol.SessionSummary, sel string) []protocol.SessionSummary {
	if fullUUID.MatchString(sel) {
		for _, s := range sessions {
			if s.ID == sel {
				return []protocol.SessionSummary{s}
			}
		}
		return nil
	}
	// Exact slug wins over @state aliases and short-id prefixes. The daemon keeps slugs unique,
	// but sessions restored from an older daemon may share one: return every
	// holder so the caller reports ambiguity instead of picking the first.
	var out []protocol.SessionSummary
	for _, s := range sessions {
		if s.Slug == sel {
			out = append(out, s)
		}
	}
	if len(out) > 0 {
		return out
	}
	if state, ok := stateAliases[sel]; ok {
		for _, s := range sessions {
			if s.State == state {
				out = append(out, s)
			}
		}
		return out
	}
	if hexShortID.MatchString(sel) {
		for _, s := range sessions {
			if s.ShortID == sel || (len(s.ID) >= len(sel) && s.ID[:len(sel)] == sel) {
				out = append(out, s)
			}
		}
	}
	return out
}
