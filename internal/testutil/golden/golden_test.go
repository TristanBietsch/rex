package golden

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeT records failures instead of failing the real test.
type fakeT struct {
	testing.TB
	errs  []string
	fatal string
}

func (f *fakeT) Helper() {}
func (f *fakeT) Errorf(format string, args ...any) {
	f.errs = append(f.errs, strings.TrimSpace(fmt.Sprintf(format, args...)))
}
func (f *fakeT) Fatalf(format string, args ...any) { f.fatal = fmt.Sprintf(format, args...) }

func writeGolden(t *testing.T, path string, secs ...Section) {
	t.Helper()
	*update = true
	defer func() { *update = false }()
	Check(t, path, secs...)
}

func TestCheck_MatchesAndShowsESC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "testdata", "g.txtar")
	secs := []Section{{"text", "\x1b[1mhi\x1b[0m\n"}, {"txt", "hi"}}
	writeGolden(t, path, secs...)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "␛[1mhi␛[0m")
	require.NotContains(t, string(raw), "\x1b")
	require.Contains(t, string(raw), "-- txt --\nhi\n")

	Check(t, path, secs...)
}

func TestCheck_MismatchFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "testdata", "g.txtar")
	writeGolden(t, path, Section{"a", "one"})

	f := &fakeT{TB: t}
	Check(f, path, Section{"a", "two"})
	require.Len(t, f.errs, 1)
	require.Contains(t, f.errs[0], "section a")

	f = &fakeT{TB: t}
	Check(f, path, Section{"a", "one"}, Section{"b", "x"})
	require.Len(t, f.errs, 1)
	require.Contains(t, f.errs[0], "unexpected section b")

	f = &fakeT{TB: t}
	Check(f, path)
	require.Len(t, f.errs, 1)
	require.Contains(t, f.errs[0], "missing section a")
}

func TestCheck_MissingFileFatal(t *testing.T) {
	f := &fakeT{TB: t}
	Check(f, filepath.Join(t.TempDir(), "testdata", "none.txtar"), Section{"a", "x"})
	require.Contains(t, f.fatal, "-update")
}

func TestCheck_RefusesPathOutsideTestdata(t *testing.T) {
	for _, path := range []string{
		filepath.Join(t.TempDir(), "g.txtar"),
		"g.txtar",
		"../escape/g.txtar",
		"mytestdata/g.txtar",
	} {
		f := &fakeT{TB: t}
		Check(f, path, Section{"a", "x"})
		require.Contains(t, f.fatal, "testdata", path)
		_, err := os.Stat(path)
		require.Error(t, err, "nothing written for %s", path)
	}
	// -update must refuse too: it is the writing path.
	*update = true
	defer func() { *update = false }()
	f := &fakeT{TB: t}
	path := filepath.Join(t.TempDir(), "w.txtar")
	Check(f, path, Section{"a", "x"})
	require.Contains(t, f.fatal, "testdata")
	_, err := os.Stat(path)
	require.Error(t, err)
}
