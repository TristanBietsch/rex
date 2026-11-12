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
}

const writerQueue = 64

// newPTYWriter starts the writer goroutine; it stops when done closes (a
// write already blocked in the kernel ends when the PTY is closed).
func newPTYWriter(w io.Writer, done <-chan struct{}, onErr func(error)) *ptyWriter {
	pw := &ptyWriter{w: w, ch: make(chan writeUnit, writerQueue), done: done, dead: make(chan struct{})}
	go func() {
		for {
			select {
			case <-done:
				return
			case u := <-pw.ch:
				pw.busy.Store(true)
				ok, err := pw.write(u)
				pw.busy.Store(false)
				if u.res != nil {
					u.res <- ok && err == nil
				}
				if err != nil {
					close(pw.dead)
					if onErr != nil {
						onErr(err)
					}
					return
				}
			}
		}
	}()
	return pw
}

// write writes the unit's parts back to back. ok is false when the guard
// dropped the rest of the unit or the writer ended during a gap.
func (p *ptyWriter) write(u writeUnit) (ok bool, err error) {
	for i, part := range u.parts {
		if i > 0 && u.gap > 0 {
			select {
			case <-p.done:
				return false, nil
			case <-time.After(u.gap):
			}
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
