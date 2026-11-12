package pty

import "testing"

func TestSanitizeForDisplay(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "hello world", "hello world"},
		{"strips_color", "\x1b[33myellow\x1b[0m", "yellow"},
		{"strips_clear_screen", "\x1b[2J\x1b[Hhello", "hello"},
		{"strips_erase_to_eol", "before\x1b[Kafter", "beforeafter"},
		{"replaces_carriage_return", "first\rsecond", "first second"},
		{"strips_bel_and_bs", "x\x07y\x08z", "x y z"},
		{"collapses_runs_of_controls", "a\r\r\r\rb", "a b"},
		{"strips_osc_set_title", "\x1b]0;title\x07payload", "payload"},
		{"keeps_unicode", "ok ✓ done", "ok ✓ done"},
		{"trims_trailing_space_after_strip", "data\x1b[K   ", "data"},
		// F-05: C1 controls (U+0080-U+009F) are 8-bit escape introducers;
		// U+009B is CSI and U+009D is OSC on terminals that honor them.
		{"strips_c1_csi", "a\u009b31mb", "a 31mb"},
		{"strips_c1_osc_and_st", "x\u009d0;t\u009cy", "x 0;t y"},
		{"strips_c1_range_ends", "\u0080p\u009fq", "p q"},
		{"keeps_latin1_after_c1", "caf\u00e9\u0085ok", "caf\u00e9 ok"},
		{"escape_after_invalid_utf8", "\xc9\x1b[31mred", "red"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeForDisplay(c.in)
			if got != c.want {
				t.Fatalf("sanitizeForDisplay(%q)\n  got  %q\n  want %q", c.in, got, c.want)
			}
		})
	}
}

func TestHasVisibleText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"only_whitespace", "   \t\n", false},
		{"cursor_show", "\x1b[?25h", false},
		{"cursor_hide", "\x1b[?25l", false},
		{"cursor_blink_pair", "\x1b[?25l\x1b[?25h", false},
		{"sgr_reset", "\x1b[0m", false},
		{"clear_screen_only", "\x1b[2J\x1b[H", false},
		{"escape_only_burst", "\x1b[?25l\x1b[H\x1b[K\x1b[?25h", false},
		{"text", "hi", true},
		{"colored_text", "\x1b[33mhi\x1b[0m", true},
		{"text_with_position", "\x1b[5;1H› ready", true},
		{"unicode", "● done", true},
		{"only_bel", "\x07", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasVisibleText([]byte(c.in)); got != c.want {
				t.Fatalf("hasVisibleText(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// Text typed into an agent (the initial prompt, a Reply) must not carry
// terminal input of its own: an ESC[201~ in it closed the bracketed paste
// early and the rest reached the agent as keystrokes. Line breaks and tabs
// stay; "\r\n" and "\r" become "\n".
func TestTypedText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain prompt", "plain prompt"},
		{"list:\n- a\n\t- b", "list:\n- a\n\t- b"},
		{"crlf\r\nline\rend", "crlf\nline\nend"},
		{"before\x1b[201~after\r", "beforeafter\n"},
		{"osc\x1b]0;title\x07 bell\x07 del\x7f c1\u0085 x", "osc bell del c1 x"},
		{"bad\xffutf8", "badutf8"},
	}
	for _, c := range cases {
		if got := TypedText(c.in); got != c.want {
			t.Errorf("TypedText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
