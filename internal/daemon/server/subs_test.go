package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Canceled output and health subscriptions must leave no entries behind:
// every client connection creates them.
func TestServer_CanceledSubscriptionsAreRemoved(t *testing.T) {
	s := &Server{}
	var got int
	keep := s.SubscribeSessionOutput("sess", func([]byte) { got++ })
	for i := 0; i < 100; i++ {
		s.SubscribeSessionOutput("sess", func([]byte) { t.Error("canceled output sub called") })()
		s.SubscribeSessionOutput("other", func([]byte) {})()
		s.SubscribeSummarizerHealth(func(protocol.SummarizerHealth) { t.Error("canceled health sub called") })()
	}
	s.broadcastSessionOutput("sess", []byte("x"))
	s.BroadcastSummarizerHealth(true, "")
	require.Equal(t, 1, got)
	require.Len(t, s.outputSubs, 1)
	require.Len(t, s.outputSubs["sess"], 1)
	require.Empty(t, s.summarizerHealthSubs)

	keep()
	require.Empty(t, s.outputSubs)
}
