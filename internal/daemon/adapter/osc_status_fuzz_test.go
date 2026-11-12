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
		"\x1b]7501;?\x07\x1b]7501;?\x1b\\x\x1b]7501;state=done:kind=auth\x07",
		"\x1b]7501;state=blocked:kind=permission:app=qwen\x1b\\\x1b]7501;state=clear\x07",
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
		rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic chunking for a test
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

		if wr, sr := whole.WaitReason(), split.WaitReason(); wr != sr {
			t.Fatalf("chunking changed the wait reason: %q vs %q", wr, sr)
		}
		if wk, sk := whole.BlockedKind(), split.BlockedKind(); wk != sk {
			t.Fatalf("chunking changed the kind: %q vs %q", wk, sk)
		}
		if wq, sq := whole.TakeReply(), split.TakeReply(); !bytes.Equal(wq, sq) {
			t.Fatalf("chunking changed the replies: %q vs %q", wq, sq)
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
