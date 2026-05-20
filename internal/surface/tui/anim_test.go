package tui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRenderAnimFrameTypewriter(t *testing.T) {
	a := DescAnim{
		From:      "old description text",
		To:        "running pnpm test",
		Effect:    "typewriter",
		StartedAt: time.Now(),
		Duration:  300 * time.Millisecond,
	}
	// At p=0 → empty + padding to width.
	got := renderAnimFrame(a, 20, a.StartedAt)
	if utf8.RuneCountInString(got) != 20 {
		t.Fatalf("p=0 not width=20: %q (%d runes)", got, utf8.RuneCountInString(got))
	}
	if strings.TrimSpace(got) != "" {
		t.Fatalf("p=0 should be blank, got %q", got)
	}
	// At p=1 → full text + padding.
	got = renderAnimFrame(a, 20, a.StartedAt.Add(a.Duration))
	if utf8.RuneCountInString(got) != 20 {
		t.Fatalf("p=1 not width=20: %q", got)
	}
	if !strings.HasPrefix(got, "running pnpm test") {
		t.Fatalf("p=1 missing target prefix: %q", got)
	}
}

func TestRenderAnimFrameDecodeWidthInvariant(t *testing.T) {
	a := DescAnim{
		From:      "",
		To:        "running tests",
		Effect:    "decode",
		StartedAt: time.Now(),
		Duration:  400 * time.Millisecond,
	}
	for _, p := range []float64{0, 0.25, 0.5, 0.75, 1.0} {
		at := a.StartedAt.Add(time.Duration(float64(a.Duration) * p))
		got := renderAnimFrame(a, 30, at)
		if utf8.RuneCountInString(got) != 30 {
			t.Fatalf("decode p=%v width: got %d want 30 (%q)", p, utf8.RuneCountInString(got), got)
		}
	}
}

func TestRenderAnimFrameWipeShowsCursor(t *testing.T) {
	a := DescAnim{
		From:      "old line",
		To:        "new line",
		Effect:    "wipe",
		StartedAt: time.Now(),
		Duration:  250 * time.Millisecond,
	}
	mid := a.StartedAt.Add(125 * time.Millisecond)
	got := renderAnimFrame(a, 20, mid)
	if utf8.RuneCountInString(got) != 20 {
		t.Fatalf("wipe width: %q", got)
	}
	if !strings.ContainsRune(got, '█') {
		t.Fatalf("wipe midway should contain █: %q", got)
	}
}

func TestRenderAnimFrameDecodeNoiseAlphabet(t *testing.T) {
	a := DescAnim{
		From:      "",
		To:        "abcdefghij",
		Effect:    "decode",
		StartedAt: time.Now(),
		Duration:  400 * time.Millisecond,
	}
	// At p=0.01 essentially nothing has settled.
	got := renderAnimFrame(a, 10, a.StartedAt.Add(time.Millisecond))
	allowed := "!@#$%&*+=?<>~/\\abcdefghij "
	for _, r := range got {
		if !strings.ContainsRune(allowed, r) {
			t.Fatalf("decode unexpected glyph %q in %q", r, got)
		}
	}
}
