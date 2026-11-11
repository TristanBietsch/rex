package overview

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var t0 = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

func sess(id string, st protocol.State, mod func(*protocol.SessionSummary)) protocol.SessionSummary {
	s := protocol.SessionSummary{ID: id, Slug: id, State: st, LastEventAt: t0}
	if mod != nil {
		mod(&s)
	}
	return s
}

func TestClassify(t *testing.T) {
	marked := func(s *protocol.SessionSummary) { s.RetireAt = t0.Add(time.Hour) }
	review := func(s *protocol.SessionSummary) { s.WaitReason = protocol.WaitReview; s.LastLine = "Done. Tests pass." }
	reviewQ := func(s *protocol.SessionSummary) { s.WaitReason = protocol.WaitReview; s.LastLine = "Ship it?" }
	reviewQSpaced := func(s *protocol.SessionSummary) { s.WaitReason = protocol.WaitReview; s.LastLine = "Ship it? \x1b[0m " }
	input := func(s *protocol.SessionSummary) { s.WaitReason = protocol.WaitInput; s.LastLine = "Done." }

	tests := []struct {
		name string
		s    protocol.SessionSummary
		want Bucket
	}{
		{"marked working", sess("a", protocol.StateWorking, marked), Completed},
		{"marked failed", sess("a", protocol.StateFailed, marked), Completed},
		{"marked needs_input", sess("a", protocol.StateNeedsInput, marked), Completed},
		{"failed", sess("a", protocol.StateFailed, nil), Failed},
		{"crashed", sess("a", protocol.StateCrashed, nil), Failed},
		{"done", sess("a", protocol.StateDone, nil), Review},
		{"needs_input unknown reason", sess("a", protocol.StateNeedsInput, nil), NeedsYou},
		{"needs_input review", sess("a", protocol.StateNeedsInput, review), Review},
		{"needs_input review but question", sess("a", protocol.StateNeedsInput, reviewQ), NeedsYou},
		{"question with escapes and padding", sess("a", protocol.StateNeedsInput, reviewQSpaced), NeedsYou},
		{"needs_input input reason", sess("a", protocol.StateNeedsInput, input), NeedsYou},
		{"working", sess("a", protocol.StateWorking, nil), Working},
		{"queued", sess("a", protocol.StateQueued, nil), Working},
		{"working with stale review reason", sess("a", protocol.StateWorking, review), Working},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Classify(tc.s))
		})
	}
}

func TestBucket_Names(t *testing.T) {
	want := map[Bucket][2]string{
		NeedsYou:  {"needs you", "needs_you"},
		Review:    {"review", "review"},
		Failed:    {"failed", "failed"},
		Working:   {"working", "working"},
		Completed: {"completed", "completed"},
		Bucket(9): {"unknown", "unknown"},
	}
	for b, names := range want {
		require.Equal(t, names[0], b.String())
		require.Equal(t, names[1], b.Key())
	}
	require.Equal(t, []Bucket{NeedsYou, Review, Failed, Working, Completed}, Buckets)
}

func TestGroup_OrdersMostRecentFirst(t *testing.T) {
	ss := []protocol.SessionSummary{
		sess("old", protocol.StateWorking, func(s *protocol.SessionSummary) { s.LastEventAt = t0.Add(-time.Hour) }),
		sess("new", protocol.StateWorking, func(s *protocol.SessionSummary) { s.LastEventAt = t0 }),
		sess("tie-b", protocol.StateWorking, func(s *protocol.SessionSummary) { s.LastEventAt = t0.Add(-2 * time.Hour) }),
		sess("tie-a", protocol.StateWorking, func(s *protocol.SessionSummary) { s.LastEventAt = t0.Add(-2 * time.Hour) }),
		// A completed session sorts by the time it was marked.
		sess("mark-early", protocol.StateWorking, func(s *protocol.SessionSummary) {
			s.RetireAt, s.RetiredAt, s.LastEventAt = t0.Add(time.Hour), t0.Add(-3*time.Hour), t0
		}),
		sess("mark-late", protocol.StateWorking, func(s *protocol.SessionSummary) {
			s.RetireAt, s.RetiredAt, s.LastEventAt = t0.Add(time.Hour), t0.Add(-time.Minute), t0.Add(-5*time.Hour)
		}),
		// A legacy mark without RetiredAt falls back to the last event.
		sess("mark-legacy", protocol.StateDone, func(s *protocol.SessionSummary) {
			s.RetireAt, s.LastEventAt = t0.Add(time.Hour), t0.Add(-4*time.Hour)
		}),
	}
	g := Group(ss)
	ids := func(b Bucket) []string {
		var out []string
		for _, s := range g[b] {
			out = append(out, s.ID)
		}
		return out
	}
	require.Equal(t, []string{"new", "old", "tie-a", "tie-b"}, ids(Working))
	require.Equal(t, []string{"mark-late", "mark-early", "mark-legacy"}, ids(Completed))
	require.Empty(t, g[NeedsYou])
}
