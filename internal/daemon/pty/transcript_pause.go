package pty

import (
	"log/slog"
	"time"
)

// transcriptRetryEvery is how long a transcript that failed to write (full
// disk, quota) stays paused before the next write tries again.
const transcriptRetryEvery = 30 * time.Second

// pausingTranscript keeps a session alive when its transcript can't be
// written. A failed write pauses the transcript for transcriptRetryEvery:
// output in that time is dropped from transcript.log (the agent, its
// screen and attach clients still get all of it), then writing resumes and
// the daemon log records the gap. Killing the agent instead, as before,
// lost its work because the disk filled up.
//
// Only the reader goroutine calls Write, so it needs no lock.
type pausingTranscript struct {
	w   transcriptWriter
	id  string
	now func() time.Time

	pausedAt    time.Time // zero while writing
	retryAt     time.Time
	dropped     int64
	failedTries int
}

func newPausingTranscript(w transcriptWriter, id string, now func() time.Time) *pausingTranscript {
	if now == nil {
		now = time.Now
	}
	return &pausingTranscript{w: w, id: id, now: now}
}

// Write appends b, or drops it while paused.
func (p *pausingTranscript) Write(b []byte) {
	now := p.now()
	if p.paused() && now.Before(p.retryAt) {
		p.dropped += int64(len(b))
		return
	}
	if err := p.w.Write(b); err != nil {
		p.dropped += int64(len(b)) // a partial write is counted whole
		p.retryAt = now.Add(transcriptRetryEvery)
		if !p.paused() {
			p.pausedAt = now
			slog.Warn("pty: transcript write failed; transcript paused, session keeps running",
				"session", p.id, "err", err, "retry_in", transcriptRetryEvery)
		} else {
			p.failedTries++
			slog.Debug("pty: transcript still failing", "session", p.id, "err", err, "tries", p.failedTries)
		}
		return
	}
	if p.paused() {
		slog.Warn("pty: transcript resumed; output missing from transcript.log",
			"session", p.id, "dropped_bytes", p.dropped, "paused_for", now.Sub(p.pausedAt).Round(time.Second))
		p.pausedAt, p.retryAt, p.dropped, p.failedTries = time.Time{}, time.Time{}, 0, 0
	}
}

func (p *pausingTranscript) paused() bool { return !p.pausedAt.IsZero() }
