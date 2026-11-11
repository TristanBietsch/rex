package registry

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ResumeIDPlaceholder is the resume.args/new_args placeholder for the
// agent's conversation id. It may sit inside one argv element
// ("--conversation={id}"); substitution never splits or adds elements.
// (Contrast model.effort.arg_template's {value}, which is whitespace-split.)
const ResumeIDPlaceholder = "{id}"

// ResumeSpec says how an agent's conversation is identified and resumed.
type ResumeSpec struct {
	// Args is the argv that resumes a conversation. Required; contains {id}.
	Args []string `yaml:"args"`
	// NewArgs, when set, is added at spawn with a fresh UUID so rex knows the
	// id up front (Claude: --session-id). Contains {id}.
	NewArgs []string `yaml:"new_args,omitempty"`
	// CaptureRegex, when set, reads the id at child exit: group 1 of the last
	// match on the normalized screen text (multi-line mode).
	CaptureRegex string `yaml:"capture_regex,omitempty"`
}

// ArgsFor returns Args with {id} replaced by id.
func (r *ResumeSpec) ArgsFor(id string) []string { return substituteID(r.Args, id) }

// NewArgsFor returns NewArgs with {id} replaced by id (nil when unset).
func (r *ResumeSpec) NewArgsFor(id string) []string { return substituteID(r.NewArgs, id) }

// CaptureRegexp compiles CaptureRegex. It returns nil, nil for a nil spec or
// an unset regex.
func (r *ResumeSpec) CaptureRegexp() (*regexp.Regexp, error) {
	if r == nil || r.CaptureRegex == "" {
		return nil, nil
	}
	re, err := regexp.Compile("(?m)" + r.CaptureRegex)
	if err != nil {
		return nil, fmt.Errorf("resume.capture_regex: %w", err)
	}
	return re, nil
}

func substituteID(args []string, id string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, ResumeIDPlaceholder, id)
	}
	return out
}

func hasPlaceholder(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, ResumeIDPlaceholder) })
}

func validateResume(toolID string, r *ResumeSpec) error {
	if r == nil {
		return nil
	}
	if !hasPlaceholder(r.Args) {
		return fmt.Errorf("tool %q: resume.args is required and must contain %s", toolID, ResumeIDPlaceholder)
	}
	if len(r.NewArgs) > 0 && !hasPlaceholder(r.NewArgs) {
		return fmt.Errorf("tool %q: resume.new_args must contain %s", toolID, ResumeIDPlaceholder)
	}
	if r.CaptureRegex != "" {
		re, err := r.CaptureRegexp()
		if err != nil {
			return fmt.Errorf("tool %q: %w", toolID, err)
		}
		if re.NumSubexp() < 1 {
			return fmt.Errorf("tool %q: resume.capture_regex needs a capture group for the id", toolID)
		}
	}
	return nil
}
