package server

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// pipeConn returns a clientConn over one end of a synchronous net.Pipe (a
// write blocks until the peer reads) and the peer end.
func pipeConn(t *testing.T, queueLen int, writeTimeout time.Duration) (*clientConn, net.Conn) {
	t.Helper()
	server, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	return newClientConn(server, 1, queueLen, writeTimeout), peer
}

func TestClientConn_PreservesOrderAndFlushesOnFinish(t *testing.T) {
	c, peer := pipeConn(t, 16, time.Second)
	for i := range 10 {
		c.enqueue(fmt.Appendf(nil, "line %d\n", i))
	}
	finished := make(chan struct{})
	go func() { c.finish(); close(finished) }()

	br := bufio.NewReader(peer)
	for i := range 10 {
		line, err := br.ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("line %d\n", i), line)
	}
	_, err := br.ReadString('\n')
	require.ErrorIs(t, err, io.EOF, "finish must close the socket after the flush")
	<-finished
	require.False(t, c.aborted.Load())
}

func TestClientConn_WriteTimeoutAborts(t *testing.T) {
	c, peer := pipeConn(t, 16, 50*time.Millisecond)
	c.enqueue([]byte("never read\n"))
	require.Eventually(t, c.aborted.Load, 2*time.Second, 5*time.Millisecond)
	c.enqueue([]byte("dropped\n")) // after abort: no panic, no block
	c.finish()
	_, err := peer.Read(make([]byte, 1))
	require.Error(t, err, "the aborted socket is closed")
}

func TestClientConn_OverflowAborts(t *testing.T) {
	c, _ := pipeConn(t, 2, time.Hour)
	for range 4 { // the writer holds one, the queue two, the fourth overflows
		c.enqueue([]byte("x\n"))
	}
	require.Eventually(t, c.aborted.Load, 2*time.Second, 5*time.Millisecond)
	c.finish()
}

func TestClientConn_FinishIsBoundedWhenPeerStalls(t *testing.T) {
	c, _ := pipeConn(t, 4, 100*time.Millisecond)
	c.enqueue([]byte("a\n"))
	c.enqueue([]byte("b\n"))
	start := time.Now()
	c.finish()
	require.Less(t, time.Since(start), 2*time.Second)
	require.True(t, c.aborted.Load())
}

func TestClientConn_EnqueueAfterFinishIsDropped(t *testing.T) {
	c, peer := pipeConn(t, 4, time.Second)
	go func() { _, _ = io.Copy(io.Discard, peer) }()
	c.finish()
	require.NotPanics(t, func() { c.enqueue([]byte("late\n")) })
	require.NotPanics(t, func() { c.sendEvent("Error", "", make(chan int)) }) // unencodable: logged, dropped
}
