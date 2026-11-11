package adapter

import (
	"bytes"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func newAgyOSC(t *testing.T) *OSCStatus {
	t.Helper()
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, ok := reg.Find("antigravity")
	require.True(t, ok)
	ad, err := For(tool, "")
	require.NoError(t, err)
	o, ok := ad.(*OSCStatus)
	require.True(t, ok, "builtin antigravity builds the OSC adapter, got %T", ad)
	return o
}

// osc builds one synthetic status sequence (BEL terminated).
func osc(fields string) []byte { return []byte("\x1b]7501;" + fields + "\x07") }

const (
	footerIdle = "\x1b[2J > hello\r\n ? for shortcuts"
	footerBusy = "\x1b[2J Generating...\r\n esc to cancel"
	quiet      = 2 * time.Second // past the heuristic's 1.2 s idle gate
)

func TestOSCStatus_BeforeAnySequenceUsesFooter(t *testing.T) {
	o := newAgyOSC(t)
	require.Equal(t, protocol.StateNeedsInput, o.Detect([]byte(footerIdle), quiet))
	require.Equal(t, protocol.StateWorking, o.Detect([]byte(footerIdle+footerBusy), quiet))
	require.Equal(t, protocol.StateWorking, o.Detect([]byte(footerIdle), 0))
	require.True(t, o.IsReadyForInput([]byte(footerIdle), 0))
	require.False(t, o.IsReadyForInput([]byte(footerBusy), 0))
}

func TestOSCStatus_StateMapping(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  protocol.State
	}{
		{"working", protocol.StateWorking},
		{"idle", protocol.StateNeedsInput},
		{"blocked", protocol.StateNeedsInput},
		{"error", protocol.StateNeedsInput},
	} {
		t.Run(tc.state, func(t *testing.T) {
			o := newAgyOSC(t)
			o.Observe(osc("state=" + tc.state + ":app=antigravity"))
			// No idle gate: the sequence is exact, so it wins at once.
			require.Equal(t, tc.want, o.Detect(nil, 0))
		})
	}
}

func TestOSCStatus_Terminators(t *testing.T) {
	for name, term := range map[string]string{"BEL": "\x07", "ST": "\x1b\\"} {
		t.Run(name, func(t *testing.T) {
			o := newAgyOSC(t)
			o.Observe([]byte("\x1b]7501;state=working:app=antigravity" + term))
			require.Equal(t, protocol.StateWorking, o.Detect(nil, 0))
			o.Observe([]byte("\x1b]7501;state=idle:app=antigravity" + term))
			require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0))
		})
	}
}

func TestOSCStatus_SplitAtEveryByteBoundary(t *testing.T) {
	for name, term := range map[string]string{"BEL": "\x07", "ST": "\x1b\\"} {
		stream := []byte("text\x1b[1m\x1b]7501;state=blocked:app=antigravity:kind=auth" + term + "tail")
		for cut := 0; cut <= len(stream); cut++ {
			o := newAgyOSC(t)
			o.Observe(stream[:cut])
			o.Observe(stream[cut:])
			require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0), "%s cut=%d", name, cut)
		}
		// One byte per chunk, working then idle.
		o := newAgyOSC(t)
		for _, b := range []byte("x\x1b]7501;state=working" + term + "\x1b]7501;state=idle" + term) {
			o.Observe([]byte{b})
		}
		require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0), name)
	}
}

func TestOSCStatus_MultipleSequencesInOneChunkLastWins(t *testing.T) {
	o := newAgyOSC(t)
	o.Observe(append(osc("state=idle"), osc("state=working")...))
	require.Equal(t, protocol.StateWorking, o.Detect(nil, 0))
	o.Observe(append(osc("state=working"), osc("state=idle")...))
	require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0))
}

func TestOSCStatus_IgnoresGarbageAndUnknown(t *testing.T) {
	o := newAgyOSC(t)
	o.Observe(osc("state=working"))
	for _, junk := range [][]byte{
		osc("state=bogus"),
		osc("nostate=1"),
		osc(""),
		osc("state"),
		osc("=idle"),
		osc("kind=auth:future=thing"),
		[]byte("\x1b]7500;state=idle\x07"), // other OSC number
		[]byte("\x1b]75010;state=idle\x07"),
		[]byte("\x1b]7501;state=idle\x1b[0m rest"), // aborted by another escape
		[]byte("\x1b]0;title\x07"),
		{0xff, 0xfe, 0x1b, 0x1b, 0x1b, ']'},
	} {
		o.Observe(junk)
		require.Equal(t, protocol.StateWorking, o.Detect(nil, 0), "%q", junk)
	}
	// Unknown extra keys next to a valid state still count.
	o.Observe(osc("future=1:state=idle:other=x"))
	require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0))
}

