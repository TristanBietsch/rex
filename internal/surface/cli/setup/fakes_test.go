package setup

import (
	"sync"
	"time"
)

// fakeDaemon stands in for rex-daemon in update and uninstall tests, so no
// test ever signals a real process. Its socket answers while up; stop
// takes it down unless stubborn.
type fakeDaemon struct {
	mu       sync.Mutex
	up       bool
	stubborn bool  // stop "succeeds" but the daemon keeps answering
	stopErr  error // returned by stop
	stops    int
	probes   int
	// downAfter, when > 0, keeps the socket answering for that many probes
	// after stop, like a daemon that takes a moment to persist and exit.
	downAfter int
	// onStop runs inside stop, before the daemon goes down. Tests use it to
	// check what still exists at the moment the daemon is stopped.
	onStop func()
	// log, when set, records "stop" in call order alongside other fakes.
	log *[]string
}

func (f *fakeDaemon) control() daemonControl {
	return daemonControl{
		socket:    "fake.sock",
		stop:      f.stop,
		reachable: f.reachable,
		timeout:   50 * time.Millisecond,
		interval:  time.Millisecond,
	}
}

func (f *fakeDaemon) stop() error {
	if f.onStop != nil {
		f.onStop()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	if f.log != nil {
		*f.log = append(*f.log, "stop")
	}
	if f.stopErr != nil {
		return f.stopErr
	}
	if !f.stubborn && f.downAfter == 0 {
		f.up = false
	}
	return nil
}

func (f *fakeDaemon) reachable(string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes++
	if f.up && f.stops > 0 && f.downAfter > 0 && !f.stubborn {
		f.downAfter--
		if f.downAfter == 0 {
			f.up = false
		}
		return true
	}
	return f.up
}

func (f *fakeDaemon) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops
}
