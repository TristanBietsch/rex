package adapter

import (
	"bytes"
	"encoding/base64"
	"math/rand"
	"testing"

)

func fuzzOSC(t testing.TB) *OSCStatus {
	t.Helper()
	h, err := NewHeuristic(`\? for shortcuts`, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return NewOSCStatus(h)
}

// FuzzOSCStatusObserve: any byte stream is safe to observe, the carry stays
// bounded, and the result does not depend on how the stream is chunked.
func FuzzOSCStatusObserve(f *testing.F) {
	msg := base64.StdEncoding.EncodeToString([]byte("Allow rm?"))
	for _, s := range []string{
		"\x1b]7501;state=working:app=antigravity\x07",
		"\x1b]7501;state=idle\x1b\\",
		"x\x1b]7501;state=blocked:kind=auth:msg=" + msg + "\x07tail",
		"\x1b]7501;state=idle:msg=!!notbase64!!\x07",
		"\x1b]7501;state=idle\x1b[0m rest",
		"\x1b]7501;state=bogus\x07\x1b]7501;state=working\x07",
		"\x1b]7501;state=idle:msg=" + string(bytes.Repeat([]byte("A"), 2000)) + "\x07",
		"\x1b]7501;state=idle:msg=" + string(bytes.Repeat([]byte("A"), 2000)),
		"\x1b]750",
		"\x1b",
		"\xff\xfe\x1b\x1b\x1b]",
	} {
		f.Add([]byte(s), int64(1))
	}
	f.Fuzz(func(t *testing.T, data []byte, seed int64) {
		whole := fuzzOSC(t)
		whole.Observe(data)

		split := fuzzOSC(t)
		rng := rand.New(rand.NewSource(seed))
		for rest := data; len(rest) > 0; {
			n := 1 + rng.Intn(len(rest))
			split.Observe(rest[:n])
			rest = rest[n:]
			split.mu.Lock()
			c := len(split.carry)
			split.mu.Unlock()
			if c > maxOSCCarry {
				t.Fatalf("carry %d > %d", c, maxOSCCarry)
			}
		}

		whole.mu.Lock()
		defer whole.mu.Unlock()
		split.mu.Lock()
		defer split.mu.Unlock()
		if len(whole.carry) > maxOSCCarry {
			t.Fatalf("carry %d > %d", len(whole.carry), maxOSCCarry)
		}
		if whole.seen != split.seen || whole.state != split.state || whole.msg != split.msg {
			t.Fatalf("chunking changed the result: whole=(%v,%q,%q) split=(%v,%q,%q)",
				whole.seen, whole.state, whole.msg, split.seen, split.state, split.msg)
		}
	})
}
