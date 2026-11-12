package pty

import (
	"regexp"
	"time"
)

// unansweredDetector notes, once, that an agent's folder-trust dialog is on
// screen although nothing answers it (the folder is not trusted), so the log
// says why the session waits for the user. Like trustAnswerer it looks only
// during trustWindow after spawn.
type unansweredDetector struct {
	shown   *regexp.Regexp
	started time.Time
	done    bool
}

func newUnansweredDetector(shown *regexp.Regexp, now time.Time) *unansweredDetector {
	if shown == nil {
		return nil
	}
	return &unansweredDetector{shown: shown, started: now}
}

// next reports true exactly once: the first time shown matches the screen
// inside the window.
func (d *unansweredDetector) next(screen string, now time.Time) bool {
	if d == nil || d.done {
		return false
	}
	if now.Sub(d.started) > trustWindow {
		d.done = true
		return false
	}
	if !d.shown.MatchString(screen) {
		return false
	}
	d.done = true
	return true
}
