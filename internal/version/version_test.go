package version

import "testing"

func TestResolve(t *testing.T) {
	cases := []struct {
		linked, module, want string
	}{
		{"v2.0.0", "v0.0.0-20261112093000-abcdef123456", "v2.0.0"},
		{"dev", "v0.0.0-20261112093000-abcdef123456", "v0.0.0-20261112093000-abcdef123456"},
		{"", "v1.2.3", "v1.2.3"},
		{"dev", "(devel)", "dev"},
		{"dev", "", "dev"},
		{"", "", "dev"},
	}
	for _, c := range cases {
		if got := resolve(c.linked, c.module); got != c.want {
			t.Errorf("resolve(%q, %q) = %q, want %q", c.linked, c.module, got, c.want)
		}
	}
}

func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Fatal("Version is empty")
	}
}