func TestOSCStatus_OversizedPayloadDroppedAndCarryBounded(t *testing.T) {
	o := newAgyOSC(t)
	o.Observe(osc("state=working"))
	// An unterminated payload far past the cap is dropped, not buffered.
	big := append([]byte("\x1b]7501;state=idle:msg="), bytes.Repeat([]byte("A"), 4096)...)
	for i := 0; i < 50; i++ {
		o.Observe(big)
		o.mu.Lock()
		n := len(o.carry)
		o.mu.Unlock()
		require.LessOrEqual(t, n, maxOSCCarry)
	}
	require.Equal(t, protocol.StateWorking, o.Detect(nil, 0), "oversized sequence never lands")
	// A terminated but oversized sequence is ignored too.
	o.Observe(append(append([]byte("\x1b]7501;state=idle:msg="), bytes.Repeat([]byte("A"), 2000)...), 0x07))
	require.Equal(t, protocol.StateWorking, o.Detect(nil, 0))
	// And the parser still works afterwards.
	o.Observe(osc("state=idle"))
	require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0))
}

func TestOSCStatus_PlainOutputKeepsNoCarry(t *testing.T) {
	o := newAgyOSC(t)
	o.Observe(bytes.Repeat([]byte("hello world "), 1000))
	o.mu.Lock()
	defer o.mu.Unlock()
	require.Empty(t, o.carry)
}

func TestOSCStatus_PrecedenceOverFooter(t *testing.T) {
	o := newAgyOSC(t)
	// OSC working beats an idle footer.
	o.Observe(osc("state=working"))
	require.Equal(t, protocol.StateWorking, o.Detect([]byte(footerIdle), quiet))
	// OSC idle beats a busy footer.
	o.Observe(osc("state=idle"))
	require.Equal(t, protocol.StateNeedsInput, o.Detect([]byte(footerBusy), quiet))
	// IsReadyForInput stays the footer's call.
	require.True(t, o.IsReadyForInput([]byte(footerIdle), 0))
}

func TestOSCStatus_TrustDialogBlockedIsNeedsInputAtOnce(t *testing.T) {
	// Regression: the trust dialog matches neither footer pattern, so the
	// heuristic reported "working" for 15 s. The blocked sequence ends that.
	dialog := "Do you trust the contents of this project?\r\n> Yes, I trust this folder\r\n  No, exit\r\n↑/↓ Navigate · enter Confirm"
	o := newAgyOSC(t)
	require.Equal(t, protocol.StateWorking, o.Detect([]byte(dialog), 3*time.Second), "footer alone: still working")
	o.Observe(osc("state=blocked:app=antigravity:kind=trust"))
	require.Equal(t, protocol.StateNeedsInput, o.Detect([]byte(dialog), 0))
}

func TestOSCStatus_StaleWorkingGuard(t *testing.T) {
	o := newAgyOSC(t)
	o.Observe(osc("state=working"))
	require.Equal(t, protocol.StateWorking, o.Detect(nil, staleWorking-time.Millisecond))
	// Esc interrupt: no idle sequence follows, only silence.
	require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, staleWorking))
	// A later sequence still takes over.
	o.Observe(osc("state=working"))
	require.Equal(t, protocol.StateWorking, o.Detect(nil, time.Second))
}

func TestOSCStatus_MessageDecodedSanitizedCapped(t *testing.T) {
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	o := newAgyOSC(t)
	require.Empty(t, o.Message())
	o.Observe(osc("state=error:msg=" + enc("quota exhausted")))
	require.Equal(t, "quota exhausted", o.Message())

	// Control bytes and escapes are stripped, first line only.
	o.Observe(osc("state=blocked:msg=" + enc("\x1b[31mAllow rm?\x07\nsecond line")))
	require.Equal(t, "Allow rm?", o.Message())

	// Bad base64 is ignored but the state still lands, and the old text goes.
	o.Observe(osc("state=idle:msg=!!notbase64!!"))
	require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0))
	require.Empty(t, o.Message())

	// Long text is capped in runes.
	o.Observe(osc("state=error:msg=" + enc(strings.Repeat("é", 150))))
	require.Equal(t, maxOSCMessageRunes, len([]rune(o.Message())))

	// A new state without a msg clears the old message.
	o.Observe(osc("state=working"))
	require.Empty(t, o.Message())
}

func TestOSCStatus_ConcurrentObserveDetect(t *testing.T) {
	o := newAgyOSC(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = o.Detect([]byte(footerIdle), quiet)
				_ = o.Message()
				_ = o.IsReadyForInput([]byte(footerBusy), 0)
			}
		}
	}()
	for i := 0; i < 500; i++ {
		o.Observe(osc("state=working"))
		o.Observe([]byte("\x1b]7501;state=id"))
		o.Observe([]byte("le\x07"))
	}
	close(stop)
	wg.Wait()
	require.Equal(t, protocol.StateNeedsInput, o.Detect(nil, 0))
}

func TestFor_StatusOnlyWrapsHeuristic(t *testing.T) {
	tool := registry.Tool{ID: "x", Models: []registry.Model{{ID: "m"}},
		Detect: registry.Detect{Kind: "heuristic", PromptRegex: "p", IdleMs: 100}}
	ad, err := For(tool, "")
	require.NoError(t, err)
	require.IsType(t, &HeuristicCLI{}, ad)
	_, isObs := ad.(OutputObserver)
	require.False(t, isObs)

	tool.Detect.Status = registry.StatusOSC7501
	ad, err = For(tool, "")
	require.NoError(t, err)
	require.IsType(t, &OSCStatus{}, ad)
	_, isObs = ad.(OutputObserver)
	require.True(t, isObs)

	tool.Detect.Status = "nope"
	_, err = For(tool, "")
	require.ErrorContains(t, err, "unsupported detect.status")
}
