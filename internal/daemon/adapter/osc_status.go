package adapter

import (
	"bytes"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// OutputObserver is implemented by adapters that read the agent's raw output
// stream, not only the window the supervisor polls. The supervisor calls
// Observe from the PTY reader goroutine with every chunk, in order, after it
// has written the chunk to the screen; chunk is only valid during the call.
type OutputObserver interface {
	Observe(chunk []byte)
}

// OSC7501Env is the env var that makes Antigravity report its state on OSC
// 7501. The daemon sets it to "on" on the child.
const OSC7501Env = "AGY_CLI_OSC7501"

const (
	// oscIntro starts an Antigravity terminal status sequence.
	oscIntro = "\x1b]7501;"
	// maxOSCCarry bounds the bytes kept between chunks for a sequence split
	// across reads, and so the longest accepted sequence (introducer and
	// payload). Longer ones are dropped.
	maxOSCCarry = 512
	// maxOSCMessageRunes caps the kept message text. The decoded message is
	// already bounded by maxOSCCarry.
	maxOSCMessageRunes = 120
)

// OSCStatus reads the state Antigravity reports on OSC 7501 and falls back to
// the footer heuristic until the first sequence arrives.
//
// A sequence is ESC ] 7501 ; key=value : key=value ... terminated by BEL or
// ST. The keys used are state (working, idle, blocked, error) and msg
// (standard base64 of untrusted text). Unknown states and keys are ignored.
// working means working; idle (the turn ended), blocked (a question, a
// permission or the trust dialog) and error all mean the agent waits on the
// user. Process exit, not these, decides done and failed.
type OSCStatus struct {
	inner *HeuristicCLI

	mu    sync.Mutex
	carry []byte // unfinished sequence from the previous chunks, <= maxOSCCarry
	seen  bool   // a valid state sequence arrived
	state protocol.State
	msg   string // sanitized message of the latest sequence, "" if none
}

// NewOSCStatus wraps a heuristic adapter.
func NewOSCStatus(inner *HeuristicCLI) *OSCStatus {
	return &OSCStatus{inner: inner}
}

// Observe implements OutputObserver.
func (o *OSCStatus) Observe(chunk []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	data := chunk
	if len(o.carry) > 0 {
		data = append(append(make([]byte, 0, len(o.carry)+len(chunk)), o.carry...), chunk...)
	}
	o.carry = o.carry[:0]
	for {
		i := bytes.Index(data, []byte(oscIntro))
		if i < 0 {
			o.carry = append(o.carry, introPrefixTail(data)...)
			return
		}
		payload, consumed, status := scanOSCPayload(data[i+len(oscIntro):])
		switch status {
		case oscComplete:
			o.apply(string(payload))
			data = data[i+len(oscIntro)+consumed:]
		case oscNeedMore:
			o.carry = append(o.carry, data[i:]...)
			return
		default: // oscDrop: not a sequence we accept; resume after the introducer
			data = data[i+len(oscIntro):]
		}
	}
}

type oscScan int

const (
	oscComplete oscScan = iota
	oscNeedMore
	oscDrop
)

// scanOSCPayload reads one sequence body from s (the bytes after the
// introducer). It returns the payload and the bytes consumed including the
// terminator when complete; oscNeedMore when s ends inside the sequence and
// it still fits the carry; oscDrop when it is too long or an unrelated
// escape interrupts it.
func scanOSCPayload(s []byte) (payload []byte, consumed int, status oscScan) {
	limit := maxOSCCarry - len(oscIntro)
	for i, b := range s {
		if i >= limit {
			return nil, 0, oscDrop
		}
		switch b {
		case 0x07:
			return s[:i], i + 1, oscComplete
		case 0x1b:
			if i+1 == len(s) {
				return nil, 0, oscNeedMore // maybe the first byte of ST
			}
			if s[i+1] == '\\' {
				return s[:i], i + 2, oscComplete
			}
			return nil, 0, oscDrop
		}
	}
	if len(s) >= limit {
		return nil, 0, oscDrop
	}
	return nil, 0, oscNeedMore
}

// introPrefixTail returns the longest suffix of data that is a proper prefix
// of the introducer, the only bytes worth carrying when no sequence started.
func introPrefixTail(data []byte) []byte {
	for n := min(len(oscIntro)-1, len(data)); n > 0; n-- {
		if string(data[len(data)-n:]) == oscIntro[:n] {
			return data[len(data)-n:]
		}
	}
	return nil
}

// apply handles one complete payload. A payload without a known state is
// ignored whole.
func (o *OSCStatus) apply(payload string) {
	var state protocol.State
	var msg string
	known := false
	for _, f := range strings.Split(payload, ":") {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "state":
			switch v {
			case "working":
				state, known = protocol.StateWorking, true
			case "idle", "blocked", "error":
				state, known = protocol.StateNeedsInput, true
			}
		case "msg":
			if raw, err := base64.StdEncoding.DecodeString(v); err == nil {
				msg = protocol.SanitizeDisplayText(string(raw), maxOSCMessageRunes)
			}
		}
	}
	if !known {
		return
	}
	o.seen, o.state, o.msg = true, state, msg
}

// Message is the sanitized text of the latest status sequence's msg key, ""
// when it had none. It is untrusted agent text: reserved for a future
// preview, never logged.
func (o *OSCStatus) Message() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.msg
}

// Detect implements Adapter. Before the first status sequence it is the
// heuristic's answer. After, the reported state is exact (no idle gate), but
// a working report older than the stale-silence window counts as waiting:
// an interrupted turn (Esc) sends no idle sequence, and a working agent
// animates its spinner.
func (o *OSCStatus) Detect(window []byte, idle time.Duration) protocol.State {
	o.mu.Lock()
	seen, state := o.seen, o.state
	o.mu.Unlock()
	if !seen {
		return o.inner.Detect(window, idle)
	}
	if state == protocol.StateWorking && idle >= staleWorking {
		return protocol.StateNeedsInput
	}
	return state
}

// IsReadyForInput implements Adapter: the visible prompt decides.
func (o *OSCStatus) IsReadyForInput(window []byte, idle time.Duration) bool {
	return o.inner.IsReadyForInput(window, idle)
}
