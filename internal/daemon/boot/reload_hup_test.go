//go:build !windows

package boot

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReloadOnHUP_CallsReload(t *testing.T) {
	// Own SIGHUP handler first, so a signal raised before ReloadOnHUP has
	// registered its own cannot take the test process down.
	// Signals arrive asynchronously, so one raised by the last poll below can
	// land after the test ends. Cleanup (which runs last) therefore ignores
	// SIGHUP instead of restoring its default action, which would kill the
	// test binary in whatever test runs next.
	guard := make(chan os.Signal, 16)
	signal.Notify(guard, syscall.SIGHUP)
	t.Cleanup(func() { signal.Ignore(syscall.SIGHUP) })

	var n atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); ReloadOnHUP(ctx, func() error { n.Add(1); return nil }) }()
	t.Cleanup(func() { cancel(); <-done })

	require.Eventually(t, func() bool {
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
		return n.Load() > 0
	}, 5*time.Second, 20*time.Millisecond)
}
