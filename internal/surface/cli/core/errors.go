package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Kind returns the stable error kind for an exit code. Agents and the json
// format use it instead of parsing messages.
func Kind(code int) string {
	switch code {
	case ExitOK:
		return "ok"
	case ExitSelectorNotFound:
		return "not_found"
	case ExitAmbiguousSelector:
		return "ambiguous"
	case ExitDaemonUnreachable:
		return "daemon_unreachable"
	case ExitInvalidArgs:
		return "invalid_args"
	case ExitOperationRefused:
		return "refused"
	case ExitWaitTimedOut:
		return "timeout"
	default:
		return "error"
	}
}

// Errorf builds an ExitError with a hint. When format has a %w verb, that
// error becomes Err (the cause) as well as part of the message.
//
//	core.Errorf(core.ExitDaemonUnreachable, "rex daemon start",
//		"rex-daemon is not running: %w", err)
func Errorf(code int, hint, format string, a ...any) ExitError {
	wrapped := fmt.Errorf(format, a...)
	return ExitError{Code: code, Msg: wrapped.Error(), Hint: hint, Err: errors.Unwrap(wrapped)}
}

// ErrorInfo is the structured view of an error that main renders and the
// error log records.
type ErrorInfo struct {
	Code int
	Kind string
	Msg  string
	Hint string
	// Cause is the underlying error text when it adds to Msg.
	Cause string
}

// Describe extracts the exit code, message, hint and cause from err.
func Describe(err error) ErrorInfo {
	info := ErrorInfo{Code: ExitGeneric, Msg: err.Error()}
	var ee ExitError
	if errors.As(err, &ee) {
		info.Code, info.Hint = ee.Code, ee.Hint
		if ee.Err != nil && ee.Err.Error() != "" && !strings.Contains(ee.Msg, ee.Err.Error()) {
			info.Cause = ee.Err.Error()
		}
	} else {
		var ec ExitCoder
		if errors.As(err, &ec) {
			info.Code = ec.ExitCode()
		}
	}
	info.Kind = Kind(info.Code)
	return info
}

// flatLines flattens each line of s and keeps the line breaks, so a
// multi-line message (a command's captured output) prints as written but
// cannot carry a control sequence.
func flatLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = flat(l)
	}
	return strings.Join(lines, "\n")
}

// IsSilent reports whether err is a bare exit status: help was shown, or a
// signal such as "status found a session needing input". Nothing is printed
// or logged for it.
func IsSilent(err error) bool { return err == nil || err.Error() == "" }

// RenderError writes err to stderr in the current format. The txt and md
// formats keep the plain `rex: <msg>` line; text styles it; agent and json
// emit one structured line. It prints nothing for a silent error.
func (p *Printer) RenderError(err error) {
	if IsSilent(err) {
		return
	}
	info := Describe(err)
	multi := info // the plain formats keep the line breaks of message and cause
	multi.Msg, multi.Cause = flatLines(info.Msg), flatLines(info.Cause)
	info.Msg, info.Hint, info.Cause = flat(info.Msg), flat(info.Hint), flat(info.Cause)
	multi.Hint = info.Hint
	var line string
	switch {
	case p.Legacy:
		line = "rex: " + multi.Msg
	case p.Format == FormatAgent:
		fields := []Field{{"code", info.Code}, {"kind", info.Kind}}
		if p.Cmd != "" {
			fields = append(fields, Field{"cmd", p.Cmd})
		}
		fields = append(fields, Field{"msg", info.Msg})
		if info.Hint != "" {
			fields = append(fields, Field{"hint", info.Hint})
		}
		line = EncodeLine("error", fields...)
	case p.Format == FormatJSON:
		line = errorJSON(p.Cmd, info)
	case p.Format == FormatText:
		line = p.textError(multi)
	default:
		line = "rex: " + multi.Msg
		if multi.Hint != "" {
			line += "\n  hint: " + multi.Hint
		}
	}
	_, _ = fmt.Fprintln(p.Err, line)
}

func (p *Printer) textError(info ErrorInfo) string {
	s := p.ErrStyles
	// Style line by line: lipgloss pads a multi-line string to equal widths.
	var lines []string
	for i, l := range strings.Split(info.Msg, "\n") {
		if i == 0 {
			l = p.Glyphs.Fail + " " + l
		}
		lines = append(lines, s.Err.Bold(true).Render(l))
	}
	if info.Cause != "" {
		for _, l := range strings.Split(info.Cause, "\n") {
			lines = append(lines, "  "+s.Dim.Render(l))
		}
	}
	if info.Hint != "" {
		lines = append(lines, s.Accent.Render(p.Glyphs.Hint+" "+info.Hint))
	}
	return strings.Join(lines, "\n")
}

func errorJSON(cmd string, info ErrorInfo) string {
	body := struct {
		Code int    `json:"code"`
		Kind string `json:"kind"`
		Cmd  string `json:"cmd,omitempty"`
		Msg  string `json:"msg"`
		Hint string `json:"hint,omitempty"`
	}{info.Code, info.Kind, cmd, info.Msg, info.Hint}
	b, err := json.Marshal(struct {
		V     int `json:"v"`
		Error any `json:"error"`
	}{1, body})
	if err != nil {
		// Unreachable: every field is a string or int.
		return `{"v":1,"error":{"code":1,"kind":"error","msg":"internal error"}}`
	}
	return string(b)
}

func warnJSON(cmd, msg string) string {
	body := struct {
		Cmd string `json:"cmd,omitempty"`
		Msg string `json:"msg"`
	}{cmd, msg}
	b, err := json.Marshal(struct {
		V    int `json:"v"`
		Warn any `json:"warn"`
	}{1, body})
	if err != nil {
		// Unreachable: every field is a string.
		return `{"v":1,"warn":{"msg":"internal error"}}`
	}
	return string(b)
}
