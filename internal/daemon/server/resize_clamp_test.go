package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A client-chosen Resize sizes the daemon's screen mirror (one cell per
// column x row): 65535x65535 would allocate billions of cells, so the daemon
// clamps before the session's resize callback runs.
func TestResizeClamped(t *testing.T) {
	e := startAckServer(t)
	var mu sync.Mutex
	var got [][2]uint16
	e.srv.RegisterResize("s1", func(c, r uint16) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, [2]uint16{c, r})
		return nil
	})
	conn := dialTest(t, e.sock)
	w, r := helloOnConn(t, conn)

	for _, sz := range [][2]uint16{{65535, 65535}, {1, 1}, {177, 44}} {
		code, msg := call(t, conn, w, r, protocol.IntentResize, protocol.Resize{SessionID: "s1", Cols: sz[0], Rows: sz[1]})
		require.Empty(t, code, msg)
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, [][2]uint16{{1000, 500}, {20, 5}, {177, 44}}, got)
}

func TestNormalizeNewSessionClampsSize(t *testing.T) {
	p, err := normalizeNewSession(protocol.NewSession{Cols: 65535, Rows: 65535})
	require.NoError(t, err)
	require.Equal(t, [2]uint16{1000, 500}, [2]uint16{p.Cols, p.Rows})
	p, err = normalizeNewSession(protocol.NewSession{})
	require.NoError(t, err)
	require.Equal(t, [2]uint16{0, 0}, [2]uint16{p.Cols, p.Rows}, "0 stays the default")
}
