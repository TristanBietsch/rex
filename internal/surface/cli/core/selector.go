package core

import (
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var hexShortID = regexp.MustCompile(`^[0-9a-f]{4,}$`)
var fullUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ResolveSelector finds a session by id, slug, or short_id from the daemon's snapshot.
func ResolveSelector(c *client.Client, sel string) (protocol.SessionSummary, error) {
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return protocol.SessionSummary{}, HelloFailed(err)
	}
	return ResolveInSnapshot(snap.Sessions, sel)
}

// ResolveInSnapshot is the testable core.
func ResolveInSnapshot(sessions []protocol.SessionSummary, sel string) (protocol.SessionSummary, error) {
	if sel == "" {
		return protocol.SessionSummary{}, NewExitError(ExitInvalidArgs, "empty selector")
	}
	m := matchAll(sessions, sel)
	switch {
	case len(m.sessions) == 0:
		return protocol.SessionSummary{}, NewExitError(ExitSelectorNotFound, fmt.Sprintf("no session matches %q", sel))
	case len(m.sessions) == 1:
		return m.sessions[0], nil
	}
	shorts := make([]string, len(m.sessions))
	for i, s := range m.sessions {
		shorts[i] = s.ShortID
	}
	slog.Debug("cli: ambiguous selector", "selector", sel, "matches", shorts)
	if m.collision {
		// A slug that is also another session's short id: say which is which.
		parts := make([]string, len(m.sessions))
		for i, s := range m.sessions {
			parts[i] = s.ShortID + " (" + m.via[i] + ")"
		}
		return protocol.SessionSummary{}, Errorf(ExitAmbiguousSelector, "use the full session id",
			"selector %q matches %d sessions: %s; use the full id", sel, len(m.sessions), strings.Join(parts, ", "))
	}
	return protocol.SessionSummary{}, NewExitError(ExitAmbiguousSelector,
		fmt.Sprintf("selector %q matches %d sessions (%s); use a short id", sel, len(m.sessions), strings.Join(shorts, ", ")))
}

// selMatches is what a selector named. via says how each session matched
// ("slug" or "short id"); collision is set when an exact slug and the
// short-id prefix of a different session both match.
type selMatches struct {
	sessions  []protocol.SessionSummary
	via       []string
	collision bool
}

func matchAll(sessions []protocol.SessionSummary, sel string) selMatches {
	if fullUUID.MatchString(sel) {
		for _, s := range sessions {
			if s.ID == sel {
				return selMatches{sessions: []protocol.SessionSummary{s}}
			}
		}
		return selMatches{}
	}
	// An exact slug names its session. The daemon keeps slugs unique, but
	// sessions restored from an older daemon may share one: return every
	// holder so the caller reports ambiguity instead of picking the first.
	var out selMatches
	for _, s := range sessions {
		if s.Slug == sel {
			out.sessions = append(out.sessions, s)
			out.via = append(out.via, "slug")
		}
	}
	// Short ids and id prefixes are lowercase hex, so match them without
	// regard to case. A slug that is also another session's id prefix (slugs
	// like "feed" or "cafe" are valid hex) is a collision, not a win.
	lower := strings.ToLower(sel)
	if hexShortID.MatchString(lower) {
		bySlug := len(out.sessions) > 0
		for _, s := range sessions {
			if s.ShortID != lower && !strings.HasPrefix(s.ID, lower) {
				continue
			}
			if slices.ContainsFunc(out.sessions, func(o protocol.SessionSummary) bool { return o.ID == s.ID }) {
				continue // the same session by slug and by id
			}
			out.collision = out.collision || bySlug
			out.sessions = append(out.sessions, s)
			out.via = append(out.via, "short id")
		}
	}
	return out
}
