//go:build !windows

package boot

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The loop reloads once per signal and stops with its context. Driven with a
// plain channel: no real signal is sent to the test process.
func TestReloadOnSignal_ReloadsPerSignal(t *testing.T) {
	var n atomic.Int32
	sig := make(chan os.Signal)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		reloadOnSignal(ctx, sig, func() protocol.ReloadReport { n.Add(1); return protocol.ReloadReport{} })
	}()
	sig <- syscall.SIGHUP
	sig <- syscall.SIGHUP
	require.Eventually(t, func() bool { return n.Load() == 2 }, 5*time.Second, 10*time.Millisecond)
	cancel()
	<-done
	require.Equal(t, int32(2), n.Load())
}

// hupChildEnv makes TestReloadOnHUP_RealSignal act as the child process.
const hupChildEnv = "REX_TEST_HUP_CHILD"

// A real SIGHUP reaches ReloadOnHUP. It runs in a child copy of the test
// binary: signals are asynchronous, so one sent here could still be in
// flight when the test ends, and once another test's ReloadOnHUP stops,
// SIGHUP's default action (exit) is back and that late signal would kill
// the whole test binary. The child exits as soon as it has seen a reload,
// before ReloadOnHUP's signal.Stop runs, so a late signal can only end it.
func TestReloadOnHUP_RealSignal(t *testing.T) {
	if os.Getenv(hupChildEnv) == "1" {
		hupChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestReloadOnHUP_RealSignal$", "-test.count=1") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), hupChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "child: %s", out)
	require.Contains(t, string(out), "hup child: reloaded")
}

// hupChild runs ReloadOnHUP, signals itself until a reload happens and exits
// at once; exit 1 when none happens within 5 s.
func hupChild() {
	// Own handler first and never stopped, so a signal sent before
	// ReloadOnHUP has registered cannot end the child.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGHUP)
	var n atomic.Int32
	go ReloadOnHUP(context.Background(), func() protocol.ReloadReport { n.Add(1); return protocol.ReloadReport{} })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
		time.Sleep(20 * time.Millisecond)
		if n.Load() > 0 {
			_, _ = os.Stdout.WriteString("hup child: reloaded\n")
			os.Exit(0)
		}
	}
	_, _ = os.Stdout.WriteString("hup child: no reload\n")
	os.Exit(1)
}
