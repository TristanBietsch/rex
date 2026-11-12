package pty

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

// writeUnit is bytes that must reach the child together and in order: the
// parts are written back to back with gap between them (a bracketed paste,
// then its Enter), and nothing else is written in between.
type writeUnit struct {
	parts [][]byte
	gap   time.Duration
	// guard, when set, is called after each gap, right before the next part
	// is written. When it returns false the rest of the unit is dropped (what
	// was written stays written).
	guard func() bool
	// ctx, when set, is the caller's: once it ends (the caller gave up, say
	// after a timeout) the writer writes no further part of the unit, and
	// skips it entirely if it never got to start.
	ctx context.Context
	// res, when set (buffered, size 1), gets true once every part was
	// written, and false when the guard dropped the rest or the writer ended
	// first.
	res chan bool
}

// ptyWriter is the only place that writes to the child's PTY. Input from
// clients, the initial prompt, the trust answerer and terminal-query replies
// all queue here, so a slow or non-reading child blocks this goroutine alone,
// never the output reader or the supervisor loop.
type ptyWriter struct {
	w    io.Writer
	ch   chan writeUnit
	busy atomic.Bool // a unit is being written now
	done <-chan struct{}
	dead chan struct{} // closed when a write failed: the child is gone
	// pauseCh parks the writer for a hot restart (pause).
	pauseCh chan pauseReq
}

// pauseReq asks the writer to finish its queue and park until resume
// closes. ack is closed once it has parked.
type pauseReq struct {
	ack    chan struct{}
	resume <-chan struct{}
}

const writerQueue = 64

// newPTYWriter starts the writer goroutine; it stops when done closes (a
// write already blocked in the kernel ends when the PTY is closed).
func newPTYWriter(w io.Writer, done <-chan struct{}, onErr func(error)) *ptyWriter {
	pw := &ptyWriter{w: w, ch: make(chan writeUnit, writerQueue), done: done, dead: make(chan struct{}), pauseCh: make(chan pauseReq)}
	go func() {
		for {
			select {
			case <-done:
				return
			case u := <-pw.ch:
				if !pw.run(u, onErr) {
					return
				}
			case req := <-pw.pauseCh:
				// Write what is queued, so no unit is left behind, then park.
				for drained := false; !drained; {
					select {
					case u := <-pw.ch:
						if !pw.run(u, onErr) {
							close(req.ack)
							return
						}
					default:
						drained = true
					}
				}
				close(req.ack)
				select {
				case <-req.resume:
				case <-done:
					return
				}
			}
		}
	}()
	return pw
}

// write writes the unit's parts back to back. ok is false when the guard
// dropped the rest of the unit or the writer ended during a gap.
// run writes one unit and reports it; false means the writer must end (a
// write failed: the child is gone).
func (p *ptyWriter) run(u writeUnit, onErr func(error)) bool {
	p.busy.Store(true)
	ok, err := p.write(u)
	p.busy.Store(false)
	if u.res != nil {
		u.res <- ok && err == nil
	}
	if err != nil {
		close(p.dead)
		if onErr != nil {
			onErr(err)
		}
		return false
	}
	return true
}

// pause has the writer write everything queued, then park until resume
// closes. It returns once the writer has parked, or with ctx's error.
func (p *ptyWriter) pause(ctx context.Context, resume <-chan struct{}) error {
	req := pauseReq{ack: make(chan struct{}), resume: resume}
	select {
	case p.pauseCh <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return errInputClosed
	case <-p.dead:
		return errInputClosed
	}
	select {
	case <-req.ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *ptyWriter) write(u writeUnit) (bool, error) {
	var callerGone <-chan struct{}
	if u.ctx != nil {
		callerGone = u.ctx.Done() // nil channel (never ready) when there is no ctx
	}
	gone := func() bool {
		select {
		case <-callerGone:
			return true
		default:
			return false
		}
	}
	for i, part := range u.parts {
		if gone() {
			return false, nil
		}
		if i > 0 && u.gap > 0 {
			select {
			case <-p.done:
				return false, nil
			case <-callerGone:
				return false, nil
			case <-time.After(u.gap):
			}
		}
		if i > 0 && gone() {
			return false, nil
		}
		if i > 0 && u.guard != nil && !u.guard() {
			return false, nil
		}
		if _, err := p.w.Write(part); err != nil {
			return false, err
		}
	}
	return true, nil
}

// Send queues a unit behind earlier ones and returns false if ctx or the
// writer ended first. It blocks only while the queue is full.
func (p *ptyWriter) Send(ctx context.Context, u writeUnit) bool {
	select {
	case p.ch <- u:
		return true
	case <-ctx.Done():
		return false
	case <-p.done:
		return false
	case <-p.dead:
		return false
	}
}

// TrySendReply queues a terminal-query reply only when nothing else is
// pending or being written: a reply never interrupts or waits behind input
// (input might be mid-sequence), and is dropped instead of blocking.
func (p *ptyWriter) TrySendReply(b []byte) bool {
	if p.busy.Load() || len(p.ch) > 0 {
		return false
	}
	select {
	case p.ch <- writeUnit{parts: [][]byte{b}}:
		return true
	default:
		return false
	}
}
