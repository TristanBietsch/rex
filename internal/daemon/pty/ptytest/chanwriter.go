// Package ptytest is a test aid for code that writes input to a session: a
// pty.UnitWriter that delivers each part to a channel, so a test can see the
// bytes, their order and their timing without a real PTY. Production code
// never uses it: a session without a real writer refuses input.
package ptytest

import (
	"context"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/pty"
)

// ChanWriter returns a UnitWriter that sends each part of a unit to ch, with
// the unit's gap between parts and its guard asked before each later part,
// like the supervisor's writer. It blocks while ch is full, up to the
// caller's context.
func ChanWriter(ch chan []byte) pty.UnitWriter {
	return func(ctx context.Context, u pty.WriteUnit) (bool, error) {
		for i, part := range u.Parts {
			if i > 0 {
				select {
				case <-time.After(u.Gap):
				case <-ctx.Done():
					return false, ctx.Err()
				}
				if u.Guard != nil && !u.Guard() {
					return false, nil
				}
			}
			select {
			case ch <- part:
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
		return true, nil
	}
}
