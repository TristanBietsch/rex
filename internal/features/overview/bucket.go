// Package overview decides which sessions need the user, which are ready for
// review and which finished, and builds the digest the summarizer turns into
// one headline. The buckets come from rules over session fields, never from
// model text. The package is pure: it imports only the wire types.
package overview

import (
	"sort"
	"strings"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Bucket is where a session lands in the overview.
type Bucket int

// Buckets in display order: what the user should look at first comes first.
const (
	// NeedsYou is a session that waits for an answer or an approval.
	NeedsYou Bucket = iota
	// Review is a session that finished and that the user has not marked.
	Review
	// Failed is a session that failed or crashed.
	Failed
	// Working is a session that is running or about to start.
	Working
	// Completed is a session the user marked done.
	Completed
)

// Buckets lists every bucket in display order.
var Buckets = []Bucket{NeedsYou, Review, Failed, Working, Completed}

// String is the bucket's label in prose and in the digest.
func (b Bucket) String() string {
	switch b {
	case NeedsYou:
		return "needs you"
	case Review:
		return "review"
	case Failed:
		return "failed"
	case Working:
		return "working"
	case Completed:
		return "completed"
	}
	return "unknown"
}

// Key is the bucket's stable machine name (`rex overview -o json`).
func (b Bucket) Key() string {
	return strings.ReplaceAll(b.String(), " ", "_")
}

// IsQuestion reports whether a session's last line is a question. It is the
// rule the board's row text uses, so the two agree on what a question is.
func IsQuestion(lastLine string) bool {
	return strings.HasSuffix(strings.TrimSpace(protocol.FlattenDisplayText(lastLine)), "?")
}

// Classify puts one session in a bucket. Rules, in order:
//
//  1. Marked done (RetireAt set): Completed. The user signed off.
//  2. failed or crashed: Failed.
//  3. done: Review. The agent says it finished and the user has not marked it.
//  4. needs_input: Review when the hooks say the agent finished a turn
//     (wait_reason "review") and its last line is not a question, else NeedsYou.
//  5. working or queued: Working.
func Classify(s protocol.SessionSummary) Bucket {
	switch {
	case !s.RetireAt.IsZero():
		return Completed
	case s.State == protocol.StateFailed, s.State == protocol.StateCrashed:
		return Failed
	case s.State == protocol.StateDone:
		return Review
	case s.State == protocol.StateNeedsInput:
		if s.WaitReason == protocol.WaitReview && !IsQuestion(s.LastLine) {
			return Review
		}
		return NeedsYou
	}
	return Working
}

// Group buckets sessions. Each bucket's list is ordered most recent first
// (a completed session by the time it was marked), then by id.
func Group(ss []protocol.SessionSummary) map[Bucket][]protocol.SessionSummary {
	out := make(map[Bucket][]protocol.SessionSummary, len(Buckets))
	for _, s := range ss {
		b := Classify(s)
		out[b] = append(out[b], s)
	}
	for _, list := range out {
		sort.SliceStable(list, func(i, j int) bool {
			ti, tj := recency(list[i]), recency(list[j])
			if !ti.Equal(tj) {
				return ti.After(tj)
			}
			return list[i].ID < list[j].ID
		})
	}
	return out
}
