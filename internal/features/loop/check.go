package loop

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

// MaxCheckOutput caps the check output kept for the loop log.
const MaxCheckOutput = 64 << 10

// checkWaitDelay bounds how long RunCheck waits for a killed check's output
// pipe to close (a grandchild may hold it).
const checkWaitDelay = 2 * time.Second

// CheckResult is what one run of a check command did.
type CheckResult struct {
	// ExitCode is the exit status; -1 when the command did not run to an
	// exit (start failure, kill).
	ExitCode int
	// TimedOut is true when the timeout killed it.
	TimedOut bool
	// Output is stdout and stderr together, at most MaxCheckOutput bytes.
	Output []byte
	// Truncated is true when output beyond the cap was dropped.
	Truncated bool
	// Err is why the command could not start or finish (not found, bad
	// folder, canceled); nil otherwise, including for a non-zero exit.
	Err error
}

// Passed reports whether the check exited 0.
func (r CheckResult) Passed() bool { return r.Err == nil && !r.TimedOut && r.ExitCode == 0 }

// cappedBuffer keeps the first limit bytes written and drops the rest.
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := max(c.limit-c.buf.Len(), 0)
	c.buf.Write(p[:min(room, len(p))])
	if len(p) > room {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedBuffer) result() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Clone(c.buf.Bytes()), c.truncated
}

// RunCheck runs argv in dir with a timeout, in its own process group that is
// killed on timeout or when ctx ends. Output is captured up to
// MaxCheckOutput. argv comes from a validated loop file, never from agent
// output; it is executed directly, with no shell.
func RunCheck(ctx context.Context, argv []string, dir string, timeout time.Duration) CheckResult {
	if len(argv) == 0 {
		return CheckResult{ExitCode: -1, Err: errors.New("empty check command")}
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	out := &cappedBuffer{limit: MaxCheckOutput}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = checkWaitDelay
	configureKill(cmd)
	err := cmd.Run()
	res := CheckResult{ExitCode: -1}
	res.Output, res.Truncated = out.result()
	if cctx.Err() != nil {
		// The deadline or ctx killed it. Either way the check did not pass;
		// TimedOut is only the deadline.
		if ctx.Err() != nil {
			res.Err = ctx.Err()
		} else {
			res.TimedOut = true
		}
		return res
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		res.ExitCode = 0
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
	default:
		res.Err = err
	}
	return res
}
