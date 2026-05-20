package client_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

func TestClient_DialHelloSnapshot(t *testing.T) {
	d := testutil.StartDaemon(t)
	defer d.Close()

	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()

	snap, err := c.Hello("test")
	require.NoError(t, err)
	require.Equal(t, "all", snap.Filter)
	require.Empty(t, snap.Sessions)
}
