// Package golden compares rendered output with txtar golden archives.
//
// One archive holds several named sections (for example text.w80, txt, json),
// so a single file pins every output format of one view. Run the package's
// tests with -update to rewrite archives from the current output. ESC bytes
// are stored as the visible symbol ␛ so colored output stays reviewable.
package golden

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/txtar"
)

var update = flag.Bool("update", false, "rewrite golden txtar files from the current output")

// ESC is the visible stand-in for the escape byte in golden files.
const ESC = "␛"

// Section is one named block of a golden archive.
type Section struct {
	Name string
	Body string
}

// Check compares sections with the archive at path (relative to the test's
// package directory). With -update it writes the archive instead. Sections
// are compared by name and order; a missing, extra or reordered section
// fails the test.
func Check(t testing.TB, path string, sections ...Section) {
	t.Helper()
	got := encode(sections)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("golden: %v", err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden: %v (run with -update to create it)", err)
	}
	if string(want) == string(got) {
		return
	}
	t.Errorf("golden %s mismatch (run with -update to accept):\n%s", path, diff(want, got))
}

// Visible replaces ESC bytes with the ␛ symbol.
func Visible(s string) string {
	return strings.ReplaceAll(s, "\x1b", ESC)
}

func encode(sections []Section) []byte {
	a := &txtar.Archive{}
	for _, s := range sections {
		body := Visible(s.Body)
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		a.Files = append(a.Files, txtar.File{Name: s.Name, Data: []byte(body)})
	}
	return txtar.Format(a)
}

// diff reports the first differing section, or a name mismatch.
func diff(want, got []byte) string {
	w, g := txtar.Parse(want), txtar.Parse(got)
	var b strings.Builder
	n := max(len(w.Files), len(g.Files))
	for i := range n {
		switch {
		case i >= len(w.Files):
			b.WriteString("unexpected section " + g.Files[i].Name + "\n")
		case i >= len(g.Files):
			b.WriteString("missing section " + w.Files[i].Name + "\n")
		case w.Files[i].Name != g.Files[i].Name:
			b.WriteString("section " + w.Files[i].Name + " != " + g.Files[i].Name + "\n")
		case string(w.Files[i].Data) != string(g.Files[i].Data):
			b.WriteString("section " + w.Files[i].Name + ":\n--- want\n" +
				string(w.Files[i].Data) + "--- got\n" + string(g.Files[i].Data))
		}
	}
	if b.Len() == 0 {
		return "(comment or whitespace differs)"
	}
	return b.String()
}
