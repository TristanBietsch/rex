package core

import "strings"

// StringList is a repeatable string flag (flag.Value): each occurrence
// appends. Register it with fs.Var(&list, "skill", "usage").
type StringList []string

// String implements flag.Value.
func (l *StringList) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(*l, ",")
}

// Set implements flag.Value. An empty value is an error, so `-skill ""`
// cannot slip an empty name through.
func (l *StringList) Set(v string) error {
	if v == "" {
		return NewExitError(ExitInvalidArgs, "value must not be empty")
	}
	*l = append(*l, v)
	return nil
}
