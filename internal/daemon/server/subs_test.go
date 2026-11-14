package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Canceled subscriptions are removed: every connection subscribes, so nil
// slots left behind grew the lists for the daemon's lifetime.
func TestSubscriptions_CancelRemoves(t *testing.T) {
	s := &Server{}
	for range 100 {
		s.SubscribeSessionOutput("a", func([]byte) {})()
		s.SubscribeSummarizerHealth(func(protocol.SummarizerHealth) {})()
		s.SubscribeOverview(func(protocol.Overview) {})()
		s.SubscribeUsage(func(protocol.Usage) {})()
		s.SubscribeTools(func(protocol.ToolsChanged) {})()
		s.SubscribeLoops(func(protocol.LoopSummary) {})()
	}
	require.Empty(t, s.outputSubs, "a session's empty list is dropped")
	require.Zero(t, s.summarizerHealthSubs.Len())
	require.Zero(t, s.overviewSubs.Len())
	require.Zero(t, s.usageSubs.Len())
	require.Zero(t, s.toolsSubs.Len())
	require.Zero(t, s.loopSubs.Len())

	var got []string
	cancel := s.SubscribeSessionOutput("a", func(b []byte) { got = append(got, string(b)) })
	s.broadcastSessionOutput("a", []byte("x"))
	cancel()
	s.broadcastSessionOutput("a", []byte("y"))
	require.Equal(t, []string{"x"}, got)
}
