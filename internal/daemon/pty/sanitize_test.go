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
		{"drops_c1_csi", "a\u009b2Jb", "a2Jb"},
		{"drops_c1_controls", "x\u0080y\u0099z\u009dw", "xyzw"},
		{"c1_next_line_is_a_break", "x\u0085y", "x y"},
		{"drops_bidi_overrides", "\u202egnp.exe\u202c ok\u2066", "gnp.exe ok"},
		{"drops_invalid_utf8", "a\xffb\xc0c", "abc"},
		{"trims_leading_space", "  \x1b[2K  data", "data"},
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

func TestSanitizePaste(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "fix the build", "fix the build"},
		{"keeps_newline_and_tab", "a\n\tb", "a\n\tb"},
		{"paste_end_marker", "a\x1b[201~rm -rf /\r", "a[201~rm -rf /"},
		{"crlf", "one\r\ntwo", "one\ntwo"},
		{"c0_and_del", "x\x03y\x04z\x7f", "xyz"},
		{"c1_csi", "a\u009b31mb", "a31mb"},
		{"invalid_utf8", "a\x9bb\xff", "ab"},
		{"keeps_unicode", "héllo ✓ 漢字", "héllo ✓ 漢字"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizePaste(c.in); got != c.want {
				t.Fatalf("sanitizePaste(%q) = %q, want %q", c.in, got, c.want)
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
