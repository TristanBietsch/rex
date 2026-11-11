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
	info.Msg, info.Hint, info.Cause = flat(info.Msg), flat(info.Hint), flat(info.Cause)
	var line string
	switch {
	case p.Legacy:
		line = "rex: " + info.Msg
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
		line = p.textError(info)
	default:
		line = "rex: " + info.Msg
		if info.Hint != "" {
			line += "\n  hint: " + info.Hint
		}
	}
	_, _ = fmt.Fprintln(p.Err, line)
}

func (p *Printer) textError(info ErrorInfo) string {
	s := p.ErrStyles
	lines := []string{s.Err.Bold(true).Render(p.Glyphs.Fail + " " + info.Msg)}
	if info.Cause != "" {
		lines = append(lines, "  "+s.Dim.Render(info.Cause))
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
