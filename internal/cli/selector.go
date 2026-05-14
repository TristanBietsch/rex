package cli

import (
	"fmt"
	"regexp"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

var hexShortID = regexp.MustCompile(`^[0-9a-f]{4,}$`)
var fullUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ResolveSelector finds a session by id, slug, or short_id from the daemon's snapshot.
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
		return protocol.SessionSummary{}, NewExitError(ExitAmbiguousSelector,
			fmt.Sprintf("selector %q matches %d sessions; be more specific", sel, len(matches)))
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
	for _, s := range sessions {
		if s.Slug == sel {
			return []protocol.SessionSummary{s}
		}
	}
	var out []protocol.SessionSummary
	if hexShortID.MatchString(sel) {
		for _, s := range sessions {
			if s.ShortID == sel || (len(s.ID) >= len(sel) && s.ID[:len(sel)] == sel) {
				out = append(out, s)
			}
		}
	}
	return out
}
