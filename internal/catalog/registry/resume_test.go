package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func resumeTool(r *ResumeSpec) Tool {
	return Tool{ID: "x", Detect: Detect{Kind: "hooks", Format: "claude"}, Models: []Model{{ID: "m"}}, Resume: r}
}

func TestValidateResume(t *testing.T) {
	for name, tc := range map[string]struct {
		spec *ResumeSpec
		want string // "" = valid
	}{
		"nil is fine":           {nil, ""},
		"args only":             {&ResumeSpec{Args: []string{"--resume", "{id}"}}, ""},
		"embedded placeholder":  {&ResumeSpec{Args: []string{"--conversation={id}"}}, ""},
		"all fields":            {&ResumeSpec{Args: []string{"--resume", "{id}"}, NewArgs: []string{"--session-id", "{id}"}, CaptureRegex: `id=([0-9a-f-]{36})`}, ""},
		"args required":         {&ResumeSpec{NewArgs: []string{"{id}"}}, "resume.args"},
		"args need placeholder": {&ResumeSpec{Args: []string{"--resume"}}, "resume.args"},
		"new_args placeholder":  {&ResumeSpec{Args: []string{"{id}"}, NewArgs: []string{"--session-id"}}, "resume.new_args"},
		"bad regex":             {&ResumeSpec{Args: []string{"{id}"}, CaptureRegex: "("}, "capture_regex"},
		"regex needs a group":   {&ResumeSpec{Args: []string{"{id}"}, CaptureRegex: "id=abc"}, "capture_regex"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validate([]Tool{resumeTool(tc.spec)})
			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestMergeResumeReplacesWholesale(t *testing.T) {
	base := &ResumeSpec{Args: []string{"--resume", "{id}"}, NewArgs: []string{"--session-id", "{id}"}}
	over := &ResumeSpec{Args: []string{"--continue={id}"}}
	merged := mergeOne(Tool{ID: "claude", Resume: base}, Tool{ID: "claude", Resume: over})
	require.Equal(t, over, merged.Resume, "no field-by-field merge: new_args is gone")

	kept := mergeOne(Tool{ID: "claude", Resume: base}, Tool{ID: "claude", Name: "renamed"})
	require.Equal(t, base, kept.Resume)
}

func TestMergeResumeFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tools.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`tools:
  - id: claude
    resume:
      args: ["--continue={id}"]
`), 0o600))
	reg, err := Load(path)
	require.NoError(t, err)
	claude, _ := reg.Find("claude")
	require.Equal(t, []string{"--continue={id}"}, claude.Resume.Args)
	require.Empty(t, claude.Resume.NewArgs)

	require.NoError(t, os.WriteFile(path, []byte(`tools:
  - id: claude
    resume:
      args: ["--resume"]
`), 0o600))
	_, err = Load(path)
	require.ErrorContains(t, err, "resume.args")
}

func TestBuiltinClaudeResume(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	claude, ok := reg.Find("claude")
	require.True(t, ok)
	require.Equal(t, &ResumeSpec{
		Args:    []string{"--resume", "{id}"},
		NewArgs: []string{"--session-id", "{id}"},
	}, claude.Resume)

	// Antigravity's values belong to the agy parity work; nothing here yet.
	if agy, ok := reg.Find("antigravity"); ok {
		require.Nil(t, agy.Resume)
	}
}

func TestResumeSpec_Substitute(t *testing.T) {
	const id = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"
	s := &ResumeSpec{
		Args:    []string{"--resume", "{id}"},
		NewArgs: []string{"--session-id={id}", "--x", "{id}-{id}"},
	}
	require.Equal(t, []string{"--resume", id}, s.ArgsFor(id))
	require.Equal(t, []string{"--session-id=" + id, "--x", id + "-" + id}, s.NewArgsFor(id))
	// Never splits or adds elements.
	require.Len(t, (&ResumeSpec{Args: []string{"a b {id}"}}).ArgsFor("x y"), 1)
	require.Empty(t, (&ResumeSpec{Args: []string{"{id}"}}).NewArgsFor(id))
	// The source slice is not modified.
	require.Equal(t, "{id}", s.Args[1])
}

func TestResumeSpec_CaptureRegexp(t *testing.T) {
	re, err := (&ResumeSpec{Args: []string{"{id}"}}).CaptureRegexp()
	require.NoError(t, err)
	require.Nil(t, re)

	re, err = (&ResumeSpec{Args: []string{"{id}"}, CaptureRegex: `^id=([0-9a-f-]{36})$`}).CaptureRegexp()
	require.NoError(t, err)
	m := re.FindStringSubmatch("noise\nid=7d4f3c8a-1234-4abc-89ab-cdefabcdef01\nmore")
	require.Len(t, m, 2, "(?m): anchors match per line")

	_, err = (&ResumeSpec{CaptureRegex: "("}).CaptureRegexp()
	require.Error(t, err)

	var nilSpec *ResumeSpec
	re, err = nilSpec.CaptureRegexp()
	require.NoError(t, err)
	require.Nil(t, re)
}
