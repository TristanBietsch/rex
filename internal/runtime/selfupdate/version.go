package selfupdate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// semver is a parsed vMAJOR.MINOR.PATCH[-pre][+build].
type semver struct {
	nums [3]uint64
	pre  []string
}

var (
	preIdent = regexp.MustCompile(`^[0-9A-Za-z-]+$`)
	// pseudo matches the timestamp-and-commit tail of a Go pseudo-version
	// (v0.0.0-20261112093000-abcdef123456, v2.0.1-0.2026…-abcdef123456).
	pseudo = regexp.MustCompile(`(^|\.)[0-9]{14}-[0-9a-f]{12}$`)
)

func parse(v string) (semver, error) {
	var s semver
	rest, ok := strings.CutPrefix(v, "v")
	if !ok {
		return s, fmt.Errorf("version %q does not start with v", v)
	}
	rest, _, _ = strings.Cut(rest, "+") // build metadata does not order
	core, pre, hasPre := strings.Cut(rest, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return s, fmt.Errorf("version %q is not vMAJOR.MINOR.PATCH", v)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || p == "" || (len(p) > 1 && p[0] == '0') {
			return s, fmt.Errorf("version %q is not vMAJOR.MINOR.PATCH", v)
		}
		s.nums[i] = n
	}
	if hasPre {
		s.pre = strings.Split(pre, ".")
		for _, id := range s.pre {
			if !preIdent.MatchString(id) {
				return s, fmt.Errorf("version %q has a bad pre-release", v)
			}
		}
	}
	return s, nil
}

// Compare orders two release versions by semver precedence: -1, 0 or 1.
func Compare(a, b string) (int, error) {
	x, err := parse(a)
	if err != nil {
		return 0, err
	}
	y, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range x.nums {
		if x.nums[i] != y.nums[i] {
			if x.nums[i] < y.nums[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	return comparePre(x.pre, y.pre), nil
}

// comparePre applies semver's pre-release rules: no pre-release ranks
// above any, numeric identifiers compare numerically and rank below
// alphanumeric ones, and a longer list wins a tie.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdent(a[i], b[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func compareIdent(a, b string) int {
	an, aerr := strconv.ParseUint(a, 10, 64)
	bn, berr := strconv.ParseUint(b, 10, 64)
	switch {
	case aerr == nil && berr == nil:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		}
		return 0
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

// Known reports whether v is a release version that can be compared: a
// semver tag, not "dev" and not a Go pseudo-version.
func Known(v string) bool {
	s, err := parse(v)
	if err != nil {
		return false
	}
	return !pseudo.MatchString(strings.Join(s.pre, "."))
}

// NormalizeTag turns a user-given version ("2.1.0" or "v2.1.0") into a
// release tag, refusing anything that is not a plain semver tag (it ends
// up in a download URL).
func NormalizeTag(v string) (string, error) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if strings.ContainsAny(v, "+/\\?#%") {
		return "", fmt.Errorf("unexpected tag %q", v)
	}
	if _, err := parse(v); err != nil {
		return "", fmt.Errorf("unexpected tag %q: %w", v, err)
	}
	return v, nil
}
