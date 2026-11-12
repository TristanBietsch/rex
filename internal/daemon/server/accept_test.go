package server

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// flakyListener fails Accept with errs in order, then hands out conn, then
// reports itself closed.
type flakyListener struct {
	mu    sync.Mutex
	errs  []error
	conn  net.Conn
	calls int
}

func (l *flakyListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if len(l.errs) > 0 {
		err := l.errs[0]
		l.errs = l.errs[1:]
		return nil, err
	}
	if l.conn != nil {
		c := l.conn
		l.conn = nil
		return c, nil
	}
	return nil, net.ErrClosed
}

func (l *flakyListener) Close() error   { return nil }
func (l *flakyListener) Addr() net.Addr { return &net.UnixAddr{Name: "test", Net: "unix"} }

// Too many open files must not end Serve (and with it every session).
func TestAcceptConnRetriesTransientErrors(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	emfile := errors.New("accept unix: too many open files")
	l := &flakyListener{errs: []error{emfile, emfile, emfile}, conn: a}
	conn, err := acceptConn(context.Background(), l)
	require.NoError(t, err)
	require.Equal(t, a, conn)
	require.Equal(t, 4, l.calls)
}

func TestAcceptConnStopsOnClosedListener(t *testing.T) {
	l := &flakyListener{}
	_, err := acceptConn(context.Background(), l)
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestAcceptConnStopsWhenCanceledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errs := make([]error, 1000)
	for i := range errs {
		errs[i] = errors.New("transient")
	}
	l := &flakyListener{errs: errs}
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := acceptConn(ctx, l)
	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
}
