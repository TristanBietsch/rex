package adapter

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const statesReply = "\x1b]7501;?:states=idle,working,done,blocked,error\x1b\\"

func TestOSCStatus_AnswersQuery(t *testing.T) {
	for name, term := range map[string]string{"BEL": "\x07", "ST": "\x1b\\"} {
		t.Run(name, func(t *testing.T) {
			o := newAgyOSC(t)
			o.Observe([]byte("hello\x1b]7501;?" + term + "world"))
			require.Equal(t, statesReply, string(o.TakeReply()))
			require.Nil(t, o.TakeReply(), "one answer per query")
			require.False(t, o.seen, "a query is not a state")
		})
	}
}

func TestOSCStatus_QuerySplitAnsweredOnce(t *testing.T) {
	q := "x\x1b]7501;?\x1b\\y"
	for i := 1; i < len(q); i++ {
		o := newAgyOSC(t)
		o.Observe([]byte(q[:i]))
		got := string(o.TakeReply())
		o.Observe([]byte(q[i:]))
		got += string(o.TakeReply())
		require.Equal(t, statesReply, got, "split at %d", i)
	}
}

// Several queries in one chunk get one answer, and a flood gets a burst of at
// most three before the bucket (about one a second) runs dry.
func TestOSCStatus_ReplyIsBounded(t *testing.T) {
	o := newAgyOSC(t)
	now := time.Unix(1000, 0)
	o.now = func() time.Time { return now }

	o.Observe([]byte(strings.Repeat("\x1b]7501;?\x07", 400)))
	require.Equal(t, statesReply, string(o.TakeReply()), "400 queries in one chunk: one answer")
	require.Nil(t, o.TakeReply())

	replies := 1
	for range 400 {
		o.Observe([]byte("\x1b]7501;?\x07"))
		if r := o.TakeReply(); r != nil {
			require.Equal(t, statesReply, string(r))
			replies++
		}
	}
	require.Equal(t, 3, replies, "burst of 3, then nothing while the clock stands still")

	now = now.Add(1100 * time.Millisecond)
	o.Observe([]byte("\x1b]7501;?\x07"))
	require.Equal(t, statesReply, string(o.TakeReply()), "a token came back after a second")
	o.Observe([]byte("\x1b]7501;?\x07"))
	require.Nil(t, o.TakeReply())
}

// The reply we send must not be mistaken for a query when it is echoed back.
func TestOSCStatus_EchoedReplyIsNotAQuery(t *testing.T) {
	o := newAgyOSC(t)
	o.Observe([]byte(statesReply))
	require.Nil(t, o.TakeReply())
}

func TestOSCStatus_DoneAndBlockedKind(t *testing.T) {
	o := newAgyOSC(t)
	o.Observe(osc("state=working"))
	require.Equal(t, "", o.WaitReason())

	o.Observe(osc("state=done:app=qwen"))
	require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0))
	require.Equal(t, protocol.WaitReview, o.WaitReason())
	// idle after done keeps the review reason: the turn is still unread.
	o.Observe(osc("state=idle"))
	require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0))
	require.Equal(t, protocol.WaitReview, o.WaitReason())

	for _, kind := range []string{"permission", "question", "auth"} {
		o.Observe(osc("state=blocked:kind=" + kind + ":msg=" + base64.StdEncoding.EncodeToString([]byte("Tool confirmation required"))))
		require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0), kind)
		require.Equal(t, protocol.WaitInput, o.WaitReason(), kind)
		require.Equal(t, kind, o.BlockedKind())
	}
	o.Observe(osc("state=blocked:kind=nonsense"))
	require.Equal(t, "", o.BlockedKind(), "unknown kind is dropped")
	require.Equal(t, protocol.WaitInput, o.WaitReason())

	o.Observe(osc("state=error:msg=" + base64.StdEncoding.EncodeToString([]byte("boom"))))
	require.Equal(t, protocol.WaitInput, o.WaitReason())
	require.Equal(t, "boom", o.Message())

	o.Observe(osc("state=working"))
	require.Equal(t, protocol.StateWorking, o.Detect(nil, 0))
	require.Equal(t, "", o.WaitReason())
}

func TestOSCStatus_ClearChangesNothing(t *testing.T) {
	o := newAgyOSC(t)
	o.Observe(osc("state=working"))
	o.Observe(osc("state=clear"))
	require.Equal(t, protocol.StateWorking, o.Detect(nil, 0))

	o = newAgyOSC(t)
	o.Observe(osc("state=clear"))
	require.False(t, o.seen)
}
