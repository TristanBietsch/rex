package adapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func builtinAdapter(t *testing.T, id string) Adapter {
	t.Helper()
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, ok := reg.Find(id)
	require.True(t, ok, id)
	ad, err := For(tool, "")
	require.NoError(t, err)
	return ad
}

// Hand-written screens shaped like the captured ones (no real capture bytes).
const (
	qwenIdle   = "\x1b[2J > Type your message or @path/to/file\r\n ? for shortcuts"
	qwenBusy   = "\r\n ⠇ Generating... (esc to cancel, 1s)\r\n ? for shortcuts" + " esc to cancel"
	qwenAsk    = "\x1b[2J Permission Required\r\n Apply this change?\r\n 1. Allow once\r\n 5. No"
	qwenTrust  = "\x1b[2J Do you trust the files in this folder?\r\n ❯ 1. Trust folder\r\n 2. Don't trust and exit"
	grokIdle   = "\x1b[2J > \r\n Shift+Tab:mode │ Ctrl+x:shortcuts"
	grokBusy   = "\r\n ⠸ Running ...\r\n Shift+Tab:mode │ Ctrl+x:shortcuts │ Ctrl+c:cancel"
	grokTrust  = "\x1b[2J Do you trust the contents of this directory?\r\n Yes, proceed  y\r\n No, quit  n"
	cursorIdle = "\x1b[2J → Reply with the single word\r\n Auto · 6.1%\r\n ~/proj · main"
	cursorBusy = "\r\n ⠘⠤ Working\r\n Tip: use /help"
	cursorAsk  = "\x1b[2J $ touch hello.txt\r\n Waiting for approval...\r\n Run this command?"
	cursorSign = "\x1b[2J Press any key to log in..."
	grokSign   = "\x1b[2J Approve in your browser to finish signing in.\r\n Waiting for approval...\r\n ctrl+q quit"
)

// The fallback regexes (no OSC report yet): idle reads as needs_input, a busy
// footer or spinner as working.
func TestBuiltinNewToolsDetect(t *testing.T) {
	for _, tc := range []struct {
		tool, name, screen string
		want               protocol.State
	}{
		{"qwen", "idle", qwenIdle, protocol.StateNeedsInput},
		{"qwen", "busy", qwenIdle + qwenBusy, protocol.StateWorking},
		{"qwen", "permission", qwenAsk, protocol.StateNeedsInput},
		{"qwen", "trust dialog", qwenTrust, protocol.StateNeedsInput},
		{"grok", "idle", grokIdle, protocol.StateNeedsInput},
		{"grok", "busy", grokIdle + grokBusy, protocol.StateWorking},
		{"grok", "trust dialog", grokTrust, protocol.StateNeedsInput},
		{"cursor", "busy", cursorIdle + cursorBusy, protocol.StateWorking},
		{"cursor", "approval", cursorAsk, protocol.StateNeedsInput},
	} {
		t.Run(tc.tool+"/"+tc.name, func(t *testing.T) {
			ad := builtinAdapter(t, tc.tool)
			require.Equal(t, tc.want, ad.Detect([]byte(tc.screen), 2*time.Second))
		})
	}
}

// Cursor's idle screen has no text of its own, so it reads as waiting through
// the stale-silence rule (a spinner would keep animating).
func TestCursorIdleReadsAsWaitingWhenStale(t *testing.T) {
	ad := builtinAdapter(t, "cursor")
	require.Equal(t, protocol.StateWorking, ad.Detect([]byte(cursorIdle), 2*time.Second))
	require.Equal(t, protocol.StateNeedsInput, ad.Detect([]byte(cursorIdle), 16*time.Second))
}

// Cursor has no OSC 7501: no observer, no reply, heuristic only.
func TestBuiltinOSCToolsOnly(t *testing.T) {
	for _, id := range []string{"qwen", "grok", "antigravity"} {
		ad := builtinAdapter(t, id)
		_, obs := ad.(OutputObserver)
		_, rep := ad.(OutputResponder)
		require.True(t, obs && rep, id)
	}
	for _, id := range []string{"cursor", "codex", "claude", "ollama"} {
		ad := builtinAdapter(t, id)
		_, obs := ad.(OutputObserver)
		_, rep := ad.(OutputResponder)
		require.False(t, obs || rep, id)
	}
}

