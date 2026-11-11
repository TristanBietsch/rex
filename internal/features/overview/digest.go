package overview

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Digest limits.
const (
	maxDigestLines     = 40
	maxDigestBytes     = 4096
	maxCompletedLines  = 5
	maxNameRunes       = 40
	maxTaskRunes       = 80
	maxStatusRunes     = 120
	moreLineReserve    = len("- …and 999 more") + 1
	sessionsCloseSafer = "< /sessions >"
)

// closeTag matches the digest's closing delimiter however it is spelled.
var closeTag = regexp.MustCompile(`(?i)<\s*/\s*sessions\s*>`)

// CompletedWindow is how far back overviews list completed sessions.
const CompletedWindow = 24 * time.Hour

// Listed reports whether an overview lists s, given its bucket: everything
// but a completed session older than CompletedWindow.
func Listed(b Bucket, s protocol.SessionSummary, now time.Time) bool {
	return b != Completed || now.Sub(At(s)) <= CompletedWindow
}

// At is the time a session is ordered by within its bucket, and the age an
// overview shows: when the user marked it done, else its last activity.
func At(s protocol.SessionSummary) time.Time {
	if !s.RetireAt.IsZero() && !s.RetiredAt.IsZero() {
		return s.RetiredAt
	}
	return s.LastEventAt
}

// Digest is the text the model turns into a headline: one line per session in
// bucket order, for example
//
//   - [review] billing-tests · fleet: pay · task: fix rounding · status: 48 tests pass · 3m ago
//
// Every field is untrusted (task titles, model descriptions, raw question
// lines). Each is flattened to one line and cut, and a literal closing
// delimiter is defused, so no field can start a new line or close the block
// the prompt wraps the digest in. At most the 5 most recent completed
// sessions, 40 lines and 4096 bytes are kept; a cut ends with `- …and N more`.
func Digest(ss []protocol.SessionSummary, now time.Time) string {
	groups := Group(ss)
	var lines []string
	for _, b := range Buckets {
		list := groups[b]
		if b == Completed && len(list) > maxCompletedLines {
			list = list[:maxCompletedLines]
		}
		for _, s := range list {
			lines = append(lines, digestLine(b, s, now))
		}
	}
	if fits(lines) {
		return strings.Join(lines, "\n")
	}
	var kept []string
	size := 0
	for _, l := range lines {
		if len(kept) >= maxDigestLines-1 || size+len(l)+1+moreLineReserve > maxDigestBytes {
			break
		}
		kept = append(kept, l)
		size += len(l) + 1
	}
	kept = append(kept, fmt.Sprintf("- …and %d more", len(lines)-len(kept)))
	return strings.Join(kept, "\n")
}

func fits(lines []string) bool {
	if len(lines) > maxDigestLines {
		return false
	}
	size := 0
	for _, l := range lines {
		size += len(l) + 1
	}
	return size-1 <= maxDigestBytes
}

func digestLine(b Bucket, s protocol.SessionSummary, now time.Time) string {
	name := s.Name
	if name == "" {
		name = s.Slug
	}
	parts := []string{"- [" + b.String() + "] " + field(name, maxNameRunes)}
	if f := field(s.Fleet, maxNameRunes); f != "" {
		parts = append(parts, "fleet: "+f)
	}
	if t := field(s.Title, maxTaskRunes); t != "" {
		parts = append(parts, "task: "+t)
	}
	if st := field(statusText(s), maxStatusRunes); st != "" {
		parts = append(parts, "status: "+st)
	}
	parts = append(parts, ago(now, At(s)))
	return strings.Join(parts, " · ")
}

// statusText is the session's latest status: the question it asks while it
// waits on an answer, else its description.
func statusText(s protocol.SessionSummary) string {
	if s.State == protocol.StateNeedsInput && IsQuestion(s.LastLine) {
		return s.LastLine
	}
	return s.Description
}

// field makes one untrusted value safe for the digest: one line, at most
// maxRunes runes, and no closing delimiter.
func field(s string, maxRunes int) string {
	s = strings.TrimSpace(protocol.FlattenDisplayText(s))
	if r := []rune(s); len(r) > maxRunes {
		s = strings.TrimSpace(string(r[:maxRunes-1])) + "…"
	}
	return closeTag.ReplaceAllString(s, sessionsCloseSafer)
}

// ago renders now-t as "3m ago"; "just now" under a minute or when t is unset.
func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case t.IsZero(), d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// Prints are two fingerprints of a board. Structural changes when a session
// appears, goes, changes bucket or state, is renamed or retitled, or changes
// its wait reason. Content also changes with descriptions. Tokens, last lines
// and times change neither.
type Prints struct{ Structural, Content uint64 }

// Fingerprint computes both fingerprints of ss.
func Fingerprint(ss []protocol.SessionSummary) Prints {
	sorted := append([]protocol.SessionSummary(nil), ss...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	st, ct := fnv.New64a(), fnv.New64a()
	for _, s := range sorted {
		structural := strings.Join([]string{
			s.ID, Classify(s).String(), string(s.State), fmt.Sprint(!s.RetireAt.IsZero()),
			s.Name, s.Slug, s.Title, s.WaitReason,
		}, "\x1f") + "\x1e"
		_, _ = st.Write([]byte(structural))
		_, _ = ct.Write([]byte(structural + s.Description + "\x1e"))
	}
	return Prints{Structural: st.Sum64(), Content: ct.Sum64()}
}

// Relevance is how much a store change matters to the overview.
type Relevance int

const (
	// Ignored changes (tokens, last line, subagents, …) never matter.
	Ignored Relevance = iota
	// ContentChange is a new description: refresh at most once per Refresh.
	ContentChange
	// StructuralChange is a session added or removed, or a change in state,
	// mark, name, title or wait reason: refresh after the debounce.
	StructuralChange
)

// structuralKeys are the patch keys that change a session's bucket or how the
// digest names it.
var structuralKeys = []string{
	"state", "retire_at", "retired_at", "name", "slug", "title", "wait_reason", "ended_at",
}

// PatchRelevance rates one SessionUpdated patch.
func PatchRelevance(patch map[string]any) Relevance {
	for _, k := range structuralKeys {
		if _, ok := patch[k]; ok {
			return StructuralChange
		}
	}
	if _, ok := patch["description"]; ok {
		return ContentChange
	}
	return Ignored
}
