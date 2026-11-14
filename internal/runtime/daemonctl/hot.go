package daemonctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ErrHotUnavailable means the daemon cannot hot restart: it predates hot
// restart, the platform has no execve, or it refused (a session still
// starting, the new binary cannot take the handoff). The daemon is still
// running as before; the wrapped error says why.
var ErrHotUnavailable = errors.New("hot restart unavailable")

// HotRestartTimeout bounds a hot restart from the request to the new image
// answering.
const HotRestartTimeout = 30 * time.Second

// DaemonInfo is what a daemon says about itself in its Snapshot.
type DaemonInfo struct {
	PID       int
	Build     string
	StartedAt time.Time
	// Live counts sessions with a running agent (queued, working, needs you).
	Live int
}

// Info dials socket and reads the daemon's identity and live session count.
func Info(ctx context.Context, socket string) (DaemonInfo, error) {
	c, snap, err := dialHello(ctx, socket)
	if err != nil {
		return DaemonInfo{}, err
	}
	_ = c.Close()
	return infoOf(snap), nil
}

func infoOf(s *protocol.Snapshot) DaemonInfo {
	d := DaemonInfo{PID: s.DaemonPID, Build: s.DaemonBuild, StartedAt: s.DaemonStartedAt}
	for _, sum := range s.Sessions {
		switch sum.State {
		case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
			d.Live++
		default:
		}
	}
	return d
}

func dialHello(ctx context.Context, socket string) (*client.Client, *protocol.Snapshot, error) {
	dctx, cancel := context.WithTimeout(ctx, SocketDialTimeout)
	defer cancel()
	c, err := client.DialContext(dctx, socket)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrNotRunning, err)
	}
	hctx, cancelHello := context.WithTimeout(ctx, ReloadTimeout)
	defer cancelHello()
	snap, err := c.HelloContext(hctx, "rex-cli")
	if err != nil {
		_ = c.Close()
		return nil, nil, fmt.Errorf("rex-daemon hello: %w", err)
	}
	return c, snap, nil
}

// HotResult reports a hot restart: the daemon before and after.
type HotResult struct {
	Before, After DaemonInfo
}

// HotRestart asks the daemon on socket to re-exec in place and waits until
// the new image answers. A daemon that refuses (or predates the intent)
// returns an error wrapping ErrHotUnavailable and is still running.
func HotRestart(ctx context.Context, socket string) (HotResult, error) {
	ctx, cancel := context.WithTimeout(ctx, HotRestartTimeout)
	defer cancel()
	c, snap, err := dialHello(ctx, socket)
	if err != nil {
		return HotResult{}, err
	}
	before := infoOf(snap)
	if before.StartedAt.IsZero() {
		_ = c.Close()
		return HotResult{Before: before}, fmt.Errorf("%w: this rex-daemon predates hot restart", ErrHotUnavailable)
	}
	_, err = c.Do(ctx, protocol.IntentRestart, protocol.Restart{Mode: protocol.RestartHot})
	_ = c.Close()
	var ce *client.Error
	if errors.As(err, &ce) {
		return HotResult{Before: before}, fmt.Errorf("%w: %s", ErrHotUnavailable, ce.Message)
	}
	if err == nil {
		return HotResult{Before: before}, fmt.Errorf("%w: rex-daemon acknowledged without restarting", ErrHotUnavailable)
	}
	// The connection closed: the old image is gone. Wait for the new one.
	tick := time.NewTicker(StartPollInterval)
	defer tick.Stop()
	for {
		if c2, snap2, err := dialHello(ctx, socket); err == nil {
			_ = c2.Close()
			after := infoOf(snap2)
			if after.StartedAt.After(before.StartedAt) {
				return HotResult{Before: before, After: after}, nil
			}
		}
		select {
		case <-ctx.Done():
			return HotResult{Before: before}, fmt.Errorf("rex-daemon did not come back after the hot restart: %w (see rex daemon logs)", ctx.Err())
		case <-tick.C:
		}
	}
}

// Boards lists the board connections of the daemon on socket.
func Boards(ctx context.Context, socket string) ([]protocol.BoardInfo, error) {
	return boardsIntent(ctx, socket, protocol.IntentBoards, struct{}{})
}

// SwapBoards asks every board not on build to re-exec into its installed
// binary and returns the boards asked.
func SwapBoards(ctx context.Context, socket, build string) ([]protocol.BoardInfo, error) {
	return boardsIntent(ctx, socket, protocol.IntentSwapBoards, protocol.SwapBoards{Build: build})
}

func boardsIntent(ctx context.Context, socket, intent string, payload any) ([]protocol.BoardInfo, error) {
	c, _, err := dialHello(ctx, socket)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(ctx, ReloadTimeout)
	defer cancel()
	env, err := c.Do(ctx, intent, payload)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", intent, err)
	}
	var b protocol.Boards
	if err := json.Unmarshal(env.Data, &b); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", intent, err)
	}
	return b.Boards, nil
}