// qwen reports a permission prompt as blocked while its footer still reads
// like idle: the OSC state wins.
func TestBuiltinQwenOSCOverridesFooter(t *testing.T) {
	ad := builtinAdapter(t, "qwen")
	o := ad.(*OSCStatus)
	o.Observe(osc("state=working:app=qwen"))
	require.Equal(t, protocol.StateWorking, ad.Detect([]byte(qwenIdle), 2*time.Second))
	o.Observe(osc("state=blocked:kind=permission:app=qwen"))
	require.Equal(t, protocol.StateNeedsInput, ad.Detect(nil, 0))
	require.Equal(t, protocol.WaitInput, o.WaitReason())
}

func TestLoginScreenNeedsInputWithNotice(t *testing.T) {
	for _, tc := range []struct{ tool, screen, notice string }{
		{"cursor", cursorSign, "cursor: not signed in — run `cursor-agent login`"},
		{"grok", grokSign, "grok: not signed in — run `grok login`"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			ad := builtinAdapter(t, tc.tool)
			n, ok := ad.(Noticer)
			require.True(t, ok)
			require.Equal(t, "", n.Notice(), "no notice before the screen shows")
			require.Equal(t, protocol.StateNeedsInput, ad.Detect([]byte(tc.screen), 2*time.Second))
			require.Equal(t, tc.notice, n.Notice())
			// Once the agent is working again the notice goes away.
			require.Equal(t, protocol.StateWorking, ad.Detect([]byte(tc.screen), 0))
		})
	}
}

// A login screen followed by the agent's normal idle screen is not signed out.
func TestLoginNoticeClearsAfterSignIn(t *testing.T) {
	ad := builtinAdapter(t, "cursor")
	n := ad.(Noticer)
	require.Equal(t, protocol.StateNeedsInput, ad.Detect([]byte(cursorSign), 2*time.Second))
	require.NotEmpty(t, n.Notice())
	require.Equal(t, protocol.StateWorking, ad.Detect([]byte(cursorSign+cursorIdle+cursorBusy), 2*time.Second))
	require.Empty(t, n.Notice())
}

func TestToolsWithoutLoginRegexHaveNoNotice(t *testing.T) {
	ad := builtinAdapter(t, "codex")
	require.Equal(t, protocol.StateNeedsInput, ad.Detect([]byte("\x1b[2J \u203a ready Press any key to log in"), 2*time.Second))
	if n, ok := ad.(Noticer); ok {
		require.Empty(t, n.Notice())
	}
}

// Once an OSC report arrived the footer fallback's login notice is gone.
func TestOSCStatusNoticeClearsAfterFirstReport(t *testing.T) {
	ad := builtinAdapter(t, "grok")
	n := ad.(Noticer)
	require.Equal(t, protocol.StateNeedsInput, ad.Detect([]byte(grokSign), 2*time.Second))
	require.NotEmpty(t, n.Notice())
	ad.(*OSCStatus).Observe(osc("state=working"))
	require.Empty(t, n.Notice())
}

func TestForRejectsBadLoginRegex(t *testing.T) {
	_, err := For(registry.Tool{ID: "x", Command: []string{"x"}, LoginRegex: "(",
		Detect: registry.Detect{Kind: "heuristic", PromptRegex: "p", IdleMs: 10}}, "")
	require.ErrorContains(t, err, "login regex")
}

// Cursor's real frame order while busy: spinner line first, then the input
// bar and the status line below it. A full repaint leaves the status line
// after the spinner in the raw stream; the status line must not read as idle.
func TestCursorBusyFrameOrder(t *testing.T) {
	frame := "\x1b[H⠘⠣ Thinking  2 tokens\r\n→ Reply with pong\r\n Auto · 6.1%\r\n ~/proj · main"
	ad := builtinAdapter(t, "cursor")
	require.Equal(t, protocol.StateWorking, ad.Detect([]byte(cursorIdle+frame), 2*time.Second),
		"busy frame in repaint order read as idle")
}
