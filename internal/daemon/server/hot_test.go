package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/state"
)

func TestFreezeAllIsAllOrNothing(t *testing.T) {
	srv, err := New(Config{Socket: "/tmp/unused.sock", Store: state.NewStore()})
	require.NoError(t, err)
	thawed := 0
	srv.RegisterFreeze("a", func(context.Context) (pty.Frozen, func(), error) {
		return pty.Frozen{FD: 9, PID: 1}, func() { thawed++ }, nil
	})
	srv.RegisterFreeze("b", func(context.Context) (pty.Frozen, func(), error) {
		return pty.Frozen{}, nil, errors.New("stuck")
	})
	_, _, err = srv.FreezeAll(context.Background())
	require.ErrorContains(t, err, "b")
	require.ErrorContains(t, err, "stuck")
	require.Equal(t, 1, thawed, "the frozen session is thawed again")

	srv.UnregisterFreeze("b")
	got, thaw, err := srv.FreezeAll(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 9, got[0].FD)
	thaw()
	require.Equal(t, 2, thawed)
}

func TestHotRestartWithoutConfigIsRefused(t *testing.T) {
	srv, err := New(Config{Socket: "/tmp/unused.sock", Store: state.NewStore()})
	require.NoError(t, err)
	require.ErrorIs(t, srv.HotRestart(context.Background(), nil), errHotUnsupported)
}
