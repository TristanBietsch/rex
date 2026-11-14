package luacfg

import (
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

func TestMain(m *testing.M) { testenv.Run(m) }

func itoa(i int) string { return strconv.Itoa(i) }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func quoteLua(s string) string { return strconv.Quote(s) }

func readAllString(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	return string(b), err
}

func skipNoSymlinks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(dir, dir+"/l"); err != nil {
		t.Skip("symlinks unavailable")
	}
}
