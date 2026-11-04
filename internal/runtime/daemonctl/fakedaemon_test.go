package daemonctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Environment for the test binary when Start spawns it as a fake
// rex-daemon (see TestMain).
const (
	fakeModeEnv     = "REX_DAEMONCTL_FAKE_MODE" // "serve" or "exit"
	fakeSafetyLimit = 20 * time.Second          // a fake never outlives this
)

// runFakeDaemonProcess is the fake rex-daemon. It prints its argv to stderr
// (Start's log), then: "serve" listens on -socket until a Shutdown intent
// arrives; "exit" fails at startup like a daemon with a broken config. It
// exits on its own after fakeSafetyLimit so a failed test can never leave it
// running.
func runFakeDaemonProcess(mode string, args []string) int {
	time.AfterFunc(fakeSafetyLimit, func() { os.Exit(2) })
	fmt.Fprintf(os.Stderr, "fake-daemon: args %q\n", args)
	switch mode {
	case "exit":
		fmt.Fprintln(os.Stderr, "fake-daemon: loading config")
		fmt.Fprintln(os.Stderr, "fake-daemon: config broken")
		return 3
	case "serve":
		socket := ""
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-socket" {
				socket = args[i+1]
			}
		}
		if socket == "" {
			fmt.Fprintln(os.Stderr, "fake-daemon: no -socket")
			return 4
		}
		ln, err := net.Listen("unix", socket)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fake-daemon:", err)
			return 5
		}
		shutdown := make(chan struct{})
		var once sync.Once
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func() {
					defer conn.Close()
					r := protocol.NewReader(conn)
					for {
						env, err := r.Read()
						if err != nil {
							return
						}
						if env.Type == protocol.IntentShutdown {
							once.Do(func() { close(shutdown) })
						}
					}
				}()
			}
		}()
		<-shutdown
		_ = ln.Close() // unlinks the socket, last, like the real daemon
		return 0
	default:
		return 6
	}
}

// shortTempDir returns a temp dir short enough for a unix socket path
// (macOS's default $TMPDIR alone eats most of the 104-byte sun_path).
func shortTempDir(t *testing.T) string {
	t.Helper()
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = ""
	}
	dir, err := os.MkdirTemp(base, "rexdc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// fakeDaemon is an in-process stand-in for rex-daemon's socket: it records
// the intents it receives and handles Shutdown with onShutdown.
type fakeDaemon struct {
	socket  string
	ln      *net.UnixListener
	intents chan string
	wg      sync.WaitGroup
}

// shutdownLikeDaemon stops answering at once and removes the socket a
// little later, the order rex-daemon's Serve uses.
func shutdownLikeDaemon(delay time.Duration) func(*fakeDaemon) {
	return func(fd *fakeDaemon) {
		fd.ln.SetUnlinkOnClose(false)
		_ = fd.ln.Close()
		time.Sleep(delay)
		_ = os.Remove(fd.socket)
	}
}

func startFakeDaemon(t *testing.T, socket string, onShutdown func(*fakeDaemon)) *fakeDaemon {
	t.Helper()
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	fd := &fakeDaemon{socket: socket, ln: ln.(*net.UnixListener), intents: make(chan string, 16)}
	fd.wg.Add(1)
	go func() {
		defer fd.wg.Done()
		for {
			conn, err := fd.ln.Accept()
			if err != nil {
				return
			}
			fd.wg.Add(1)
			go func() {
				defer fd.wg.Done()
				fd.serve(conn, onShutdown)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = fd.ln.Close()
		fd.wg.Wait()
	})
	return fd
}

func (fd *fakeDaemon) serve(conn net.Conn, onShutdown func(*fakeDaemon)) {
	defer conn.Close()
	r := protocol.NewReader(conn)
	for {
		env, err := r.Read()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fd.intents <- "read error: " + err.Error()
			}
			return
		}
		fd.intents <- string(env.Kind) + ":" + env.Type
		if env.Type == protocol.IntentShutdown && onShutdown != nil {
			onShutdown(fd)
		}
	}
}

func (fd *fakeDaemon) requireIntent(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-fd.intents:
		require.Equal(t, want, got)
	case <-time.After(2 * time.Second):
		t.Fatalf("fake daemon never received %s", want)
	}
}

func socketIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(shortTempDir(t), "rex.sock")
}

func bg() context.Context { return context.Background() }
