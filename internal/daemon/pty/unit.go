package pty

import (
	"context"
	"errors"
	"time"
)

// WriteUnit is bytes that must reach a session's PTY together and in order,
// through the session's single writer: nothing else (client input, a trust
// answer, a terminal-query reply) is written between its parts.
type WriteUnit struct {
	Parts [][]byte
	// Gap separates the parts (a typed prompt, then its Enter).
	Gap time.Duration
	// Guard (nil: none) is asked after each gap, right before the next part.
	// False drops the rest of the unit.
	Guard func() bool
	// Wait makes the writer func return only after the unit was written (or
	// dropped); otherwise it returns once the unit is queued.
	Wait bool
}

// UnitWriter queues a WriteUnit on the session's writer. sent is true when
// every part was (or, without Wait, will be) written; false when the guard
// dropped the unit. An error means the session's input is closed.
type UnitWriter func(ctx context.Context, u WriteUnit) (sent bool, err error)

var errInputClosed = errors.New("session input is closed")

// unitWriter is the UnitWriter over pw.
func unitWriter(pw *ptyWriter) UnitWriter {
	return func(ctx context.Context, u WriteUnit) (bool, error) {
		wu := writeUnit{parts: u.Parts, gap: u.Gap, guard: u.Guard}
		if u.Wait {
			wu.res = make(chan bool, 1)
		}
		if !pw.Send(ctx, wu) {
			return false, errInputClosed
		}
		if !u.Wait {
			return true, nil
		}
		select {
		case ok := <-wu.res:
			return ok, nil
		case <-ctx.Done():
			return false, ctx.Err()
		case <-pw.done:
			return false, errInputClosed
		case <-pw.dead:
			return false, errInputClosed
		}
	}
}
